package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"speektome-server/config"
	"speektome-server/pipeline"
)

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
