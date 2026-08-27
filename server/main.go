package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/handlers"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/valkey"
	"speektome-server/vocab"
)

func main() {
	_ = godotenv.Load()
	if os.Getenv("GROQ_API_KEY") == "" && os.Getenv("DEEPINFRA_API_KEY") == "" {
		_ = godotenv.Load("server/.env")
	}

	config.GroqAPIKey = os.Getenv("GROQ_API_KEY")
	config.DeepInfraAPIKey = os.Getenv("DEEPINFRA_API_KEY")
	config.DeepSeekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
	config.CerebrasAPIKey = os.Getenv("CEREBRAS_API_KEY")
	config.WaferAPIKey = os.Getenv("WAFER_API_KEY")
	config.CloudflareAPIToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	config.CloudflareAccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	config.GoogleClientID = os.Getenv("GOOGLE_CLIENT_ID")
	config.GoogleClientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
	config.BackendURL = os.Getenv("BACKEND_URL")
	config.DatabaseURL = os.Getenv("DATABASE_URL")
	config.FrontendURL = os.Getenv("FRONTEND_URL")
	if config.DatabaseURL == "" {
		config.DatabaseURL = "speektome.db"
	}
	if config.BackendURL == "" {
		config.BackendURL = "http://localhost:8000"
	}

	database, err := db.NewDatabase(config.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	app := &handlers.App{
		DB:         database,
		VocabCache: vocab.NewVocabCache(),
	}

	// Initialize Valkey / Redis instance
	valkeyClient := valkey.InitGlobalClient()
	if valkeyClient != nil && valkeyClient.IsConnected() {
		valkeyClient.SubscribeConfigReload(context.Background(), func() {
			config.SyncConfigsFromDB(database, ratelimit.GroqRateLimiter.SetLimit)
		})
		valkeyClient.SubscribeVocabInvalidate(context.Background(), func(userID int64) {
			app.VocabCache.Invalidate(userID)
		})
	}

	config.SyncConfigsFromDB(database, ratelimit.GroqRateLimiter.SetLimit)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			config.SyncConfigsFromDB(database, ratelimit.GroqRateLimiter.SetLimit)
		}
	}()

	// Hydrate and schedule GeoIP database-backed cache
	services.SetGeoDatabase(database)
	services.SyncGeoCacheFromDB(database)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			services.SyncGeoCacheFromDB(database)
		}
	}()

	// Check and execute free-tier monthly quota reset if month rolled over
	if _, err := database.CheckAndResetFreeUserMonthlyQuotas(); err != nil {
		log.Printf("[FREE QUOTA RESET ERROR] Startup check failed: %v", err)
	}

	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := database.CheckAndResetFreeUserMonthlyQuotas(); err != nil {
				log.Printf("[FREE QUOTA RESET ERROR] Periodic check failed: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()

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
	mux.HandleFunc("GET /v1/stream", app.HandleStreamTranscribe)
	mux.HandleFunc("POST /v1/transcribe/ack", app.HandleTranscribeAck)
	mux.HandleFunc("GET /v1/languages", handlers.HandleGetLanguages)
	mux.HandleFunc("GET /languages", handlers.HandleGetLanguages)
	mux.HandleFunc("POST /validate-prompt", app.HandleValidatePrompt)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /newsletter/subscribe", app.HandleNewsletterSubscribe)
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

	log.Printf("Starting SpeekTo.Me Go Server on port 8000...")
	log.Fatal(http.ListenAndServe("127.0.0.1:8000", handler))
}

func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	hostname := u.Hostname()
	if hostname == "localhost" || hostname == "127.0.0.1" ||
		strings.HasSuffix(hostname, ".speekto.me") || hostname == "speekto.me" ||
		strings.HasSuffix(hostname, ".speektome.com") || hostname == "speektome.com" {
		return true
	}
	return false
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
