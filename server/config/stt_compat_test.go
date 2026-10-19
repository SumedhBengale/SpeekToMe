package config

import (
	"reflect"
	"testing"
)

func TestLegacySTTConfigControlsRuntimeModels(t *testing.T) {
	cfg, _ := ParseRawConfigs(map[string]string{"models_config_json": `{
		"batch_providers": {
			"groq_whisper_large_v3_turbo": {"enabled":false,"priority":1,"model":"disabled"},
			"deepinfra_whisper_large_v3": {"enabled":true,"priority":3,"model":"third"},
			"deepinfra_nemotron_3_5": {"enabled":true,"priority":2,"model":"second"},
			"deepinfra_whisper_large_v3_turbo": {"enabled":true,"priority":1,"model":"first",
				"timeout_ms":1200,"fail_fast":true,"priority_tier":true,
				"base_url":"https://example.test/transcribe","initial_prompt":"verbatim",
				"supported_languages":["en","hi"]}
		},
		"streaming_providers": {
			"self_hosted_nemotron_3_5": {"enabled":true,"priority":1,"model":"live",
				"base_url":"wss://example.test/stream","timeout_ms":2300},
			"deepinfra_nemotron_3_5": {"enabled":true,"priority":2,"model":"chunked",
				"type":"pseudo","fail_fast":true,"priority_flag":true,"initial_prompt":"exact",
				"supported_languages":["en"],"base_url":"https://example.test/chunked"},
			"deepinfra_whisper_large_v3": {"enabled":false,"priority":1,"model":"disabled"}
		}
	}`})
	batch := cfg.GetActiveBatchSTTModels()
	if len(batch) != 3 || batch[0].Model != "first" || batch[1].Model != "second" || batch[2].Model != "third" {
		t.Fatalf("runtime batch order ignored database priorities: %+v", batch)
	}
	want := STTModelSpec{Provider: "deepinfra", Model: "first", Enabled: true, TimeoutMs: 1200,
		FailFast: true, PriorityFlag: true, EndpointOverride: "https://example.test/transcribe",
		InitialPrompt: "verbatim", SupportedLanguages: []string{"en", "hi"}}
	if !reflect.DeepEqual(batch[0], want) {
		t.Fatalf("legacy request settings lost: got %+v, want %+v", batch[0], want)
	}
	stream := cfg.GetActiveStreamingSTTModels()
	if len(stream) != 2 || stream[0].Provider != "self_hosted" || stream[0].Type != "real" || stream[1].Model != "chunked" {
		t.Fatalf("runtime streaming order/type ignored configuration: %+v", stream)
	}
	if stream[0].TimeoutMs != 2300 || !stream[1].FailFast || !stream[1].PriorityFlag || stream[1].InitialPrompt != "exact" || !reflect.DeepEqual(stream[1].SupportedLanguages, []string{"en"}) {
		t.Fatalf("legacy streaming request settings lost: %+v", stream)
	}
}

func TestSTTConfigExplicitEmptyOrDisabledDoesNotRestoreDefaults(t *testing.T) {
	for _, raw := range []string{
		`{"batch_providers":{},"streaming_providers":{}}`,
		`{"batch_providers":{"deepinfra":{"enabled":false,"model":"off"}},"streaming_providers":{"deepinfra":{"enabled":false,"model":"off"}}}`,
		`{"batch_stt_models":[],"streaming_stt_models":[]}`,
		`{"batch_stt_models":[{"provider":"deepinfra","model":"off","enabled":false}],"streaming_stt_models":[{"provider":"deepinfra","model":"off","enabled":false}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			cfg, values := ParseRawConfigs(map[string]string{"models_config_json": raw})
			mgr := &CachedConfigManager{}
			mgr.Set(cfg, values)
			snapshot := mgr.Get()
			if len(snapshot.GetActiveBatchSTTModels()) != 0 || len(snapshot.GetActiveStreamingSTTModels()) != 0 {
				t.Fatal("explicitly disabled STT configuration restored built-in models")
			}
		})
	}
}

func TestSTTArraySchemaPrecedenceAndMissingDefaults(t *testing.T) {
	cfg, _ := ParseRawConfigs(map[string]string{"models_config_json": `{
		"batch_providers":{"deepinfra":{"enabled":true,"model":"legacy"}},
		"batch_stt_models":[{"provider":"custom","model":"explicit","enabled":true}],
		"streaming_providers":{"deepinfra":{"enabled":true,"model":"legacy"}},
		"streaming_stt_models":[]
	}`})
	batch := cfg.GetActiveBatchSTTModels()
	if len(batch) != 1 || batch[0].Model != "explicit" || len(cfg.GetActiveStreamingSTTModels()) != 0 {
		t.Fatalf("explicit model arrays must override legacy maps: %+v", cfg.Models)
	}
	defaults, _ := ParseRawConfigs(map[string]string{"models_config_json": `{}`})
	if len(defaults.GetActiveBatchSTTModels()) == 0 || len(defaults.GetActiveStreamingSTTModels()) == 0 {
		t.Fatal("omitted STT sections should retain defaults")
	}
}

func TestLegacyBatchSTTPriorityTiesAreStable(t *testing.T) {
	cfg, _ := ParseRawConfigs(map[string]string{"models_config_json": `{"batch_providers":{
		"deepinfra_b":{"enabled":true,"priority":2,"model":"b"},
		"deepinfra_a":{"enabled":true,"priority":2,"model":"a"},
		"custom_batch":{"enabled":true,"model":"last"}
	}}`})
	batch := cfg.GetActiveBatchSTTModels()
	if len(batch) != 3 || batch[0].Model != "a" || batch[1].Model != "b" || batch[2].Model != "last" {
		t.Fatalf("priority ties/default priority must be deterministic: %+v", batch)
	}
}
