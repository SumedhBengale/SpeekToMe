package config

import (
	"encoding/json"
	"os"
	"testing"

	"speektome-server/db"
)

func TestModelsConfigDefaultAndFallbacks(t *testing.T) {
	cfg := DefaultModelsConfig()

	pCfg := ProviderConfig{Models: cfg}
	batchOrder := pCfg.GetActiveBatchFallbackOrder()
	if len(batchOrder) < 2 || batchOrder[0] != "deepinfra_whisper_large_v3_turbo" {
		t.Errorf("Expected batch order to start with 'deepinfra_whisper_large_v3_turbo'; got %v", batchOrder)
	}

	streamingOrder := pCfg.GetActiveStreamingFallbackOrder()
	if len(streamingOrder) < 1 || streamingOrder[0] != "self_hosted_nemotron_3_5" {
		t.Errorf("Expected streaming order to start with 'self_hosted_nemotron_3_5'; got %v", streamingOrder)
	}

	llmOrder := pCfg.GetActiveLLMFallbackOrder()
	if len(llmOrder) < 3 || llmOrder[0] != "cerebras_gpt_oss_120b" {
		t.Errorf("Expected LLM order to start with 'cerebras_gpt_oss_120b'; got %v", llmOrder)
	}
}

func TestSyncConfigsFromDBWithJSON(t *testing.T) {
	dbFile := "test_config_sync.db"
	_ = os.Remove(dbFile)
	database, err := db.NewDatabase(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}
	defer func() {
		_ = database.Close()
		_ = os.Remove(dbFile)
	}()

	customJSON := ModelsConfig{
		BatchProviders: map[string]BatchProviderConfig{
			"bedrock_voxtral_mini_3b":           {Enabled: false, Priority: 1, Model: "mistral.voxtral-mini-3b-2507"},
			"bedrock_voxtral_small_24b":         {Enabled: false, Priority: 2, Model: "mistral.voxtral-small-24b-2507"},
			"deepinfra_whisper_large_v3_turbo":  {Enabled: true, Priority: 1, Model: "openai/whisper-large-v3-turbo"},
			"groq_whisper_large_v3_turbo":       {Enabled: false, Priority: 2, Model: "whisper-large-v3-turbo"},
			"cloudflare_whisper_large_v3_turbo": {Enabled: true, Priority: 3, Model: "@cf/openai/whisper-large-v3-turbo"},
			"deepinfra_nemotron_3_5":            {Enabled: false, Priority: 4, Model: "nvidia/nemotron-3.5-asr-streaming-0.6b"},
			"deepinfra_whisper_large_v3":        {Enabled: false, Priority: 5, Model: "openai/whisper-large-v3"},
		},
		StreamingProviders: map[string]StreamingProviderConfig{
			"deepinfra_nemotron_3_5": {Enabled: true, Priority: 1, Model: "nvidia/nemotron-3.5-asr-streaming-0.6b"},
		},
		LLMProviders: map[string]LLMProviderConfig{
			"deepinfra_deepseek_v4": {Enabled: true, Priority: 1, Model: "deepseek-ai/DeepSeek-V4-Flash"},
		},
	}

	jsonBytes, err := json.Marshal(customJSON)
	if err != nil {
		t.Fatal(err)
	}

	_ = database.UpdateSaasConfig("models_config_json", string(jsonBytes))

	SyncConfigsFromDB(database, nil)
	active := ConfigManager.Get()

	batchOrder := active.GetActiveBatchFallbackOrder()
	if len(batchOrder) != 2 || batchOrder[0] != "deepinfra_whisper_large_v3_turbo" || batchOrder[1] != "cloudflare_whisper_large_v3_turbo" {
		t.Errorf("Expected fallback order ['deepinfra_whisper_large_v3_turbo', 'cloudflare_whisper_large_v3_turbo']; got %v", batchOrder)
	}
	batchModels := active.GetActiveBatchSTTModels()
	if len(batchModels) != 2 || batchModels[0].Provider != "deepinfra" || batchModels[1].Provider != "cloudflare" {
		t.Fatalf("Database configuration must also control runtime transcription candidates: %+v", batchModels)
	}
}

