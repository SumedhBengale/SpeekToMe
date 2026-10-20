package main

import (
	"context"
	"encoding/base64"
	"golang.org/x/sys/unix"
	"net/http"
	"net/url"
	"os"
	log "speektome-server/privacylog"
	"speektome-server/privatevault"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/handlers"
	"speektome-server/orchestrator"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/valkey"
	"speektome-server/vocab"
)

func main() {

	//Initial try to load the .env file from the root directory
	_ = godotenv.Load()
	//Second try to load the .env file from the server directory if not found in root
	if os.Getenv("GROQ_API_KEY") == "" && os.Getenv("DEEPINFRA_API_KEY") == "" {
		_ = godotenv.Load("server/.env")
	}

	config.GroqAPIKey = os.Getenv("GROQ_API_KEY")
	config.DeepInfraAPIKey = os.Getenv("DEEPINFRA_API_KEY")
	config.DeepSeekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
	config.CerebrasAPIKey = os.Getenv("CEREBRAS_API_KEY")
	config.WaferAPIKey = os.Getenv("WAFER_API_KEY")
	config.OpenRouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
	config.CloudflareAPIToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	config.CloudflareAccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	config.AWSBearerTokenBedrock = os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	if config.AWSBearerTokenBedrock == "" {
		config.AWSBearerTokenBedrock = os.Getenv("AWS_BEDROCK_API_KEY")
	}
	config.AWSBedrockRegion = os.Getenv("AWS_REGION")
	if config.AWSBedrockRegion == "" {
		config.AWSBedrockRegion = os.Getenv("AWS_BEDROCK_REGION")
	}
	if config.AWSBedrockRegion == "" {
		config.AWSBedrockRegion = "us-east-1"
	}
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

	// Maintenance uses the same binary/config/database as the API service.
	if len(os.Args) == 2 && os.Args[1] == "--expire-subscriptions" {
		valkey.InitGlobalClient()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		count, err := database.ExpireSubscriptions(ctx, time.Now())
		if err != nil {
			log.Fatalf("[SUBSCRIPTION EXPIRY] Daily sweep failed: %v", err)
		}
		log.Printf("[SUBSCRIPTION EXPIRY] Downgraded %d expired subscriptions", count)
		database.Close()
		return
	}

	// Disable core dumps before retrieving a key. Only its Secrets Manager
	// identifier is configured on disk; the value stays in process memory.
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		log.Fatal("Cannot disable personal-storage core dumps")
	}
	keyCtx, keyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	secrets, keyErr := privatevault.LoadSecrets(keyCtx)
	keyCancel()
	if keyErr != nil {
		log.Fatal("Cannot initialize encrypted personal storage")
	}
	master, keyErr := base64.StdEncoding.DecodeString(secrets.MasterKey)
	secrets.MasterKey = ""
	if keyErr != nil {
		log.Fatal("Invalid personal-storage key")
	}
	privacy, keyErr := privatevault.New(context.Background(), database.GetDB(), master)
	clear(master)
	if keyErr != nil {
		log.Fatal("Cannot unlock encrypted personal storage")
	}
	database.Privacy = privacy
	if os.Getenv("R2_ACCESS_KEY_ID") != "" && os.Getenv("R2_SECRET_ACCESS_KEY") != "" {
		objectStore, err := privatevault.NewR2(os.Getenv("R2_ACCOUNT_ID"), os.Getenv("R2_BUCKET"), os.Getenv("R2_ACCESS_KEY_ID"), os.Getenv("R2_SECRET_ACCESS_KEY"))
		if err != nil {
			log.Fatal("Cannot initialize encrypted cloud audio")
		}
		privacy.Objects = objectStore
	} else if os.Getenv("R2_BUCKET") != "" {
		log.Fatal("R2 credentials unavailable")
	}
	if len(os.Args) == 2 && os.Args[1] == "--audit-private-data" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := privacy.Audit(ctx, os.Stdout); err != nil {
			log.Fatal("Private storage audit unavailable")
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-private-storage" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := privacy.CheckObjects(ctx); err != nil {
			log.Fatal("Encrypted object storage check failed")
		}
		log.Printf("Encrypted object storage PUT/range GET/DELETE verified")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--migrate-private-data" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		count, err := privacy.MigrateUsage(ctx)
		if err != nil {
			log.Fatal("Encrypted usage migration did not finish")
		}
		log.Printf("Encrypted legacy final results migrated: %d", count)
		return
	}

	app := &handlers.App{
		DB:         database,
		VocabCache: vocab.NewVocabCache(),
	}

	// Initialize Valkey / Redis instance
	valkeyClient := valkey.InitGlobalClient()
	if valkeyClient != nil && valkeyClient.IsConnected() {
		valkeyClient.SubscribeConfigReload(context.Background(), func() {
			config.SyncConfigsFromValkeyOrDB(database, ratelimit.GroqRateLimiter.SetLimit)
		})
		valkeyClient.SubscribeVocabInvalidate(context.Background(), func(userID int64) {
			app.VocabCache.Invalidate(userID)
		})
	}

	expiryCtx, expiryCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	expiredCount, expiryErr := database.ExpireSubscriptions(expiryCtx, time.Now())
	expiryCancel()
	if expiryErr != nil {
		log.Fatalf("[SUBSCRIPTION EXPIRY] Startup sweep failed: %v", expiryErr)
	}
	log.Printf("[SUBSCRIPTION EXPIRY] Startup downgraded %d expired subscriptions", expiredCount)

	config.SyncConfigsFromValkeyOrDB(database, ratelimit.GroqRateLimiter.SetLimit)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			config.SyncConfigsFromValkeyOrDB(database, ratelimit.GroqRateLimiter.SetLimit)
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

	mux, err := newAPIMux(context.Background(), app)
	if err != nil {
		log.Fatalf("Failed to initialize API routes: %v", err)
	}

	privacy.Run(context.Background(), func(message string) { log.Print(message) })

	// Initialize and start the EC2 Spot Orchestrator only when explicitly enabled
	spotAMI := os.Getenv("SPOT_AMI_ID")
	spotKey := os.Getenv("SPOT_KEY_NAME")
	spotSG := os.Getenv("SPOT_SECURITY_GROUP")
	spotUserData := os.Getenv("SPOT_USER_DATA")
	spotOrchestrator := orchestrator.NewOrchestrator(nil, spotAMI, spotKey, spotSG, spotUserData)
	if strings.ToLower(os.Getenv("ENABLE_SPOT_ORCHESTRATOR")) == "true" {
		log.Printf("[ORCHESTRATOR] Starting background Spot instance orchestrator...")
		spotOrchestrator.Start()
		defer spotOrchestrator.Stop()
	} else {
		log.Printf("[ORCHESTRATOR] Spot instance orchestrator is disabled (ENABLE_SPOT_ORCHESTRATOR != true).")
	}

	// EC2 Spot Orchestrator Routes
	mux.HandleFunc("POST /webhooks/aws-spot-event", spotOrchestrator.HandleSpotEvent)
	mux.HandleFunc("POST /worker/heartbeat", spotOrchestrator.HandleWorkerHeartbeat)
	mux.HandleFunc("GET /admin/spot/status", spotOrchestrator.HandleAdminStatus)

	handler := corsMiddleware(mux)

	log.Printf("Starting SpeekTo.Me Go Server on port 8000...")
	server := &http.Server{
		ErrorLog: log.HTTPLogger(), Addr: "127.0.0.1:8000", Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func isAllowedOrigin(origin string) bool {
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return true
	}
	hostname := u.Hostname()
	if hostname == "localhost" || hostname == "127.0.0.1" ||
		hostname == "tauri.localhost" || strings.HasSuffix(hostname, ".localhost") ||
		strings.HasSuffix(hostname, ".speekto.me") || hostname == "speekto.me" ||
		strings.HasSuffix(hostname, ".speektome.com") || hostname == "speektome.com" ||
		u.Scheme == "tauri" || u.Scheme == "app" || u.Scheme == "vscode-webview" {
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
