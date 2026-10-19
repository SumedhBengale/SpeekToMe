package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"speektome-server/db"
)

func diarizationFixture(t *testing.T) (*MeetingJobs, *MeetingDiarization) {
	t.Helper()
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	root := t.TempDir()
	t.Setenv("MEETING_AUDIO_DIR", root)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(root, "missing"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := &App{DB: database}
	if err := app.StartMeetingJobs(ctx, http.NewServeMux()); err != nil {
		t.Fatal(err)
	}
	j := &MeetingJobs{app: app, db: database.GetDB(), root: root, client: http.DefaultClient}
	d, err := newMeetingDiarization(j)
	if err != nil {
		t.Fatal(err)
	}
	d.token = "01234567890123456789012345678901"
	return j, d
}
func addDiarizationMeeting(t *testing.T, j *MeetingJobs, key string, created int) {
	t.Helper()
	if _, err := j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,created_at,admitted) VALUES(?,?,?,'test','{}','queued',?,1)`, key, created, key, created); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(j.root, key), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(j.root, key, "system.ogg"), []byte("fixture audio"), 0600); err != nil {
		t.Fatal(err)
	}
}
func queueTick(t *testing.T, d *MeetingDiarization) {
	t.Helper()
	if _, err := d.jobs.db.Exec(`UPDATE meeting_diarization_queue SET next_attempt=0`); err != nil {
		t.Fatal(err)
	}
	if err := d.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type fakeCPU struct {
	mu                     sync.Mutex
	jobs                   map[string]string
	submissions            []string
	active, maxActive      int
	busyOnce, loseResponse bool
	database               *MeetingJobs
	t                      *testing.T
}

func (f *fakeCPU) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer 01234567890123456789012345678901" {
		f.t.Error("missing CPU authentication")
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/diarizations/")
	if r.Method == "POST" {
		if f.busyOnce {
			f.busyOnce = false
			w.Header().Set("Retry-After", "15")
			w.WriteHeader(429)
			return
		}
		id = r.Header.Get("X-SpeekToMe-Job-ID")
		if len(id) != 32 {
			f.t.Error("missing durable identity")
		}
		if _, ok := f.jobs[id]; !ok {
			if f.active != 0 {
				f.t.Error("overlapping CPU submission")
			}
			f.active++
			f.maxActive = max(f.maxActive, f.active)
			f.jobs[id] = "processing"
			f.submissions = append(f.submissions, id)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "fixture audio" {
			f.t.Error("wrong audio channel")
		}
		if f.loseResponse {
			f.loseResponse = false
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"job_id":%q}`, id)
		return
	}
	if r.Method == "DELETE" {
		if _, ok := f.jobs[id]; !ok {
			w.WriteHeader(404)
			return
		}
		var saved string
		f.database.db.QueryRow(`SELECT j.diar_result FROM meeting_jobs j JOIN meeting_diarization_queue q ON j.job_key=q.job_key WHERE q.remote_id=?`, id).Scan(&saved)
		if f.jobs[id] == "completed" && saved == "" {
			f.t.Error("remote deleted before result saved")
		}
		delete(f.jobs, id)
		w.Write([]byte(`{}`))
		return
	}
	if strings.HasSuffix(id, "/result") {
		w.Write([]byte(`{"segments":[{"Start":0.25,"End":2.5,"Speaker":3}]}`))
		return
	}
	status, ok := f.jobs[id]
	if !ok {
		w.WriteHeader(404)
		return
	}
	fmt.Fprintf(w, `{"status":%q,"error":"test failure"}`, status)
}
func (f *fakeCPU) complete() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, status := range f.jobs {
		if status == "processing" {
			f.jobs[id] = "completed"
			f.active--
		}
	}
}
func fakeCPUServer(t *testing.T, j *MeetingJobs, d *MeetingDiarization) *fakeCPU {
	t.Helper()
	f := &fakeCPU{jobs: map[string]string{}, database: j, t: t}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	d.url = server.URL
	return f
}

func TestDiarizationFIFOOneActiveAndSaveBeforeAcknowledgment(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, j, d)
	addDiarizationMeeting(t, j, "first", 1)
	addDiarizationMeeting(t, j, "second", 2)
	queueTick(t, d)
	for range 3 {
		queueTick(t, d)
	}
	f.mu.Lock()
	if len(f.submissions) != 1 || f.maxActive != 1 {
		t.Fatal(f.submissions, f.maxActive)
	}
	first := f.submissions[0]
	f.mu.Unlock()
	var remote, status string
	j.db.QueryRow(`SELECT remote_id,status FROM meeting_diarization_queue WHERE job_key='first'`).Scan(&remote, &status)
	if remote != first || status != "running" {
		t.Fatal(remote, status)
	}
	f.complete()
	queueTick(t, d) // Save result.
	var result string
	j.db.QueryRow(`SELECT diar_result FROM meeting_jobs WHERE job_key='first'`).Scan(&result)
	if !strings.Contains(result, `"speaker":3`) {
		t.Fatal(result)
	}
	queueTick(t, d) // Acknowledge after durable save.
	queueTick(t, d) // Start second.
	f.complete()
	queueTick(t, d)
	queueTick(t, d)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submissions) != 2 || f.maxActive != 1 || len(f.jobs) != 0 {
		t.Fatal(f.submissions, f.maxActive, f.jobs)
	}
}

