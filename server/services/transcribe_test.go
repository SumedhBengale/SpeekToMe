package services

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"reflect"
	"speektome-server/modelresult"
	"strings"
	"testing"
	"time"

	"speektome-server/config"
	"speektome-server/pipeline"
)

func TestBatchTranscriptionUsesLegacyDatabaseConfiguration(t *testing.T) {
	origCfg, origRaw := config.ConfigManager.Get(), config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)
	origKey := config.DeepInfraAPIKey
	config.DeepInfraAPIKey = "test-key"
	defer func() { config.DeepInfraAPIKey = origKey }()
	pipeline.GlobalCircuitBreaker.Reset()
	defer pipeline.GlobalCircuitBreaker.Reset()

	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		model := r.FormValue("model")
		calls = append(calls, model)
		if model == "primary" {
			http.Error(w, "mock provider unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "Database routing works."})
	}))
	defer srv.Close()
	providers := map[string]config.BatchProviderConfig{
		"deepinfra_disabled": {Enabled: false, Priority: 1, Model: "disabled", BaseURL: srv.URL},
		"deepinfra_french":   {Enabled: true, Priority: 1, Model: "french-only", BaseURL: srv.URL, SupportedLanguages: []string{"fr"}},
		"deepinfra_primary":  {Enabled: true, Priority: 2, Model: "primary", BaseURL: srv.URL},
		"deepinfra_backup":   {Enabled: true, Priority: 3, Model: "backup", BaseURL: srv.URL},
	}
	data, err := json.Marshal(map[string]interface{}{"batch_providers": providers})
	if err != nil {
		t.Fatal(err)
	}
	cfg, raw := config.ParseRawConfigs(map[string]string{"models_config_json": string(data)})
	config.ConfigManager.Set(cfg, raw)
	text, _, provider, _, err := RunWhisperTranscribeWithModel(context.Background(), []byte("mock audio"), "hardcoded-default", []string{"en"})
	if err != nil || text != "Database routing works." || provider != "deepinfra" {
		t.Fatalf("Legacy database route failed: text=%q provider=%q error=%v", text, provider, err)
	}
	if !reflect.DeepEqual(calls, []string{"primary", "backup"}) {
		t.Fatalf("Database priorities, enabled flags and language filters must control HTTP requests: %v", calls)
	}
}

func TestRunWhisperTranscribeWithModel_Fallback(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origKey := config.AWSBearerTokenBedrock
	config.AWSBearerTokenBedrock = "mock-bedrock-token"
	defer func() {
		config.AWSBearerTokenBedrock = origKey
	}()

	pipeline.GlobalCircuitBreaker.Reset()
	defer pipeline.GlobalCircuitBreaker.Reset()

	// Server 1 returns 503
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Voxtral service down", http.StatusServiceUnavailable)
	}))
	defer srv1.Close()

	// Server 2 returns 200 with Bedrock Voxtral JSON
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "Decoupled STT transcribed text successfully.",
					},
				},
			},
		})
	}))
	defer srv2.Close()

	testCfg := origCfg
	testCfg.Models.BatchSTTModels = []config.STTModelSpec{
		{
			Provider:           "bedrock",
			Model:              "voxtral-candidate-1",
			Enabled:            true,
			EndpointOverride:   srv1.URL,
			TimeoutMs:          2000,
			SupportedLanguages: []string{"*"},
		},
		{
			Provider:           "bedrock",
			Model:              "voxtral-candidate-2",
			Enabled:            true,
			EndpointOverride:   srv2.URL,
			TimeoutMs:          2000,
			SupportedLanguages: []string{"*"},
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	mockAudio := []byte("RIFF1234WAVEfmt 1234data1234mock-audio-pcm-data-here-over-44-bytes")
	text, _, provider, _, err := RunWhisperTranscribeWithModel(context.Background(), mockAudio, "", []string{"en"})
	if err != nil {
		t.Fatalf("Expected fallback to candidate 2, got error: %v", err)
	}

	if text != "Decoupled STT transcribed text successfully." {
		t.Errorf("Unexpected transcribed text: %q", text)
	}
	if provider != "bedrock" {
		t.Errorf("Expected provider bedrock, got %q", provider)
	}
}

func TestRunWhisperTranscribeWithModel_LanguageRouting(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origKey := config.AWSBearerTokenBedrock
	config.AWSBearerTokenBedrock = "mock-bedrock-token"
	defer func() {
		config.AWSBearerTokenBedrock = origKey
	}()

	pipeline.GlobalCircuitBreaker.Reset()
	defer pipeline.GlobalCircuitBreaker.Reset()

	var srv1Called bool
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv1Called = true
		http.Error(w, "French only endpoint", http.StatusBadRequest)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "English transcription succeeded.",
					},
				},
			},
		})
	}))
	defer srv2.Close()

	testCfg := origCfg
	testCfg.Models.BatchSTTModels = []config.STTModelSpec{
		{
			Provider:           "bedrock",
			Model:              "voxtral-french-only",
			Enabled:            true,
			EndpointOverride:   srv1.URL,
			SupportedLanguages: []string{"fr"}, // Should be skipped for "en"
		},
		{
			Provider:           "bedrock",
			Model:              "voxtral-english-supported",
			Enabled:            true,
			EndpointOverride:   srv2.URL,
			SupportedLanguages: []string{"en"},
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	mockAudio := []byte("RIFF1234WAVEfmt 1234data1234mock-audio-pcm-data-here-over-44-bytes")
	text, _, _, _, err := RunWhisperTranscribeWithModel(context.Background(), mockAudio, "", []string{"en"})
	if err != nil {
		t.Fatalf("Expected language routing to succeed, got error: %v", err)
	}

	if srv1Called {
		t.Errorf("Candidate 1 (French only) should have been skipped without HTTP calls!")
	}
	if text != "English transcription succeeded." {
		t.Errorf("Unexpected text: %q", text)
	}
}

func TestRunStreamingTranscribeWithPrompt_Fallback(t *testing.T) {
	origCfg := config.ConfigManager.Get()
	origRaw := config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)

	origKey := config.AWSBearerTokenBedrock
	config.AWSBearerTokenBedrock = "mock-bedrock-token"
	defer func() {
		config.AWSBearerTokenBedrock = origKey
	}()

	pipeline.GlobalCircuitBreaker.Reset()
	defer pipeline.GlobalCircuitBreaker.Reset()

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Pseudo stream busy", http.StatusBadGateway)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "Streaming chunk transcribed cleanly.",
					},
				},
			},
		})
	}))
	defer srv2.Close()

	testCfg := origCfg
	testCfg.Models.StreamingSTTModels = []config.STTStreamingModelSpec{
		{
			STTModelSpec: config.STTModelSpec{
				Provider:           "bedrock",
				Model:              "mistral.voxtral-mini-3b-2507",
				Enabled:            true,
				EndpointOverride:   srv1.URL,
				TimeoutMs:          2000,
				SupportedLanguages: []string{"*"},
			},
			Type: "pseudo",
		},
		{
			STTModelSpec: config.STTModelSpec{
				Provider:           "bedrock",
				Model:              "mistral.voxtral-mini-3b-2507",
				Enabled:            true,
				EndpointOverride:   srv2.URL,
				TimeoutMs:          2000,
				SupportedLanguages: []string{"*"},
			},
			Type: "pseudo",
		},
	}
	config.ConfigManager.Set(testCfg, origRaw)

	mockAudio := []byte("RIFF1234WAVEfmt 1234data1234mock-audio-pcm-data-here-over-44-bytes")
	text, _, _, _, err := RunStreamingTranscribeWithPrompt(context.Background(), mockAudio, []string{"en"}, "Context")
	if err != nil {
		t.Fatalf("Expected streaming fallback to succeed, got error: %v", err)
	}

	if text != "Streaming chunk transcribed cleanly." {
		t.Errorf("Unexpected text: %q", text)
	}
}

