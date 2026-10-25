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
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"speektome-server/audiospool"
	"speektome-server/config"
	"speektome-server/pipeline"
	log "speektome-server/privacylog"
	"speektome-server/utils"
	"strconv"
	"strings"
	"sync"
	"time"

	"speektome-server/accountlimits"
	"speektome-server/auth"
)

type MeetingFile struct {
	Size int64  `json:"size"`
	SHA  string `json:"sha256"`
}
type MeetingUpload struct {
	Language string                 `json:"language,omitempty"`
	ID       string                 `json:"id"`
	Title    string                 `json:"title"`
	Files    map[string]MeetingFile `json:"files"`
}
type MeetingJobs struct {
	app   *App
	db    *sql.DB
	spool *audiospool.Spool
	// root is the pre-spool plaintext directory, read only to migrate old jobs.
	root, fleet, token string
	uploads            [64]sync.Mutex
	client             *http.Client
	diarization        *MeetingDiarization
}

// Spooled audio is kept only while its job needs it. Failed meetings can be
// retried within this window; after it the desktop uploads its local copy again.
const meetingSpoolRetention = 24 * time.Hour

// Processing is bounded to six hours, so nothing outlives this hard limit.
const meetingSpoolHardLimit = meetingSpoolRetention + 6*time.Hour

