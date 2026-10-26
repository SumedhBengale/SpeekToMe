package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"speektome-server/config"
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

func TestRunDeepSeekChatCompletionFallback(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origCerebrasKey := config.CerebrasAPIKey
	origDeepInfraKey := config.DeepInfraAPIKey
	config.CerebrasAPIKey = "mock-cerebras-key"
	config.DeepInfraAPIKey = "mock-deepinfra-key"
	defer func() {
		config.CerebrasAPIKey = origCerebrasKey
		config.DeepInfraAPIKey = origDeepInfraKey
	}()

	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()

	// Server 1 (Cerebras) fails with 503
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
	}))
	defer srv1.Close()

	// Server 2 (DeepInfra) succeeds
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "Polished text from candidate 2",
					},
				},
			},
		})
	}))
	defer srv2.Close()

	testCfg := origCfg
	testCfg.Models.EnhancementModels = []config.ModelSpec{
		{
			Provider:         config.ProviderCerebras,
			Model:            "gpt-oss-120b",
			Enabled:          true,
			EndpointOverride: srv1.URL,
			TimeoutMs:        2000,
		},
		{
			Provider:         config.ProviderDeepInfra,
			Model:            "deepseek-ai/DeepSeek-V4-Flash",
			Enabled:          true,
			EndpointOverride: srv2.URL,
			TimeoutMs:        2000,
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	pctx := NewPipelineContext(101)
	res, err := RunDeepSeekChatCompletion(context.Background(), srv2.Client(), "", "system prompt", "raw transcript", pctx)
	if err != nil {
		t.Fatalf("Expected fallback success, got error: %v", err)
	}

	if res != "Polished text from candidate 2" {
		t.Errorf("Expected candidate 2 output, got %q", res)
	}
	if pctx.LLMProvider != config.ProviderDeepInfra {
		t.Errorf("Expected LLMProvider deepinfra, got %q", pctx.LLMProvider)
	}
	if pctx.LLMModel != "deepseek-ai/DeepSeek-V4-Flash" {
		t.Errorf("Expected LLMModel deepseek-ai/DeepSeek-V4-Flash, got %q", pctx.LLMModel)
	}
}

func TestRunCommandTransformationStreamMidStreamReset(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origCerebrasKey := config.CerebrasAPIKey
	origDeepInfraKey := config.DeepInfraAPIKey
	config.CerebrasAPIKey = "mock-cerebras-key"
	config.DeepInfraAPIKey = "mock-deepinfra-key"
	defer func() {
		config.CerebrasAPIKey = origCerebrasKey
		config.DeepInfraAPIKey = origDeepInfraKey
	}()

	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()

	// Server 1 emits 1 chunk, then abruptly disconnects / fails
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		chunkJSON, _ := json.Marshal(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": "Partial bad chunk"}},
			},
		})
		w.Write([]byte("data: " + string(chunkJSON) + "\n\n"))
		flusher.Flush()
		// Abruptly terminate connection by hijacking or returning error
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer srv1.Close()

	// Server 2 emits clean complete response
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		chunkJSON, _ := json.Marshal(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": `{ "speektome_result": {"text":"Clean complete command output"}}`}},
			},
		})
		w.Write([]byte("data: " + string(chunkJSON) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv2.Close()

	testCfg := origCfg
	testCfg.Models.CommandModels = []config.ModelSpec{
		{
			Provider:         config.ProviderCerebras,
			Model:            "gpt-oss-120b",
			Enabled:          true,
			Stream:           true,
			EndpointOverride: srv1.URL,
			TimeoutMs:        2000,
		},
		{
			Provider:         config.ProviderDeepInfra,
			Model:            "deepseek-ai/DeepSeek-V4-Flash",
			Enabled:          true,
			Stream:           true,
			EndpointOverride: srv2.URL,
			TimeoutMs:        2000,
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	var deltas []string
	pctx := NewPipelineContext(102)
	res, err := RunCommandTransformationStream(
		context.Background(),
		srv2.Client(),
		"raw text",
		"format this",
		pctx,
		func(delta, accumulated string) error {
			deltas = append(deltas, delta)
			return nil
		},
	)

	if err != nil {
		t.Fatalf("Expected stream fallback success, got error: %v", err)
	}

	if res != "Clean complete command output" {
		t.Errorf("Expected final result 'Clean complete command output', got %q", res)
	}

	if len(deltas) != 1 || deltas[0] != "Clean complete command output" {
		t.Errorf("unvalidated provider output leaked: %v", deltas)
	}
}

func TestCircuitBreakerFastFailInPipeline(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origCerebrasKey := config.CerebrasAPIKey
	origDeepInfraKey := config.DeepInfraAPIKey
	config.CerebrasAPIKey = "mock-cerebras-key"
	config.DeepInfraAPIKey = "mock-deepinfra-key"
	defer func() {
		config.CerebrasAPIKey = origCerebrasKey
		config.DeepInfraAPIKey = origDeepInfraKey
	}()

	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()

	var srv1Hits int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&srv1Hits, 1)
		w.Header().Set("Retry-After", "10")
		http.Error(w, "Rate limit reached", http.StatusTooManyRequests)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "Response from healthy candidate 2",
					},
				},
			},
		})
	}))
	defer srv2.Close()

	testCfg := origCfg
	testCfg.Models.EnhancementModels = []config.ModelSpec{
		{
			Provider:         config.ProviderCerebras,
			Model:            "gpt-oss-120b",
			Enabled:          true,
			EndpointOverride: srv1.URL,
			TimeoutMs:        2000,
		},
		{
			Provider:         config.ProviderDeepInfra,
			Model:            "deepseek-ai/DeepSeek-V4-Flash",
			Enabled:          true,
			EndpointOverride: srv2.URL,
			TimeoutMs:        2000,
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	// Attempt 1: Server 1 returns 429, trips breaker. Server 2 succeeds.
	res1, err := RunDeepSeekChatCompletion(context.Background(), srv2.Client(), "", "system", "text", nil)
	if err != nil {
		t.Fatalf("Attempt 1 failed: %v", err)
	}
	if res1 != "Response from healthy candidate 2" {
		t.Errorf("Unexpected res1: %q", res1)
	}
	if atomic.LoadInt32(&srv1Hits) != 1 {
		t.Fatalf("Expected srv1Hits == 1, got %d", atomic.LoadInt32(&srv1Hits))
	}

	// Attempt 2: Server 1's circuit breaker is now OPEN.
	// It should FAST-FAIL candidate 1 with 0ms delay and zero HTTP calls to srv1.
	res2, err := RunDeepSeekChatCompletion(context.Background(), srv2.Client(), "", "system", "text", nil)
	if err != nil {
		t.Fatalf("Attempt 2 failed: %v", err)
	}
	if res2 != "Response from healthy candidate 2" {
		t.Errorf("Unexpected res2: %q", res2)
	}
	if atomic.LoadInt32(&srv1Hits) != 1 {
		t.Errorf("Expected srv1Hits to remain 1 due to fast-failing circuit breaker, got %d", atomic.LoadInt32(&srv1Hits))
	}
}

func TestAIEnhancementTruncationSafeguard(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origCerebrasKey := config.CerebrasAPIKey
	config.CerebrasAPIKey = "mock-cerebras-key"
	defer func() {
		config.CerebrasAPIKey = origCerebrasKey
	}()

	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()

	// Server returns heavily truncated output (only 1 word)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "<raw_transcript> truncated </raw_transcript>",
					},
				},
			},
		})
	}))
	defer srv.Close()

	testCfg := origCfg
	testCfg.Models.EnhancementModels = []config.ModelSpec{
		{
			Provider:         config.ProviderCerebras,
			Model:            "gpt-oss-120b",
			Enabled:          true,
			EndpointOverride: srv.URL,
			TimeoutMs:        2000,
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	enhancer := NewAIEnhancementTransform("", "gpt-oss-120b", "key", srv.URL, nil)
	inputText := "One two three four five six seven eight nine ten words in original transcript."

	res, err := enhancer.Transform(context.Background(), inputText, nil)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Safeguard must reject the 1-word truncation and return the verbatim input text
	if res != inputText {
		t.Errorf("Safeguard failed to preserve raw text! Expected %q, got %q", inputText, res)
	}
}

func TestGrammarPromptCannotBeOverriddenByModelConfiguration(t *testing.T) {
	orig := config.ConfigManager.Get()
	raw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(orig, raw)
	key := config.CerebrasAPIKey
	config.CerebrasAPIKey = "mock"
	defer func() { config.CerebrasAPIKey = key }()
	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		if len(p.Messages) < 2 || !strings.Contains(p.Messages[0].Content, "Fix grammar") || strings.Contains(p.Messages[0].Content, "Rewrite as marketing") {
			t.Error("grammar policy overridden")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "We use STMLOCKtest0END today."}}}})
	}))
	defer srv.Close()
	cfg := orig
	cfg.Models.EnhancementModels = []config.ModelSpec{{Provider: config.ProviderCerebras, Model: "gpt-oss-120b", Enabled: true, EndpointOverride: srv.URL, TimeoutMs: 2000, SystemPrompt: "Rewrite as marketing"}}
	config.ConfigManager.Set(cfg, raw)
	if _, e := NewGrammarEnhancementTransform("", "", " ").Transform(context.Background(), "We use STMLOCKtest0END today.", nil); e != nil {
		t.Fatal(e)
	}
}

