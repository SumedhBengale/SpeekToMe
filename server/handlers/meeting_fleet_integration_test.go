package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"speektome-server/db"
	"testing"
	"time"
)

// Opt-in acceptance test: real workers, isolated local database, no production
// accounts. Its client disconnects after durable commit; a restarted processor
// completes and saves the result independently.
func TestRealFleetDurableMeeting(t *testing.T) {
	fleet := os.Getenv("SPEEKTOME_REAL_FLEET_URL")
	if fleet == "" {
		t.Skip("requires real fleet")
	}
	audio, err := os.ReadFile(os.Getenv("SPEEKTOME_REAL_AUDIO"))
	if err != nil {
		t.Fatal(err)
	}
	token := os.Getenv("SPEEKTOME_REAL_TOKEN_FILE")
	if token == "" {
		t.Fatal("token file required")
	}
	t.Setenv("SPEEKTOME_FLEET_URL", fleet)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("MEETING_PROCESSING_ENABLED", "true")
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "acceptance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.GetDB().Exec(`INSERT INTO users(id,email,name) VALUES(1,'acceptance@example.invalid','Local speaker')`); err != nil {
		t.Fatal(err)
	}
	if err = database.CreateSession("acceptance-session", 1, 0); err != nil {
		t.Fatal(err)
	}
	app := &App{DB: database}
	initCtx, stopInit := context.WithCancel(context.Background())
	stopInit()
	mux := http.NewServeMux()
	if err = app.StartMeetingJobs(initCtx, mux); err != nil {
		t.Fatal(err)
	}
	clientServer := httptest.NewServer(mux)
	send := func(method, path, offset string, body []byte) (int, []byte) {
		r, err := http.NewRequest(method, clientServer.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer acceptance-session")
		if offset != "" {
			r.Header.Set("Upload-Offset", offset)
		}
		res, err := clientServer.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	sum := sha256.Sum256(audio)
	files := map[string]MeetingFile{}
	for _, channel := range []string{"mic", "system", "mixed"} {
		files[channel] = MeetingFile{Size: int64(len(audio)), SHA: hex.EncodeToString(sum[:])}
	}
	manifest, _ := json.Marshal(MeetingUpload{ID: "real-acceptance", Title: "Real fleet acceptance", Files: files})
	if code, data := send("POST", "/v1/meetings/jobs", "", manifest); code != 200 {
		t.Fatal(code, string(data))
	}
	for _, channel := range []string{"mic", "system", "mixed"} {
		if code, data := send("PUT", "/v1/meetings/jobs/real-acceptance/audio/"+channel, "0", audio); code != 204 {
			t.Fatal(code, string(data))
		}
	}
	if code, data := send("POST", "/v1/meetings/jobs/real-acceptance/commit", "", nil); code != 202 {
		t.Fatal(code, string(data))
	}
	clientServer.Close() // No client connection exists while processing runs.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = app.StartMeetingJobs(ctx, http.NewServeMux()); err != nil {
		t.Fatal(err)
	}
	key := jobKey(1, "real-acceptance")
	deadline := time.Now().Add(90 * time.Second)
	var status, lastError, mic, system, diar string
	var meetingID int64
	for time.Now().Before(deadline) {
		err = database.GetDB().QueryRow(`SELECT status,error,meeting_id,mic_result,system_result,diar_result FROM meeting_jobs WHERE job_key=?`, key).Scan(&status, &lastError, &meetingID, &mic, &system, &diar)
		if err != nil {
			t.Fatal(err)
		}
		if status == "completed" || status == "failed" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("status=%s error=%s", status, lastError)
	}
	meeting, err := database.GetMeeting(meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if meeting.Transcript == "" || meeting.SegmentsJSON == "[]" || mic == "" || system == "" || diar == "" {
		t.Fatalf("missing persisted output: %+v", meeting)
	}
	t.Logf("real worker completed mic/system ASR and diarization; saved meeting %d after upload client disconnected", meetingID)
}

// Real CPU admission with an isolated coordinator database and two simultaneous
// committed meetings. Neither production users nor their job rows are touched.
func TestRealCPUQueueSerial(t *testing.T) {
	if os.Getenv("SPEEKTOME_REAL_FLEET_URL") == "" {
		t.Skip("requires real CPU server")
	}
	audio, err := os.ReadFile(os.Getenv("SPEEKTOME_REAL_AUDIO"))
	if err != nil {
		t.Fatal(err)
	}
	j, d := diarizationFixture(t)
	credential, err := os.ReadFile(os.Getenv("SPEEKTOME_REAL_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	d.token = string(bytes.TrimSpace(credential))
	for i, key := range []string{"real-first", "real-second"} {
		addDiarizationMeeting(t, j, key, i)
		if err := os.WriteFile(filepath.Join(j.root, key, "system.ogg"), audio, 0600); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(120 * time.Second)
	observedQueued := false
	for time.Now().Before(deadline) {
		if err := d.tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		var running, waiting, done int
		if err := j.db.QueryRow(`SELECT SUM(status='running'),SUM(status='queued'),SUM(status='completed' AND acknowledged=1) FROM meeting_diarization_queue`).Scan(&running, &waiting, &done); err != nil {
			t.Fatal(err)
		}
		if running > 1 {
			t.Fatal("more than one active diarization")
		}
		if running == 1 && waiting == 1 {
			observedQueued = true
		}
		if done == 2 {
			if !observedQueued {
				t.Fatal("did not observe second item queued")
			}
			rows, err := j.db.Query(`SELECT q.position,q.job_key,q.remote_id,length(j.diar_result) FROM meeting_diarization_queue q JOIN meeting_jobs j ON j.job_key=q.job_key ORDER BY q.position`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var position, length int
				var key, id string
				if err := rows.Scan(&position, &key, &id, &length); err != nil {
					t.Fatal(err)
				}
				if length < 20 {
					t.Fatal("missing result")
				}
				t.Logf("position=%d job=%s remote=%s persisted_result_bytes=%d", position, key, id, length)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("real diarization queue did not drain within two minutes")
}
