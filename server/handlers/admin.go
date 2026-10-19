package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/ratelimit"
	"speektome-server/valkey"
)

func (a *App) HandleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}
	cfg := config.ConfigManager.GetRawAll()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (a *App) HandleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	for k, v := range req {
		if err := a.DB.UpdateSaasConfig(k, v); err != nil {
			log.Printf("[ADMIN CONFIG ERROR] Failed to update key %s: %v", k, err)
			http.Error(w, "Failed to update configuration key", http.StatusInternalServerError)
			return
		}
	}
	config.SyncConfigsFromDB(a.DB, ratelimit.GroqRateLimiter.SetLimit)
	if valkey.GlobalClient != nil && valkey.GlobalClient.IsConnected() {
		_ = valkey.GlobalClient.PublishConfigReload(r.Context())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (a *App) HandleAdminGetRateLimits(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}
	counts := ratelimit.GlobalUserRateLimiter.GetCounts()
	response := make(map[string]int)
	for k, v := range counts {
		response[fmt.Sprintf("user_%d", k)] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (a *App) HandleAdminDeleteRateLimits(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr != "" {
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid user_id parameter", http.StatusBadRequest)
			return
		}
		ratelimit.GlobalUserRateLimiter.ResetUser(userID)
		if err := a.limits().ResetRates(r.Context(), userID); err != nil {
			http.Error(w, "Failed to reset account rate limits", 503)
			return
		}
		log.Printf("[ADMIN ACTION] Reset rate limit cache for user %d", userID)
	} else {
		ratelimit.GlobalUserRateLimiter.ResetAll()
		if err := a.limits().ResetRates(r.Context(), 0); err != nil {
			http.Error(w, "Failed to reset account rate limits", 503)
			return
		}
		log.Println("[ADMIN ACTION] Reset all rate limit caches")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

func (a *App) HandleAdminGetUsers(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}

	users, err := a.DB.GetAdminUsersList()
	if err != nil {
		log.Printf("[DB ERROR] Failed to load user list: %v", err)
		http.Error(w, "Failed to load user list", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}

func (a *App) HandleAdminUpdatePlan(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}

	idStr := r.PathValue("id")
	targetUserID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req struct {
		PlanTier   string `json:"plan_tier"`
		PlanActive int    `json:"plan_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	err = a.DB.AdminUpdateUserPlan(targetUserID, req.PlanTier, req.PlanActive)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update plan: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (a *App) HandleAdminUpdateRateLimit(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}

	idStr := r.PathValue("id")
	targetUserID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req struct {
		RateLimitRPM *int `json:"rate_limit_rpm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	err = a.DB.AdminUpdateUserRateLimit(targetUserID, req.RateLimitRPM)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update rate limit: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (a *App) HandleAdminGetWebhookLogs(w http.ResponseWriter, r *http.Request) {
	_, err := auth.AuthenticateAdmin(r, a.DB)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		}
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	logs, err := a.DB.GetWebhookLogs(limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch webhook logs: %v", err), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []db.WebhookLogEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}
