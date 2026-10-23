package handlers

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	log "speektome-server/privacylog"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/utils"
	"speektome-server/vocab"
)

type App struct {
	DB         *db.Database
	VocabCache *vocab.VocabCache
}

func (a *App) HandleAuthLogin(w http.ResponseWriter, r *http.Request) {
	portStr := r.URL.Query().Get("port")
	redirectTo := r.URL.Query().Get("redirect_to")
	clientState := r.URL.Query().Get("client_state")

	var state string

	if portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			http.Error(w, "Bad request: invalid local redirect port", http.StatusBadRequest)
			return
		}
		if clientState != "" {
			state = fmt.Sprintf("%d_%s_%s", port, auth.GenerateNonce(), clientState)
		} else {
			state = fmt.Sprintf("%d_%s", port, auth.GenerateNonce())
		}
	} else if redirectTo != "" {
		if !auth.IsValidRedirect(redirectTo) {
			http.Error(w, "Bad request: invalid redirect_to parameter", http.StatusBadRequest)
			return
		}
		if clientState != "" {
			state = fmt.Sprintf("web_%x_%s_%s", []byte(redirectTo), auth.GenerateNonce(), clientState)
		} else {
			state = fmt.Sprintf("web_%x_%s", []byte(redirectTo), auth.GenerateNonce())
		}
	} else {
		http.Error(w, "Bad request: missing port or redirect_to parameters", http.StatusBadRequest)
		return
	}

	if portStr == "" || r.URL.Query().Get("exchange") == "1" {
		state = "exchange_" + state
	}
	if err := a.issueOAuthState(w, r, state); err != nil {
		http.Error(w, "Cannot start sign in", 503)
		return
	}
	redirectURI := fmt.Sprintf("%s/auth/callback", config.BackendURL)

	oauthURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid%%20email%%20profile&state=%s",
		config.GoogleClientID, url.QueryEscape(redirectURI), state,
	)
	http.Redirect(w, r, oauthURL, http.StatusFound)
}

// oauthTarget is where a completed Google sign-in returns.
type oauthTarget struct {
	exchange    bool
	web         bool
	port        int
	redirectTo  string
	clientState string
}

// parseOAuthState decodes the state issued by HandleAuthLogin. The caller has
// already consumed it, so its contents are trusted only after validation.
func parseOAuthState(state string) (oauthTarget, error) {
	var target oauthTarget
	target.exchange = strings.HasPrefix(state, "exchange_")
	parts := strings.Split(strings.TrimPrefix(state, "exchange_"), "_")
	if len(parts) < 2 {
		return target, fmt.Errorf("Bad request: invalid state parameter")
	}
	if parts[0] == "web" {
		decoded, err := hex.DecodeString(parts[1])
		if err != nil {
			return target, fmt.Errorf("Bad request: invalid redirect hex")
		}
		target.web, target.redirectTo = true, string(decoded)
		if !auth.IsValidRedirect(target.redirectTo) {
			return target, fmt.Errorf("Bad request: invalid redirect_to parameter")
		}
		if len(parts) >= 4 {
			target.clientState = parts[3]
		}
		return target, nil
	}
	port, err := strconv.Atoi(parts[0])
	if err != nil || port <= 0 || port > 65535 {
		return target, fmt.Errorf("Bad request: invalid state port value")
	}
	target.port = port
	if len(parts) >= 3 {
		target.clientState = parts[2]
	}
	return target, nil
}

type googleProfile struct {
	Sub      string `json:"sub"`
	Email    string `json:"email"`
	Verified bool   `json:"email_verified"`
	Name     string `json:"name"`
}

