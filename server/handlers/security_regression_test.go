package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"speektome-server/auth"
	"speektome-server/db"
	"speektome-server/privatevault"
	"strings"
	"testing"
	"time"
)

func auditDB(t *testing.T) *db.Database {
	d, e := db.NewDatabase(filepath.Join(t.TempDir(), "audit.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	_, e = d.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active,razorpay_subscription_id) VALUES(1,'synthetic@example.invalid','Synthetic','pro',1,'sub_current')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.CreateSession("audit-synthetic-token", 1, 0); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestSecurityArbitraryPagesDevRedirectRejected(t *testing.T) {
	if auth.IsValidRedirect("https://unrelated-audit-project.pages.dev/callback") {
		t.Fatal("security invariant violated")
	}
	t.Log("PASS: unrelated Pages tenant rejected")
}
func TestSecurityCancellationFailurePreservesSubscription(t *testing.T) {
	d := auditDB(t)
	prior := razorpayAPIBaseURL
	defer func() { razorpayAPIBaseURL = prior }()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"description":"synthetic outage"}}`, 500)
	}))
	defer provider.Close()
	razorpayAPIBaseURL = provider.URL
	t.Setenv("RAZORPAY_KEY_ID", "synthetic")
	t.Setenv("RAZORPAY_KEY_SECRET", "synthetic")
	r := httptest.NewRequest("POST", "/billing/cancel-subscription", nil)
	r.Header.Set("Authorization", "Bearer audit-synthetic-token")
	w := httptest.NewRecorder()
	(&App{DB: d}).HandleCancelSubscription(w, r)
	var active int
	d.GetDB().QueryRow("SELECT plan_active FROM users WHERE id=1").Scan(&active)
	if w.Code != 502 || active != 1 {
		t.Fatal("security invariant violated", w.Code, active)
	}
	t.Log("Regression protection verified")
}
func TestSecurityOldWebhookCannotCancelCurrent(t *testing.T) {
	d := auditDB(t)
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "synthetic-secret")
	payload := `{"event":"subscription.cancelled","payload":{"subscription":{"entity":{"id":"sub_obsolete","status":"cancelled","notes":{"user_id":"1"}}}}}`
	w := signedWebhookRequest(t, d, "audit-stale-event", "synthetic-secret", payload)
	var active int
	var sub string
	d.GetDB().QueryRow("SELECT plan_active,razorpay_subscription_id FROM users WHERE id=1").Scan(&active, &sub)
	if w.Code != 200 || active != 1 || sub != "sub_current" {
		t.Fatal("security invariant violated", w.Code, active, sub)
	}
	t.Log("Regression protection verified")
}
func TestSecurityLatePaymentPreservesPeriodAndUsage(t *testing.T) {
	d := auditDB(t)
	now := time.Now().Unix()
	for i, shift := range []int64{0, -30 * 86400} {
		_, e := d.ApplySuccessfulSubscriptionPayment(1, "sub_current", fmt.Sprint("payment", i), "", fmt.Sprint("payment:", i), "", "test", 100, "USD", "captured", now+shift, now+shift+30*86400, "")
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			d.GetDB().Exec("UPDATE users SET monthly_usage_seconds=123 WHERE id=1")
		}
	}
	var end int64
	var used float64
	d.GetDB().QueryRow("SELECT razorpay_current_end,monthly_usage_seconds FROM users WHERE id=1").Scan(&end, &used)
	if end != now+30*86400 || used != 123 {
		t.Fatal("security invariant violated", end, used)
	}
	t.Log("Regression protection verified")
}
func TestSecurityAccountDeletionRemovesDurableJobs(t *testing.T) {
	d := auditDB(t)
	vault, e := privatevault.New(context.Background(), d.GetDB(), bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	d.Privacy = vault
	_, e = d.GetDB().Exec(`CREATE TABLE meeting_jobs(job_key TEXT,user_id INTEGER,title TEXT,mic_result TEXT,status TEXT,admitted INTEGER);INSERT INTO meeting_jobs VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,'synthetic title','synthetic private transcript','completed',0)`)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.DeleteUserAccount(1); e != nil {
		t.Fatal(e)
	}
	var n int
	d.GetDB().QueryRow("SELECT COUNT(*) FROM meeting_jobs WHERE user_id=1").Scan(&n)
	if n != 0 {
		t.Fatal("security invariant violated", n)
	}
	t.Log("Regression protection verified")
}
func TestSecurityCrossAccountHistoryAndMeetingsDenied(t *testing.T) {
	d := auditDB(t)
	vault, e := privatevault.New(context.Background(), d.GetDB(), bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	d.Privacy = vault
	_, e = d.GetDB().Exec(`INSERT INTO users(id,email,name) VALUES(2,'other@example.invalid','Other');INSERT INTO meetings(id,user_id,title,transcript,duration,audio_path,summary,action_items,segments_json) VALUES(12,2,'Other meeting','Synthetic private body',1,'synthetic.ogg','','','[]')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = vault.SaveFinal(context.Background(), 2, "21cba7da-2101-47eb-8ba1-24d0024cff64", "dictation", "Synthetic other-user history"); e != nil {
		t.Fatal(e)
	}
	app := &App{DB: d}
	for _, kind := range []string{"detail", "audio", "speakers", "delete", "history"} {
		r := httptest.NewRequest("GET", "/resource", strings.NewReader(`{"speaker_names":{}}`))
		r.Header.Set("Authorization", "Bearer audit-synthetic-token")
		r.SetPathValue("id", "12")
		w := httptest.NewRecorder()
		switch kind {
		case "detail":
			app.HandleGetMeetingDetail(w, r)
		case "audio":
			app.HandleGetMeetingAudio(w, r)
		case "speakers":
			app.HandleSaveMeetingSpeakers(w, r)
		case "delete":
			app.HandleDeleteMeeting(w, r)
		case "history":
			app.HandlePrivateHistory(w, r)
		}
		if kind == "history" {
			if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
				t.Fatal("cross-account history leak", w.Code)
			}
		} else if w.Code != 403 {
			t.Fatal("cross-account access not forbidden", kind, w.Code)
		}
	}
	t.Log("PASS: other-account meeting detail/audio/speaker update/delete denied; encrypted history scoped")
}

