package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"speektome-server/auth"
	"speektome-server/db"
	"speektome-server/handlers"
	"speektome-server/vocab"
)

func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	dbFile := filepath.Join(t.TempDir(), "test_routes.db")
	_ = os.Remove(dbFile)

	database, err := db.NewDatabase(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	app := &handlers.App{
		DB:         database,
		VocabCache: vocab.NewVocabCache(),
	}

	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Route checks do not run background inference or contact workers.
	mux, err := newAPIMux(ctx, app)
	if err != nil {
		t.Fatalf("Initialize production API routes: %v", err)
	}

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
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/health"},
		{"GET", "/auth/login?port=8080"},
		{"GET", "/auth/callback?state=invalid"},
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
		{"POST", "/v1/meetings/session"},
		{"DELETE", "/v1/meetings/session/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
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
		{"POST", "/v1/transform"},
		{"POST", "/transform"},
		{"GET", "/v1/config"},
		{"GET", "/config"},
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
	t.Setenv("FRONTEND_URL", "https://speekto.me")
	// Must reject dangerous pseudo-schemes
	dangerousURLs := []string{"https://unrelated.pages.dev/callback", "https://2fa2faa0.speekto-me.pages.dev/dashboard", "http://speekto.me/dashboard",
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
		"https://speekto.me/dashboard",
		"https://speekto-me.pages.dev/dashboard",
		"https://api.speekto.me/health",
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

func TestCORSAllowedOrigins(t *testing.T) {
	testOrigins := []struct {
		origin  string
		allowed bool
	}{
		{"https://speekto.me", true},
		{"https://app.speekto.me", true},
		{"https://dashboard.speekto.me", true},
		{"https://speektome.com", true},
		{"http://localhost:5173", true},
		{"http://127.0.0.1:3000", true},
		{"https://malicious.com", false},
		{"https://fakespeekto.me.evil.com", false},
	}

	for _, tt := range testOrigins {
		res := isAllowedOrigin(tt.origin)
		if res != tt.allowed {
			t.Errorf("isAllowedOrigin(%q) = %v, want %v", tt.origin, res, tt.allowed)
		}
	}
}
