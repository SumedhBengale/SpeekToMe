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
	"strings"
	"sync"
	"testing"
	"time"

	"speektome-server/db"
)

func TestMeetingJobPersistsStagesAndCompletesWithoutClient(t *testing.T) {
	database, e := db.NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	_, e = database.GetDB().Exec(`INSERT INTO users(id,email,name) VALUES(1,'owner@example.com','Owner')`)
	if e != nil {
		t.Fatal(e)
	}
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	root := t.TempDir()
	t.Setenv("MEETING_AUDIO_DIR", root)
	var mu sync.Mutex
	calls := map[string]int{}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer 01234567890123456789012345678901" {
			t.Error("missing worker authentication")
		}
		if r.URL.Path == "/fleet/meetings/acquire" || r.URL.Path == "/fleet/meetings/release" {
			w.WriteHeader(204)
			return
		}
		stage := filepath.Base(r.URL.Path)
		mu.Lock()
		calls[stage]++
		mu.Unlock()
		if stage == "diar" {
			w.Write([]byte(`{"segments":[{"start":0,"end":2,"speaker":2}]}`))
			return
		}
		w.Write([]byte(`{"text":"hello","words":[{"word":"hello","start":0.2,"end":0.8}]}`))
	}))
	defer worker.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	app := &App{DB: database}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := http.NewServeMux()
	initContext, stopInit := context.WithCancel(context.Background())
	stopInit()
	if e = app.StartMeetingJobs(initContext, mux); e != nil {
		t.Fatal(e)
	}
	key := jobKey(1, "persistent-meeting")
	os.Mkdir(filepath.Join(root, key), 0700)
	for _, channel := range []string{"mic", "system"} {
		os.WriteFile(filepath.Join(root, key, channel+".ogg"), []byte("fake-audio"), 0600)
	}
	id, e := database.CreateMeeting(1, "meeting", 1, filepath.Join(root, key, "mixed.ogg"))
	if e != nil {
		t.Fatal(e)
	}
	// Microphone stage already succeeded on an earlier worker; only other stages
	// should execute after restart/retry.
	_, e = database.GetDB().Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,meeting_id,created_at,attempt,diar_result,mic_result) VALUES(?,1,'persistent-meeting','meeting','{}','processing',?,0,'attempt','{"segments":[{"start":0,"end":2,"speaker":2}]}',?)`, key, id, `{"words":[{"word":"local","start":0.1,"end":0.5}]}`)
	if e != nil {
		t.Fatal(e)
	}
	processor := &MeetingJobs{app: app, db: database.GetDB(), root: root, fleet: worker.URL, token: "01234567890123456789012345678901", client: worker.Client()}
	processor.process(ctx, key, "attempt")
	var status string
	database.GetDB().QueryRow(`SELECT status FROM meeting_jobs WHERE job_key=?`, key).Scan(&status)
	if status != "completed" {
		t.Fatal(status)
	}
	meeting, e := database.GetMeeting(id)
	if e != nil {
		t.Fatal(e)
	}
	var segments []MeetingSegment
	if json.Unmarshal([]byte(meeting.SegmentsJSON), &segments) != nil || len(segments) != 2 {
		t.Fatal(meeting)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["mic"] != 0 || calls["system"] != 1 || calls["diar"] != 0 {
		t.Fatal(calls)
	}
}
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
		if _, err := database.GetDB().Exec(`INSERT INTO users(id,email,name) VALUES(?,?,?)`, id, fmt.Sprintf("user%d@example.com", id), "Test"); err != nil {
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