func TestTaskPromptCannotBeOverriddenByModelConfiguration(t *testing.T) {
	orig := config.ConfigManager.Get()
	raw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(orig, raw)
	key := config.CerebrasAPIKey
	config.CerebrasAPIKey = "mock"
	defer func() { config.CerebrasAPIKey = key }()
	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		// Production configures a dictation prompt on every enhancement model.
		if len(p.Messages) < 2 || !strings.Contains(p.Messages[0].Content, "Summarize this meeting") || strings.Contains(p.Messages[0].Content, "Clean up dictation") {
			t.Error("a model-level prompt replaced the task's instructions")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"ok","action_items":[]}`}}}})
	}))
	defer srv.Close()
	cfg := orig
	cfg.Models.EnhancementModels = []config.ModelSpec{{Provider: config.ProviderCerebras, Model: "gpt-oss-120b", Enabled: true, EndpointOverride: srv.URL, TimeoutMs: 2000, SystemPrompt: "Clean up dictation"}}
	config.ConfigManager.Set(cfg, raw)
	if _, e := RunTaskCompletion(context.Background(), nil, "Summarize this meeting.", "transcript", nil); e != nil {
		t.Fatal(e)
	}
}

func TestGrammarEnhancementSendsCustomPrompt(t *testing.T) {
	orig := config.ConfigManager.Get()
	raw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(orig, raw)
	key := config.CerebrasAPIKey
	config.CerebrasAPIKey = "mock"
	defer func() { config.CerebrasAPIKey = key }()
	GlobalCircuitBreaker.Reset()
	defer GlobalCircuitBreaker.Reset()
	var system string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		if len(p.Messages) > 0 {
			system = p.Messages[0].Content
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "Hello there, team."}}}})
	}))
	defer srv.Close()
	cfg := orig
	cfg.Models.EnhancementModels = []config.ModelSpec{{Provider: config.ProviderCerebras, Model: "gpt-oss-120b", Enabled: true, EndpointOverride: srv.URL, TimeoutMs: 2000}}
	config.ConfigManager.Set(cfg, raw)
	if _, e := NewGrammarEnhancementTransform("Use British spelling.", "", " ").Transform(context.Background(), "hello there team", nil); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(system, "Fix grammar") || !strings.Contains(system, "Additional User Instructions:\nUse British spelling.") {
		t.Fatalf("Grammar prompt must carry the user's instructions: %q", system)
	}
}