var clientMeetingID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,96}$`)
var jobKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (a *App) StartMeetingJobs(ctx context.Context, mux *http.ServeMux) error {
	root := os.Getenv("MEETING_AUDIO_DIR")
	if root == "" {
		root = "uploads/meetings/jobs"
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	fleet := os.Getenv("SPEEKTOME_FLEET_URL")
	if fleet == "" {
		fleet = "http://127.0.0.1:8095"
	}
	tokenPath := os.Getenv("SPEEKTOME_WORKER_TOKEN_FILE")
	if tokenPath == "" {
		tokenPath = "/etc/speektome-fleet/worker.token"
	}
	token, _ := os.ReadFile(tokenPath)
	jobs := &MeetingJobs{app: a, db: a.DB.GetDB(), root: root, fleet: strings.TrimRight(fleet, "/"), token: strings.TrimSpace(string(token)), client: &http.Client{}}
	_, e = jobs.db.Exec(`CREATE TABLE IF NOT EXISTS meeting_jobs (
 job_key TEXT PRIMARY KEY,user_id INTEGER NOT NULL,client_id TEXT NOT NULL,title TEXT NOT NULL,files_json TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'uploading',meeting_id INTEGER,created_at INTEGER NOT NULL,next_attempt INTEGER NOT NULL DEFAULT 0,
 lease_until INTEGER NOT NULL DEFAULT 0,attempt TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,
 mic_result TEXT NOT NULL DEFAULT '',system_result TEXT NOT NULL DEFAULT '',diar_result TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',
 UNIQUE(user_id,client_id)); CREATE INDEX IF NOT EXISTS idx_meeting_jobs_queue ON meeting_jobs(status,next_attempt,lease_until);`)
	if e != nil {
		return e
	}
	// Additive migration preserves existing durable jobs across server restarts.
	var hasLanguage bool
	columns, err := jobs.db.Query("PRAGMA table_info(meeting_jobs)")
	if err != nil {
		return err
	}
	for columns.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err = columns.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			columns.Close()
			return err
		}
		if name == "language" {
			hasLanguage = true
		}
	}
	if err = columns.Err(); err != nil {
		columns.Close()
		return err
	}
	columns.Close()
	if !hasLanguage {
		if _, err = jobs.db.Exec("ALTER TABLE meeting_jobs ADD COLUMN language TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	// Existing jobs start unadmitted; the dispatcher admits one parent job per account.
	var admittedColumn int
	_ = jobs.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('meeting_jobs') WHERE name='admitted'").Scan(&admittedColumn)
	if admittedColumn == 0 {
		if _, e = jobs.db.Exec("ALTER TABLE meeting_jobs ADD COLUMN admitted INTEGER NOT NULL DEFAULT 0"); e != nil {
			return e
		}
	}
	if _, e = jobs.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_meeting_one_account_job ON meeting_jobs(user_id) WHERE admitted=1"); e != nil {
		return e
	}
	var purgedColumn int
	_ = jobs.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('meeting_jobs') WHERE name='spool_purged'").Scan(&purgedColumn)
	if purgedColumn == 0 {
		if _, e = jobs.db.Exec("ALTER TABLE meeting_jobs ADD COLUMN spool_purged INTEGER NOT NULL DEFAULT 0"); e != nil {
			return e
		}
	}
	if jobs.spool, e = newMeetingSpool(a); e != nil {
		return e
	}
	jobs.diarization, e = newMeetingDiarization(jobs)
	if e != nil {
		return e
	}
	go jobs.diarization.loop(ctx)
	mux.HandleFunc("POST /v1/meetings/jobs", jobs.create)
	mux.HandleFunc("GET /v1/meetings/jobs/{clientID}", jobs.status)
	mux.HandleFunc("PUT /v1/meetings/jobs/{clientID}/audio/{channel}", jobs.upload)
	mux.HandleFunc("POST /v1/meetings/jobs/{clientID}/commit", jobs.commit)
	mux.HandleFunc("POST /v1/meetings/jobs/{clientID}/retry", jobs.retryHandler)
	if len(jobs.token) < 32 {
		log.Printf("[MEETINGS] durable upload enabled; processor waiting for worker token file")
	}
	go jobs.loop(ctx)
	return nil
}
func (j *MeetingJobs) lock(key string) func() {
	index := 0
	for _, b := range []byte(key) {
		index = (index*31 + int(b)) % len(j.uploads)
	}
	j.uploads[index].Lock()
	return j.uploads[index].Unlock
}
func jobKey(user int64, id string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d/%s", user, id)))
	return hex.EncodeToString(sum[:])
}
func (j *MeetingJobs) identity(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	u, e := auth.Authenticate(r, j.app.DB)
	if e != nil {
		http.Error(w, "unauthorized", 401)
		return 0, "", false
	}
	if r.Method != "GET" && (u.PlanTier != "pro" || u.PlanActive != 1) {
		http.Error(w, "Meeting recording requires an active Pro subscription", 403)
		return 0, "", false
	}
	if j.accountDeleting(r.Context(), u.ID) {
		http.Error(w, "Account deletion is pending", 409)
		return 0, "", false
	}
	id := r.PathValue("clientID")
	if !clientMeetingID.MatchString(id) {
		http.Error(w, "invalid meeting id", 400)
		return 0, "", false
	}
	operation, rpm := "meeting_status", 120
	if r.Method == "PUT" {
		operation, rpm = "meeting_upload", 600
	}
	if r.Method == "POST" {
		operation, rpm = "meeting_update", 10
	}
	if !j.app.accountRate(w, r, u.ID, operation, rpm) {
		return 0, "", false
	}
	return u.ID, jobKey(u.ID, id), true
}
func (j *MeetingJobs) create(w http.ResponseWriter, r *http.Request) {
	user, e := auth.Authenticate(r, j.app.DB)
	if e != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	if user.PlanTier != "pro" || user.PlanActive != 1 {
		http.Error(w, "Meeting recording requires an active Pro subscription", 403)
		return
	}
	if j.accountDeleting(r.Context(), user.ID) {
		http.Error(w, "Account deletion is pending", 409)
		return
	}
	if !j.app.accountRate(w, r, user.ID, "meeting_manifest", 30) {
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	var req MeetingUpload
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req) != nil || !clientMeetingID.MatchString(req.ID) || len(req.Title) > 512 {
		http.Error(w, "invalid meeting manifest", 400)
		return
	}
	language, languageErr := utils.ResolveSpeakingLanguage(req.Language, user.Languages)
	if languageErr != nil {
		http.Error(w, languageErr.Error(), 400)
		return
	}
	if req.Title == "" {
		req.Title = "Meeting"
	}
	if len(req.Files) != 3 {
		http.Error(w, "mic, system, and mixed files required", 400)
		return
	}
	for _, channel := range []string{"mic", "system", "mixed"} {
		f, ok := req.Files[channel]
		digest, e := hex.DecodeString(f.SHA)
		if !ok || e != nil || len(digest) != 32 || f.Size < 1 || f.Size > 512<<20 {
			http.Error(w, "invalid audio manifest", 400)
			return
		}
	}
	key := jobKey(user.ID, req.ID)
	data, _ := json.Marshal(req.Files)
	var existingManifest string
	existingErr := j.db.QueryRowContext(r.Context(), "SELECT files_json FROM meeting_jobs WHERE job_key=?", key).Scan(&existingManifest)
	if existingErr == nil {
		if existingManifest != string(data) {
			http.Error(w, "meeting id already has different audio", 409)
			return
		}
		j.respond(w, key)
		return
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		accountlimits.Write(w, accountlimits.Unavailable())
		return
	}
	if !j.app.accountRate(w, r, user.ID, "meeting_create", 2) {
		return
	}

	unlock := j.lock(key)
	defer unlock()
	_, e = j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,created_at,language) SELECT ?,?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM meeting_jobs WHERE user_id=? AND admitted=0 AND status IN ('uploading','queued'))<3 ON CONFLICT(user_id,client_id) DO NOTHING`, key, user.ID, req.ID, req.Title, string(data), time.Now().Unix(), language, user.ID)
	if e != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	var existing string
	if err := j.db.QueryRow(`SELECT files_json FROM meeting_jobs WHERE job_key=?`, key).Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			accountlimits.Write(w, accountlimits.Denied("meeting_queue_full", "Your account already has three meetings waiting to be processed. Your recording is saved locally; upload it after a queued meeting finishes.", 30))
		} else {
			accountlimits.Write(w, accountlimits.Unavailable())
		}
		return
	}
	if existing != string(data) {
		http.Error(w, "meeting id already has different audio", 409)
		return
	}
	j.respond(w, key)
}
func (j *MeetingJobs) respond(w http.ResponseWriter, key string) {
	var status, files, lastError string
	var meeting sql.NullInt64
	if e := j.db.QueryRow(`SELECT status,files_json,error,meeting_id FROM meeting_jobs WHERE job_key=?`, key).Scan(&status, &files, &lastError, &meeting); e != nil {
		http.Error(w, "meeting not found", 404)
		return
	}
	var manifest map[string]MeetingFile
	json.Unmarshal([]byte(files), &manifest)
	offsets := map[string]int64{}
	for channel := range manifest {
		offsets[channel], _ = j.spool.Size(context.Background(), key, channel)
	}
	response := map[string]any{"status": status, "offsets": offsets, "error": lastError, "meeting_id": meeting.Int64}
	var diarStatus, diarError string
	if j.db.QueryRow(`SELECT status,error FROM meeting_diarization_queue WHERE job_key=?`, key).Scan(&diarStatus, &diarError) == nil {
		response["diarization"] = map[string]string{"status": diarStatus, "error": diarError}
	}
	if meeting.Valid && status == "completed" {
		if result, e := j.app.DB.GetMeeting(meeting.Int64); e == nil {
			response["meeting"] = result
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
func (j *MeetingJobs) status(w http.ResponseWriter, r *http.Request) {
	_, key, ok := j.identity(w, r)
	if ok {
		unlock := j.lock(key)
		defer unlock()
		j.respond(w, key)
	}
}
func (j *MeetingJobs) upload(w http.ResponseWriter, r *http.Request) {
	userID, key, ok := j.identity(w, r)
	if !ok {
		return
	}
	channel := r.PathValue("channel")
	if channel != "mic" && channel != "system" && channel != "mixed" {
		http.Error(w, "invalid channel", 400)
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Second))
	defer http.NewResponseController(w).SetReadDeadline(time.Time{})
	data, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if readErr != nil || len(data) == 0 {
		http.Error(w, "invalid upload chunk", 413)
		return
	}
	offset, e := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if e != nil || offset < 0 {
		http.Error(w, "invalid offset", 400)
		return
	}
	unlock := j.lock(key)
	defer unlock()
	var status, files string
	if j.db.QueryRow(`SELECT status,files_json FROM meeting_jobs WHERE job_key=?`, key).Scan(&status, &files) != nil {
		http.Error(w, "meeting not found", 404)
		return
	}
	if status != "uploading" {
		http.Error(w, "upload already committed", 409)
		return
	}
	var manifest map[string]MeetingFile
	json.Unmarshal([]byte(files), &manifest)
	if offset+int64(len(data)) > manifest[channel].Size {
		http.Error(w, "invalid upload chunk", 413)
		return
	}
	// The chunk is sealed before it reaches storage and acknowledged only after
	// its index row is durable.
	next, e := j.spool.Append(r.Context(), userID, key, channel, offset, data)
	clear(data)
	if errors.Is(e, audiospool.ErrOffset) {
		w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
		http.Error(w, "offset mismatch", 409)
		return
	}
	if e != nil {
		http.Error(w, "storage write failed", 503)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
	w.WriteHeader(204)
}
func (j *MeetingJobs) commit(w http.ResponseWriter, r *http.Request) {
	userID, key, ok := j.identity(w, r)
	if !ok {
		return
	}
	unlock := j.lock(key)
	defer unlock()
	var status, files, title string
	if j.db.QueryRow(`SELECT status,files_json,title FROM meeting_jobs WHERE job_key=?`, key).Scan(&status, &files, &title) != nil {
		http.Error(w, "meeting not found", 404)
		return
	}
	if status != "uploading" {
		j.respond(w, key)
		return
	}
	var manifest map[string]MeetingFile
	json.Unmarshal([]byte(files), &manifest)
	// Verify each channel from the sealed spool: checksum against the manifest
	// and duration from the Ogg pages themselves, never from client claims.
	duration := 0.0
	for _, channel := range []string{"mic", "system", "mixed"} {
		expected := manifest[channel]
		seconds, size, sum, e := j.verifyChannel(r.Context(), userID, key, channel)
		if errors.Is(e, audiospool.ErrCorrupt) || (e == nil && size != expected.Size) {
			http.Error(w, "upload incomplete", 409)
			return
		}
		if errors.Is(e, audiospool.ErrInvalidAudio) || (e == nil && (math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 14400)) {
			http.Error(w, "invalid recording or duration exceeds four hours", 422)
			return
		}
		if e != nil {
			http.Error(w, "storage unavailable", 503)
			return
		}
		if sum != expected.SHA {
			http.Error(w, "audio checksum mismatch", 422)
			return
		}
		if seconds > duration {
			duration = seconds
		}
	}
	var existingMeeting sql.NullInt64
	_ = j.db.QueryRowContext(r.Context(), `SELECT meeting_id FROM meeting_jobs WHERE job_key=?`, key).Scan(&existingMeeting)
	if existingMeeting.Valid {
		// A re-upload after the server spool expired: the meeting row and its
		// quota charge already exist from the first commit.
		if _, e := j.db.ExecContext(r.Context(), `UPDATE meeting_jobs SET status='queued',error='',next_attempt=0 WHERE job_key=? AND status='uploading'`, key); e != nil {
			http.Error(w, "cannot enqueue meeting", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		j.respond(w, key)
		return
	}
	tx, e := j.db.BeginTx(r.Context(), nil)
	if e != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	defer tx.Rollback()
	var tier string
	var active int
	if e = tx.QueryRow(`SELECT COALESCE(plan_tier,'free'),COALESCE(plan_active,0) FROM users WHERE id=?`, userID).Scan(&tier, &active); e != nil {
		http.Error(w, "account unavailable", 503)
		return
	}
	if j.app.DB.Privacy != nil && (tier != "pro" || active != 1) {
		http.Error(w, "Meeting recording requires an active Pro subscription", 403)
		return
	}
	limit := 3600.0
	quotaKey := "quota_free_monthly_seconds"
	if tier == "pro" && active == 1 {
		limit = 180000
		quotaKey = "quota_paid_monthly_seconds"
	}
	quotas := config.ConfigManager.GetRawAll()
	value, configured := quotas[quotaKey]
	if !configured {
		legacy := "free_monthly_limit_seconds"
		if tier == "pro" && active == 1 {
			legacy = "paid_monthly_limit_seconds"
		}
		value = quotas[legacy]
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
		limit = parsed
	}
	quota, e := tx.Exec(`UPDATE users SET monthly_usage_seconds=COALESCE(monthly_usage_seconds,0)+? WHERE id=? AND COALESCE(monthly_usage_seconds,0)+?<=?`, duration, userID, duration, limit)
	if e != nil {
		http.Error(w, "quota unavailable", 503)
		return
	}
	changed, _ := quota.RowsAffected()
	if changed != 1 {
		http.Error(w, "monthly meeting quota exceeded", 403)
		return
	}
	res, e := tx.Exec(`INSERT INTO meetings(user_id,title,duration,audio_path,transcript,summary,action_items,segments_json) VALUES(?,?,?,'','','','','[]')`, userID, title, duration)
	if e != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	id, e := res.LastInsertId()
	if e == nil {
		_, e = tx.Exec(`UPDATE meeting_jobs SET status='queued',meeting_id=? WHERE job_key=? AND status='uploading'`, id, key)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		http.Error(w, "cannot enqueue meeting", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	j.respond(w, key)
}
func (j *MeetingJobs) fleetCall(ctx context.Context, path string, body any) error {
	data, _ := json.Marshal(body)
	request, e := http.NewRequestWithContext(ctx, "POST", j.fleet+path, strings.NewReader(string(data)))
	if e != nil {
		return e
	}
	request.Header.Set("Authorization", "Bearer "+j.token)
	request.Header.Set("Content-Type", "application/json")
	response, e := j.client.Do(request)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("fleet status %d", response.StatusCode)
	}
	return nil
}
func (j *MeetingJobs) loop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	j.migrateLegacyAudio(ctx)
	for {
		j.expireUploads(ctx)
		j.maintainSpool(ctx)
		j.dispatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Incomplete uploads never consume monthly audio quota. Expire abandoned ones
// so they cannot retain disk space or all three pending slots indefinitely.
func (j *MeetingJobs) expireUploads(ctx context.Context) {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	rows, err := j.db.QueryContext(ctx, "SELECT job_key FROM meeting_jobs WHERE status='uploading' AND created_at<? LIMIT 16", cutoff)
	if err != nil {
		return
	}
	var keys []string
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			keys = append(keys, key)
		}
	}
	rows.Close()
	for _, key := range keys {
		unlock := j.lock(key)
		// A re-upload after expiry already owns a meeting row; keep the job so
		// a later retry can attach to it instead of deleting the meeting.
		_, _ = j.db.ExecContext(ctx, "UPDATE meeting_jobs SET status='failed',spool_purged=1,error='The upload did not finish. Retry to upload it again from this device.' WHERE job_key=? AND status='uploading' AND created_at<? AND meeting_id IS NOT NULL", key, cutoff)
		result, err := j.db.ExecContext(ctx, "DELETE FROM meeting_jobs WHERE job_key=? AND status='uploading' AND created_at<? AND meeting_id IS NULL", key, cutoff)
		if err == nil {
			if n, _ := result.RowsAffected(); n == 1 {
				// A failed delete leaves an orphan that maintenance removes.
				_ = j.spool.Delete(ctx, key)
			}
		}
		unlock()
	}
}
func (j *MeetingJobs) dispatch(ctx context.Context) {
	if len(j.token) < 32 || os.Getenv("MEETING_PROCESSING_ENABLED") == "false" {
		return
	}
	// Recover a release interrupted after durable completion, without freeing
	// a still-admitted job that is cancelling in-flight worker requests.
	_, _ = j.db.ExecContext(ctx, `DELETE FROM account_leases WHERE operation='meeting processing' AND owner IN (SELECT job_key FROM meeting_jobs WHERE admitted=0 AND status IN ('completed','failed','cancelled'))`)
	terminal, err := j.db.QueryContext(ctx, "SELECT job_key FROM meeting_jobs WHERE admitted=1 AND status IN ('failed','cancelled') AND lease_until<?", time.Now().Unix())
	if err == nil {
		var keys []string
		for terminal.Next() {
			var key string
			if terminal.Scan(&key) == nil {
				keys = append(keys, key)
			}
		}
		terminal.Close()
		for _, key := range keys {
			if _, e := j.db.ExecContext(ctx, "UPDATE meeting_jobs SET admitted=0 WHERE job_key=? AND status IN ('failed','cancelled')", key); e == nil {
				j.releaseProcessing(key)
			}
		}
	}
	// Keep account admission through CPU waits, not just while a GPU worker is leased.
	activeRows, err := j.db.QueryContext(ctx, "SELECT job_key,user_id FROM meeting_jobs WHERE admitted=1")
	if err == nil {
		type activeJob struct {
			key  string
			user int64
		}
		var active []activeJob
		for activeRows.Next() {
			var job activeJob
			if activeRows.Scan(&job.key, &job.user) == nil {
				active = append(active, job)
			}
		}
		activeRows.Close()
		for _, job := range active {
			_, _ = j.app.limits().AcquireOwner(ctx, job.user, "meeting processing", job.key, 6*time.Hour+5*time.Minute)
		}
	}
	rows, e := j.db.QueryContext(ctx, `SELECT job_key FROM meeting_jobs WHERE ((status='queued' AND next_attempt<=?) OR (status IN ('processing','analyzing') AND lease_until<?)) AND (admitted=0 OR mic_result='' OR system_result='' OR diar_result<>'') AND NOT EXISTS (SELECT 1 FROM meeting_jobs other WHERE other.user_id=meeting_jobs.user_id AND other.admitted=1 AND other.job_key<>meeting_jobs.job_key) ORDER BY admitted DESC,created_at LIMIT 16`, time.Now().Unix(), time.Now().Unix())
	if e != nil {
		return
	}
	var keys []string
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			keys = append(keys, key)
		}
	}
	rows.Close()
	for _, key := range keys {
		var userID int64
		if j.db.QueryRowContext(ctx, "SELECT user_id FROM meeting_jobs WHERE job_key=?", key).Scan(&userID) != nil {
			continue
		}
		if _, err := j.app.limits().AcquireOwner(ctx, userID, "meeting processing", key, 6*time.Hour+5*time.Minute); err != nil {
			continue
		}
		claim := make([]byte, 16)
		rand.Read(claim)
		attempt := hex.EncodeToString(claim)
		now := time.Now().Unix()
		res, e := j.db.ExecContext(ctx, `UPDATE meeting_jobs SET admitted=1,status='processing',attempt=?,attempts=attempts+1,lease_until=? WHERE job_key=? AND ((status='queued' AND next_attempt<=?) OR (status IN ('processing','analyzing') AND lease_until<?)) AND NOT EXISTS (SELECT 1 FROM meeting_jobs other WHERE other.user_id=meeting_jobs.user_id AND other.admitted=1 AND other.job_key<>meeting_jobs.job_key)`, attempt, now+90, key, now, now)
		if e != nil {
			continue
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			continue
		}
		var mic, system string
		if e = j.db.QueryRowContext(ctx, `SELECT mic_result,system_result FROM meeting_jobs WHERE job_key=? AND attempt=?`, key, attempt).Scan(&mic, &system); e != nil {
			continue
		}
		if mic != "" && system != "" {
			go j.process(ctx, key, attempt)
			continue
		}
		leaseCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		e = j.fleetCall(leaseCtx, "/fleet/meetings/acquire", map[string]string{"id": key + "_" + attempt})
		cancel()
		if e != nil {
			j.retry(key, attempt, "waiting for meeting capacity", false)
			j.db.ExecContext(ctx, `UPDATE meeting_jobs SET attempts=MAX(0,attempts-1) WHERE job_key=? AND attempt=?`, key, attempt)
			continue
		}
		go j.process(ctx, key, attempt)
	}
}
func (j *MeetingJobs) releaseProcessing(key string) {
	var user int64
	if j.db.QueryRow("SELECT user_id FROM meeting_jobs WHERE job_key=?", key).Scan(&user) == nil {
		(accountlimits.Store{DB: j.db}).Release(user, "meeting processing", key)
	}
}
func (j *MeetingJobs) retry(key, attempt, message string, permanent bool) {
	status := "queued"
	var attempts int
	j.db.QueryRow(`SELECT attempts FROM meeting_jobs WHERE job_key=? AND attempt=?`, key, attempt).Scan(&attempts)
	if permanent || attempts >= 12 {
		status = "failed"
	}
	result, err := j.db.Exec(`UPDATE meeting_jobs SET admitted=CASE WHEN ?='failed' THEN 0 ELSE admitted END,status=?,error=?,lease_until=0,next_attempt=? WHERE job_key=? AND attempt=? AND status='processing'`, status, status, message, time.Now().Add(30*time.Second).Unix(), key, attempt)
	if err == nil && status == "failed" {
		if n, _ := result.RowsAffected(); n == 1 {
			j.releaseProcessing(key)
		}
	}

}
func (j *MeetingJobs) process(parent context.Context, key, attempt string) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	defer func() {
		result, err := j.db.Exec(`UPDATE meeting_jobs SET admitted=0 WHERE job_key=? AND status IN ('failed','cancelled')`, key)
		if err == nil {
			if n, _ := result.RowsAffected(); n > 0 {
				j.releaseProcessing(key)
			}
		}
	}()

	defer func() {
		releaseCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		j.fleetCall(releaseCtx, "/fleet/meetings/release", map[string]string{"id": key + "_" + attempt})
	}()
	var mic, system, diar string
	if j.db.QueryRow(`SELECT mic_result,system_result,diar_result FROM meeting_jobs WHERE job_key=? AND attempt=?`, key, attempt).Scan(&mic, &system, &diar) != nil {
		return
	}
	var needsGPU = mic == "" || system == ""
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				var status string
				if j.db.QueryRowContext(ctx, `SELECT status FROM meeting_jobs WHERE job_key=? AND attempt=?`, key, attempt).Scan(&status) == nil && status == "completed" {
					return
				}
				if needsGPU {
					renewCtx, stop := context.WithTimeout(ctx, 10*time.Second)
					e := j.fleetCall(renewCtx, "/fleet/meetings/acquire", map[string]string{"id": key + "_" + attempt})
					stop()
					if e != nil {
						cancel()
						return
					}
				}
				res, e := j.db.ExecContext(ctx, `UPDATE meeting_jobs SET lease_until=? WHERE job_key=? AND attempt=? AND status IN ('processing','analyzing')`, time.Now().Add(90*time.Second).Unix(), key, attempt)
				if e != nil {
					cancel()
					return
				}
				n, _ := res.RowsAffected()
				if n != 1 {
					cancel()
					return
				}
			}
		}
	}()
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	var failure error
	permanent := false
	for stage, result := range map[string]string{"mic": mic, "system": system} {
		if result != "" {
			continue
		}
		wg.Add(1)
		go func(stage string) {
			defer wg.Done()
			data, isPermanent, e := j.stage(ctx, key, attempt, stage)
			if e == nil {
				column := stage + "_result"
				sealed, sealErr := j.sealStage(ctx, key, column, string(data))
				if sealErr != nil {
					resultMu.Lock()
					failure = sealErr
					resultMu.Unlock()
					return
				}
				res, saveErr := j.db.ExecContext(ctx, `UPDATE meeting_jobs SET `+column+`=? WHERE job_key=? AND attempt=? AND status='processing'`, sealed, key, attempt)
				e = saveErr
				if e == nil {
					n, _ := res.RowsAffected()
					if n != 1 {
						e = errors.New("job lease lost")
					}
				}
			}
			if e != nil {
				resultMu.Lock()
				if failure == nil {
					failure = e
				}
				permanent = permanent || isPermanent
				resultMu.Unlock()
			}
		}(stage)
	}
	wg.Wait()
	if failure != nil {
		j.retry(key, attempt, failure.Error(), permanent)
		return
	}
	if e := j.db.QueryRowContext(ctx, `SELECT diar_result FROM meeting_jobs WHERE job_key=?`, key).Scan(&diar); e != nil {
		j.retry(key, attempt, e.Error(), false)
		return
	}
	if diar == "" {
		_, _ = j.db.ExecContext(ctx, `UPDATE meeting_jobs SET status='queued',error='waiting for diarization',lease_until=0,next_attempt=0,attempts=MAX(0,attempts-1) WHERE job_key=? AND attempt=? AND status='processing'`, key, attempt)
		return
	}
	if e := j.finish(ctx, key, attempt); e != nil {
		j.retry(key, attempt, e.Error(), false)
	}
}
func (j *MeetingJobs) stage(ctx context.Context, key, attempt, stage string) ([]byte, bool, error) {
	if stage != "mic" && stage != "system" {
		return nil, true, errors.New("GPU stage must be mic or system")
	}
	audio, size, e := j.openChannel(ctx, key, stage)
	if e != nil {
		return nil, errors.Is(e, audiospool.ErrCorrupt), e
	}
	defer audio.Close()
	request, e := http.NewRequestWithContext(ctx, "POST", j.fleet+"/v1/meeting/"+stage+"?language="+url.QueryEscape(j.language(key)), audio)
	if e != nil {
		return nil, true, e
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", "audio/ogg")
	request.Header.Set("Authorization", "Bearer "+j.token)
	request.Header.Set("X-SpeekToMe-Meeting-ID", key+"_"+attempt)
	response, e := j.client.Do(request)
	if e != nil {
		return nil, false, e
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if e != nil {
		return nil, false, e
	}
	if response.StatusCode != 200 {
		return nil, response.StatusCode == 400 || response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 413 || response.StatusCode == 422, fmt.Errorf("%s stage returned HTTP %d", stage, response.StatusCode)
	}
	if !json.Valid(data) {
		return nil, false, errors.New("invalid worker output")
	}
	return data, false, nil
}

type jobWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type jobTranscript struct {
	Text  string    `json:"text"`
	Words []jobWord `json:"words"`
}
type jobSpeaker struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
}

func mergeMeeting(mic, system jobTranscript, speakers []jobSpeaker, name string) []ClientMeetingTurn {
	if name == "" {
		name = "You"
	}
	turns := []ClientMeetingTurn{}
	for channel, transcript := range map[string]jobTranscript{"mic": mic, "system": system} {
		var previous *ClientMeetingTurn
		for _, word := range transcript.Words {
			speakerID, speakerName := "speaker_local", name
			if channel == "system" {
				speakerID, speakerName = "unknown", "Unknown speaker"
				best := 0.0
				for _, speaker := range speakers {
					overlap := min(word.End, speaker.End) - max(word.Start, speaker.Start)
					if overlap > best {
						best = overlap
						speakerID = fmt.Sprintf("spk_%d", speaker.Speaker)
						speakerName = fmt.Sprintf("Speaker %d", speaker.Speaker)
					}
				}
			}
			if previous != nil && previous.SpeakerID == speakerID && word.Start-previous.EndTime < 1.0 {
				previous.Text += " " + word.Word
				previous.EndTime = word.End
				turns[len(turns)-1] = *previous
			} else {
				turn := ClientMeetingTurn{Channel: channel, SpeakerID: speakerID, SpeakerName: speakerName, StartTime: word.Start, EndTime: word.End, Text: word.Word, IsFinal: true}
				turns = append(turns, turn)
				previous = &turn
			}
		}
	}
	sort.SliceStable(turns, func(a, b int) bool { return turns[a].StartTime < turns[b].StartTime })
	for index := range turns {
		turns[index].ID = fmt.Sprintf("turn-%d", index)
	}
	return turns
}
func (j *MeetingJobs) finish(ctx context.Context, key, attempt string) error {
	var micData, systemData, diarData, name string
	var meeting, userID int64
	if e := j.db.QueryRowContext(ctx, `SELECT j.mic_result,j.system_result,j.diar_result,j.meeting_id,j.user_id,COALESCE(u.name,'') FROM meeting_jobs j JOIN users u ON u.id=j.user_id WHERE j.job_key=? AND j.attempt=? AND j.status='processing'`, key, attempt).Scan(&micData, &systemData, &diarData, &meeting, &userID, &name); e != nil {
		return e
	}
	if j.app.DB.Privacy != nil {
		for col, p := range map[string]*string{"mic_result": &micData, "system_result": &systemData, "diar_result": &diarData} {
			value, e := j.app.DB.Privacy.OpenStored(ctx, userID, "job-"+col, key, *p)
			if e != nil {
				return e
			}
			*p = value
		}
	}
	var mic, system jobTranscript
	var diar struct {
		Segments []jobSpeaker `json:"segments"`
	}
	if json.Unmarshal([]byte(micData), &mic) != nil || json.Unmarshal([]byte(systemData), &system) != nil || json.Unmarshal([]byte(diarData), &diar) != nil {
		return errors.New("invalid persisted stage output")
	}
	turns := mergeMeeting(mic, system, diar.Segments, name)
	segments := []MeetingSegment{}
	var lines []string
	for _, turn := range turns {
		segments = append(segments, MeetingSegment{Start: turn.StartTime, End: turn.EndTime, Text: turn.Text, Speaker: turn.SpeakerName, SpeakerID: turn.SpeakerID, Channel: turn.Channel})
		lines = append(lines, turn.SpeakerName+": "+turn.Text)
	}
	transcript := strings.Join(lines, "\n")
	terms, err := j.app.DB.GetActiveVocabularyTerms(userID)
	if err != nil {
		return err
	}
	audit := finalizeVocabulary(ctx, transcript, terms, nil)
	transcript = audit.Resolved
	resolvedLines := strings.Split(transcript, "\n")
	if len(resolvedLines) == len(segments) {
		for i, line := range resolvedLines {
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				segments[i].Speaker = parts[0]
				segments[i].Text = parts[1]
			}
		}
	}
	grammarInput, restore := audit.Protect()
	data, _ := json.Marshal(segments)
	fields := map[string]string{"transcript": transcript, "segments_json": string(data), "vocabulary_audit": auditJSON(audit)}
	if j.app.DB.Privacy != nil {
		if e := j.app.DB.Privacy.MeetingFields(ctx, userID, meeting, fields, true); e != nil {
			return e
		}
	}
	tx, e := j.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, `UPDATE meeting_jobs SET status='analyzing',error='' WHERE job_key=? AND attempt=? AND status='processing'`, key, attempt)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("job ownership changed")
	}
	_, e = tx.ExecContext(ctx, `UPDATE meetings SET transcript=?,segments_json=?,summary=?,action_items=?,vocabulary_audit=? WHERE id=? AND user_id=?`, fields["transcript"], fields["segments_json"], "", "", fields["vocabulary_audit"], meeting, userID)
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	// GPU work is complete. Do not hold a worker while the optional external
	// summary model runs on the main server. Releasing this attempt is idempotent.
	releaseCtx, releaseCancel := context.WithTimeout(ctx, 10*time.Second)
	_ = j.fleetCall(releaseCtx, "/fleet/meetings/release", map[string]string{"id": key + "_" + attempt})
	releaseCancel()
	if transcript != "" {
		analysisCtx, stop := context.WithTimeout(ctx, 3*time.Minute)
		defer stop()
		polished, summary, actions, analysisErr := generateMeetingAIAnalysis(analysisCtx, grammarInput, pipeline.NewPipelineContext(userID))
		if analysisErr == nil {
			e = j.app.DB.UpdateMeetingResult(meeting, restore(polished), audit.RestoreMentions(summary), audit.RestoreMentions(actions), fieldsPlainSegments(data), auditJSON(audit))
			if e != nil {
				log.Printf("[MEETINGS] summary persistence failed for %d: %v", meeting, e)
			}
		}
	}
	completed, e := j.db.ExecContext(ctx, `UPDATE meeting_jobs SET status='completed',admitted=0,lease_until=0,mic_result='',system_result='',diar_result='' WHERE job_key=? AND attempt=? AND status='analyzing'`, key, attempt)
	if e != nil {
		return e
	}
	n, _ = completed.RowsAffected()
	if n != 1 {
		return errors.New("meeting ownership changed before completion")
	}
	j.releaseProcessing(key)
	return nil
}

