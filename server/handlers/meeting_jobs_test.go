package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"speektome-server/audiospool"
	"strings"
	"testing"
	"time"

	"speektome-server/db"
)

func TestMergeMeetingKeepsChannelsAndUnknownSpeakers(t *testing.T) {
	mic := jobTranscript{Words: []jobWord{{"me", 0, 1}}}
	system := jobTranscript{Words: []jobWord{{"them", 0, 1}, {"unknown", 3, 4}}}
	turns := mergeMeeting(mic, system, []jobSpeaker{{0, 2, 3}}, "Owner")
	if len(turns) != 3 {
		t.Fatal(turns)
	}
	found := map[string]bool{}
	for _, turn := range turns {
		found[turn.SpeakerID] = true
	}
	if !found["speaker_local"] || !found["spk_3"] || !found["unknown"] {
		t.Fatal(turns)
	}
}
func TestStaleMeetingAttemptCannotOverwriteNewOwner(t *testing.T) {
	database, e := db.NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := &App{DB: database}
	if e = app.StartMeetingJobs(ctx, http.NewServeMux()); e != nil {
		t.Fatal(e)
	}
	_, e = database.GetDB().Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,created_at,attempt,lease_until) VALUES('key',1,'id','title','{}','processing',0,'new',?)`, time.Now().Add(time.Hour).Unix())
	if e != nil {
		t.Fatal(e)
	}
	processor := &MeetingJobs{db: database.GetDB()}
	processor.retry("key", "old", "stale failure", true)
	var status, attempt string
	database.GetDB().QueryRow(`SELECT status,attempt FROM meeting_jobs WHERE job_key='key'`).Scan(&status, &attempt)
	if status != "processing" || attempt != "new" {
		t.Fatal(status, attempt)
	}
}

func TestMeetingUploadResumesAndIsolatesUsers(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, id := range []int{1, 2} {
		if _, err := database.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(?,?,?,'pro',1)`, id, fmt.Sprintf("user%d@example.com", id), "Test"); err != nil {
			t.Fatal(err)
		}
	}
	database.CreateSession("owner", 1, 0)
	database.CreateSession("other", 2, 0)
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "absent"))
	app := &App{DB: database}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux := http.NewServeMux()
	if err := app.StartMeetingJobs(ctx, mux); err != nil {
		t.Fatal(err)
	}
	audio := []byte("audio bytes")
	sum := sha256.Sum256(audio)
	files := map[string]MeetingFile{}
	for _, channel := range []string{"mic", "system", "mixed"} {
		files[channel] = MeetingFile{Size: int64(len(audio)), SHA: hex.EncodeToString(sum[:])}
	}
	manifest, _ := json.Marshal(MeetingUpload{ID: "resumable", Title: "Test", Files: files, Language: "hi"})
	send := func(method, path, token, offset string, data []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		if offset != "" {
			r.Header.Set("Upload-Offset", offset)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := send("POST", "/v1/meetings/jobs", "owner", "", manifest); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var savedLanguage string
	if err := database.GetDB().QueryRow(`SELECT language FROM meeting_jobs WHERE client_id='resumable'`).Scan(&savedLanguage); err != nil || savedLanguage != "hi" {
		t.Fatalf("meeting language was not persisted: %q, %v", savedLanguage, err)
	}
	endpoint := "/v1/meetings/jobs/resumable/audio/mic"
	if w := send("PUT", endpoint, "owner", "0", audio[:4]); w.Code != 204 || w.Header().Get("Upload-Offset") != "4" {
		t.Fatal(w)
	}
	if w := send("PUT", endpoint, "owner", "0", audio); w.Code != 409 || w.Header().Get("Upload-Offset") != "4" {
		t.Fatal(w)
	}
	if w := send("PUT", endpoint, "other", "4", audio[4:]); w.Code != 404 {
		t.Fatal("other user reached upload", w.Code)
	}
	if w := send("PUT", endpoint, "owner", "4", audio[4:]); w.Code != 204 {
		t.Fatal(w)
	}
	if w := send("GET", "/v1/meetings/jobs/resumable", "owner", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"mic":11`) {
		t.Fatal(w.Body.String())
	}
	// Corrupt but correctly sized other channels: commit must fail before decode.
	for _, channel := range []string{"system", "mixed"} {
		send("PUT", "/v1/meetings/jobs/resumable/audio/"+channel, "owner", "0", bytes.Repeat([]byte{'x'}, len(audio)))
	}
	if w := send("POST", "/v1/meetings/jobs/resumable/commit", "owner", "", nil); w.Code != 422 {
		t.Fatal("checksum not enforced", w.Code, w.Body.String())
	}
}

func TestMeetingLanguageSurvivesAccountChangesAndMigration(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "language.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sql := database.GetDB()
	_, err = sql.Exec(`INSERT INTO users(id,email,languages) VALUES(1,'language@example.com','["en","hi"]');
        CREATE TABLE meeting_jobs (job_key TEXT PRIMARY KEY,user_id INTEGER,language TEXT NOT NULL DEFAULT '');
        INSERT INTO meeting_jobs VALUES('saved',1,'hi'),('automatic',1,'auto'),('legacy',1,'');`)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &MeetingJobs{db: sql}
	if got := jobs.language("legacy"); got != "en-US" {
		t.Fatal(got)
	}
	_, err = sql.Exec(`UPDATE users SET languages='["fr"]' WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"saved": "hi-IN", "automatic": "auto", "legacy": "en-US"} {
		if got := jobs.language(key); got != want {
			t.Errorf("%s: %s != %s", key, got, want)
		}
	}
}

func TestMeetingSpeakerZeroAndHigherIDsAreSeparateFromMicrophone(t *testing.T) {
	turns := mergeMeeting(jobTranscript{Words: []jobWord{{"local", 0, 1}}},
		jobTranscript{Words: []jobWord{{"zero", 1, 2}, {"ten", 3, 4}}},
		[]jobSpeaker{{1, 2, 0}, {3, 4, 10}}, "Owner")
	if len(turns) != 3 {
		t.Fatal(turns)
	}
	if turns[0].Channel != "mic" || turns[0].SpeakerID != "speaker_local" {
		t.Fatal(turns[0])
	}
	for i, id := range []string{"spk_0", "spk_10"} {
		if turns[i+1].Channel != "system" || turns[i+1].SpeakerID != id {
			t.Fatal(turns[i+1])
		}
	}
}

func TestMeetingUploadIsSealedAndCommitVerifiesOgg(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "sealed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(1,'owner@example.com','Owner','pro',1)`); err != nil {
		t.Fatal(err)
	}
	database.CreateSession("owner", 1, 0)
	legacy := t.TempDir()
	t.Setenv("MEETING_AUDIO_DIR", legacy)
	spoolDir := t.TempDir()
	t.Setenv("MEETING_SPOOL_DIR", spoolDir)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "absent"))
	app := &App{DB: database}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux := http.NewServeMux()
	if err := app.StartMeetingJobs(ctx, mux); err != nil {
		t.Fatal(err)
	}
	audio := audiospool.SyntheticOggOpus(42)
	sum := sha256.Sum256(audio)
	files := map[string]MeetingFile{}
	for _, channel := range []string{"mic", "system", "mixed"} {
		files[channel] = MeetingFile{Size: int64(len(audio)), SHA: hex.EncodeToString(sum[:])}
	}
	manifest, _ := json.Marshal(MeetingUpload{ID: "sealed", Title: "Test", Files: files, Language: "en"})
	send := func(method, path, offset string, data []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer owner")
		if offset != "" {
			r.Header.Set("Upload-Offset", offset)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := send("POST", "/v1/meetings/jobs", "", manifest); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, channel := range []string{"mic", "system", "mixed"} {
		if w := send("PUT", "/v1/meetings/jobs/sealed/audio/"+channel, "0", audio); w.Code != 204 {
			t.Fatal(channel, w.Code, w.Body.String())
		}
	}
	if w := send("POST", "/v1/meetings/jobs/sealed/commit", "", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var duration float64
	var audioPath string
	if err := database.GetDB().QueryRow(`SELECT duration,audio_path FROM meetings WHERE user_id=1`).Scan(&duration, &audioPath); err != nil {
		t.Fatal(err)
	}
	if duration < 41.9 || duration > 42.1 || audioPath != "" {
		t.Fatalf("meeting duration %v from Ogg pages, audio path %q", duration, audioPath)
	}
	for _, dir := range []string{legacy, spoolDir} {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				if data, _ := os.ReadFile(path); bytes.Contains(data, []byte("OpusHead")) {
					t.Fatalf("plaintext audio stored at %s", path)
				}
			}
			return nil
		})
	}
	// Completed meetings release their spool; nothing remains afterwards.
	if _, err := database.GetDB().Exec(`UPDATE meeting_jobs SET status='completed' WHERE client_id='sealed'`); err != nil {
		t.Fatal(err)
	}
	// Deleting sealed chunks needs no key, so a second spool handle suffices.
	spool, err := newMeetingSpool(app)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &MeetingJobs{app: app, db: database.GetDB(), spool: spool}
	jobs.maintainSpool(context.Background())
	if jobs.spool.Has(context.Background(), jobKey(1, "sealed")) {
		t.Fatal("completed meeting kept spooled audio")
	}
}

