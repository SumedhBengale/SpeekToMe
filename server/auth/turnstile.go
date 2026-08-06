package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"time"
)

type TurnstileResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	Action      string   `json:"action"`
	Cdata       string   `json:"cdata"`
}

// VerifyTurnstile calls the canonical Cloudflare Turnstile siteverify endpoint.
func VerifyTurnstile(token string, remoteIP string) bool {
	secret := os.Getenv("TURNSTILE_SECRET")
	if secret == "" || token == "" {
		return false
	}

	client := &http.Client{Timeout: 5 * time.Second}
	data := url.Values{}
	data.Set("secret", secret)
	data.Set("response", token)
	if remoteIP != "" {
		data.Set("remoteip", remoteIP)
	}

	resp, err := client.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", data)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var res TurnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false
	}

	return res.Success
}