// fetchGoogleProfile exchanges an authorization code and returns the
// account's verified profile.
func fetchGoogleProfile(code string) (googleProfile, int, error) {
	var profile googleProfile
	client := &http.Client{Timeout: 10 * time.Second}
	vals := url.Values{}
	vals.Set("code", code)
	vals.Set("client_id", config.GoogleClientID)
	vals.Set("client_secret", config.GoogleClientSecret)
	vals.Set("redirect_uri", fmt.Sprintf("%s/auth/callback", config.BackendURL))
	vals.Set("grant_type", "authorization_code")
	resp, err := client.PostForm("https://oauth2.googleapis.com/token", vals)
	if err != nil {
		log.Printf("[OAUTH ERROR] Token exchange request failed: %v", err)
		return profile, http.StatusInternalServerError, fmt.Errorf("Token exchange failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[OAUTH ERROR] Token exchange status %d", resp.StatusCode)
		return profile, http.StatusInternalServerError, fmt.Errorf("Token exchange failed")
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokens); err != nil {
		return profile, http.StatusInternalServerError, fmt.Errorf("Failed to parse token response")
	}
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	userResp, err := client.Do(req)
	if err != nil {
		log.Printf("[OAUTH ERROR] Failed to fetch profile: %v", err)
		return profile, http.StatusInternalServerError, fmt.Errorf("Failed to fetch user profile")
	}
	defer userResp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(userResp.Body, 1<<20)).Decode(&profile); err != nil {
		return profile, http.StatusInternalServerError, fmt.Errorf("Failed to parse profile response")
	}
	if userResp.StatusCode != 200 || !profile.Verified || profile.Sub == "" || profile.Email == "" {
		return profile, http.StatusForbidden, fmt.Errorf("A verified Google account is required")
	}
	if profile.Name == "" {
		profile.Name = strings.Split(profile.Email, "@")[0]
	}
	return profile, http.StatusOK, nil
}