func TestSecurityExistingSubscriptionCannotBeOverwritten(t *testing.T) {
	d := auditDB(t)
	prior := razorpayAPIBaseURL
	defer func() { razorpayAPIBaseURL = prior }()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprint(w, `{"id":"plan_test","status":"active","item":{"amount":100,"currency":"USD"}}`)
		} else {
			fmt.Fprint(w, `{"id":"sub_new"}`)
		}
	}))
	defer provider.Close()
	razorpayAPIBaseURL = provider.URL
	t.Setenv("RAZORPAY_KEY_ID", "synthetic")
	t.Setenv("RAZORPAY_KEY_SECRET", "synthetic")
	t.Setenv("RAZORPAY_PLAN_ID", "plan_test")
	r := httptest.NewRequest("POST", "/billing/create-subscription", nil)
	r.Header.Set("Authorization", "Bearer audit-synthetic-token")
	w := httptest.NewRecorder()
	(&App{DB: d}).HandleCreateSubscription(w, r)
	var sub string
	d.GetDB().QueryRow("SELECT razorpay_subscription_id FROM users WHERE id=1").Scan(&sub)
	if w.Code != 409 || sub != "sub_current" {
		t.Fatal("security invariant violated", w.Code, sub)
	}
	t.Log("Regression protection verified")
}
func TestSecurityInvalidWebhookDoesNotPersistPayload(t *testing.T) {
	d := auditDB(t)
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "synthetic-secret")
	r := httptest.NewRequest("POST", "/webhooks/razorpay", strings.NewReader(`{"untrusted":"synthetic"}`))
	r.Header.Set("X-Razorpay-Signature", "invalid")
	w := httptest.NewRecorder()
	(&App{DB: d}).HandleRazorpayWebhook(w, r)
	var n int
	d.GetDB().QueryRow("SELECT COUNT(*) FROM webhook_logs").Scan(&n)
	if w.Code != 400 || n != 0 {
		t.Fatal("security invariant violated", w.Code, n)
	}
	t.Log("Regression protection verified")
}

