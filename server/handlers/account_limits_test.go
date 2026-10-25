package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"speektome-server/accountlimits"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/pipeline"
	"speektome-server/ratelimit"
)

func limitApp(t *testing.T) *App {
	t.Helper()
	d, e := db.NewDatabase(filepath.Join(t.TempDir(), "limits.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	for _, id := range []int{1, 2} {
		if _, e = d.GetDB().Exec("INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(?,?,?,'pro',1)", id, fmt.Sprintf("user%d@example.test", id), "Test"); e != nil {
			t.Fatal(e)
		}
		if e = d.CreateSession(fmt.Sprintf("user%d", id), int64(id), 0); e != nil {
			t.Fatal(e)
		}
	}
	if e = d.CreateSession("second-session", 1, 0); e != nil {
		t.Fatal(e)
	}
	return &App{DB: d}
}
func transformRequest(a *App, token, path string, stream bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(fmt.Sprintf(`{"selected_text":"hello","command":"polish","stream":%t}`, stream)))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	a.HandleTransform(w, r)
	return w
}
func mockCommand(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	cfg, raw, key := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.CerebrasAPIKey
	t.Cleanup(func() {
		config.ConfigManager.Set(cfg, raw)
		config.CerebrasAPIKey = key
		pipeline.GlobalCircuitBreaker.Reset()
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	config.CerebrasAPIKey = "test-only"
	next := cfg
	next.Models.CommandModels = []config.ModelSpec{{Provider: config.ProviderCerebras, Model: "test", Enabled: true, EndpointOverride: server.URL, TimeoutMs: 5000}}
	config.ConfigManager.Set(next, raw)
	pipeline.GlobalCircuitBreaker.Reset()
}
func TestTransformAccountConcurrencyAliasesAndUsage(t *testing.T) {
	a := limitApp(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	waitForProvider := func() {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("account did not reach the mock provider before the deadline")
		}
	}

	var calls atomic.Int32
	mockCommand(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["max_tokens"] == nil {
			t.Error("missing output reservation cap")
		}
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"speektome_result\":{\"text\":\"Hello\"}}"}}],"usage":{"prompt_tokens":20,"completion_tokens":2}}`)
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- transformRequest(a, "user1", "/transform", false) }()
	waitForProvider()
	blocked := transformRequest(a, "second-session", "/v1/transform", true)
	if blocked.Code != 429 || !strings.Contains(blocked.Body.String(), "transform_busy") || blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("second session bypassed lock: %d %s", blocked.Code, blocked.Body.String())
	}
	other := make(chan *httptest.ResponseRecorder, 1)
	go func() { other <- transformRequest(a, "user2", "/v1/transform", false) }()
	waitForProvider()
	unblock()
	if (<-done).Code != 200 || (<-other).Code != 200 {
		t.Fatal("independent accounts blocked")
	}
	var input, output int64
	if e := a.DB.GetDB().QueryRow("SELECT input_tokens,output_tokens FROM command_usage WHERE user_id=1").Scan(&input, &output); e != nil {
		t.Fatal(e)
	}
	if input != 20 || output != 2 {
		t.Fatalf("usage not reconciled: %d %d", input, output)
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls %d", calls.Load())
	}
}
func TestTransformExhaustionRejectsBeforeStreamOrProvider(t *testing.T) {
	a := limitApp(t)
	var calls atomic.Int32
	mockCommand(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("exhausted account reached provider")
	})
	var anchor string
	a.DB.GetDB().QueryRow("SELECT COALESCE(plan_activated_at,created_at) FROM users WHERE id=1").Scan(&anchor)
	period, _ := accountlimits.Period(anchor, time.Now())
	for _, tc := range []struct {
		input, output int64
		code          string
	}{{5000000, 0, "command_input_quota_exhausted"}, {0, 1000000, "command_output_quota_exhausted"}} {
		_, e := a.DB.GetDB().Exec("INSERT INTO command_usage(user_id,period,input_tokens,output_tokens) VALUES(1,?,?,?) ON CONFLICT(user_id,period) DO UPDATE SET input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens", period, tc.input, tc.output)
		if e != nil {
			t.Fatal(e)
		}
		for _, stream := range []bool{false, true} {
			w := transformRequest(a, "user1", "/v1/transform", stream)
			if w.Code != 429 || !strings.Contains(w.Body.String(), tc.code) || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("quota rejection %d %s", w.Code, w.Body.String())
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("provider was billed")
	}
}
func TestMeetingRecordingPermitIsAccountWideAndOwnerBound(t *testing.T) {
	a := limitApp(t)
	start := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/meetings/session", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		a.HandleStartMeetingSession(w, r)
		return w
	}
	first := start("user1")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var permit struct {
		ID string `json:"id"`
	}
	json.Unmarshal(first.Body.Bytes(), &permit)
	if w := start("second-session"); w.Code != 429 {
		t.Fatal("another device admitted", w.Code)
	}
	if w := start("user2"); w.Code != 200 {
		t.Fatal("unrelated account blocked", w.Code)
	}
	a.limits().Release(2, "meeting recording", permit.ID)
	if w := start("user1"); w.Code != 429 {
		t.Fatal("another account released permit")
	}
	a.limits().Release(1, "meeting recording", permit.ID)
	if w := start("user1"); w.Code != 200 {
		t.Fatal("permit not released", w.Code)
	}
}
func TestMeetingQueueAdmissionAndResumes(t *testing.T) {
	a := limitApp(t)
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "absent"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux := http.NewServeMux()
	if e := a.StartMeetingJobs(ctx, mux); e != nil {
		t.Fatal(e)
	}
	send := func(id, token string) *httptest.ResponseRecorder {
		a.DB.GetDB().Exec("DELETE FROM account_rate_windows WHERE operation='meeting_create'")
		body := fmt.Sprintf(`{"id":%q,"files":{"mic":{"size":1,"sha256":"%s"},"system":{"size":1,"sha256":"%s"},"mixed":{"size":1,"sha256":"%s"}}}`, id, strings.Repeat("0", 64), strings.Repeat("0", 64), strings.Repeat("0", 64))
		r := httptest.NewRequest("POST", "/v1/meetings/jobs", bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, id := range []string{"one", "two", "three"} {
		if w := send(id, "user1"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := send("four", "user1"); w.Code != 429 || !strings.Contains(w.Body.String(), "meeting_queue_full") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("one", "second-session"); w.Code != 200 {
		t.Fatal("idempotent resume was rejected", w.Code)
	}
	if w := send("four", "user2"); w.Code != 200 {
		t.Fatal("another account queue was blocked", w.Code)
	}
	_, e := a.DB.GetDB().Exec("UPDATE meeting_jobs SET admitted=1,status='processing' WHERE user_id=1 AND client_id='one'")
	if e != nil {
		t.Fatal(e)
	}
	if w := send("four", "user1"); w.Code != 200 {
		t.Fatal("processing job counted as queued", w.Code)
	}
	if _, e = a.DB.GetDB().Exec("UPDATE meeting_jobs SET admitted=1 WHERE user_id=1 AND client_id='two'"); e == nil {
		t.Fatal("second processing job admitted")
	}
}

func TestTransformRateLimitAndProEntitlement(t *testing.T) {
	a := limitApp(t)
	mockCommand(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"speektome_result\":{\"text\":\"ok\"}}"}}],"usage":{"prompt_tokens":20,"completion_tokens":2}}`)
	})
	for i := 0; i < 10; i++ {
		token := "user1"
		if i%2 == 0 {
			token = "second-session"
		}
		if w := transformRequest(a, token, "/transform", false); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := transformRequest(a, "user1", "/v1/transform", false); w.Code != 429 || !strings.Contains(w.Body.String(), "transform_rate_limit") {
		t.Fatal(w.Code, w.Body.String())
	}
	a.DB.GetDB().Exec("UPDATE users SET plan_active=0 WHERE id=2")
	if w := transformRequest(a, "user2", "/v1/transform", false); w.Code != 403 || !strings.Contains(w.Body.String(), "command_pro_required") {
		t.Fatal("free account reached command provider", w.Code, w.Body.String())
	}
}
func TestAbandonedUploadsExpireWithoutDeletingCommittedJobs(t *testing.T) {
	a := limitApp(t)
	root := t.TempDir()
	t.Setenv("MEETING_AUDIO_DIR", root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := a.StartMeetingJobs(ctx, http.NewServeMux()); e != nil {
		t.Fatal(e)
	}
	j := &MeetingJobs{app: a, db: a.DB.GetDB(), spool: testMeetingSpool(t, a), root: root}
	for _, status := range []string{"uploading", "queued"} {
		_, e := j.db.Exec("INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,created_at) VALUES(?,1,?,'test','{}',?,?)", status, status, status, time.Now().Add(-25*time.Hour).Unix())
		if e != nil {
			t.Fatal(e)
		}
	}
	j.expireUploads(context.Background())
	var count int
	j.db.QueryRow("SELECT COUNT(*) FROM meeting_jobs").Scan(&count)
	if count != 1 {
		t.Fatalf("incorrect expiration: %d", count)
	}
	var status string
	j.db.QueryRow("SELECT status FROM meeting_jobs").Scan(&status)
	if status != "queued" {
		t.Fatal("deleted committed audio")
	}
}

func TestAccountProcessesOneMeetingAtATime(t *testing.T) {
	a := limitApp(t)
	root := t.TempDir()
	t.Setenv("MEETING_AUDIO_DIR", root)
	initCtx, stopInit := context.WithCancel(context.Background())
	stopInit()
	if e := a.StartMeetingJobs(initCtx, http.NewServeMux()); e != nil {
		t.Fatal(e)
	}
	j := &MeetingJobs{app: a, db: a.DB.GetDB(), spool: testMeetingSpool(t, a), root: root, token: strings.Repeat("x", 32), client: http.DefaultClient}
	// One meeting is mid-pipeline (waiting on diarization, say) under a live lease.
	if _, e := j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,created_at,admitted,attempt,lease_until) VALUES('in-flight',1,'in-flight','test','{}','analyzing',0,1,'a',?)`, time.Now().Add(time.Minute).Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e := j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,created_at) VALUES('next-meeting',1,'next-meeting','test','{}','queued',1)`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j.dispatch(ctx)
	var status string
	var admitted int
	j.db.QueryRow("SELECT status,admitted FROM meeting_jobs WHERE job_key='next-meeting'").Scan(&status, &admitted)
	if admitted != 0 || status != "queued" {
		t.Fatal("second meeting bypassed account admission", status, admitted)
	}
	j.db.QueryRow("SELECT status,admitted FROM meeting_jobs WHERE job_key='in-flight'").Scan(&status, &admitted)
	if admitted != 1 || status != "analyzing" {
		t.Fatal("in-flight meeting was disturbed", status, admitted)
	}
}
func TestRecordingPermitCanRecoverAnUnacknowledgedStart(t *testing.T) {
	a := limitApp(t)
	id := strings.Repeat("a", 32)
	request := httptest.NewRequest("POST", "/v1/meetings/session", strings.NewReader(fmt.Sprintf(`{"id":%q}`, id)))
	request.Header.Set("Authorization", "Bearer user1")
	w := httptest.NewRecorder()
	a.HandleStartMeetingSession(w, request)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatal(w.Code, w.Body.String())
	}
	// The response could be lost: another session of this account can still release
	// the known owner from its persisted local marker before attempting capture.
	request = httptest.NewRequest("DELETE", "/v1/meetings/session/"+id, nil)
	request.SetPathValue("id", id)
	request.Header.Set("Authorization", "Bearer second-session")
	w = httptest.NewRecorder()
	a.HandleEndMeetingSession(w, request)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	if _, e := a.limits().Acquire(context.Background(), 1, "meeting recording", time.Minute); e != nil {
		t.Fatal("lost HTTP response stranded account permit", e)
	}
}