func TestSyncConfigsFromDBDefaultSeed(t *testing.T) {
	dbFile := "test_config_seed.db"
	_ = os.Remove(dbFile)
	database, err := db.NewDatabase(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}
	defer func() {
		_ = database.Close()
		_ = os.Remove(dbFile)
	}()

	SyncConfigsFromDB(database, nil)
	active := ConfigManager.Get()

	batchOrder := active.GetActiveBatchFallbackOrder()
	if len(batchOrder) < 2 || batchOrder[0] != "deepinfra_whisper_large_v3_turbo" {
		t.Fatalf("Expected batch order to start with deepinfra_whisper_large_v3_turbo; got %v", batchOrder)
	}

	raw := ConfigManager.GetRawAll()
	if raw["meeting_stream_url"] != "ws://GPU_HOST_IP:80/v1/stream" {
		t.Errorf("Expected meeting_stream_url to be ws://GPU_HOST_IP:80/v1/stream; got %s", raw["meeting_stream_url"])
	}
	if raw["unified_stream_url"] != "ws://GPU_HOST_IP:80/v1/stream" {
		t.Errorf("Expected unified_stream_url to be ws://GPU_HOST_IP:80/v1/stream; got %s", raw["unified_stream_url"])
	}
	if active.Models.StreamingProviders["self_hosted_nemotron_3_5"].BaseURL != "ws://GPU_HOST_IP:80/v1/stream" {
		t.Errorf("Expected self_hosted_nemotron_3_5 BaseURL to be ws://GPU_HOST_IP:80/v1/stream; got %s", active.Models.StreamingProviders["self_hosted_nemotron_3_5"].BaseURL)
	}
}

func TestEnhancementAndCommandModelsOrdering(t *testing.T) {
	pCfg := ProviderConfig{Models: DefaultModelsConfig()}

	enh := pCfg.GetActiveEnhancementModels()
	if len(enh) < 3 {
		t.Fatalf("Expected at least 3 active enhancement models, got %d", len(enh))
	}
	if enh[0].Provider != ProviderCerebras || enh[0].Model != "gpt-oss-120b" {
		t.Errorf("Expected first enhancement model to be Cerebras gpt-oss-120b, got %s/%s", enh[0].Provider, enh[0].Model)
	}

	cmd := pCfg.GetActiveCommandModels()
	if len(cmd) < 3 {
		t.Fatalf("Expected at least 3 active command models, got %d", len(cmd))
	}
	if cmd[0].Provider != ProviderDeepInfra || cmd[0].Model != "deepseek-ai/DeepSeek-V4-Flash" {
		t.Errorf("Expected first command model to be DeepInfra DeepSeek, got %s/%s", cmd[0].Provider, cmd[0].Model)
	}
}

func TestCachedConfigManagerImmutability(t *testing.T) {
	mgr := &CachedConfigManager{
		config: ProviderConfig{
			Models: DefaultModelsConfig(),
		},
		rawConfigs: make(map[string]string),
	}

	snap1 := mgr.Get()
	if len(snap1.Models.EnhancementModels) == 0 {
		t.Fatal("Expected non-empty EnhancementModels in snapshot")
	}

	// Mutate the returned slice
	snap1.Models.EnhancementModels[0].Model = "mutated-model"

	// Fetch a fresh snapshot
	snap2 := mgr.Get()
	if snap2.Models.EnhancementModels[0].Model == "mutated-model" {
		t.Errorf("Immutability violated: mutation of returned slice affected CachedConfigManager internal state")
	}
}

