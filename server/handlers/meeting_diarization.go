package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	log "speektome-server/privacylog"
	"strconv"
	"strings"
	"time"
)

// MeetingDiarization owns a durable FIFO independent of GPU attempts. A SQL
// lease serializes coordinators. Remote identity is persisted BEFORE upload so
// an ambiguous response/restart can reattach to the same job instead of
// submitting duplicate work.
//
// Two backends share one HTTP job contract. The dedicated CPU gateway runs one
// job at a time over the private network. When a diarizer public key is
// configured, the external serverless GPU endpoint (deployment/modal-diarization)
// receives every recording sealed to that key, behind Modal proxy auth plus the
// bearer key, and up to `limit` jobs run at once.
type MeetingDiarization struct {
	jobs                  *MeetingJobs
	url, token            string
	recipient             *diarizationRecipient
	proxyKey, proxySecret string
	limit                 int
	label                 string
}

func newMeetingDiarization(j *MeetingJobs) (*MeetingDiarization, error) {
	_, err := j.db.Exec(`CREATE TABLE IF NOT EXISTS meeting_diarization_queue (
 position INTEGER PRIMARY KEY AUTOINCREMENT,job_key TEXT NOT NULL UNIQUE,created_at INTEGER NOT NULL,status TEXT NOT NULL DEFAULT 'queued',
 remote_id TEXT NOT NULL DEFAULT '',generation INTEGER NOT NULL DEFAULT 0,
 acknowledged INTEGER NOT NULL DEFAULT 0,next_attempt INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '');
 CREATE INDEX IF NOT EXISTS idx_meeting_diarization_queue ON meeting_diarization_queue(status,created_at);
 DROP INDEX IF EXISTS idx_meeting_diarization_single_running;
 CREATE TABLE IF NOT EXISTS meeting_diarization_lock (id INTEGER PRIMARY KEY CHECK(id=1),owner TEXT NOT NULL DEFAULT '',expires INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO meeting_diarization_lock(id) VALUES(1);`)
	if err != nil {
		return nil, err
	}
	// Account-sealed response key of the job's current envelope (empty for CPU jobs).
	var responseKeyColumn int
	_ = j.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('meeting_diarization_queue') WHERE name='response_key'").Scan(&responseKeyColumn)
	if responseKeyColumn == 0 {
		if _, err = j.db.Exec("ALTER TABLE meeting_diarization_queue ADD COLUMN response_key TEXT NOT NULL DEFAULT ''"); err != nil {
			return nil, err
		}
	}
	endpoint := strings.TrimRight(os.Getenv("SPEEKTOME_DIARIZATION_URL"), "/")
	if endpoint == "" {
		endpoint = "http://DIARIZATION_PRIVATE_IP:8081"
	}
	token := os.Getenv("SPEEKTOME_DIARIZATION_TOKEN")
	if token == "" {
		tokenPath := os.Getenv("SPEEKTOME_DIARIZATION_TOKEN_FILE")
		if tokenPath == "" {
			tokenPath = "/etc/speektome-diarization/token"
		}
		data, _ := os.ReadFile(tokenPath) // Queue persists while credentials are unavailable.
		token = string(data)
	}
	d := &MeetingDiarization{jobs: j, url: endpoint, token: strings.TrimSpace(token), limit: 1, label: "CPU diarization",
		proxyKey: os.Getenv("SPEEKTOME_DIARIZATION_MODAL_KEY"), proxySecret: os.Getenv("SPEEKTOME_DIARIZATION_MODAL_SECRET")}
	if encoded := strings.TrimSpace(os.Getenv("SPEEKTOME_DIARIZATION_PUBLIC_KEY")); encoded != "" {
		if d.recipient, err = parseDiarizationRecipient(encoded); err != nil {
			return nil, fmt.Errorf("SPEEKTOME_DIARIZATION_PUBLIC_KEY: %w", err)
		}
		d.limit, d.label = 8, "GPU diarization"
	}
	if n, err := strconv.Atoi(os.Getenv("SPEEKTOME_DIARIZATION_CONCURRENCY")); err == nil && n > 0 {
		d.limit = n
	}
	return d, nil
}

