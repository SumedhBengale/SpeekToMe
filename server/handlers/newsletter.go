package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"speektome-server/auth"
)

func (a *App) HandleNewsletterSubscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	var req struct {
		Email          string `json:"email"`
		TurnstileToken string `json:"cf-turnstile-response"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if os.Getenv("TURNSTILE_SECRET") != "" {
		ip := auth.GetClientIP(r)
		if !auth.VerifyTurnstile(req.TurnstileToken, ip) {
			http.Error(w, "Turnstile verification failed", http.StatusForbidden)
			return
		}
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		http.Error(w, "Valid email address is required", http.StatusBadRequest)
		return
	}

	if err := a.DB.AddNewsletterSubscriber(email); err != nil {
		log.Printf("[DB ERROR] Failed to save newsletter subscription for %s: %v", email, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Subscribed successfully",
	})
}
