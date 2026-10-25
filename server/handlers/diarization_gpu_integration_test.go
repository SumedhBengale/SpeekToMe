package handlers

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// Real serverless GPU endpoint (deployment/modal-diarization) with an isolated
// coordinator database: three committed meetings run at once through the
// production client, sealed to the deployed diarizer key, behind both locks.
//
//	SPEEKTOME_REAL_GPU_ENV=../.env SPEEKTOME_REAL_AUDIO=meeting.opus go test ./handlers -run RealGPU -v
func TestRealGPUDiarizationEncryptedConcurrent(t *testing.T) {
	envFile := os.Getenv("SPEEKTOME_REAL_GPU_ENV")
	if envFile == "" {
		t.Skip("requires the deployed GPU diarization endpoint")
	}
	values, err := godotenv.Read(envFile)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		if strings.HasPrefix(name, "SPEEKTOME_DIARIZATION_") {
			t.Setenv(name, value)
		}
	}
	audio, err := os.ReadFile(os.Getenv("SPEEKTOME_REAL_AUDIO"))
	if err != nil {
		t.Fatal(err)
	}
	j, _ := diarizationFixture(t)
	d, err := newMeetingDiarization(j)
	if err != nil || d.recipient == nil {
		t.Fatal("GPU diarization not configured", err)
	}

	// Lock 1: Modal's edge refuses requests without the proxy token. Lock 2: the
	// endpoint refuses the proxy token without the coordinator's bearer key.
	for name, headers := range map[string]map[string]string{
		"no credentials":    {},
		"bearer only":       {"Authorization": "Bearer " + d.token},
		"proxy only":        {"Modal-Key": d.proxyKey, "Modal-Secret": d.proxySecret},
		"proxy, bad bearer": {"Modal-Key": d.proxyKey, "Modal-Secret": d.proxySecret, "Authorization": "Bearer " + strings.Repeat("x", 64)},
	} {
		req, _ := http.NewRequest(http.MethodGet, d.url+"/v1/diarizations/"+strings.Repeat("0", 32), nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 && resp.StatusCode != 403 {
			t.Fatalf("%s: HTTP %d, want rejection", name, resp.StatusCode)
		}
		t.Logf("%s: rejected with HTTP %d", name, resp.StatusCode)
	}

	keys := []string{"gpu-first", "gpu-second", "gpu-third"}
	for i, key := range keys {
		addDiarizationMeeting(t, j, key, i+1)
		if err := j.spool.Delete(context.Background(), key); err != nil {
			t.Fatal(err)
		}
		spoolAudio(t, j.spool, int64(i+1), key, map[string][]byte{"system": audio})
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			speakers, err := d.run(ctx, key)
			if err != nil || len(speakers) == 0 {
				t.Error(key, "diarization failed:", err)
				return
			}
			t.Logf("%s: %d speaker segments", key, len(speakers))
		}()
	}
	wg.Wait()
	t.Logf("%d meetings diarized concurrently in %.1fs", len(keys), time.Since(started).Seconds())
}

// The front endpoint rejects stale or mismatched envelopes before spawning a GPU job.
func TestRealGPUEndpointRejectsBadEnvelopes(t *testing.T) {
	envFile := os.Getenv("SPEEKTOME_REAL_GPU_ENV")
	if envFile == "" {
		t.Skip("requires the deployed GPU diarization endpoint")
	}
	values, err := godotenv.Read(envFile)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		if strings.HasPrefix(name, "SPEEKTOME_DIARIZATION_") {
			t.Setenv(name, value)
		}
	}
	j, _ := diarizationFixture(t)
	d, err := newMeetingDiarization(j)
	if err != nil || d.recipient == nil {
		t.Fatal("GPU diarization not configured", err)
	}
	job := strings.Repeat("ab", 16)
	for name, c := range map[string]struct {
		sealedFor, header string
		issued            time.Time
	}{
		"issued an hour ago":    {job, job, time.Now().Add(-time.Hour)},
		"job header mismatched": {job, strings.Repeat("cd", 16), time.Now()},
	} {
		sealed, _, err := sealDiarizationRequest(strings.NewReader("not audio"), 9, d.recipient, c.sealedFor, c.issued, nil)
		if err != nil {
			t.Fatal(err)
		}
		data, code, _, err := d.request(context.Background(), http.MethodPost, "/v1/diarizations", sealed, c.header)
		if err != nil || code != 400 {
			t.Fatalf("%s: HTTP %d %s %v", name, code, data, err)
		}
		t.Logf("%s: rejected with HTTP %d %s", name, code, data)
	}
}