func (d *MeetingDiarization) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if os.Getenv("MEETING_PROCESSING_ENABLED") != "false" {
			if err := d.tick(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[MEETINGS] %s queue unavailable: %v", d.label, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *MeetingDiarization) tick(parent context.Context) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	// Bound each upload/status/result operation below the database lease lifetime.
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	claim := make([]byte, 16)
	if _, err := rand.Read(claim); err != nil {
		return err
	}
	owner := hex.EncodeToString(claim)
	now := time.Now().Unix()
	res, err := d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_lock SET owner=?,expires=? WHERE id=1 AND expires<?`, owner, now+120, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = d.jobs.db.ExecContext(release, `UPDATE meeting_diarization_lock SET owner='',expires=0 WHERE id=1 AND owner=?`, owner)
	}()

	// Reconcile committed jobs, including uploads committed before this deployment.
	_, err = d.jobs.db.ExecContext(ctx, `INSERT OR IGNORE INTO meeting_diarization_queue(job_key,created_at)
 SELECT job_key,created_at FROM meeting_jobs WHERE admitted=1 AND status IN ('queued','processing') AND diar_result='' ORDER BY created_at,job_key`)
	if err != nil {
		return err
	}
	// Retry a previously failed stage only on explicit meeting retry. Delay reset
	// until its old remote result has been acknowledged, retaining crash recovery.
	_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='queued',generation=generation+1,remote_id='',response_key='',error='',next_attempt=0,acknowledged=0
 WHERE status IN ('failed','cancelled') AND acknowledged=1 AND job_key IN (SELECT job_key FROM meeting_jobs WHERE status IN ('queued','processing') AND diar_result='')`)
	if err != nil {
		return err
	}
	_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='cancelled',acknowledged=1 WHERE status='queued'
 AND NOT EXISTS (SELECT 1 FROM meeting_jobs j WHERE j.job_key=meeting_diarization_queue.job_key AND j.status IN ('queued','processing'))`)
	if err != nil {
		return err
	}
	if len(d.token) < 24 {
		return nil
	}
	// Terminal results are acknowledged only after their outcome is durable.
	var cleanupKey, cleanupID string
	err = d.jobs.db.QueryRowContext(ctx, `SELECT job_key,remote_id FROM meeting_diarization_queue WHERE status IN ('completed','failed','cancelled') AND acknowledged=0 AND next_attempt<=? ORDER BY position LIMIT 1`, now).Scan(&cleanupKey, &cleanupID)
	if err == nil {
		if cleanupID == "" {
			_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET acknowledged=1 WHERE job_key=?`, cleanupKey)
			return err
		}
		_, code, _, requestErr := d.request(ctx, http.MethodDelete, "/v1/diarizations/"+cleanupID, nil, "")
		if requestErr == nil && (code == 200 || code == 404) {
			_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET acknowledged=1,response_key='' WHERE job_key=?`, cleanupKey)
			return err
		}
		return d.deferItem(ctx, cleanupKey, "waiting to acknowledge "+d.label, 15*time.Second)
	}
	if err != sql.ErrNoRows {
		return err
	}
	// Running entries (including uncertain uploads and pending retry timers) hold
	// their slot, so at most `limit` remote jobs exist at once; the CPU gateway's
	// limit of one preserves strict FIFO.
	type runningJob struct {
		key, remote string
		next        int64
	}
	rows, err := d.jobs.db.QueryContext(ctx, `SELECT job_key,remote_id,next_attempt FROM meeting_diarization_queue WHERE status='running' ORDER BY position`)
	if err != nil {
		return err
	}
	var running []runningJob
	for rows.Next() {
		var r runningJob
		if err = rows.Scan(&r.key, &r.remote, &r.next); err != nil {
			rows.Close()
			return err
		}
		running = append(running, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	var first error
	for _, r := range running {
		if r.next <= now {
			if err := d.advance(ctx, r.key, r.remote); err != nil && first == nil {
				first = err
			}
		}
	}
	for active := len(running); active < max(d.limit, 1) && ctx.Err() == nil; active++ {
		var key string
		var generation int
		err = d.jobs.db.QueryRowContext(ctx, `SELECT job_key,generation FROM meeting_diarization_queue WHERE status='queued' AND job_key IN (SELECT job_key FROM meeting_jobs WHERE admitted=1) ORDER BY position LIMIT 1`).Scan(&key, &generation)
		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("meeting-diarization/%s/%d", key, generation)))
		remote := hex.EncodeToString(sum[:16])
		if _, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='running',remote_id=?,acknowledged=0 WHERE job_key=? AND status='queued'`, remote, key); err != nil {
			return err
		}
		if err := d.advance(ctx, key, remote); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// advance polls one running remote job, submitting it when the remote has no
