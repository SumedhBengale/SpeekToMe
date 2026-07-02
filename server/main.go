package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

var (
	GROQ_API_KEY        string
	DEEPSEEK_API_KEY    string
	GOOGLE_CLIENT_ID    string
	GOOGLE_CLIENT_SECRET string
	BACKEND_URL         string
	DATABASE_URL        string
	MOCK_AUTH           bool

	GROQ_API_URL     = "https://api.groq.com/openai/v1/audio/transcriptions"
	DEEPSEEK_API_URL = "https://api.deepseek.com/chat/completions"

	database *Database
)

func main() {
	_ = godotenv.Load()

	GROQ_API_KEY = os.Getenv("GROQ_API_KEY")
	DEEPSEEK_API_KEY = os.Getenv("DEEPSEEK_API_KEY")
	GOOGLE_CLIENT_ID = os.Getenv("GOOGLE_CLIENT_ID")
	GOOGLE_CLIENT_SECRET = os.Getenv("GOOGLE_CLIENT_SECRET")
	BACKEND_URL = os.Getenv("BACKEND_URL")
	DATABASE_URL = os.Getenv("DATABASE_URL")
	if DATABASE_URL == "" {
		DATABASE_URL = "speekey.db"
	}
	if BACKEND_URL == "" {
		BACKEND_URL = "http://localhost:8000"
	}

	MOCK_AUTH = GOOGLE_CLIENT_ID == "" || GOOGLE_CLIENT_ID == "mock"

	var err error
	database, err = NewDatabase(DATABASE_URL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Router using standard library multiplexer routing
	mux := http.NewServeMux()

	mux.HandleFunc("GET /auth/login", handleAuthLogin)
	mux.HandleFunc("GET /auth/callback", handleAuthCallback)
	mux.HandleFunc("GET /auth/me", handleAuthMe)
	mux.HandleFunc("POST /auth/logout", handleAuthLogout)
	mux.HandleFunc("POST /generate", handleGenerate)
	mux.HandleFunc("GET /health", handleHealth)

	// Admin API routes
	mux.HandleFunc("GET /admin/users", handleAdminListUsers)
	mux.HandleFunc("POST /admin/users/plan", handleAdminUpdateUserPlan)
	mux.HandleFunc("GET /admin/config", handleAdminGetConfig)
	mux.HandleFunc("POST /admin/config", handleAdminUpdateConfig)

	// Wrap in CORS middleware
	handler := corsMiddleware(mux)

	log.Printf("Starting Speekey Go Server on port 8000...")
	log.Printf("Google Auth Mock Mode: %t", MOCK_AUTH)
	log.Fatal(http.ListenAndServe(":8000", handler))
}

// CORS middleware wrapper
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE, PUT")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Origin")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Generate secure session tokens
func generateSessionToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return fmt.Sprintf("speekey_session_%x", b)
}

// Generate random hex nonces for OAuth states
func generateNonce() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// Calculate audio duration from standard WAV header
func getWavDuration(data []byte) float64 {
	if len(data) < 44 {
		return 0
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0
	}
	channels := uint32(binary.LittleEndian.Uint16(data[22:24]))
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	bitsPerSample := uint32(binary.LittleEndian.Uint16(data[34:36]))

	bytesPerSample := bitsPerSample / 8
	if channels == 0 || sampleRate == 0 || bytesPerSample == 0 {
		return 0
	}
	dataSize := len(data) - 44
	numSamples := uint32(dataSize) / (channels * bytesPerSample)
	return float64(numSamples) / float64(sampleRate)
}

// Extract bearer token and retrieve user
func authenticate(r *http.Request) (*UserProfile, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, fmt.Errorf("missing bearer token")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	user, err := database.GetUserBySession(token)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired session token")
	}
	return user, nil
}

// ----------------- HTTP HANDLERS -----------------

func handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	portStr := r.URL.Query().Get("port")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		http.Error(w, "Bad request: missing local redirect port", http.StatusBadRequest)
		return
	}

	if MOCK_AUTH {
		// Immediately redirect to callback
		redirectURL := fmt.Sprintf("%s/auth/callback?code=mock_code&state=%d_mock", BACKEND_URL, port)
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	redirectURI := fmt.Sprintf("%s/auth/callback", BACKEND_URL)
	state := fmt.Sprintf("%d_%s", port, generateNonce())

	oauthURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid%%20email%%20profile&state=%s",
		GOOGLE_CLIENT_ID, url.QueryEscape(redirectURI), state,
	)
	http.Redirect(w, r, oauthURL, http.StatusFound)
}

