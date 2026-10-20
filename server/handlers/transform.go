package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"speektome-server/accountlimits"
	log "speektome-server/privacylog"
	"strings"
	"time"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/modelresult"
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

	// Command Mode is a Pro feature in the client; enforce that entitlement here too.
	var anchor, tier string
	var active int
	var userRPM sql.NullInt64
	var billingStart, billingEnd int64
	if err := a.DB.GetDB().QueryRowContext(r.Context(), "SELECT COALESCE(plan_activated_at,created_at,''),COALESCE(plan_tier,'free'),COALESCE(plan_active,0),COALESCE(razorpay_current_start,0),COALESCE(razorpay_current_end,0),rate_limit_rpm FROM users WHERE id=?", user.ID).Scan(&anchor, &tier, &active, &billingStart, &billingEnd, &userRPM); err != nil {
		accountlimits.Write(w, accountlimits.Unavailable())
		return
	}
	if tier != "pro" || active != 1 {
		accountlimits.Write(w, &accountlimits.Failure{Code: "command_pro_required", Message: "Command Mode requires an active Pro subscription.", Status: http.StatusForbidden})
		return
	}
	rpm := int(configuredLimit("ratelimit_command_rpm", 10))
	if userRPM.Valid && userRPM.Int64 < int64(rpm) {
		rpm = int(userRPM.Int64)
	}
	if !a.accountRate(w, r, user.ID, "transform", rpm) {
		return
	}
	store := a.limits()
	owner, err := store.Acquire(r.Context(), user.ID, "transform", 3*time.Minute)
	if err != nil {
		accountlimits.Write(w, err)
		return
	}
	defer store.Release(user.ID, "transform", owner)
	// A total deadline keeps the lease longer than every provider fallback combined.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	period, reset := accountlimits.BillingPeriod(anchor, billingStart, billingEnd, time.Now())
	ctx = pipeline.WithCommandBudget(ctx, &commandBudget{store: store, user: user.ID, period: period, reset: reset})

	r = r.WithContext(ctx)
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	if deadline, ok := ctx.Deadline(); ok {
		_ = controller.SetWriteDeadline(deadline)
		defer controller.SetWriteDeadline(time.Time{})
	}

	var req TransformRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
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
		started := false
		startStream := func() {
			if !started {
				started = true
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("X-Accel-Buffering", "no")
			}
		}

		// Hold provider deltas until the complete result has passed the error
		// contract. No partial refusal, error JSON or failed attempt may reach
		// the desktop's typing/clipboard path.
		transformed, err := pipeline.RunCommandTransformationStream(ctx, http.DefaultClient, selectedText, command, pctx, func(string, string) error { return nil })

		if err != nil {
			if limitStatus(err) != http.StatusBadGateway {
				accountlimits.Write(w, err)
			} else {
				modelresult.Write(w, err, "command")
			}
			return
		}

		log.Printf("[TRANSFORM STREAM SUCCESS] User %d | Input: %d chars | Output: %d chars | Model: %s | Provider: %s",
			user.ID, len(selectedText), len(transformed), pctx.LLMModel, pctx.LLMProvider)

		startStream()
		chunkBytes, _ := json.Marshal(map[string]interface{}{"delta": transformed, "text": transformed, "done": false})
		fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
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

	transformed, err := pipeline.RunCommandTransformation(ctx, http.DefaultClient, selectedText, command, pctx)
	if err != nil {
		log.Printf("[TRANSFORM ERROR] User %d failed transformation: %v", user.ID, err)
		if limitStatus(err) != http.StatusBadGateway {
			accountlimits.Write(w, err)
		} else {
			modelresult.Write(w, err, "command")
		}
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