// writeAccountNotice renders a small page for sign-in outcomes the browser
// shows directly. Both strings are fixed text supplied by the server.
func writeAccountNotice(w http.ResponseWriter, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>%s</title></head>
<body style="font-family: sans-serif; background-color: #ffffee; padding: 40px; text-align: center;">
<div style="background: white; border: 2px solid black; border-radius: 12px; padding: 30px; max-width: 500px; margin: 0 auto; box-shadow: 4px 4px 0px black;">
<h2 style="color: #c0392b;">🔒 %s</h2>
<p>%s</p>
</div>
</body>
</html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

func (a *App) HandleAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if !a.consumeOAuthState(w, r, state) {
		http.Error(w, "Invalid or expired sign-in state; please sign in again", 400)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	target, err := parseOAuthState(state)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	profile, status, err := fetchGoogleProfile(code)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	userID, err := a.DB.GetOrCreateUser(profile.Sub, profile.Email, profile.Name)
	if err != nil {
		log.Printf("[DB ERROR] GetOrCreateUser failed: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	locked, err := a.DB.IsUserLocked(userID)
	if err != nil {
		log.Printf("[DB ERROR] IsUserLocked failed: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if locked {
		writeAccountNotice(w, "Account Locked", "Your account has been locked. Please contact support for assistance.")
		return
	}
	isAdmin, has2FA, err := a.DB.GetUserAdminStatus(userID)
	if err != nil {
		log.Printf("[DB ERROR] GetUserAdminStatus failed: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if isAdmin && !has2FA {
		writeAccountNotice(w, "Access Denied", "This account is not allowed to login.")
		return
	}
	isTemp := 0
	if isAdmin {
		isTemp = 1
	}

	if target.exchange {
		code := auth.GenerateSessionToken()
		if e := a.issueAuthCode(code, userID, isTemp); e != nil {
			http.Error(w, "Sign in unavailable", 503)
			return
		}
		destination := target.redirectTo
		if !target.web {
			destination = fmt.Sprintf("http://127.0.0.1:%d/callback", target.port)
		}
		u, e := url.Parse(destination)
		if e != nil {
			http.Error(w, "Invalid return URL", 400)
			return
		}
		q := u.Query()
		q.Set("code", code)
		if target.clientState != "" {
			q.Set("client_state", target.clientState)
		}
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), 302)
		return
	}
	token := auth.GenerateSessionToken()
	if err = a.DB.CreateSession(token, userID, isTemp); err != nil {
		log.Printf("[DB ERROR] CreateSession failed: %v", err)
		http.Error(w, "Database session error", http.StatusInternalServerError)
		return
	}

	// Compatibility for installed desktop clients: credentials return only over
	// the caller's nonce-bound loopback callback. New clients use a one-time code.
	finalURL := fmt.Sprintf("http://127.0.0.1:%d/callback?token=%s&client_state=%s", target.port, token, url.QueryEscape(target.clientState))
	if isTemp == 1 {
		finalURL = fmt.Sprintf("http://127.0.0.1:%d/callback?temp_token=%s&require_2fa=true&client_state=%s", target.port, token, url.QueryEscape(target.clientState))
	}
	http.Redirect(w, r, finalURL, http.StatusFound)
}

// HandleAuthMe reads configuration from memory. Admin updates sync it at
// once (and across instances through Valkey); a background loop catches up
// every minute, so a database read per desktop poll is unnecessary.
func (a *App) HandleAuthMe(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	isPaid := user.PlanActive == 1 && user.PlanTier == "pro"
	var monthlyLimitSeconds float64
	cfg := config.ConfigManager.GetRawAll()
	if isPaid {
		monthlyLimitSeconds = 180000.0
		if val, ok := cfg["quota_paid_monthly_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		} else if val, ok := cfg["paid_monthly_limit_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	} else {
		monthlyLimitSeconds = 3600.0
		if val, ok := cfg["quota_free_monthly_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		} else if val, ok := cfg["free_monthly_limit_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	}

	notificationStr := ""
	if val, ok := cfg["notification"]; ok {
		notificationStr = val
	}

	commandUsage, usageErr := a.commandUsage(r.Context(), user.ID)
	if usageErr != nil {
		log.Printf("[COMMAND USAGE] Account %d: %v", user.ID, usageErr)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                     user.ID,
		"command_usage":          commandUsage,
		"email":                  user.Email,
		"name":                   user.Name,
		"plan_tier":              user.PlanTier,
		"plan_active":            user.PlanActive == 1,
		"plan_activated_at":      user.PlanActivatedAt,
		"duration_seconds":       user.MonthlyUsageSeconds,
		"monthly_limit_seconds":  monthlyLimitSeconds,
		"is_admin":               user.IsAdmin == 1,
		"languages":              user.Languages,
		"notification":           notificationStr,
		"transform_input_tokens": config.ConfigManager.GetTransformInputWordLimit(),
	})
}

func (a *App) HandleUpdateLanguages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req struct {
		Languages []string `json:"languages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	validated, err := utils.ValidateLanguages(req.Languages)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if err := a.DB.UpdateUserLanguages(user.ID, validated); err != nil {
		log.Printf("[DB ERROR] UpdateUserLanguages failed for user %d: %v", user.ID, err)
		http.Error(w, "Failed to update language preferences", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "success",
		"languages": validated,
	})
}

func (a *App) HandleGetUserHistory(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	logs, err := a.DB.GetUserUsageLogs(user.ID)
	if err != nil {
		log.Printf("[DB ERROR] GetUserUsageLogs failed for user %d: %v", user.ID, err)
		http.Error(w, "Failed to retrieve transcription history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}

func (a *App) HandleAuthLogout(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		_ = a.DB.DeleteSession(token)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "logged_out",
	})
}

type SignupRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	Name           string `json:"name"`
	TurnstileToken string `json:"cf-turnstile-response"`
}

func (a *App) HandleAuthSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req SignupRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if os.Getenv("TURNSTILE_SECRET") != "" {
		ip := auth.GetClientIP(r)
		if !auth.VerifyTurnstile(req.TurnstileToken, ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bot verification failed. Please complete the CAPTCHA."})
			return
		}
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	if req.Email == "" || req.Password == "" || req.Name == "" {
		http.Error(w, "Email, password, and name are required", http.StatusBadRequest)
		return
	}

	addr, err := mail.ParseAddress(req.Email)
	if err != nil || addr.Address != req.Email {
		http.Error(w, "Invalid email address format", http.StatusBadRequest)
		return
	}

	if err := auth.ValidatePasswordStrength(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ip := auth.GetClientIP(r)
	if !ratelimit.AuthRateLimiterInst.Allow("signup_ip:"+ip, 10, 1*time.Minute) || !ratelimit.AuthRateLimiterInst.Allow("signup_email:"+req.Email, 5, 1*time.Minute) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Too many signup requests. Please try again later."})
		return
	}

	verificationToken := auth.GenerateCryptoToken(32)

	userID, err := a.DB.CreateEmailUser(req.Email, req.Password, req.Name, verificationToken)
	if err != nil {
		log.Printf("[SIGNUP ERROR] Failed to register user (%s): %v", req.Email, err)
		http.Error(w, "Failed to register user. The email address may already be in use.", http.StatusConflict)
		return
	}

	err = services.SendVerificationEmail(req.Email, req.Name, verificationToken)
	if err != nil {
		log.Printf("ERROR: Failed to send verification email to %s: %v", req.Email, err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "verification_required",
		"message": "Account created! Please check your email to verify your address before logging in.",
		"user": map[string]interface{}{
			"id":          userID,
			"email":       req.Email,
			"name":        req.Name,
			"plan_tier":   "free",
			"plan_active": false,
		},
	})
}

type LoginRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	TurnstileToken string `json:"cf-turnstile-response"`
}

func (a *App) HandleAuthLoginCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req LoginRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if os.Getenv("TURNSTILE_SECRET") != "" {
		ip := auth.GetClientIP(r)
		if !auth.VerifyTurnstile(req.TurnstileToken, ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bot verification failed. Please complete the CAPTCHA."})
			return
		}
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if req.Email == "" || req.Password == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	ip := auth.GetClientIP(r)
	if !ratelimit.AuthRateLimiterInst.Allow("login_ip:"+ip, 10, 1*time.Minute) || !ratelimit.AuthRateLimiterInst.Allow("login_email:"+req.Email, 5, 1*time.Minute) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Too many login attempts. Please try again later."})
		return
	}

	userID, err := a.DB.AuthenticateEmailUser(req.Email, req.Password)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err.Error() == "ACCOUNT_LOCKED" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Account is locked. Please contact support.",
			})
		} else if err.Error() == "EMAIL_UNVERIFIED" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Email is not verified. Please check your email for the verification link.",
			})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid email or password",
			})
		}
		return
	}

	isAdmin, has2FA, err := a.DB.GetUserAdminStatus(userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if isAdmin && !has2FA {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "This account is not allowed to login.",
		})
		return
	}

	token := auth.GenerateSessionToken()
	isTemp := 0
	if isAdmin {
		isTemp = 1
	}

	err = a.DB.CreateSession(token, userID, isTemp)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	if isTemp == 1 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"require_2fa": true,
			"temp_token":  token,
			"email":       req.Email,
		})
		return
	}

	user, err := a.DB.GetUserBySession(token)
	if err != nil {
		http.Error(w, "Failed to load user info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":          user.ID,
			"email":       user.Email,
			"name":        user.Name,
			"plan_tier":   user.PlanTier,
			"plan_active": user.PlanActive == 1,
			"is_admin":    user.IsAdmin == 1,
		},
	})
}

func (a *App) HandleAuth2FAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req struct {
		TempToken string `json:"temp_token"`
		Code      string `json:"code"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.TempToken = strings.TrimSpace(req.TempToken)
	req.Code = strings.TrimSpace(req.Code)

	if req.TempToken == "" || req.Code == "" {
		http.Error(w, "temp_token and code are required", http.StatusBadRequest)
		return
	}

	ip := auth.GetClientIP(r)
	if !ratelimit.AuthRateLimiterInst.Allow("2fa_ip:"+ip, 5, 1*time.Minute) || !ratelimit.AuthRateLimiterInst.Allow("2fa_token:"+req.TempToken, 5, 1*time.Minute) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Too many 2FA attempts. Please try again later."})
		return
	}

	tempUser, totpSecret, err := a.DB.GetUserByTempSession(req.TempToken)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err.Error() == "ACCOUNT_LOCKED" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Account is locked. Please contact support.",
			})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid or expired temporary session token",
			})
		}
		return
	}

	if totpSecret == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "This account is not allowed to login.",
		})
		return
	}

	step, valid := auth.VerifyTOTPAt(totpSecret, req.Code, time.Now())
	if valid {
		claimed, claimErr := a.DB.ClaimTOTPStep(tempUser.ID, step)
		if claimErr != nil {
			http.Error(w, "Failed to verify code", http.StatusInternalServerError)
			return
		}
		// An observed code cannot be replayed within its validity window.
		valid = claimed
	}
	if !valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid 2FA verification code",
		})
		return
	}

	err = a.DB.PromoteSession(req.TempToken)
	if err != nil {
		http.Error(w, "Failed to promote session", http.StatusInternalServerError)
		return
	}

	fullProfile, err := a.DB.GetUserBySession(req.TempToken)
	if err != nil {
		http.Error(w, "Failed to load user info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token": req.TempToken,
		"user": map[string]interface{}{
			"id":          fullProfile.ID,
			"email":       fullProfile.Email,
			"name":        fullProfile.Name,
			"plan_tier":   fullProfile.PlanTier,
			"plan_active": fullProfile.PlanActive == 1,
			"is_admin":    fullProfile.IsAdmin == 1,
		},
	})
}