func TestSyncConfigsFromDBWithNewDecoupledSchema(t *testing.T) {
	dbFile := "test_config_decoupled.db"
	_ = os.Remove(dbFile)
	database, err := db.NewDatabase(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}
	defer func() {
		_ = database.Close()
		_ = os.Remove(dbFile)
	}()

	customJSON := ModelsConfig{
		EnhancementModels: []ModelSpec{
			{Provider: ProviderGroq, Model: "llama-3.3-70b-versatile", Enabled: true, TimeoutMs: 12000},
			{Provider: ProviderOpenRouter, Model: "deepseek/deepseek-chat", Enabled: true, TimeoutMs: 18000},
		},
		CommandModels: []ModelSpec{
			{Provider: ProviderBedrock, Model: "nvidia.nemotron-super-3-120b", Enabled: true, TimeoutMs: 22000, Stream: true},
		},
	}

	jsonBytes, err := json.Marshal(customJSON)
	if err != nil {
		t.Fatal(err)
	}

	_ = database.UpdateSaasConfig("models_config_json", string(jsonBytes))

	SyncConfigsFromDB(database, nil)
	active := ConfigManager.Get()

	enh := active.GetActiveEnhancementModels()
	if len(enh) != 2 || enh[0].Provider != ProviderGroq || enh[1].Provider != ProviderOpenRouter {
		t.Errorf("Expected active enhancement models [groq, openrouter], got %v", enh)
	}

	cmd := active.GetActiveCommandModels()
	if len(cmd) != 1 || cmd[0].Provider != ProviderBedrock {
		t.Errorf("Expected active command models [bedrock], got %v", cmd)
	}
}

func TestLegacyLLMConfigurationControlsRuntime(t *testing.T) {
	raw := `{"command_llm_providers":{"deepinfra_later":{"enabled":true,"model":"second","priority":5,"stream":true},"bedrock_first":{"enabled":true,"model":"first","priority":1,"timeout_ms":4321,"base_url":"https://custom.example","system_prompt":"custom instruction"},"deepseek_disabled":{"enabled":false,"model":"disabled","priority":2}},"llm_providers":{"groq_custom":{"enabled":true,"model":"custom-enhancer","priority":1}}}`
	cfg, _ := ParseRawConfigs(map[string]string{"models_config_json": raw})
	models := cfg.GetActiveCommandModels()
	if len(models) != 2 || models[0].Provider != "bedrock" || models[0].Model != "first" || models[0].TimeoutMs != 4321 || models[0].EndpointOverride != "https://custom.example" || models[0].SystemPrompt != "custom instruction" || models[1].Model != "second" {
		t.Fatalf("legacy runtime ignored: %+v", models)
	}
	if got := cfg.GetActiveEnhancementModels(); len(got) != 1 || got[0].Model != "custom-enhancer" {
		t.Fatalf("legacy enhancement ignored: %+v", got)
	}
}
func TestExplicitDisabledOrEmptyModelsNeverReenableDefaults(t *testing.T) {
	for _, raw := range []string{
		`{"command_models":[],"enhancement_models":[]}`,
		`{"command_models":[{"provider":"deepinfra","model":"x","enabled":false}],"enhancement_models":[]}`,
		`{"command_llm_providers":{},"llm_providers":{}}`,
		`{"command_llm_providers":{"deepinfra":{"enabled":false,"model":"x"}},"llm_providers":{"cerebras":{"enabled":false,"model":"x"}}}`,
	} {
		cfg, _ := ParseRawConfigs(map[string]string{"models_config_json": raw})
		if len(cfg.GetActiveCommandModels()) != 0 || len(cfg.GetActiveEnhancementModels()) != 0 {
			t.Fatal("disabled models silently reenabled", raw)
		}
	}
	cfg, _ := ParseRawConfigs(map[string]string{"models_config_json": `{"command_models":[{"provider":"groq","model":"modern","enabled":true}],"command_llm_providers":{"deepinfra":{"model":"legacy","enabled":true}}}`})
	if got := cfg.GetActiveCommandModels(); len(got) != 1 || got[0].Model != "modern" {
		t.Fatalf("modern precedence broken %+v", got)
	}
}
func TestLegacyDefaultCommandPromptKeepsGenerationMode(t *testing.T) {
	specs := legacyLLMModels(map[string]LLMProviderConfig{"deepinfra": {Model: "x", Enabled: true, SystemPrompt: DefaultCommandLLMSystemPrompt}}, true)
	if len(specs) != 1 || specs[0].SystemPrompt != "" {
		t.Fatal("transformation prompt overrides generation")
	}
}
