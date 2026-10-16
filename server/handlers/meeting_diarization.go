package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MeetingDiarization owns a durable FIFO independent of GPU attempts. A SQL
// lease serializes coordinators; the CPU gateway separately enforces capacity=1.
// Remote identity is persisted BEFORE upload so an ambiguous response/restart
// can reattach to the same job instead of submitting duplicate work.
type MeetingDiarization struct {
	jobs       *MeetingJobs
	url, token string
}

func newMeetingDiarization(j *MeetingJobs) (*MeetingDiarization, error) {
	_, err := j.db.Exec(`CREATE TABLE IF NOT EXISTS meeting_diarization_queue (
 position INTEGER PRIMARY KEY AUTOINCREMENT,job_key TEXT NOT NULL UNIQUE,created_at INTEGER NOT NULL,status TEXT NOT NULL DEFAULT 'queued',
 remote_id TEXT NOT NULL DEFAULT '',generation INTEGER NOT NULL DEFAULT 0,
 acknowledged INTEGER NOT NULL DEFAULT 0,next_attempt INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '');
 CREATE INDEX IF NOT EXISTS idx_meeting_diarization_queue ON meeting_diarization_queue(status,created_at);
 CREATE UNIQUE INDEX IF NOT EXISTS idx_meeting_diarization_single_running ON meeting_diarization_queue(status) WHERE status='running';
 CREATE TABLE IF NOT EXISTS meeting_diarization_lock (id INTEGER PRIMARY KEY CHECK(id=1),owner TEXT NOT NULL DEFAULT '',expires INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO meeting_diarization_lock(id) VALUES(1);`)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(os.Getenv("SPEEKTOME_DIARIZATION_URL"), "/")
	if endpoint == "" {
		endpoint = "http://DIARIZATION_PRIVATE_IP:8081"
	}
	tokenPath := os.Getenv("SPEEKTOME_DIARIZATION_TOKEN_FILE")
	if tokenPath == "" {
		tokenPath = "/etc/speektome-diarization/token"
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		token = nil
	} // Queue persists while credentials are unavailable.
	return &MeetingDiarization{jobs: j, url: endpoint, token: strings.TrimSpace(string(token))}, nil
}

