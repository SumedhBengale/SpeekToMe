package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"speektome-server/auth"
	"speektome-server/db"
	"speektome-server/handlers"
)

func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	dbFile := "test_routes.db"
	_ = os.Remove(dbFile)

	database, err := db.NewDatabase(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	app := &handlers.App{
		DB: database,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /auth/login", app.HandleAuthLogin)
	mux.HandleFunc("GET /auth/callback", app.HandleAuthCallback)
	mux.HandleFunc("POST /auth/signup", app.HandleAuthSignup)
	mux.HandleFunc("POST /auth/login", app.HandleAuthLoginCredentials)
	mux.HandleFunc("POST /auth/2fa/verify", app.HandleAuth2FAVerify)
	mux.HandleFunc("GET /auth/verify", app.HandleAuthVerify)
	mux.HandleFunc("GET /auth/me", app.HandleAuthMe)
	mux.HandleFunc("GET /usage/history", app.HandleGetUserHistory)
	mux.HandleFunc("POST /auth/logout", app.HandleAuthLogout)
	mux.HandleFunc("POST /generate", app.HandleGenerate)
	mux.HandleFunc("GET /v1/stream", app.HandleStreamTranscribe)
	mux.HandleFunc("POST /v1/transcribe/ack", app.HandleTranscribeAck)
	mux.HandleFunc("POST /validate-prompt", app.HandleValidatePrompt)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /auth/vocabulary", app.HandleGetVocabulary)
	mux.HandleFunc("POST /auth/vocabulary", app.HandleAddVocabulary)
	mux.HandleFunc("DELETE /auth/vocabulary", app.HandleDeleteVocabulary)
	mux.HandleFunc("POST /meetings", app.HandleUploadMeeting)
	mux.HandleFunc("GET /meetings", app.HandleGetMeetings)
	mux.HandleFunc("GET /meetings/{id}", app.HandleGetMeetingDetail)
	mux.HandleFunc("DELETE /meetings/{id}", app.HandleDeleteMeeting)
	mux.HandleFunc("GET /meetings/{id}/audio", app.HandleGetMeetingAudio)
	mux.HandleFunc("POST /billing/create-subscription", app.HandleCreateSubscription)
	mux.HandleFunc("POST /billing/verify-subscription", app.HandleVerifySubscription)
	mux.HandleFunc("POST /billing/cancel-subscription", app.HandleCancelSubscription)
	mux.HandleFunc("POST /webhooks/razorpay", app.HandleRazorpayWebhook)

	mux.HandleFunc("GET /admin/users", app.HandleAdminGetUsers)
	mux.HandleFunc("POST /admin/users/{id}/plan", app.HandleAdminUpdatePlan)
	mux.HandleFunc("POST /admin/users/{id}/rate-limit", app.HandleAdminUpdateRateLimit)
	mux.HandleFunc("GET /admin/config", app.HandleAdminGetConfig)
	mux.HandleFunc("POST /admin/config", app.HandleAdminUpdateConfig)
	mux.HandleFunc("GET /admin/webhook-logs", app.HandleAdminGetWebhookLogs)
	mux.HandleFunc("GET /admin/cache/rate-limits", app.HandleAdminGetRateLimits)
	mux.HandleFunc("DELETE /admin/cache/rate-limits", app.HandleAdminDeleteRateLimits)

	handler := corsMiddleware(mux)
	ts := httptest.NewServer(handler)

	cleanup := func() {
		ts.Close()
		_ = database.Close()
		_ = os.Remove(dbFile)
	}

	return ts, cleanup
}

func TestAllRouteRegistrations(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	client := ts.Client()

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/health"},
		{"GET", "/auth/login?port=8080"},
		{"GET", "/auth/callback?state=8080_nonce"},
		{"POST", "/auth/signup"},
		{"POST", "/auth/login"},
		{"POST", "/auth/2fa/verify"},
		{"GET", "/auth/verify?token=test"},
		{"GET", "/auth/me"},
		{"GET", "/usage/history"},
		{"POST", "/auth/logout"},
		{"POST", "/generate"},
		{"GET", "/v1/stream"},
		{"POST", "/v1/transcribe/ack"},
		{"POST", "/validate-prompt"},
		{"GET", "/auth/vocabulary"},
		{"POST", "/auth/vocabulary"},
		{"DELETE", "/auth/vocabulary"},
		{"POST", "/meetings"},
		{"GET", "/meetings"},
		{"GET", "/meetings/1"},
		{"DELETE", "/meetings/1"},
		{"GET", "/meetings/1/audio"},
		{"POST", "/billing/create-subscription"},
		{"POST", "/billing/verify-subscription"},
		{"POST", "/billing/cancel-subscription"},
		{"POST", "/webhooks/razorpay"},
		{"GET", "/admin/users"},
		{"POST", "/admin/users/1/plan"},
		{"POST", "/admin/users/1/rate-limit"},
		{"GET", "/admin/config"},
		{"POST", "/admin/config"},
		{"GET", "/admin/webhook-logs"},
		{"GET", "/admin/cache/rate-limits"},
		{"DELETE", "/admin/cache/rate-limits"},
	}

	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			req, err := http.NewRequest(r.method, ts.URL+r.path, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("Route %s %s returned 404 Not Found! The route is broken.", r.method, r.path)
			} else {
				t.Logf("Route %s %s returned status %d (Route exists and is matched)", r.method, r.path, resp.StatusCode)
			}
		})
	}
}

