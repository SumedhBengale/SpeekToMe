package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"speektome-server/accountlimits"
	"speektome-server/config"
	"testing"
)

type testCommandBudget struct {
	reserves int
	usage    [][2]int64
	deny     bool
}

func (b *testCommandBudget) Reserve(context.Context, string, string, string, string) (int64, error) {
	b.reserves++
	if b.deny {
		return 0, accountlimits.Denied("command_input_quota_exhausted", "Monthly Command Mode input allowance exhausted.", 60)
	}
	return 7, nil
}
func (b *testCommandBudget) Usage(in, out int64) { b.usage = append(b.usage, [2]int64{in, out}) }
func TestCommandBudgetAccountsForFallbackAndFinalStreamingUsage(t *testing.T) {
	cfg, raw, key := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.DeepInfraAPIKey
	defer func() { config.ConfigManager.Set(cfg, raw); config.DeepInfraAPIKey = key; GlobalCircuitBreaker.Reset() }()
	config.DeepInfraAPIKey = "test-only"
	GlobalCircuitBreaker.Reset()
	rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "busy", 429) }))
	defer rejected.Close()
	streamed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["max_tokens"] != float64(7) {
			t.Error("missing remaining output cap", payload)
		}
		options, _ := payload["stream_options"].(map[string]any)
		if options["include_usage"] != true {
			t.Error("stream usage not requested")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"speektome_result\\\":{\\\"text\\\":\\\"hello\\\"}}\"}}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer streamed.Close()
	next := cfg
	next.Models.CommandModels = []config.ModelSpec{
		{Provider: config.ProviderDeepInfra, Model: "first", Enabled: true, EndpointOverride: rejected.URL, TimeoutMs: 1000},
		{Provider: config.ProviderDeepInfra, Model: "second", Enabled: true, EndpointOverride: streamed.URL, TimeoutMs: 1000, Stream: true},
	}
	config.ConfigManager.Set(next, raw)
	budget := &testCommandBudget{}
	text, e := RunCommandTransformationStream(WithCommandBudget(context.Background(), budget), streamed.Client(), "input", "polish", nil, func(string, string) error { return nil })
	if e != nil || text != "hello" {
		t.Fatal(text, e)
	}
	if budget.reserves != 2 || len(budget.usage) != 2 || budget.usage[0] != [2]int64{0, 0} || budget.usage[1] != [2]int64{20, 2} {
		t.Fatalf("incorrect attempt accounting: %+v", budget)
	}
	budget = &testCommandBudget{deny: true}
	_, e = RunCommandTransformationStream(WithCommandBudget(context.Background(), budget), streamed.Client(), "input", "polish", nil, nil)
	if e == nil || budget.reserves != 1 {
		t.Fatal("quota refusal fell through to another provider", e, budget.reserves)
	}
}

func TestAmbiguousFailuresDoNotRefundReservedTokens(t *testing.T) {
	cfg, raw, key := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.DeepInfraAPIKey
	defer func() { config.ConfigManager.Set(cfg, raw); config.DeepInfraAPIKey = key; GlobalCircuitBreaker.Reset() }()
	config.DeepInfraAPIKey = "test-only"
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			GlobalCircuitBreaker.Reset()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !stream {
					http.Error(w, "upstream failed after admission", 500)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n")
				w.(http.Flusher).Flush()
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
			}))
			defer server.Close()
			next := cfg
			next.Models.CommandModels = []config.ModelSpec{{Provider: config.ProviderDeepInfra, Model: "ambiguous", Enabled: true, Stream: stream, EndpointOverride: server.URL, TimeoutMs: 1000}}
			config.ConfigManager.Set(next, raw)
			budget := &testCommandBudget{}
			_, e := RunCommandTransformationStream(WithCommandBudget(context.Background(), budget), server.Client(), "input", "polish", nil, func(string, string) error { return nil })
			if e == nil || budget.reserves != 1 || len(budget.usage) != 0 {
				t.Fatalf("ambiguous usage received free refund: %v %+v", e, budget)
			}
		})
	}
}