func (a *App) HandleAuthVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Bad request: missing verification token", http.StatusBadRequest)
		return
	}

	email, err := a.DB.VerifyUserEmail(token)
	if err != nil {
		log.Printf("[VERIFICATION ERROR] Email verification failed: %v", err)
		http.Error(w, "Verification failed: invalid or expired token", http.StatusBadRequest)
		return
	}

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:8080"
	}

	redirectURL := fmt.Sprintf("%s/dashboard?verified=true&email=%s", frontendURL, url.QueryEscape(email))

	escapedEmail := html.EscapeString(email)
	escapedRedirectURL := html.EscapeString(redirectURL)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Account Verified</title>
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<style>
		body {
			margin: 0; padding: 0;
			font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
			background: linear-gradient(135deg, #ffffee 0%%, #fffdf9 100%%);
			color: #1a1a1a;
			display: flex; justify-content: center; align-items: center;
			min-height: 100vh;
		}
		.card {
			background: #fffdf9;
			border: 2px solid #1a1a1a;
			border-radius: 12px;
			padding: 35px;
			width: 400px;
			text-align: center;
			box-shadow: 4px 4px 0px #1a1a1a;
		}
		h2 { margin: 0 0 10px 0; color: #034f46; }
		p { color: rgba(26, 26, 26, 0.8); font-size: 14px; margin-bottom: 25px; }
		.btn {
			display: inline-block;
			background: #f0d7ff;
			border: 2px solid #1a1a1a;
			border-radius: 6px;
			color: #1a1a1a;
			padding: 10px 20px;
			text-decoration: none;
			font-size: 14.5px;
			font-weight: bold;
			box-shadow: 2px 2px 0px #1a1a1a;
			transition: all 0.15s ease;
		}
		.btn:hover { background: #e2c1fb; transform: translate(-1px, -1px); box-shadow: 3px 3px 0px #1a1a1a; }
	</style>
	<meta http-equiv="refresh" content="3;url=%s">
</head>
<body>
	<div class="card">
		<h2>🎉 Email Verified!</h2>
		<p>Thank you. Your email address <strong>%s</strong> has been successfully verified.</p>
		<p>Redirecting you to the SpeekTo.Me Dashboard in 3 seconds...</p>
		<a class="btn" href="%s">Go to Dashboard</a>
	</div>
</body>
</html>`, escapedRedirectURL, escapedEmail, escapedRedirectURL)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlContent))
}

func (a *App) HandleGetProfileSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	summary, err := a.DB.GetUserProfileSummary(user.ID)
	if err != nil {
		log.Printf("[DB ERROR] GetUserProfileSummary failed for user %d: %v", user.ID, err)
		http.Error(w, "Failed to retrieve user profile summary", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

type ForgotPasswordRequest struct {
	Email          string `json:"email"`
	TurnstileToken string `json:"cf-turnstile-response"`
}

func (a *App) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req ForgotPasswordRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	targetEmail := strings.TrimSpace(strings.ToLower(req.Email))

	// If authenticated user didn't specify an email, fall back to current user's email
	if targetEmail == "" {
		if authUser, authErr := auth.Authenticate(r, a.DB); authErr == nil && authUser != nil {
			targetEmail = strings.TrimSpace(strings.ToLower(authUser.Email))
		}
	}

	if targetEmail == "" {
		http.Error(w, "Email address is required", http.StatusBadRequest)
		return
	}

	if os.Getenv("TURNSTILE_SECRET") != "" {
		ip := auth.GetClientIP(r)
		if !auth.VerifyTurnstile(req.TurnstileToken, ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bot verification failed. Please complete the CAPTCHA."})
			return
		}
	}

	ip := auth.GetClientIP(r)
	if !ratelimit.AuthRateLimiterInst.Allow("forgot_ip:"+ip, 5, 5*time.Minute) || !ratelimit.AuthRateLimiterInst.Allow("forgot_email:"+targetEmail, 5, 5*time.Minute) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Too many reset attempts. Please try again in a few minutes."})
		return
	}

	resetToken := auth.GenerateCryptoToken(32)
	expiresAt := time.Now().Add(1 * time.Hour)

	name, err := a.DB.SetPasswordResetToken(targetEmail, resetToken, expiresAt)
	if err == nil {
		if name == "" {
			name = strings.Split(targetEmail, "@")[0]
		}
		if sendErr := services.SendPasswordResetEmail(targetEmail, name, resetToken); sendErr != nil {
			log.Printf("[EMAIL ERROR] Failed to send password reset email to %s: %v", targetEmail, sendErr)
		}
	} else {
		// Log but do not reveal user existence to caller
		log.Printf("[PASSWORD RESET] User lookup failed for %s: %v", targetEmail, err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "If an account with this email exists, a password reset link has been sent.",
	})
}

type ResetPasswordRequest struct {
	Token          string `json:"token"`
	Password       string `json:"password"`
	TurnstileToken string `json:"cf-turnstile-response"`
}

func (a *App) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req ResetPasswordRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.Token = strings.TrimSpace(req.Token)
	req.Password = strings.TrimSpace(req.Password)

	if req.Token == "" || req.Password == "" {
		http.Error(w, "Token and new password are required", http.StatusBadRequest)
		return
	}

	if os.Getenv("TURNSTILE_SECRET") != "" {
		ip := auth.GetClientIP(r)
		if !auth.VerifyTurnstile(req.TurnstileToken, ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bot verification failed. Please complete the CAPTCHA."})
			return
		}
	}

	if err := auth.ValidatePasswordStrength(req.Password); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	if err := a.DB.ResetPasswordWithToken(req.Token, string(hashedPassword)); err != nil {
		log.Printf("[PASSWORD RESET ERROR] Failed to reset password with token: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Your password has been successfully updated. You may now log in.",
	})
}

func (a *App) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" && r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	release, lockErr := a.lockBilling(user.ID)
	if lockErr != nil {
		http.Error(w, "Billing operation in progress; please retry", 409)
		return
	}
	defer release()
	if e := a.DB.GetDB().QueryRow("SELECT COALESCE(razorpay_subscription_id,'') FROM users WHERE id=?", user.ID).Scan(&user.RazorpaySubscriptionID); e != nil {
		http.Error(w, "Account unavailable", 503)
		return
	}
	if err := a.cancelProviderSubscription(user.RazorpaySubscriptionID); err != nil {
		http.Error(w, "Cannot confirm billing cancellation. Your account has been retained; please retry.", 502)
		return
	}
	if err := a.DB.DeleteUserAccount(user.ID); err != nil {
		log.Printf("[DB ERROR] DeleteUserAccount failed for user %d: %v", user.ID, err)
		http.Error(w, "Failed to delete account. Please contact support.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "deleted",
		"message": "Account and all associated data have been permanently deleted.",
	})
}