func (d *MeetingDiarization) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if os.Getenv("MEETING_PROCESSING_ENABLED") != "false" {
			if err := d.tick(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[MEETINGS] CPU diarization queue unavailable: %v", err)
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
 SELECT job_key,created_at FROM meeting_jobs WHERE status IN ('queued','processing') AND diar_result='' ORDER BY created_at,job_key`)
	if err != nil {
		return err
	}
	// Retry a previously failed stage only on explicit meeting retry. Delay reset
	// until its old remote result has been acknowledged, retaining crash recovery.
	_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='queued',generation=generation+1,remote_id='',error='',next_attempt=0,acknowledged=0
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
			_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET acknowledged=1 WHERE job_key=?`, cleanupKey)
			return err
		}
		return d.deferItem(ctx, cleanupKey, "waiting to acknowledge CPU diarization", 15*time.Second)
	}
	if err != sql.ErrNoRows {
		return err
	}
	var key, remote string
	var next int64
	// A running (including uncertain upload) entry blocks ALL later submissions,
	// even while its retry timer is pending.
	err = d.jobs.db.QueryRowContext(ctx, `SELECT job_key,remote_id,next_attempt FROM meeting_diarization_queue WHERE status='running' ORDER BY position LIMIT 1`).Scan(&key, &remote, &next)
	if err == sql.ErrNoRows {
		var generation int
		err = d.jobs.db.QueryRowContext(ctx, `SELECT job_key,generation FROM meeting_diarization_queue WHERE status='queued' ORDER BY position LIMIT 1`).Scan(&key, &generation)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("meeting-diarization/%s/%d", key, generation)))
		remote = hex.EncodeToString(sum[:16])
		_, err = d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET status='running',remote_id=?,acknowledged=0 WHERE job_key=? AND status='queued'`, remote, key)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if next > now {
		return nil
	}
	data, code, headers, err := d.request(ctx, http.MethodGet, "/v1/diarizations/"+remote, nil, "")
	if err != nil {
		return d.deferItem(ctx, key, "CPU diarization connection unavailable", 15*time.Second)
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
		return d.deferItem(ctx, key, "invalid CPU diarization status", 15*time.Second)
	}
	switch state.Status {
	case "uploading", "uploaded", "processing":
		return d.deferItem(ctx, key, "", 2*time.Second)
	case "failed":
		return d.fail(ctx, key, "CPU diarization failed: "+state.Error)
	case "completed":
		data, code, headers, err = d.request(ctx, http.MethodGet, "/v1/diarizations/"+remote+"/result", nil, "")
		if err != nil {
			return d.deferItem(ctx, key, "CPU diarization result unavailable", 15*time.Second)
		}
		if code != 200 {
			return d.httpFailure(ctx, key, code, headers)
		}
		normalized, err := normalizeDiarizationResult(data)
		if err != nil {
			return d.fail(ctx, key, "invalid CPU diarization result: "+err.Error())
		}
		tx, err := d.jobs.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		saved, err := tx.ExecContext(ctx, `UPDATE meeting_jobs SET diar_result=?,error='',next_attempt=0 WHERE job_key=? AND status IN ('queued','processing')`, string(normalized), key)
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
		return d.deferItem(ctx, key, "unrecognized CPU diarization status", 15*time.Second)
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
	f, err := os.Open(filepath.Join(d.jobs.root, key, "system.ogg"))
	if err != nil {
		return d.fail(ctx, key, "system audio unavailable for CPU diarization")
	}
	defer f.Close()
	data, code, headers, err := d.request(ctx, http.MethodPost, "/v1/diarizations", f, remote)
	if err != nil {
		return d.deferItem(ctx, key, "CPU upload response unavailable; reconnecting to saved job ID", 15*time.Second)
	}
	if code != 202 {
		return d.httpFailure(ctx, key, code, headers)
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	if json.Unmarshal(data, &accepted) != nil || accepted.JobID != remote {
		return d.fail(ctx, key, "CPU gateway must support coordinator job identities")
	}
	return d.deferItem(ctx, key, "", 2*time.Second)
}

func (d *MeetingDiarization) request(ctx context.Context, method, path string, body io.Reader, remote string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.url+path, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	if body != nil {
		req.Header.Set("Content-Type", "audio/ogg")
		req.Header.Set("X-SpeekToMe-Job-ID", remote)
		if file, ok := body.(*os.File); ok {
			if stat, e := file.Stat(); e == nil {
				req.ContentLength = stat.Size()
			}
		}
	}
	response, err := d.jobs.client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
	if len(data) > 32<<20 {
		return nil, response.StatusCode, response.Header, errors.New("CPU response exceeds limit")
	}
	return data, response.StatusCode, response.Header, err
}

func (d *MeetingDiarization) deferItem(ctx context.Context, key, message string, delay time.Duration) error {
	_, err := d.jobs.db.ExecContext(ctx, `UPDATE meeting_diarization_queue SET next_attempt=?,error=? WHERE job_key=?`, time.Now().Add(delay).Unix(), message, key)
	return err
}
func (d *MeetingDiarization) httpFailure(ctx context.Context, key string, code int, headers http.Header) error {
	if code == 400 || code == 401 || code == 403 || code == 413 || code == 422 {
		return d.fail(ctx, key, fmt.Sprintf("CPU diarization HTTP %d", code))
	}
	delay := 15 * time.Second
	if seconds, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && seconds > 0 {
		delay = time.Duration(min(seconds, 300)) * time.Second
	}
	return d.deferItem(ctx, key, fmt.Sprintf("waiting for CPU diarization (HTTP %d)", code), delay)
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
	_, err = tx.ExecContext(ctx, `UPDATE meeting_jobs SET status='failed',error=?,lease_until=0 WHERE job_key=? AND status IN ('queued','processing')`, message, key)
	if err != nil {
		return err
	}
	return tx.Commit()
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
