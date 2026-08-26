package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPipelineContext(t *testing.T) {
	pctx := NewPipelineContext(42)
	if pctx.UserID != 42 {
		t.Errorf("Expected UserID 42; got %d", pctx.UserID)
	}

	enhancer := NewAIEnhancementTransform("custom prompt", "deepseek-v4-flash", "test-key", "https://api.deepseek.com")
	if enhancer.Name() != "AI Enhancement" {
		t.Errorf("Expected name 'AI Enhancement'; got %s", enhancer.Name())
	}
}

func TestCerebrasChatCompletionMock(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-cerebras-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "This is polished transcription output.",
					},
				},
			},
		})
	}))
	defer mockServer.Close()

	client := mockServer.Client()
	res, loc, err := callChatCompletionsAPI(context.Background(), client, mockServer.URL, "test-cerebras-key", "gpt-oss-120b", "System Prompt", "User Prompt", false)
	if err != nil {
		t.Fatalf("callChatCompletionsAPI failed: %v", err)
	}
	if res != "This is polished transcription output." {
		t.Errorf("Unexpected result: %q", res)
	}
	_ = loc
}

func TestWaferChatCompletionMock(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-wafer-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if thinking, ok := payload["thinking"].(map[string]interface{}); !ok || thinking["type"] != "disabled" {
			http.Error(w, "Missing thinking disabled parameter", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "Wafer polished transcript.",
					},
				},
			},
		})
	}))
	defer mockServer.Close()

	client := mockServer.Client()
	res, loc, err := callChatCompletionsAPI(context.Background(), client, mockServer.URL, "test-wafer-key", "DeepSeek-V4-Flash-0731-Fast", "System Prompt", "User Prompt", true)
	if err != nil {
		t.Fatalf("callChatCompletionsAPI for Wafer failed: %v", err)
	}
	if res != "Wafer polished transcript." {
		t.Errorf("Unexpected result: %q", res)
	}
	_ = loc
}