func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	parts := strings.Split(state, "_")
	if len(parts) < 1 {
		http.Error(w, "Bad request: invalid state parameter", http.StatusBadRequest)
		return
	}
	port, err := strconv.Atoi(parts[0])
	if err != nil || port <= 0 {
		http.Error(w, "Bad request: invalid state port value", http.StatusBadRequest)
		return
	}

	var googleID, email, name string

	if MOCK_AUTH {
		googleID = "mock_google_id_123"
		email = "mock_user@example.com"
		name = "Mock User"
	} else {
		// Exchange code
		client := &http.Client{Timeout: 10 * time.Second}
		tokenURL := "https://oauth2.googleapis.com/token"

		vals := url.Values{}
		vals.Set("code", code)
		vals.Set("client_id", GOOGLE_CLIENT_ID)
		vals.Set("client_secret", GOOGLE_CLIENT_SECRET)
		vals.Set("redirect_uri", fmt.Sprintf("%s/auth/callback", BACKEND_URL))
		vals.Set("grant_type", "authorization_code")

		resp, err := client.PostForm(tokenURL, vals)
		if err != nil {
			http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			http.Error(w, fmt.Sprintf("Token exchange failed with status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
			return
		}

		var tokens struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
			http.Error(w, "Failed to parse token response", http.StatusInternalServerError)
			return
		}

		// Get User profile
		req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		userResp, err := client.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch profile: %v", err), http.StatusInternalServerError)
			return
		}
		defer userResp.Body.Close()

		var profile struct {
			Sub   string `json:"sub"`
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(userResp.Body).Decode(&profile); err != nil {
			http.Error(w, "Failed to parse profile response", http.StatusInternalServerError)
			return
		}

		googleID = profile.Sub
		email = profile.Email
		name = profile.Name
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
	}

	userID, err := database.GetOrCreateUser(googleID, email, name)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	token := generateSessionToken()
	err = database.CreateSession(token, userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database session error: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect browser to client local loopback listener
	loopbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?token=%s", port, token)
	http.Redirect(w, r, loopbackURL, http.StatusFound)
}

func handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user, err := authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"email": user.Email,
		"name":  user.Name,
	})
}

