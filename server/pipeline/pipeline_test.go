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

	enhancer := NewAIEnhancementTransform("custom prompt", "deepseek-v4-flash", "test-key", "https://api.deepseek.com", []string{"SpeekToMe"})
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
	res, loc, err := callChatCompletionsAPI(context.Background(), client, mockServer.URL, "test-cerebras-key", "gpt-oss-120b", "System Prompt", "User Prompt", false, false, false)
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
	res, loc, err := callChatCompletionsAPI(context.Background(), client, mockServer.URL, "test-wafer-key", "DeepSeek-V4-Flash-0731-Fast", "System Prompt", "User Prompt", true, false, false)
	if err != nil {
		t.Fatalf("callChatCompletionsAPI for Wafer failed: %v", err)
	}
	if res != "Wafer polished transcript." {
		t.Errorf("Unexpected result: %q", res)
	}
	_ = loc
}

func TestChatCompletionsStreamMock(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-stream-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if stream, ok := payload["stream"].(bool); !ok || !stream {
			http.Error(w, "stream must be true", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		chunks := []string{"Hello", " world", " from", " stream!"}
		for _, c := range chunks {
			chunkJSON, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"delta": map[string]string{
							"content": c,
						},
					},
				},
			})
			w.Write([]byte("data: " + string(chunkJSON) + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer mockServer.Close()

	client := mockServer.Client()
	var collectedDeltas []string
	var lastAccumulated string

	res, _, err := callChatCompletionsAPIStream(
		context.Background(),
		client,
		mockServer.URL,
		"test-stream-key",
		"deepseek-v4-flash",
		"System Prompt",
		"User Prompt",
		true,
		false,
		false,
		func(delta, accumulated string) error {
			collectedDeltas = append(collectedDeltas, delta)
			lastAccumulated = accumulated
			return nil
		},
	)

	if err != nil {
		t.Fatalf("callChatCompletionsAPIStream failed: %v", err)
	}

	expected := "Hello world from stream!"
	if res != expected {
		t.Errorf("Expected full text %q, got %q", expected, res)
	}
	if lastAccumulated != expected {
		t.Errorf("Expected lastAccumulated %q, got %q", expected, lastAccumulated)
	}
	if len(collectedDeltas) != 4 {
		t.Errorf("Expected 4 chunks, got %d", len(collectedDeltas))
	}
}

func TestBedrockNemotronSuper120BStreamingMock(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-bedrock-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["model"] != "nvidia.nemotron-super-3-120b" {
			http.Error(w, "Model mismatch", http.StatusBadRequest)
			return
		}
		if payload["stream"] != true {
			http.Error(w, "Expected stream=true", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		chunks := []string{"Nemotron ", "Super ", "120B ", "streamed!"}
		for _, c := range chunks {
			chunkJSON, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"delta": map[string]string{
							"content": c,
						},
					},
				},
			})
			w.Write([]byte("data: " + string(chunkJSON) + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer mockServer.Close()

	client := mockServer.Client()
	var deltas []string

	res, _, err := callChatCompletionsAPIStream(
		context.Background(),
		client,
		mockServer.URL,
		"test-bedrock-token",
		"nvidia.nemotron-super-3-120b",
		"System Prompt",
		"Target Text",
		false,
		false,
		false,
		func(delta, accumulated string) error {
			deltas = append(deltas, delta)
			return nil
		},
	)

	if err != nil {
		t.Fatalf("callChatCompletionsAPIStream failed for Nemotron: %v", err)
	}

	expected := "Nemotron Super 120B streamed!"
	if res != expected {
		t.Errorf("Expected result %q; got %q", expected, res)
	}
	if len(deltas) != 4 {
		t.Errorf("Expected 4 chunks; got %d", len(deltas))
	}
}

func TestDeepInfraPriorityFlagMock(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["priority"] != true {
			http.Error(w, "Expected priority=true", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "DeepInfra priority response.",
					},
				},
			},
		})
	}))
	defer mockServer.Close()

	client := mockServer.Client()
	res, _, err := callChatCompletionsAPI(context.Background(), client, mockServer.URL, "test-key", "deepseek-ai/DeepSeek-V4-Flash", "System Prompt", "User Prompt", false, false, true)
	if err != nil {
		t.Fatalf("callChatCompletionsAPI failed with priority flag: %v", err)
	}
	if res != "DeepInfra priority response." {
		t.Errorf("Unexpected result: %q", res)
	}
}