func TestRazorpayWebhookPayloadParsing(t *testing.T) {
	rawPayloads := []string{
		`{"entity":"event","account_id":"acc_Rxf2NVNa8irhhR","event":"subscription.charged","contains":["subscription","payment"],"payload":{"subscription":{"entity":{"id":"sub_TJP2pgrSktuo7A","entity":"subscription","plan_id":"plan_T8yjLW3pBOKGcQ","notes":[]}},"payment":{"entity":{"id":"pay_TLdgeMdbo1Luij","notes":[]}}}}`,
		`{"entity":"event","account_id":"acc_Rxf2NVNa8irhhR","event":"subscription.charged","contains":["subscription","payment"],"payload":{"subscription":{"entity":{"id":"sub_TLdmas00uHBQEQ","notes":{"user_id":"7"}}},"payment":{"entity":{"id":"pay_TLdmjGMaZxCKcL","notes":[]}}}}`,
	}

	for i, raw := range rawPayloads {
		var event struct {
			Event   string `json:"event"`
			Payload struct {
				Subscription struct {
					Entity struct {
						ID    string            `json:"id"`
						Notes handlers.NotesMap `json:"notes"`
					} `json:"entity"`
				} `json:"subscription"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Errorf("Payload %d failed to unmarshal: %v", i+1, err)
		} else {
			t.Logf("Payload %d unmarshaled successfully, event=%s subID=%s notes=%v", i+1, event.Event, event.Payload.Subscription.Entity.ID, event.Payload.Subscription.Entity.Notes)
		}
	}
}

func TestIsValidRedirectSecurity(t *testing.T) {
	// Must reject dangerous pseudo-schemes
	dangerousURLs := []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(document.cookie)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
		"http://evil-attacker.com/steal-token",
		"https://evil-speekto.me.attacker.com",
		"//evil.com/phish",
		"/\\evil.com",
	}

	for _, u := range dangerousURLs {
		if auth.IsValidRedirect(u) {
			t.Errorf("Security violation: IsValidRedirect(%q) returned true for dangerous URL!", u)
		}
	}

	// Must accept safe legitimate redirect URLs
	safeURLs := []string{
		"/dashboard",
		"/dashboard?verified=true",
		"http://localhost:8080/dashboard",
		"http://localhost:5173/auth/callback",
		"http://127.0.0.1:8000/auth/callback",
		"https://speekto-me.pages.dev/dashboard",
		"https://2fa2faa0.speekto-me.pages.dev/dashboard",
		"https://speekto.me/dashboard",
		"https://api.speekto.me/health",
		"tauri://localhost/callback",
		"speektome://auth/callback",
	}

	for _, u := range safeURLs {
		if !auth.IsValidRedirect(u) {
			t.Errorf("False positive: IsValidRedirect(%q) returned false for safe URL!", u)
		}
	}
}

func TestGoogleOAuthAccountLinking(t *testing.T) {
	databasePath := t.TempDir() + "/oauth_link_test.db"
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("NewDatabase failed: %v", err)
	}
	defer database.Close()

	// 1. User signs up via email/password first
	email := "linking_user@example.com"
	userID, err := database.CreateEmailUser(email, "Password1234", "Linking User", "verify_token_123")
	if err != nil {
		t.Fatalf("CreateEmailUser failed: %v", err)
	}

	// 2. User later logs in with Google OAuth using the same email
	googleID := "google_oauth_sub_987654"
	linkedUserID, err := database.GetOrCreateUser(googleID, email, "Linking User Google")
	if err != nil {
		t.Fatalf("GetOrCreateUser failed: %v", err)
	}

	if linkedUserID != userID {
		t.Errorf("Expected linked user ID %d, got %d", userID, linkedUserID)
	}

	// Verify google_id is linked and user is verified
	sessionToken := "test_session_token_link"
	err = database.CreateSession(sessionToken, linkedUserID, 0)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	profile, err := database.GetUserBySession(sessionToken)
	if err != nil {
		t.Fatalf("GetUserBySession failed: %v", err)
	}

	if profile.ID != userID || profile.Email != email {
		t.Errorf("Profile mismatch: id=%d, email=%s", profile.ID, profile.Email)
	}
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	databasePath := t.TempDir() + "/pwd_revoke_test.db"
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("NewDatabase failed: %v", err)
	}
	defer database.Close()

	userID, err := database.CreateEmailUser("revoketest@example.com", "OldPassword123", "Revoke User", "tok")
	if err != nil {
		t.Fatalf("CreateEmailUser: %v", err)
	}

	// Create 2 active sessions
	session1 := "session_token_111"
	session2 := "session_token_222"
	_ = database.CreateSession(session1, userID, 0)
	_ = database.CreateSession(session2, userID, 0)

	// Verify sessions are active
	if _, err := database.GetUserBySession(session1); err != nil {
		t.Fatalf("session1 should be active: %v", err)
	}

	// Issue password reset token
	resetToken := "reset_token_secret_xyz"
	_, err = database.SetPasswordResetToken("revoketest@example.com", resetToken, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("SetPasswordResetToken failed: %v", err)
	}

	// Reset password
	err = database.ResetPasswordWithToken(resetToken, "NewHashedPassword123")
	if err != nil {
		t.Fatalf("ResetPasswordWithToken failed: %v", err)
	}

	// Verify all prior sessions are revoked
	if _, err := database.GetUserBySession(session1); err == nil {
		t.Errorf("Security failure: session1 was NOT revoked after password reset!")
	}
	if _, err := database.GetUserBySession(session2); err == nil {
		t.Errorf("Security failure: session2 was NOT revoked after password reset!")
	}
}