func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		_ = database.DeleteSession(token)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "logged_out",
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                  "ok",
		"groq_key_configured":     GROQ_API_KEY != "",
		"deepseek_key_configured": DEEPSEEK_API_KEY != "",
		"google_auth_mock":        MOCK_AUTH,
	})
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	// Authenticate the request
	user, authErr := authenticate(r)
	if authErr != nil {
		http.Error(w, authErr.Error(), http.StatusUnauthorized)
		return
	}

	// 1. Paid plan activation check
	if user.PlanActive != 1 {
		log.Printf("[PLAN VERIFICATION] Request from %s denied: Plan is not active (tier: %s)", user.Email, user.PlanTier)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "No active paid plan. Please subscribe or activate your plan to use Speekey.",
		})
		return
	}

	if GROQ_API_KEY == "" {
		http.Error(w, "GROQ_API_KEY environment variable is not set", http.StatusInternalServerError)
		return
	}

	// Parse multipart request up to 10MB
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "Failed to parse multipart request", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Bad request: missing file parameter", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read audio samples
	var wavBuf bytes.Buffer
	if _, err := io.Copy(&wavBuf, file); err != nil {
		http.Error(w, "Failed to read audio file payload", http.StatusInternalServerError)
		return
	}
	audioBytes := wavBuf.Bytes()
	audioSizeBytes := len(audioBytes)

	// Fetch dynamic configurations
	rpmLimit := 15
	fupLimitSeconds := 3600.0
	config, err := database.GetSaasConfig()
	if err == nil {
		if val, ok := config["paid_rate_limit_rpm"]; ok {
			if limit, err := strconv.Atoi(val); err == nil {
				rpmLimit = limit
			}
		}
		if val, ok := config["paid_fup_limit_seconds_daily"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				fupLimitSeconds = limit
			}
		}
	}

	// 2. Rate Limit Check (Requests Per Minute)
	reqCount, err := database.GetUserRequestCountLastMinute(user.ID)
	if err == nil && reqCount >= rpmLimit {
		log.Printf("[RATE LIMIT] Request from %s denied: Exceeded limit (%d/%d requests in the last minute)", user.Email, reqCount, rpmLimit)
		_ = database.LogUsage(user.ID, 0, 0, 0, 0, false, "", "", "", "failed_rate_limit", audioSizeBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Rate limit exceeded. Maximum %d requests per minute allowed.", rpmLimit),
		})
		return
	}

	// 3. FUP Limit Check (Daily audio duration in seconds)
	audioDuration := getWavDuration(audioBytes)
	dailyDuration, err := database.GetUserDurationLast24Hours(user.ID)
	if err == nil && dailyDuration+audioDuration > fupLimitSeconds {
		log.Printf("[FUP LIMIT] Request from %s denied: Exceeded limit (%.1f + %.1f > %.1f daily seconds)", user.Email, dailyDuration, audioDuration, fupLimitSeconds)
		_ = database.LogUsage(user.ID, audioDuration, 0, 0, 0, false, "", "", "", "failed_fup_limit", audioSizeBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Fair Usage Policy (FUP) limit exceeded. Maximum daily audio duration allowed is %.0f seconds. You have already used %.1f seconds in the last 24 hours.", fupLimitSeconds, dailyDuration),
		})
		return
	}

	// Capture options
	model := r.FormValue("model")
	if model == "" {
		model = "whisper-large-v3-turbo"
	}
	enhance := r.FormValue("enhance") == "true"
	prompt := r.FormValue("prompt")

	totalStart := time.Now()
	groqStart := time.Now()

	var rawText string
	var enhancedText string
	var groqLatency float64
	var dsLatency float64

	// 1. Post to Groq Whisper
	groqBody := &bytes.Buffer{}
	writer := multipart.NewWriter(groqBody)
	_ = writer.WriteField("model", model)
	filePart, err := writer.CreateFormFile("file", "recording.wav")
	if err == nil {
		_, _ = filePart.Write(audioBytes)
	}
	_ = writer.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", GROQ_API_URL, groqBody)
	if err == nil {
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+GROQ_API_KEY)
		resp, postErr := client.Do(req)
		if postErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var result struct {
					Text string `json:"text"`
				}
				if json.NewDecoder(resp.Body).Decode(&result) == nil {
					rawText = strings.TrimSpace(result.Text)
					groqLatency = time.Since(groqStart).Seconds()
				}
			}
		}
	}

	// Filter common Whisper silence hallucinations
	hallucinations := map[string]bool{
		"thank you": true, "thank you.": true, "thanks for watching": true, "thanks for watching.": true,
		"thank you for watching.": true, "please subscribe": true, "subtitles by": true, "subtitles by amara": true,
	}
	checkText := strings.Trim(strings.ToLower(rawText), ".,!?\"'")
	if hallucinations[checkText] {
		rawText = ""
	}

	// 2. Call DeepSeek enhancement
	if enhance && rawText != "" {
		if DEEPSEEK_API_KEY == "" {
			totalLatency := time.Since(totalStart).Seconds()
			_ = database.LogUsage(user.ID, audioDuration, groqLatency, 0, totalLatency, true, "deepseek-v4-flash", rawText, "", "failed_missing_deepseek_key", audioSizeBytes)
			http.Error(w, "DEEPSEEK_API_KEY environment variable is not set", http.StatusInternalServerError)
			return
		}

		systemPrompt := "You are an expert transcription editor. Enhance the provided transcription by correcting grammar, punctuation, and typos. Preserve all technical terms, proper names, and original meaning. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text."
		if strings.TrimSpace(prompt) != "" {
			systemPrompt = fmt.Sprintf(
				"You are an expert transcription editor. Enhance the provided transcription according to these custom instructions:\n%s\n\nPreserve the original meaning unless instructed otherwise. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text.",
				strings.TrimSpace(prompt),
			)
		}

		dsPayload := map[string]interface{}{
			"model": "deepseek-v4-flash",
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": rawText},
			},
			"thinking": map[string]string{"type": "disabled"},
		}

		jsonPayload, _ := json.Marshal(dsPayload)
		dsStart := time.Now()

		req, err := http.NewRequest("POST", DEEPSEEK_API_URL, bytes.NewBuffer(jsonPayload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+DEEPSEEK_API_KEY)

			resp, postErr := client.Do(req)
			if postErr == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var result struct {
						Choices []struct {
							Message struct {
								Content string `json:"content"`
							} `json:"message"`
						} `json:"choices"`
					}
					if json.NewDecoder(resp.Body).Decode(&result) == nil && len(result.Choices) > 0 {
						enhancedText = strings.TrimSpace(result.Choices[0].Message.Content)
						dsLatency = time.Since(dsStart).Seconds()
					}
				}
			}
		}
	}

	totalLatency := time.Since(totalStart).Seconds()
	finalOutput := rawText
	if enhance && rawText != "" {
		finalOutput = enhancedText
	}

	status := "success"
	if rawText == "" && enhance {
		status = "success_skipped_empty"
	}
	if enhance && rawText != "" && enhancedText == "" {
		status = "failed_deepseek_error"
	}
	if rawText == "" && !enhance {
		// Empty audio file or silence check bypassed
		status = "success_empty"
	}
	if rawText == "" && groqLatency == 0.0 {
		status = "failed_groq_error"
	}

	// Log usage to database
	_ = database.LogUsage(user.ID, audioDuration, groqLatency, dsLatency, totalLatency, enhance, "deepseek-v4-flash", rawText, finalOutput, status, audioSizeBytes)

	// Output beautiful analytics block to the console
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("                SPEEKEY ANALYTICS (GO)")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("User:             %s (ID: %d)\n", user.Email, user.ID)
	fmt.Printf("Audio Duration:   %.2f seconds\n", audioDuration)
	fmt.Printf("Bandwidth / Size: %s (%d bytes)\n", formatSize(audioSizeBytes), audioSizeBytes)
	fmt.Printf("Groq Whisper:     %.3f seconds\n", groqLatency)
	if enhance && rawText != "" {
		fmt.Printf("DeepSeek LLM:     %.3f seconds\n", dsLatency)
		fmt.Printf("Enhancement:      Enabled (Model: deepseek-v4-flash)\n")
		fmt.Printf("Raw Transcript:   %s\n", rawText)
		fmt.Printf("Final Output:     %s\n", enhancedText)
	} else {
		fmt.Printf("DeepSeek LLM:     N/A (Disabled/Skipped)\n")
		fmt.Printf("Final Output:     %s\n", rawText)
	}
	fmt.Printf("Total Server Time:%.3f seconds\n", totalLatency)
	fmt.Println(strings.Repeat("=", 50) + "\n")

	// Return response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"text": finalOutput,
	})
}