func TestSecurityOAuthStateCookieAndCodeReplay(t *testing.T) {
	a := &App{DB: auditDB(t)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/login", nil)
	if e := a.issueOAuthState(w, r, "opaque-state"); e != nil {
		t.Fatal(e)
	}
	if a.consumeOAuthState(httptest.NewRecorder(), httptest.NewRequest("GET", "/auth/callback", nil), "opaque-state") {
		t.Fatal("accepted callback without browser cookie")
	}
	callback := httptest.NewRequest("GET", "/auth/callback", nil)
	callback.AddCookie(w.Result().Cookies()[0])
	if !a.consumeOAuthState(httptest.NewRecorder(), callback, "opaque-state") {
		t.Fatal("valid browser state rejected")
	}
	if a.consumeOAuthState(httptest.NewRecorder(), callback, "opaque-state") {
		t.Fatal("state replay accepted")
	}
	code := auth.GenerateSessionToken()
	if e := a.issueAuthCode(code, 1, 0); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/auth/exchange", strings.NewReader(fmt.Sprintf(`{"code":%q}`, code)))
		a.HandleAuthExchange(rec, req)
		want := 200
		if i == 1 {
			want = 401
		}
		if rec.Code != want {
			t.Fatal("code replay/consumption", rec.Code, want)
		}
	}
}
func TestSecuritySessionHashesAndMeetingEncryption(t *testing.T) {
	d := auditDB(t)
	var stored string
	if e := d.GetDB().QueryRow("SELECT token FROM sessions LIMIT 1").Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored == "audit-synthetic-token" || !strings.HasPrefix(stored, "sha256:") {
		t.Fatal("bearer stored as plaintext")
	}
	if _, e := d.GetUserBySession(stored); e == nil {
		t.Fatal("stored hash can be used as a bearer")
	}
	vault, e := privatevault.New(context.Background(), d.GetDB(), bytes.Repeat([]byte{2}, 32))
	if e != nil {
		t.Fatal(e)
	}
	d.Privacy = vault
	id, e := d.CreateMeeting(1, "Synthetic", 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = d.UpdateMeetingResult(id, "private transcript", "private summary", "private actions", `[{"speaker":"s0","text":"private segment"}]`, `{"private":"audit"}`); e != nil {
		t.Fatal(e)
	}
	if ok, err := d.SaveMeetingSpeakerNames(id, 1, `{"s0":"Private speaker"}`, "{}"); err != nil || !ok {
		t.Fatal("speaker names save", err)
	}
	// The meeting pipeline seals the description exactly like this in its final write.
	fields := map[string]string{"description": "private description"}
	if e = vault.MeetingFields(context.Background(), 1, id, fields, true); e != nil {
		t.Fatal(e)
	}
	if _, e = d.GetDB().Exec("UPDATE meetings SET description=? WHERE id=?", fields["description"], id); e != nil {
		t.Fatal(e)
	}
	if m, err := d.GetMeeting(id); err != nil || m.Description != "private description" {
		t.Fatal("description does not decrypt on read", err)
	}
	for _, column := range privatevault.MeetingTextColumns {
		var ciphertext string
		if err := d.GetDB().QueryRow("SELECT "+column+" FROM meetings WHERE id=?", id).Scan(&ciphertext); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(ciphertext, "vault:v1:") {
			t.Fatalf("meeting %s not encrypted", column)
		}
	}
	if e = d.GetDB().QueryRow("SELECT transcript FROM meetings WHERE id=?", id).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(stored, "vault:v1:") {
		t.Fatal("meeting plaintext persisted")
	}
	m, e := d.GetMeeting(id)
	if e != nil || m.Transcript != "private transcript" || m.Summary != "private summary" || m.ActionItems != "private actions" || !strings.Contains(m.SegmentsJSON, "private segment") || m.SpeakerNames["s0"] != "Private speaker" || !strings.Contains(string(m.VocabularyAudit), "audit") {
		t.Fatal("meeting read regression", e)
	}
	if _, e = vault.OpenStored(context.Background(), 1, "meeting-summary", fmt.Sprint(id), stored); e == nil {
		t.Fatal("cross-field ciphertext substitution accepted")
	}
	if e = vault.MigrateMeetingText(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = vault.MigrateMeetingText(context.Background()); e != nil {
		t.Fatal("migration not restart safe", e)
	}
}

func TestMeetingEncryptionMigratesLegacyContentAndRecoveryCheckpoints(t *testing.T) {
	d := auditDB(t)
	id, err := d.CreateMeeting(1, "Synthetic legacy meeting", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.UpdateMeetingResult(id, "legacy transcript", "legacy summary", "legacy actions", "[]", "{}"); err != nil {
		t.Fatal(err)
	}
	_, err = d.GetDB().Exec(`CREATE TABLE meeting_jobs(job_key TEXT PRIMARY KEY,user_id INTEGER,status TEXT,mic_result TEXT,system_result TEXT,diar_result TEXT);
        INSERT INTO meeting_jobs VALUES('pending-synthetic',1,'processing','private microphone','private system','private diarization');
        INSERT INTO meeting_jobs VALUES('completed-synthetic',1,'completed','old microphone','old system','old diarization')`)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := privatevault.New(context.Background(), d.GetDB(), bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	d.Privacy = vault
	for i := 0; i < 2; i++ {
		if err = vault.MigrateMeetingText(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range privatevault.MeetingTextColumns {
		var ciphertext string
		if err = d.GetDB().QueryRow("SELECT "+column+" FROM meetings WHERE id=?", id).Scan(&ciphertext); err != nil {
			t.Fatal(err)
		}
		if ciphertext != "" && !strings.HasPrefix(ciphertext, "vault:v1:") {
			t.Fatalf("legacy %s not encrypted", column)
		}
	}
	for column, expected := range map[string]string{"mic_result": "private microphone", "system_result": "private system", "diar_result": "private diarization"} {
		var ciphertext string
		if err = d.GetDB().QueryRow("SELECT " + column + " FROM meeting_jobs WHERE job_key='pending-synthetic'").Scan(&ciphertext); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(ciphertext, "vault:v1:") {
			t.Fatalf("checkpoint %s not encrypted", column)
		}
		plain, err := vault.OpenStored(context.Background(), 1, "job-"+column, "pending-synthetic", ciphertext)
		if err != nil || plain != expected {
			t.Fatal("checkpoint recovery failed", err)
		}
		if _, err = vault.OpenStored(context.Background(), 1, "job-"+column, "different-job", ciphertext); err == nil {
			t.Fatal("cross-job ciphertext substitution accepted")
		}
		if err = d.GetDB().QueryRow("SELECT " + column + " FROM meeting_jobs WHERE job_key='completed-synthetic'").Scan(&ciphertext); err != nil || ciphertext != "" {
			t.Fatal("completed checkpoint not cleared", err)
		}
	}
}
