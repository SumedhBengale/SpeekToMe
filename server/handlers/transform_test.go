package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"speektome-server/config"
	"strings"
	"testing"

	"speektome-server/auth"
	"speektome-server/db"
)

func TestHandleTransformValidation(t *testing.T) {
	databasePath := t.TempDir() + string(os.PathSeparator) + "test_transform.db"
	t.Setenv("SESSION_SECRET", "test-transform-secret")

	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("NewDatabase failed: %v", err)
	}
	defer database.Close()

	app := &App{DB: database}

	userID, err := database.CreateEmailUser("transform@example.com", "Password123", "Transform User", "token")
	if err != nil {
		t.Fatalf("CreateEmailUser: %v", err)
	}

	if _, err := database.GetDB().Exec("UPDATE users SET plan_tier='pro',plan_active=1 WHERE id=?", userID); err != nil {
		t.Fatal(err)
	}

	sessionToken := auth.GenerateSessionToken()
	if err := database.CreateSession(sessionToken, userID, 30); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// 1. Unauthenticated test
	req := httptest.NewRequest("POST", "/v1/transform", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	app.HandleTransform(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
	}

	// 2. Both selected_text and command empty -> 400 Bad Request
	body, _ := json.Marshal(map[string]string{
		"selected_text": "",
		"command":       "",
	})
	req = httptest.NewRequest("POST", "/v1/transform", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w = httptest.NewRecorder()
	app.HandleTransform(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for both empty, got %d", w.Code)
	}

	// 3. Missing command with valid selected_text should pass validation (defaults to polish, not 400 Bad Request)
	body, _ = json.Marshal(map[string]string{
		"selected_text": "Hey there, fix my grammar",
		"command":       "",
	})
	req = httptest.NewRequest("POST", "/v1/transform", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w = httptest.NewRecorder()
	app.HandleTransform(w, req)
	if w.Code == http.StatusBadRequest {
		t.Errorf("Expected empty command with valid selected_text to pass validation, but got 400: %s", w.Body.String())
	}

	// 4. Text longer than dynamic word limit (default 10,000 words) -> 400 Bad Request
	longText := strings.Repeat("word ", 10001)
	body, _ = json.Marshal(map[string]string{
		"selected_text": longText,
		"command":       "Summarize",
	})
	req = httptest.NewRequest("POST", "/v1/transform", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w = httptest.NewRecorder()
	app.HandleTransform(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for text > 10,000 words, got %d", w.Code)
	}

	// 5. Empty selected_text with valid command should pass request validation (not 400 Bad Request)
	body, _ = json.Marshal(map[string]string{
		"selected_text": "",
		"command":       "Write an email",
	})
	req = httptest.NewRequest("POST", "/v1/transform", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w = httptest.NewRecorder()
	app.HandleTransform(w, req)
	if w.Code == http.StatusBadRequest {
		t.Errorf("Expected empty selected_text to pass validation, but got 400 Bad Request: %s", w.Body.String())
	}
}

func TestTransformNeverStreamsModelErrorsAsText(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			a := limitApp(t)
			mockCommand(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				marker := `{"speektome_error":{"code":"missing_input","private":"PRIVATE SOURCE"}}`
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": marker}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			})
			cfg := config.ConfigManager.Get()
			cfg.Models.CommandModels[0].Stream = true
			config.ConfigManager.Set(cfg, config.ConfigManager.GetRawAll())
			w := transformRequest(a, "user1", "/v1/transform", stream)
			if w.Code != 422 || !strings.Contains(w.Body.String(), `"code":"missing_input"`) || strings.Contains(w.Body.String(), "PRIVATE SOURCE") || strings.Contains(w.Body.String(), "data:") || strings.Contains(w.Body.String(), "speektome_error") {
				t.Fatalf("model output escaped: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
