package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"speektome-server/config"
	"speektome-server/modelresult"
	"strings"
	"testing"
)

func TestModelErrorsAndContractOverride(t *testing.T) {
	cfg, raw, key := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.DeepInfraAPIKey
	defer func() { config.ConfigManager.Set(cfg, raw); config.DeepInfraAPIKey = key; GlobalCircuitBreaker.Reset() }()
	config.DeepInfraAPIKey = "test"
	for _, stream := range []bool{false, true} {
		for _, native := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/native=%t", stream, native), func(t *testing.T) {
				GlobalCircuitBreaker.Reset()
				attempts := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempts++
					var payload struct {
						Messages []struct {
							Content string `json:"content"`
						} `json:"messages"`
					}
					json.NewDecoder(r.Body).Decode(&payload)
					if !strings.Contains(payload.Messages[0].Content, "APPLICATION ERROR CONTRACT") || !strings.Contains(payload.Messages[0].Content, "custom prompt") {
						t.Error("error contract lost to model override")
					}
					field, value := "content", `{"speektome_error":{"code":"missing_input"}}`
					if native {
						field, value = "refusal", "PRIVATE reason"
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{field: value}}}})
						fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
					} else {
						json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{field: value}}}})
					}
				}))
				defer srv.Close()
				next := cfg
				spec := config.ModelSpec{Provider: "deepinfra", Model: "test", Enabled: true, Stream: stream, EndpointOverride: srv.URL, SystemPrompt: "custom prompt"}
				next.Models.CommandModels = []config.ModelSpec{spec, spec}
				config.ConfigManager.Set(next, raw)
				_, err := RunCommandTransformationStream(context.Background(), srv.Client(), "text", "translate", nil, func(string, string) error { return nil })
				var e *modelresult.Error
				if !errors.As(err, &e) || attempts != 1 || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("model error lost or retried", err, attempts)
				}
				allowed, done := GlobalCircuitBreaker.CanExecute("deepinfra", "test")
				if !allowed {
					t.Fatal("business error tripped provider circuit")
				}
				done(true, 200, nil, nil)
			})
		}
	}
}