func TestDelayedBillingWebhookCannotOpenAnotherTokenAllowance(t *testing.T) {
	a := limitApp(t)
	var calls atomic.Int32
	mockCommand(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("billing fallback opened another quota")
	})
	start := time.Now().Add(-32 * 24 * time.Hour).Truncate(time.Second).UTC()
	end := time.Now().Add(-24 * time.Hour).Unix()
	if _, e := a.DB.GetDB().Exec("UPDATE users SET razorpay_current_start=?,razorpay_current_end=? WHERE id=1", start.Unix(), end); e != nil {
		t.Fatal(e)
	}
	if _, e := a.DB.GetDB().Exec("INSERT INTO command_usage(user_id,period,input_tokens) VALUES(1,?,5000000)", start.Format(time.RFC3339)); e != nil {
		t.Fatal(e)
	}
	w := transformRequest(a, "user1", "/v1/transform", false)
	if w.Code != 429 || !strings.Contains(w.Body.String(), "renewal is confirmed") {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("unexpected paid inference")
	}
}

func TestCommandUsageRenewalAndLateSettlement(t *testing.T) {
	a := limitApp(t)
	if e := a.DB.SetPendingSubscription(1, "sub_test"); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Second)
	oldStart, oldEnd := now.AddDate(0, -1, 0), now.Add(time.Hour)
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET razorpay_current_start=?,razorpay_current_end=?,monthly_usage_seconds=42 WHERE id=1`, oldStart.Unix(), oldEnd.Unix()); err != nil {
		t.Fatal(err)
	}
	oldPeriod := oldStart.Format(time.RFC3339)
	reservation, err := a.limits().Reserve(context.Background(), 1, oldPeriod, oldEnd, 100, 200, 5000000, 1000000)
	if err != nil {
		t.Fatal(err)
	}
	check := func(input, output int64, period string) {
		t.Helper()
		usage, err := a.commandUsage(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if usage["input_used"] != input || usage["output_used"] != output || usage["period_start"] != period {
			t.Fatalf("unexpected usage: %#v", usage)
		}
	}
	check(100, 200, oldPeriod)
	// Status events can announce a later period, but must not refill allowances.
	if err := a.DB.UpdateSubscriptionState(1, "sub_test", "pending", now.Unix(), now.AddDate(0, 1, 0).Unix(), nil); err != nil {
		t.Fatal(err)
	}
	check(100, 200, oldPeriod)
	newStart, newEnd := now, now.AddDate(0, 1, 0)
	applied, err := a.DB.ApplySuccessfulSubscriptionPayment(1, "sub_test", "pay_new", "inv_new", "payment:pay_new", "evt_new", "subscription.charged", 200, "USD", "captured", newStart.Unix(), newEnd.Unix(), "{}")
	if err != nil || !applied {
		t.Fatalf("renewal: %v %v", applied, err)
	}
	newPeriod := newStart.Format(time.RFC3339)
	check(0, 0, newPeriod)
	var transcription float64
	if err := a.DB.GetDB().QueryRow(`SELECT monthly_usage_seconds FROM users WHERE id=1`).Scan(&transcription); err != nil || transcription != 0 {
		t.Fatalf("transcription reset: %v %v", transcription, err)
	}
	fresh, err := a.limits().Reserve(context.Background(), 1, newPeriod, newEnd, 40, 50, 5000000, 1000000)
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.Settle(20, 30); err != nil {
		t.Fatal(err)
	}
	if err = reservation.Settle(10, 15); err != nil {
		t.Fatal(err)
	}
	check(20, 30, newPeriod)
	applied, err = a.DB.ApplySuccessfulSubscriptionPayment(1, "sub_test", "pay_new", "inv_new", "payment:pay_new", "evt_duplicate", "invoice.paid", 200, "USD", "paid", newStart.Unix(), newEnd.Unix(), "{}")
	if err != nil || applied {
		t.Fatalf("duplicate: %v %v", applied, err)
	}
	check(20, 30, newPeriod)
	// Even another notification with a distinct payment ID for this same period
	// cannot clear already consumed Command Mode usage.
	_, err = a.DB.ApplySuccessfulSubscriptionPayment(1, "sub_test", "pay_same_period", "inv_other", "payment:pay_same_period", "evt_other", "subscription.charged", 200, "USD", "captured", newStart.Unix(), newEnd.Unix(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	check(20, 30, newPeriod)
	other, err := a.commandUsage(context.Background(), 2)
	if err != nil || other["input_used"] != int64(0) {
		t.Fatalf("other account: %#v %v", other, err)
	}
}

func TestAuthMeCommandAllowanceUsesConfiguredLimits(t *testing.T) {
	a := limitApp(t)
	if err := a.DB.UpdateSaasConfig("quota_command_input_monthly_tokens", "5000"); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.UpdateSaasConfig("quota_command_output_monthly_tokens", "2000"); err != nil {
		t.Fatal(err)
	}
	cfg, raw := config.ConfigManager.Get(), config.ConfigManager.GetRawAll()
	t.Cleanup(func() { config.ConfigManager.Set(cfg, raw) })
	// Admin updates and the background loop sync configuration; /auth/me reads memory.
	config.SyncConfigsFromDB(a.DB, ratelimit.GroqRateLimiter.SetLimit)
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	end := start.AddDate(0, 1, 0)
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET razorpay_current_start=?,razorpay_current_end=? WHERE id=1`, start.Unix(), end.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.GetDB().Exec(`INSERT INTO command_usage(user_id,period,input_tokens,output_tokens) VALUES(1,?,1000,640)`, start.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/auth/me", nil)
	r.Header.Set("Authorization", "Bearer second-session")
	w := httptest.NewRecorder()
	a.HandleAuthMe(w, r)
	var response struct {
		Usage struct {
			InputLimit  int64   `json:"input_limit"`
			OutputLimit int64   `json:"output_limit"`
			Remaining   float64 `json:"remaining_percent"`
			Enabled     bool    `json:"enabled"`
		} `json:"command_usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || response.Usage.InputLimit != 5000 || response.Usage.OutputLimit != 2000 || response.Usage.Remaining < 67.99 || response.Usage.Remaining > 68.01 || !response.Usage.Enabled {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
}

func TestCommandUsageExhaustionPendingRenewalAndUnavailable(t *testing.T) {
	a := limitApp(t)
	start := time.Now().UTC().AddDate(0, -2, 0).Truncate(time.Second)
	end := start.AddDate(0, 1, 0)
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET razorpay_current_start=?,razorpay_current_end=? WHERE id=1`, start.Unix(), end.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.GetDB().Exec(`INSERT INTO command_usage(user_id,period,input_tokens,output_tokens) VALUES(1,?,5000000,0)`, start.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	usage, err := a.commandUsage(context.Background(), 1)
	if err != nil || usage["remaining_percent"] != float64(0) || usage["input_remaining"] != int64(0) || usage["renewal_pending"] != true {
		t.Fatalf("exhaustion: %#v %v", usage, err)
	}
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET plan_tier='free' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	usage, err = a.commandUsage(context.Background(), 1)
	if err != nil || usage["enabled"] != false {
		t.Fatalf("free entitlement: %#v %v", usage, err)
	}
	if _, err := a.DB.GetDB().Exec(`DROP TABLE command_usage`); err != nil {
		t.Fatal(err)
	}
	usage, err = a.commandUsage(context.Background(), 1)
	if err == nil || usage != nil {
		t.Fatalf("unavailable counters presented as unused allowance: %#v %v", usage, err)
	}
}