// record of it and saving its result before acknowledgment.
func (d *MeetingDiarization) advance(ctx context.Context, key, remote string) error {
	data, code, headers, err := d.request(ctx, http.MethodGet, "/v1/diarizations/"+remote, nil, "")
	if err != nil {
		return d.deferItem(ctx, key, d.label+" connection unavailable", 15*time.Second)
	}
	if code == 404 {
		return d.submit(ctx, key, remote)
	}
	if code != 200 {
		return d.httpFailure(ctx, key, code, headers)
	}
	var state struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(data, &state) != nil {
		return d.deferItem(ctx, key, "invalid "+d.label+" status", 15*time.Second)
	}
	switch state.Status {
	case "uploading", "uploaded", "processing":
		return d.deferItem(ctx, key, "", 2*time.Second)
	case "failed":
		return d.fail(ctx, key, d.label+" failed: "+state.Error)
	case "completed":
		data, code, headers, err = d.request(ctx, http.MethodGet, "/v1/diarizations/"+remote+"/result", nil, "")
		if err != nil {
			return d.deferItem(ctx, key, d.label+" result unavailable", 15*time.Second)
		}
		if code != 200 {
			return d.httpFailure(ctx, key, code, headers)
		}
		if d.recipient != nil {
			if data, err = d.openResult(ctx, key, remote, data); err != nil {
				return d.fail(ctx, key, "unreadable "+d.label+" result")
			}
		}
		normalized, err := normalizeDiarizationResult(data)
		if err != nil {
			return d.fail(ctx, key, "invalid "+d.label+" result: "+err.Error())
		}
		sealed, err := d.jobs.sealStage(ctx, key, "diar_result", string(normalized))
		if err != nil {
			return err
		}
		tx, err := d.jobs.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		saved, err := tx.ExecContext(ctx, `UPDATE meeting_jobs SET diar_result=?,error='',next_attempt=0 WHERE job_key=? AND status IN ('queued','processing')`, sealed, key)
		if err != nil {
			return err
		}
		count, err := saved.RowsAffected()
		if err != nil {
			return err
		}
		status := "completed"
		if count == 0 {
			status = "cancelled"
		}
		_, err = tx.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status=?,error='',next_attempt=0 WHERE job_key=?`, status, key)
		if err != nil {
			return err
		}
		return tx.Commit()
	default:
		return d.deferItem(ctx, key, "unrecognized "+d.label+" status", 15*time.Second)
	}
}

func (d *MeetingDiarization) submit(ctx context.Context, key, remote string) error {
	var status string
	if err := d.jobs.db.QueryRowContext(ctx, `SELECT status FROM meeting_jobs WHERE job_key=?`, key).Scan(&status); err == sql.ErrNoRows || (err == nil && status != "queued" && status != "processing") {
		_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='cancelled',acknowledged=1 WHERE job_key=?`, key)
		return err
	} else if err != nil {
		return err
	}
	audio, size, err := d.jobs.openChannel(ctx, key, "system")
	if err != nil {
		return d.fail(ctx, key, "system audio unavailable for "+d.label)
	}
	defer audio.Close()
	var body io.Reader = sizedBody{audio, size}
	if d.recipient != nil {
		sealed, responseKey, err := sealDiarizationRequest(audio, size, d.recipient, remote, time.Now(), nil)
		if err != nil {
			return d.fail(ctx, key, "cannot seal audio for "+d.label)
		}
		// Persisted before upload: a lost 202 or a restart must still be able to open
		// the result of the envelope the remote may already hold.
		stored, err := d.jobs.sealStage(ctx, key, "diar_response_key", base64.StdEncoding.EncodeToString(responseKey))
		if err != nil {
			return err
		}
		if _, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET response_key=? WHERE job_key=?`, stored, key); err != nil {
			return err
		}
		body = sealed
	}
	data, code, headers, err := d.request(ctx, http.MethodPost, "/v1/diarizations", body, remote)
	if err != nil {
		return d.deferItem(ctx, key, d.label+" upload response unavailable; reconnecting to saved job ID", 15*time.Second)
	}
	if code != 202 {
		return d.httpFailure(ctx, key, code, headers)
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	if json.Unmarshal(data, &accepted) != nil || accepted.JobID != remote {
		return d.fail(ctx, key, d.label+" must support coordinator job identities")
	}
	return d.deferItem(ctx, key, "", 2*time.Second)
}

func (d *MeetingDiarization) openResult(ctx context.Context, key, remote string, data []byte) ([]byte, error) {
	var stored string
	if err := d.jobs.db.QueryRowContext(ctx, `SELECT response_key FROM meeting_diarization_queue WHERE job_key=?`, key).Scan(&stored); err != nil || stored == "" {
		return nil, errEnvelope
	}
	encoded, err := d.jobs.openStage(ctx, key, "diar_response_key", stored)
	if err != nil {
		return nil, err
	}
	responseKey, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return openDiarizationResponse(data, responseKey, remote)
}

// sizedBody streams decrypted spool audio with a known length, so the gateway
// can reject oversized uploads before reading them.
type sizedBody struct {
	io.Reader
	size int64
}

func (s sizedBody) Len() int64 { return s.size }

func (d *MeetingDiarization) request(ctx context.Context, method, path string, body io.Reader, remote string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.url+path, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	if d.proxyKey != "" {
		req.Header.Set("Modal-Key", d.proxyKey)
		req.Header.Set("Modal-Secret", d.proxySecret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "audio/ogg")
		if d.recipient != nil {
			req.Header.Set("Content-Type", "application/vnd.speektome.diarization-envelope")
		}
		req.Header.Set("X-SpeekToMe-Job-ID", remote)
		if sized, ok := body.(interface{ Len() int64 }); ok {
			req.ContentLength = sized.Len()
		}
	}
	response, err := d.jobs.client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
	if len(data) > 32<<20 {
		return nil, response.StatusCode, response.Header, errors.New(d.label + " response exceeds limit")
	}
	return data, response.StatusCode, response.Header, err
}

func (d *MeetingDiarization) deferItem(ctx context.Context, key, message string, delay time.Duration) error {
	_, err := d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET next_attempt=?,error=? WHERE job_key=?`, time.Now().Add(delay).Unix(), message, key)
	return err
}
func (d *MeetingDiarization) httpFailure(ctx context.Context, key string, code int, headers http.Header) error {
	if code == 400 || code == 401 || code == 403 || code == 413 || code == 422 {
		return d.fail(ctx, key, fmt.Sprintf("%s HTTP %d", d.label, code))
	}
	delay := 15 * time.Second
	if seconds, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && seconds > 0 {
		delay = time.Duration(min(seconds, 300)) * time.Second
	}
	return d.deferItem(ctx, key, fmt.Sprintf("waiting for %s (HTTP %d)", d.label, code), delay)
}
func (d *MeetingDiarization) fail(ctx context.Context, key, message string) error {
	if len(message) > 512 {
		message = message[:512]
	}
	tx, err := d.jobs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='failed',error=?,next_attempt=0 WHERE job_key=?`, message, key)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE meeting_jobs SET admitted=CASE WHEN status='processing' THEN admitted ELSE 0 END,status='failed',error=? WHERE job_key=? AND status IN ('queued','processing')`, message, key)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		var admitted int
		if d.jobs.db.QueryRow("SELECT admitted FROM meeting_jobs WHERE job_key=?", key).Scan(&admitted) == nil && admitted == 0 {
			d.jobs.releaseProcessing(key)
		}
	}
	return err
}

