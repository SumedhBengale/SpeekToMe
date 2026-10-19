package main

import (
	"context"
	"net/http"
	"speektome-server/handlers"
)

// newAPIMux is shared by the server and HTTP contract tests. Registering a
// parallel test-only router can hide missing desktop/server dependencies.
func newAPIMux(ctx context.Context, app *handlers.App) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	if err := app.StartMeetingJobs(ctx, mux); err != nil {
		return nil, err
	}

	mux.HandleFunc("GET /auth/login", app.HandleAuthLogin)
	mux.HandleFunc("GET /auth/callback", app.HandleAuthCallback)
	mux.HandleFunc("POST /auth/signup", app.HandleAuthSignup)
	mux.HandleFunc("POST /auth/login", app.HandleAuthLoginCredentials)
	mux.HandleFunc("POST /auth/2fa/verify", app.HandleAuth2FAVerify)
	mux.HandleFunc("GET /auth/verify", app.HandleAuthVerify)
	mux.HandleFunc("GET /auth/me", app.HandleAuthMe)
	mux.HandleFunc("GET /auth/profile/summary", app.HandleGetProfileSummary)
	mux.HandleFunc("POST /auth/forgot-password", app.HandleForgotPassword)
	mux.HandleFunc("POST /auth/reset-password", app.HandleResetPassword)
	mux.HandleFunc("DELETE /auth/account", app.HandleDeleteAccount)
	mux.HandleFunc("POST /auth/delete-account", app.HandleDeleteAccount)
	mux.HandleFunc("POST /auth/languages", app.HandleUpdateLanguages)
	mux.HandleFunc("GET /usage/history", app.HandleGetUserHistory)
	mux.HandleFunc("POST /auth/logout", app.HandleAuthLogout)
	mux.HandleFunc("POST /generate", app.HandleGenerate)
	mux.HandleFunc("POST /v1/transform", app.HandleTransform)
	mux.HandleFunc("POST /transform", app.HandleTransform)
	mux.HandleFunc("GET /v1/stream", app.HandleStreamTranscribe)
	mux.HandleFunc("POST /v1/transcribe/ack", app.HandleTranscribeAck)
	mux.HandleFunc("GET /v1/languages", handlers.HandleGetLanguages)
	mux.HandleFunc("GET /languages", handlers.HandleGetLanguages)
	mux.HandleFunc("GET /config/promo", handlers.HandleGetOnboardingPromo)
	mux.HandleFunc("GET /promo", handlers.HandleGetOnboardingPromo)
	mux.HandleFunc("GET /config/video", handlers.HandleGetOnboardingVideo)
	mux.HandleFunc("GET /video", handlers.HandleGetOnboardingVideo)
	mux.HandleFunc("GET /v1/config", handlers.HandleGetClientConfig)
	mux.HandleFunc("GET /config", handlers.HandleGetClientConfig)
	mux.HandleFunc("POST /validate-prompt", app.HandleValidatePrompt)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /newsletter/subscribe", app.HandleNewsletterSubscribe)
	mux.HandleFunc("GET /auth/vocabulary", app.HandleGetVocabulary)
	mux.HandleFunc("POST /auth/vocabulary", app.HandleAddVocabulary)
	mux.HandleFunc("DELETE /auth/vocabulary", app.HandleDeleteVocabulary)
	mux.HandleFunc("POST /v1/meetings/session", app.HandleStartMeetingSession)
	mux.HandleFunc("DELETE /v1/meetings/session/{id}", app.HandleEndMeetingSession)
	mux.HandleFunc("POST /meetings", app.HandleUploadMeeting)
	mux.HandleFunc("POST /v1/audio/transcriptions", app.HandleServer2BatchTranscription)
	mux.HandleFunc("GET /v1/meetings/live", app.HandleMeetingStream)
	mux.HandleFunc("GET /meetings", app.HandleGetMeetings)
	mux.HandleFunc("GET /meetings/{id}", app.HandleGetMeetingDetail)
	mux.HandleFunc("PUT /meetings/{id}/speakers", app.HandleSaveMeetingSpeakers)
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

	return mux, nil
}
