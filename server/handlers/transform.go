package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/pipeline"
)

type TransformRequest struct {
	SelectedText string `json:"selected_text"`
	Command      string `json:"command"`
	Stream       bool   `json:"stream,omitempty"`
}

type TransformResponse struct {
	Text     string `json:"text"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// HandleTransform handles POST /v1/transform and POST /transform for Command / Transform mode
func (a *App) HandleTransform(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, authErr := auth.Authenticate(r, a.DB)
	if authErr != nil {
		http.Error(w, authErr.Error(), http.StatusUnauthorized)
		return
	}

	var req TransformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	selectedText := strings.TrimSpace(req.SelectedText)
	command := strings.TrimSpace(req.Command)

	if selectedText == "" && command == "" {
		http.Error(w, "Request cannot be empty: provide selected_text or command", http.StatusBadRequest)
		return
	}

	if command == "" {
		command = "Fix grammar, improve clarity, and polish the text"
	}

	maxWords := config.ConfigManager.GetTransformInputWordLimit()
	selectedWords := len(strings.Fields(selectedText))
	commandWords := len(strings.Fields(command))
	totalWords := selectedWords + commandWords

	if totalWords > maxWords || selectedWords > maxWords || commandWords > maxWords {
		http.Error(w, fmt.Sprintf("Request exceeds maximum limit of %d words (received %d words)", maxWords, totalWords), http.StatusBadRequest)
		return
	}

	pctx := &pipeline.PipelineContext{
		UserID: user.ID,
	}

	isStream := req.Stream || strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	flusher, canFlush := w.(http.Flusher)

	if isStream && canFlush {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher.Flush()

		transformed, err := pipeline.RunCommandTransformationStream(r.Context(), http.DefaultClient, selectedText, command, pctx, func(delta, accumulated string) error {
			if delta == "[RESET]" {
				chunkBytes, _ := json.Marshal(map[string]interface{}{
					"reset": true,
					"delta": "",
					"text":  "",
					"done":  false,
				})
				_, writeErr := fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
				if writeErr != nil {
					return writeErr
				}
				flusher.Flush()
				return nil
			}

			chunkBytes, _ := json.Marshal(map[string]interface{}{
				"delta": delta,
				"text":  accumulated,
				"done":  false,
			})
			_, writeErr := fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
			if writeErr != nil {
				return writeErr
			}
			flusher.Flush()
			return nil
		})

		if err != nil {
			log.Printf("[TRANSFORM ERROR] User %d failed transformation: %v", user.ID, err)
			errBytes, _ := json.Marshal(map[string]interface{}{
				"error": err.Error(),
				"done":  true,
			})
			fmt.Fprintf(w, "data: %s\n\n", string(errBytes))
			flusher.Flush()
			return
		}

		log.Printf("[TRANSFORM STREAM SUCCESS] User %d | Input: %d chars | Output: %d chars | Model: %s | Provider: %s",
			user.ID, len(selectedText), len(transformed), pctx.LLMModel, pctx.LLMProvider)

		finalBytes, _ := json.Marshal(map[string]interface{}{
			"delta":    "",
			"text":     transformed,
			"done":     true,
			"model":    pctx.LLMModel,
			"provider": pctx.LLMProvider,
		})
		fmt.Fprintf(w, "data: %s\n\n", string(finalBytes))
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	transformed, err := pipeline.RunCommandTransformation(r.Context(), http.DefaultClient, selectedText, command, pctx)
	if err != nil {
		log.Printf("[TRANSFORM ERROR] User %d failed transformation: %v", user.ID, err)
		http.Error(w, fmt.Sprintf("Transformation failed: %v", err), http.StatusBadGateway)
		return
	}

	log.Printf("[TRANSFORM SUCCESS] User %d | Input: %d chars | Output: %d chars | Model: %s | Provider: %s",
		user.ID, len(selectedText), len(transformed), pctx.LLMModel, pctx.LLMProvider)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TransformResponse{
		Text:     transformed,
		Model:    pctx.LLMModel,
		Provider: pctx.LLMProvider,
	})
}