var cpuSpeakerPattern = regexp.MustCompile(`^(?:speaker[_ -]?)?(\d+)$`)

// The dedicated runtime returns Start/End/Speaker, with numeric speaker IDs.
// Accept a string numeric ID too, but reject incomplete/malformed results instead
// of silently assigning every word to speaker zero. Empty segments are valid.
func normalizeDiarizationResult(data []byte) ([]byte, error) {
	var result struct {
		Segments json.RawMessage `json:"segments"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if len(result.Segments) == 0 || string(result.Segments) == "null" {
		return nil, errors.New("missing segments")
	}
	var raw []struct {
		Start   *float64        `json:"start"`
		End     *float64        `json:"end"`
		Speaker json.RawMessage `json:"speaker"`
	}
	if err := json.Unmarshal(result.Segments, &raw); err != nil {
		return nil, err
	}
	segments := make([]jobSpeaker, 0, len(raw))
	for _, s := range raw {
		if s.Start == nil || s.End == nil || math.IsNaN(*s.Start) || math.IsNaN(*s.End) || math.IsInf(*s.Start, 0) || math.IsInf(*s.End, 0) || *s.Start < 0 || *s.End < *s.Start {
			return nil, errors.New("invalid segment timing")
		}
		var speaker int
		if err := json.Unmarshal(s.Speaker, &speaker); err != nil {
			var label string
			if json.Unmarshal(s.Speaker, &label) != nil {
				return nil, errors.New("invalid speaker")
			}
			match := cpuSpeakerPattern.FindStringSubmatch(strings.ToLower(label))
			if match == nil {
				return nil, errors.New("invalid speaker label")
			}
			var err error
			speaker, err = strconv.Atoi(match[1])
			if err != nil {
				return nil, err
			}
		}
		if len(s.Speaker) == 0 || string(s.Speaker) == "null" || speaker < 0 {
			return nil, errors.New("missing or negative speaker")
		}
		segments = append(segments, jobSpeaker{Start: *s.Start, End: *s.End, Speaker: speaker})
	}
	return json.Marshal(struct {
		Segments []jobSpeaker `json:"segments"`
	}{segments})
}
