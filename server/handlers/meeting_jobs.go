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
	"os"
	"path/filepath"
	"regexp"
	"speektome-server/audiospool"
	"speektome-server/config"
	log "speektome-server/privacylog"
	"speektome-server/utils"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"speektome-server/accountlimits"
	"speektome-server/auth"
)

type MeetingFile struct {
	Size int64  `json:"size"`
	SHA  string `json:"sha256"`
}
type MeetingUpload struct {
	Language string `json:"language,omitempty"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	// Description is the organiser's note about the meeting, used only as
	// background for the summary.
	Description string                 `json:"description,omitempty"`
	Files       map[string]MeetingFile `json:"files"`
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

// meetingDescriptionLimit keeps the manifest well inside its 8 KB read limit.
const meetingDescriptionLimit = 2000

// A four-hour meeting's signed live transcript is about 3-5 MB.
const meetingTranscriptLimit = 16 << 20

// meetingAttempts bounds automatic attempts before a meeting shows as failed.
const meetingAttempts = 4

// meetingChannel lists the uploaded files: three recordings and the optional
// signed live transcript.
func meetingChannel(channel string) bool {
	return channel == "mic" || channel == "system" || channel == "mixed" || channel == "transcript"
}

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
	// Account-sealed, like every other meeting text field.
	var descriptionColumn int
	_ = jobs.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('meeting_jobs') WHERE name='description'").Scan(&descriptionColumn)
	if descriptionColumn == 0 {
		if _, e = jobs.db.Exec("ALTER TABLE meeting_jobs ADD COLUMN description TEXT NOT NULL DEFAULT ''"); e != nil {
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
	req.Description = strings.TrimSpace(req.Description)
	if !utf8.ValidString(req.Description) || utf8.RuneCountInString(req.Description) > meetingDescriptionLimit {
		http.Error(w, "meeting description is too long", 400)
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
	_, hasTranscript := req.Files["transcript"]
	_, hasMic := req.Files["mic"]
	_, hasSystem := req.Files["system"]
	_, hasMixed := req.Files["mixed"]
	if !hasMic || !hasSystem || !hasMixed || (len(req.Files) != 3 && !(len(req.Files) == 4 && hasTranscript)) {
		http.Error(w, "mic, system, and mixed files required", 400)
		return
	}
	for channel, f := range req.Files {
		limit := int64(512 << 20)
		if channel == "transcript" {
			limit = meetingTranscriptLimit
		}
		digest, e := hex.DecodeString(f.SHA)
		if !meetingChannel(channel) || e != nil || len(digest) != 32 || f.Size < 1 || f.Size > limit {
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

	description := req.Description
	if description != "" && j.app.DB.Privacy != nil {
		if description, e = j.app.DB.Privacy.SealStored(r.Context(), user.ID, "job-description", key, description); e != nil {
			accountlimits.Write(w, accountlimits.Unavailable())
			return
		}
	}
	unlock := j.lock(key)
	defer unlock()
	_, e = j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,created_at,language,description) SELECT ?,?,?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM meeting_jobs WHERE user_id=? AND admitted=0 AND status IN ('uploading','queued'))<3 ON CONFLICT(user_id,client_id) DO NOTHING`, key, user.ID, req.ID, req.Title, string(data), time.Now().Unix(), language, description, user.ID)
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
	if !meetingChannel(channel) {
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
	if _, listed := manifest[channel]; !listed || offset+int64(len(data)) > manifest[channel].Size {
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
	if expected, ok := manifest["transcript"]; ok {
		size, sum, e := j.checksumChannel(r.Context(), userID, key, "transcript")
		if errors.Is(e, audiospool.ErrCorrupt) || (e == nil && size != expected.Size) {
			http.Error(w, "upload incomplete", 409)
			return
		}
		if e != nil {
			http.Error(w, "storage unavailable", 503)
			return
		}
		if sum != expected.SHA {
			http.Error(w, "transcript checksum mismatch", 422)
			return
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
	rows, e := j.db.QueryContext(ctx, `SELECT job_key FROM meeting_jobs WHERE ((status='queued' AND next_attempt<=?) OR (status IN ('processing','analyzing') AND lease_until<?)) AND NOT EXISTS (SELECT 1 FROM meeting_jobs other WHERE other.user_id=meeting_jobs.user_id AND other.admitted=1 AND other.job_key<>meeting_jobs.job_key) ORDER BY admitted DESC,created_at LIMIT 16`, time.Now().Unix(), time.Now().Unix())
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
	// Every attempt re-runs the whole in-memory pipeline, including the LLM steps.
	if permanent || attempts >= meetingAttempts {
		status = "failed"
	}
	result, err := j.db.Exec(`UPDATE meeting_jobs SET admitted=CASE WHEN ?='failed' THEN 0 ELSE admitted END,status=?,error=?,lease_until=0,next_attempt=? WHERE job_key=? AND attempt=? AND status IN ('processing','analyzing')`, status, status, message, time.Now().Add(30*time.Second).Unix(), key, attempt)
	if err == nil && status == "failed" {
		if n, _ := result.RowsAffected(); n == 1 {
			j.releaseProcessing(key)
		}
	}

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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	j.respond(w, key)
}

func (j *MeetingJobs) accountDeleting(ctx context.Context, user int64) bool {
	if j.app.DB.Privacy == nil {
		return false
	}
	var n int
	e := j.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM private_account_deletions WHERE user_id=?", user).Scan(&n)
	return e != nil || n > 0
}
