package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"speektome-server/config"
	"speektome-server/pipeline"
	"testing"
)

func TestFreeBatchCannotUseOtherModelsOrFallbackToThem(t *testing.T) {
	orig, raw, key := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.DeepInfraAPIKey
	t.Cleanup(func() {
		config.ConfigManager.Set(orig, raw)
		config.DeepInfraAPIKey = key
		pipeline.GlobalCircuitBreaker.Reset()
	})
	config.DeepInfraAPIKey = "test-only"
	calls := 0
	fail := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "openai/whisper-large-v3-turbo" {
			t.Error("Free reached forbidden model", r.FormValue("model"))
		}
		if fail {
			http.Error(w, "synthetic unavailable", 503)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "Free transcription."})
	}))
	defer s.Close()
	cfg := orig
	cfg.Models.BatchSTTModels = []config.STTModelSpec{
		{Provider: "custom", Model: "expensive-first", Enabled: true, EndpointOverride: s.URL},
		{Provider: "deepinfra", Model: "openai/whisper-large-v3-turbo", Enabled: true, EndpointOverride: s.URL},
		{Provider: "custom", Model: "whisper-large-v3", Enabled: true, EndpointOverride: s.URL},
	}
	config.ConfigManager.Set(cfg, raw)
	text, _, _, _, err := RunFreeBatchTranscribe(context.Background(), []byte("synthetic"), []string{"en"})
	if err != nil || text != "Free transcription." || calls != 1 {
		t.Fatal(text, err, calls)
	}
	fail = true
	calls = 0
	pipeline.GlobalCircuitBreaker.Reset()
	_, _, _, _, err = RunFreeBatchTranscribe(context.Background(), []byte("synthetic"), []string{"en"})
	if err == nil || calls != 1 {
		t.Fatal("forbidden fallback after Whisper failure", err, calls)
	}
	cfg.Models.BatchSTTModels = cfg.Models.BatchSTTModels[:1]
	config.ConfigManager.Set(cfg, raw)
	calls = 0
	_, _, _, _, err = RunFreeBatchTranscribe(context.Background(), []byte("synthetic"), []string{"en"})
	if err == nil || calls != 0 {
		t.Fatal("must fail closed when no allowed model is configured", err, calls)
	}
}
