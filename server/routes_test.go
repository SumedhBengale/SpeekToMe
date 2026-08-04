package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"speektome-server/db"
	"speektome-server/handlers"
	"speektome-server/pipeline"
)

func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	dbFile := "test_routes.db"
	_ = os.Remove(dbFile)

	database, err := db.NewDatabase(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	vocabCache := pipeline.NewVocabularyCache()

	app := &handlers.App{
		DB:         database,
		VocabCache: vocabCache,
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
	mux.HandleFunc("GET /admin/cache/vocabulary", app.HandleAdminGetVocabCache)
	mux.HandleFunc("DELETE /admin/cache/vocabulary", app.HandleAdminDeleteVocabCache)

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
		{"GET", "/admin/cache/vocabulary"},
		{"DELETE", "/admin/cache/vocabulary"},
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