func TestDiarizationBusyPreservesFIFOAndLostResponseRecoversAfterRestart(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, j, d)
	f.busyOnce = true
	f.loseResponse = true
	addDiarizationMeeting(t, j, "first", 1)
	addDiarizationMeeting(t, j, "second", 2)
	queueTick(t, d) // External worker busy: first stays head.
	if len(f.submissions) != 0 {
		t.Fatal("busy response lost queue")
	}
	queueTick(t, d) // Remote accepts, connection closes before 202.
	restarted := &MeetingDiarization{jobs: j, url: d.url, token: d.token}
	queueTick(t, restarted) // Reattach to persisted identity, no second POST.
	f.mu.Lock()
	if len(f.submissions) != 1 {
		t.Fatal(f.submissions)
	}
	f.mu.Unlock()
	f.complete()
	queueTick(t, restarted)
	queueTick(t, restarted)
	queueTick(t, restarted)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submissions) != 2 || f.maxActive != 1 {
		t.Fatal(f.submissions, f.maxActive)
	}
}

func TestDiarizationSQLLeaseSerializesCoordinators(t *testing.T) {
	j, d := diarizationFixture(t)
	addDiarizationMeeting(t, j, "first", 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(503) }))
	defer server.Close()
	d.url = server.URL
	done := make(chan error, 1)
	go func() { done <- d.tick(context.Background()) }()
	<-entered
	second := &MeetingDiarization{jobs: j, url: d.url, token: d.token}
	if err := second.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDiarizationFailureRequiresExplicitRetryAndKeepsASR(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, j, d)
	addDiarizationMeeting(t, j, "first", 1)
	j.db.Exec(`UPDATE meeting_jobs SET mic_result='saved mic',system_result='saved system'`)
	queueTick(t, d)
	f.mu.Lock()
	for id := range f.jobs {
		f.jobs[id] = "failed"
		f.active--
	}
	oldID := f.submissions[0]
	f.mu.Unlock()
	queueTick(t, d)
	queueTick(t, d)
	queueTick(t, d)
	var status, mic string
	j.db.QueryRow(`SELECT status,mic_result FROM meeting_jobs WHERE job_key='first'`).Scan(&status, &mic)
	if status != "failed" || mic != "saved mic" {
		t.Fatal(status, mic)
	}
	if len(f.submissions) != 1 {
		t.Fatal("auto retried failed input")
	}
	j.db.Exec(`UPDATE meeting_jobs SET status='queued',admitted=1 WHERE job_key='first'`) // Explicit retry handler transition.
	queueTick(t, d)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submissions) != 2 || f.submissions[1] == oldID {
		t.Fatal(f.submissions)
	}
}

func TestNormalizeCPUDiarizationRejectsMalformedAndAcceptsEmpty(t *testing.T) {
	for _, input := range []string{`{}`, `{"segments":null}`, `{"segments":[{"Start":0,"End":1}]}`, `{"segments":[{"Start":-1,"End":1,"Speaker":0}]}`, `{"segments":[{"Start":2,"End":1,"Speaker":0}]}`} {
		if _, err := normalizeDiarizationResult([]byte(input)); err == nil {
			t.Fatal("accepted", input)
		}
	}
	for _, input := range []string{`{"segments":[]}`, `{"segments":[{"Start":0,"End":1,"Speaker":0}]}`, `{"segments":[{"start":0,"end":1,"speaker":"speaker_2"}]}`} {
		data, err := normalizeDiarizationResult([]byte(input))
		if err != nil || !json.Valid(data) {
			t.Fatal(input, err)
		}
	}
}

func TestCompletedASRReleasesGPUWhileCPUQueued(t *testing.T) {
	j, _ := diarizationFixture(t)
	addDiarizationMeeting(t, j, "first", 1)
	if _, err := j.db.Exec(`UPDATE meeting_jobs SET status='processing',attempt='gpu',attempts=12`); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	releases, stages := 0, 0
	fleet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/fleet/meetings/release" {
			releases++
			w.WriteHeader(204)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/diar") {
			t.Error("diarization sent to GPU")
		}
		stages++
		w.Write([]byte(`{"words":[]}`))
	}))
	defer fleet.Close()
	j.fleet = fleet.URL
	j.token = "01234567890123456789012345678901"
	os.WriteFile(filepath.Join(j.root, "first", "mic.ogg"), []byte("fixture audio"), 0600)
	j.process(context.Background(), "first", "gpu")
	var status, mic, system string
	var attempts int
	if err := j.db.QueryRow(`SELECT status,mic_result,system_result,attempts FROM meeting_jobs WHERE job_key='first'`).Scan(&status, &mic, &system, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || mic == "" || system == "" || attempts != 11 {
		t.Fatal(status, mic, system, attempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if releases != 1 || stages != 2 {
		t.Fatal(releases, stages)
	}
}