func (j *MeetingJobs) language(key string) string {
	var language string
	if j.db.QueryRow(`SELECT language FROM meeting_jobs WHERE job_key=?`, key).Scan(&language) == nil && language != "" {
		return utils.ResolveNemotronLanguage([]string{language})
	}
	// Jobs uploaded before this migration have no saved choice.
	var encoded string
	if j.db.QueryRow(`SELECT COALESCE(u.languages,'["en"]') FROM users u JOIN meeting_jobs j ON u.id=j.user_id WHERE j.job_key=?`, key).Scan(&encoded) != nil {
		return "en-US"
	}
	var languages []string
	json.Unmarshal([]byte(encoded), &languages)
	resolved := utils.ResolveNemotronLanguage(languages)
	_, _ = j.db.Exec(`UPDATE meeting_jobs SET language=? WHERE job_key=? AND language=''`, resolved, key)
	return resolved
}

func (j *MeetingJobs) retryHandler(w http.ResponseWriter, r *http.Request) {
	_, key, ok := j.identity(w, r)
	if !ok {
		return
	}
	// Once its spool expired, a failed meeting needs the desktop's local copy
	// again; the job returns to uploading and keeps its meeting row.
	res, e := j.db.Exec(`UPDATE meeting_jobs SET status=CASE WHEN spool_purged=1 THEN 'uploading' ELSE 'queued' END,mic_result=CASE WHEN spool_purged=1 THEN '' ELSE mic_result END,system_result=CASE WHEN spool_purged=1 THEN '' ELSE system_result END,diar_result=CASE WHEN spool_purged=1 THEN '' ELSE diar_result END,spool_purged=0,attempts=0,next_attempt=0,error='',created_at=CASE WHEN spool_purged=1 THEN ? ELSE created_at END WHERE job_key=? AND status='failed' AND (SELECT COUNT(*) FROM meeting_jobs pending WHERE pending.user_id=meeting_jobs.user_id AND pending.admitted=0 AND pending.status IN ('uploading','queued'))<3`, time.Now().Unix(), key)
	if e != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		accountlimits.Write(w, accountlimits.Denied("meeting_retry_unavailable", "This meeting cannot be retried yet. Your account may already have three queued meetings.", 30))
		return
	}
	// Successful checkpoints are retained. A failed CPU job gets a new remote
	// identity only after the previous terminal job has been acknowledged.
	_, _ = j.db.ExecContext(r.Context(), `UPDATE meeting_diarization_queue SET status='queued',generation=generation+1,remote_id='',error='',next_attempt=0 WHERE job_key=? AND status IN ('failed','cancelled') AND acknowledged=1`, key)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	j.respond(w, key)
}

func fieldsPlainSegments(data []byte) string { return string(data) }
func (j *MeetingJobs) sealStage(ctx context.Context, key, col, text string) (string, error) {
	if j.app.DB.Privacy == nil {
		return text, nil
	}
	var user int64
	if e := j.db.QueryRowContext(ctx, "SELECT user_id FROM meeting_jobs WHERE job_key=?", key).Scan(&user); e != nil {
		return "", e
	}
	return j.app.DB.Privacy.SealStored(ctx, user, "job-"+col, key, text)
}

func (j *MeetingJobs) openStage(ctx context.Context, key, col, text string) (string, error) {
	if j.app.DB.Privacy == nil {
		return text, nil
	}
	var user int64
	if e := j.db.QueryRowContext(ctx, "SELECT user_id FROM meeting_jobs WHERE job_key=?", key).Scan(&user); e != nil {
		return "", e
	}
	return j.app.DB.Privacy.OpenStored(ctx, user, "job-"+col, key, text)
}

func (j *MeetingJobs) accountDeleting(ctx context.Context, user int64) bool {
	if j.app.DB.Privacy == nil {
		return false
	}
	var n int
	e := j.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM private_account_deletions WHERE user_id=?", user).Scan(&n)
	return e != nil || n > 0
}