func TestASRProtocolErrorIsNotSuccessfulText(t *testing.T) {
	original, raw := config.ConfigManager.Get(), config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(original, raw)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for i := 0; i < 2; i++ {
			if _, _, err = conn.Read(r.Context()); err != nil {
				return
			}
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","error":{"message":"PRIVATE provider detail"}}`))
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := original
	cfg.Models.StreamingProviders = map[string]config.StreamingProviderConfig{"test": {Enabled: true, BaseURL: strings.Replace(server.URL, "http://", "ws://", 1)}}
	config.ConfigManager.Set(cfg, raw)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := CreateUpstreamStreamingSession(ctx, "test", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(websocket.StatusNormalClosure, "")
	err = session.AwaitFinal(ctx)
	var resultErr *modelresult.Error
	if !errors.As(err, &resultErr) || resultErr.Code != "asr_failed" || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("ASR error lost", err)
	}
}

func TestStreamingInitialPromptKeepsRollingContext(t *testing.T) {
	origCfg, origRaw := config.ConfigManager.Get(), config.ConfigManager.GetRawAll()
	defer config.ConfigManager.Set(origCfg, origRaw)
	origKey := config.DeepInfraAPIKey
	config.DeepInfraAPIKey = "test-key"
	defer func() { config.DeepInfraAPIKey = origKey }()
	pipeline.GlobalCircuitBreaker.Reset()
	defer pipeline.GlobalCircuitBreaker.Reset()

	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		prompts = append(prompts, r.FormValue("prompt"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "next chunk"})
	}))
	defer srv.Close()

	testCfg := origCfg
	testCfg.Models.StreamingSTTModels = []config.STTStreamingModelSpec{{
		STTModelSpec: config.STTModelSpec{Provider: "deepinfra", Model: "openai/whisper-large-v3-turbo", Enabled: true, EndpointOverride: srv.URL, TimeoutMs: 2000, InitialPrompt: "Transcribe verbatim.", SupportedLanguages: []string{"*"}},
		Type:         "pseudo",
	}}
	config.ConfigManager.Set(testCfg, origRaw)

	for _, rolling := range []string{" we moved the meeting ", ""} {
		if _, _, _, _, err := RunStreamingTranscribeWithPrompt(context.Background(), []byte("mock audio"), []string{"en"}, rolling); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(prompts, []string{"Transcribe verbatim. we moved the meeting", "Transcribe verbatim."}) {
		t.Fatalf("Configured prompt must precede the rolling context, not replace it: %q", prompts)
	}
}
