package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	j := &MeetingJobs{app: app, db: database.GetDB(), spool: testMeetingSpool(t, app), root: root, client: http.DefaultClient}
	d, err := newMeetingDiarization(j)
	if err != nil {
		t.Fatal(err)
	}
	d.token, d.poll, d.maxWait = "01234567890123456789012345678901", 5*time.Millisecond, 10*time.Millisecond
	j.diarization = d
	return j, d
}
func addDiarizationMeeting(t *testing.T, j *MeetingJobs, key string, created int) {
	t.Helper()
	if _, err := j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,created_at,admitted) VALUES(?,?,?,'test','{}','processing',?,1)`, key, created, key, created); err != nil {
		t.Fatal(err)
	}
	spoolAudio(t, j.spool, int64(created), key, map[string][]byte{"system": []byte("fixture audio")})
}

// fakeCPU is the dedicated diarization gateway's job contract. Jobs complete on
// their second status poll.
type fakeCPU struct {
	mu                     sync.Mutex
	jobs                   map[string]int
	submissions, deleted   []string
	active, maxActive      int
	busyOnce, loseResponse bool
	rejectAll              bool
	audio                  string        // the system audio every submission must carry
	hold                   chan struct{} // when set, uploads wait for it to close
	t                      *testing.T
}

func (f *fakeCPU) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer 01234567890123456789012345678901" {
		f.t.Error("missing CPU authentication")
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/diarizations/")
	switch {
	case r.Method == "POST":
		if f.rejectAll {
			w.WriteHeader(422)
			return
		}
		if f.busyOnce {
			f.busyOnce = false
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		if f.hold != nil {
			<-f.hold
		}
		id = r.Header.Get("X-SpeekToMe-Job-ID")
		if len(id) != 32 {
			f.t.Error("missing job identity")
		}
		if body, _ := io.ReadAll(r.Body); string(body) != f.audio {
			f.t.Error("wrong audio channel")
		}
		f.jobs[id] = 0
		f.submissions = append(f.submissions, id)
		f.active++
		f.maxActive = max(f.maxActive, f.active)
		if f.loseResponse {
			f.loseResponse = false
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"job_id":%q}`, id)
	case r.Method == "DELETE":
		if _, ok := f.jobs[id]; ok {
			delete(f.jobs, id)
			f.active--
		}
		f.deleted = append(f.deleted, id)
		w.Write([]byte(`{}`))
	case strings.HasSuffix(id, "/result"):
		w.Write([]byte(`{"segments":[{"Start":0.25,"End":2.5,"Speaker":3}]}`))
	default:
		polls, ok := f.jobs[id]
		if !ok {
			w.WriteHeader(404)
			return
		}
		f.jobs[id] = polls + 1
		status := "processing"
		if polls > 0 {
			status = "completed"
		}
		fmt.Fprintf(w, `{"status":%q,"error":""}`, status)
	}
}
func fakeCPUServer(t *testing.T, d *MeetingDiarization) *fakeCPU {
	t.Helper()
	f := &fakeCPU{jobs: map[string]int{}, audio: "fixture audio", t: t}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	d.url = server.URL
	return f
}

func TestDiarizationReturnsSpeakersAndDeletesTheRemoteJob(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, d)
	addDiarizationMeeting(t, j, "first", 1)
	speakers, err := d.run(context.Background(), "first")
	if err != nil || len(speakers) != 1 || speakers[0] != (jobSpeaker{0.25, 2.5, 3}) {
		t.Fatal(speakers, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submissions) != 1 || len(f.deleted) != 1 || f.deleted[0] != f.submissions[0] {
		t.Fatal("remote job not cleaned up", f.submissions, f.deleted)
	}
}

func TestDiarizationRetriesBusyAndLostResponses(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, d)
	f.busyOnce, f.loseResponse = true, true
	addDiarizationMeeting(t, j, "first", 1)
	speakers, err := d.run(context.Background(), "first")
	if err != nil || len(speakers) != 1 {
		t.Fatal(speakers, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// The lost response's job is abandoned under its own identity; the retry
	// submits a fresh one.
	if len(f.submissions) != 2 || f.submissions[0] == f.submissions[1] {
		t.Fatal(f.submissions)
	}
}

func TestDiarizationRejectionIsPermanent(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, d)
	f.rejectAll = true
	addDiarizationMeeting(t, j, "first", 1)
	_, err := d.run(context.Background(), "first")
	var failure *meetingError
	if err == nil || !errorsAs(err, &failure) || !failure.permanent {
		t.Fatal("a rejected recording must not be retried", err)
	}
}

func TestDiarizationLimitBoundsConcurrentJobs(t *testing.T) {
	j, d := diarizationFixture(t)
	f := fakeCPUServer(t, d)
	d.slots = make(chan struct{}, 1) // the CPU gateway runs one job at a time
	var wg sync.WaitGroup
	for i, key := range []string{"first", "second", "third"} {
		addDiarizationMeeting(t, j, key, i+1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.run(context.Background(), key); err != nil {
				t.Error(key, err)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submissions) != 3 || f.maxActive != 1 {
		t.Fatal("overlapping CPU jobs", f.submissions, f.maxActive)
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

func errorsAs(err error, target **meetingError) bool { return errors.As(err, target) }