func TestMeetingUploadAcceptsTheSignedTranscriptFile(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "transcript.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(1,'owner@example.com','Owner','pro',1)`); err != nil {
		t.Fatal(err)
	}
	database.CreateSession("owner", 1, 0)
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("MEETING_SPOOL_DIR", t.TempDir())
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "absent"))
	app := &App{DB: database}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux := http.NewServeMux()
	if err := app.StartMeetingJobs(ctx, mux); err != nil {
		t.Fatal(err)
	}
	send := func(method, path, offset string, data []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer owner")
		if offset != "" {
			r.Header.Set("Upload-Offset", offset)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	audio := audiospool.SyntheticOggOpus(10)
	transcript := []byte(`{"type":"words","batch":{},"sig":""}` + "\n")
	describe := func(data []byte) MeetingFile {
		sum := sha256.Sum256(data)
		return MeetingFile{Size: int64(len(data)), SHA: hex.EncodeToString(sum[:])}
	}
	// The transcript is optional and can never replace a recording.
	missingMixed, _ := json.Marshal(MeetingUpload{ID: "bad", Files: map[string]MeetingFile{"mic": describe(audio), "system": describe(audio), "transcript": describe(transcript)}})
	if w := send("POST", "/v1/meetings/jobs", "", missingMixed); w.Code != 400 {
		t.Fatal("accepted a meeting without its mixed recording", w.Code)
	}
	files := map[string]MeetingFile{"mic": describe(audio), "system": describe(audio), "mixed": describe(audio), "transcript": describe(transcript)}
	long, _ := json.Marshal(MeetingUpload{ID: "long", Title: "Test", Description: strings.Repeat("é", meetingDescriptionLimit+1), Files: files})
	if w := send("POST", "/v1/meetings/jobs", "", long); w.Code != 400 {
		t.Fatal("accepted an oversized description", w.Code)
	}
	manifest, _ := json.Marshal(MeetingUpload{ID: "with-transcript", Title: "Test", Description: "Weekly sync", Files: files, Language: "en"})
	if w := send("POST", "/v1/meetings/jobs", "", manifest); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, channel := range []string{"mic", "system", "mixed"} {
		if w := send("PUT", "/v1/meetings/jobs/with-transcript/audio/"+channel, "0", audio); w.Code != 204 {
			t.Fatal(channel, w.Code, w.Body.String())
		}
	}
	// A transcript altered after the manifest was declared fails the checksum.
	if w := send("PUT", "/v1/meetings/jobs/with-transcript/audio/transcript", "0", bytes.ToUpper(transcript)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	var stored string
	if err := database.GetDB().QueryRow(`SELECT description FROM meeting_jobs WHERE client_id='with-transcript'`).Scan(&stored); err != nil || stored != "Weekly sync" {
		t.Fatal("description not stored on the job", stored, err)
	}
	if w := send("POST", "/v1/meetings/jobs/with-transcript/commit", "", nil); w.Code != 422 {
		t.Fatal("transcript checksum not enforced", w.Code, w.Body.String())
	}
	// The same flow with the declared transcript commits.
	files2 := map[string]MeetingFile{"mic": describe(audio), "system": describe(audio), "mixed": describe(audio), "transcript": describe(transcript)}
	manifest2, _ := json.Marshal(MeetingUpload{ID: "with-transcript-2", Title: "Test", Files: files2, Language: "en"})
	send("POST", "/v1/meetings/jobs", "", manifest2)
	for _, channel := range []string{"mic", "system", "mixed"} {
		send("PUT", "/v1/meetings/jobs/with-transcript-2/audio/"+channel, "0", audio)
	}
	send("PUT", "/v1/meetings/jobs/with-transcript-2/audio/transcript", "0", transcript)
	if w := send("POST", "/v1/meetings/jobs/with-transcript-2/commit", "", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
}
