package handlers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"speektome-server/auth"
)

const Server2BatchASRURL = "http://BATCH_ASR_HOST_IP:8080/v1/audio/transcriptions"

// HandleServer2BatchTranscription handles OpenAI-compatible batch audio transcription requests
// by authenticating the user and proxying directly to Server 2's Parakeet ASR engine.
func (a *App) HandleServer2BatchTranscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, authErr := auth.Authenticate(r, a.DB)
	if authErr != nil {
		http.Error(w, authErr.Error(), http.StatusUnauthorized)
		return
	}

	// Create outgoing request to Server 2
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	outReq, err := http.NewRequestWithContext(r.Context(), "POST", Server2BatchASRURL, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to construct backend request: %v", err), http.StatusInternalServerError)
		return
	}

	// Forward Content-Type (multipart boundary)
	if ct := r.Header.Get("Content-Type"); ct != "" {
		outReq.Header.Set("Content-Type", ct)
	}

	resp, err := client.Do(outReq)
	if err != nil {
		log.Printf("[BATCH ASR PROXY ERROR] Failed to forward to Server 2: %v", err)
		http.Error(w, fmt.Sprintf("Inference engine unavailable: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