// formatSize converts bytes to human readable format (KB or MB)
func formatSize(bytes int) string {
	const kb = 1024
	const mb = 1024 * 1024
	if bytes >= mb {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(mb))
	} else if bytes >= kb {
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(kb))
	}
	return fmt.Sprintf("%d Bytes", bytes)
}

// authenticateAdmin checks if request contains the valid admin secret
func authenticateAdmin(r *http.Request) bool {
	adminSecret := os.Getenv("ADMIN_SECRET")
	if adminSecret == "" {
		adminSecret = "speekey_admin_secret_123!" // Default secure fallback
	}
	secret := r.Header.Get("X-Admin-Secret")
	if secret == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			secret = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return secret == adminSecret
}

// GET /admin/users - List users with usage stats
func handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if !authenticateAdmin(r) {
		http.Error(w, "Unauthorized: invalid admin secret", http.StatusUnauthorized)
		return
	}
	users, err := database.ListUsersWithStats()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list users: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}

// POST /admin/users/plan - Update a user's plan tier and active state
func handleAdminUpdateUserPlan(w http.ResponseWriter, r *http.Request) {
	if !authenticateAdmin(r) {
		http.Error(w, "Unauthorized: invalid admin secret", http.StatusUnauthorized)
		return
	}
	var req struct {
		UserID     int64  `json:"user_id"`
		PlanTier   string `json:"plan_tier"`
		PlanActive int    `json:"plan_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	err := database.UpdateUserPlan(req.UserID, req.PlanTier, req.PlanActive)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update user plan: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// GET /admin/config - Get SaaS configurations
func handleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	if !authenticateAdmin(r) {
		http.Error(w, "Unauthorized: invalid admin secret", http.StatusUnauthorized)
		return
	}
	config, err := database.GetSaasConfig()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get config: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(config)
}

// POST /admin/config - Update SaaS configurations
func handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if !authenticateAdmin(r) {
		http.Error(w, "Unauthorized: invalid admin secret", http.StatusUnauthorized)
		return
	}
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	for k, v := range req {
		if err := database.UpdateSaasConfig(k, v); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update key %s: %v", k, err), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}
