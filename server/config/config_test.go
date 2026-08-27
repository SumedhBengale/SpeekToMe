package config

import (
	"encoding/json"
	"os"
	"testing"

	"speektome-server/db"
)

func TestModelsConfigDefaultAndFallbacks(t *testing.T) {
	cfg := DefaultModelsConfig()

	if cfg.DefaultBatchProvider != "bedrock_voxtral_mini_3b" {
		t.Errorf("Expected default batch provider 'bedrock_voxtral_mini_3b'; got %s", cfg.DefaultBatchProvider)
	}
	if cfg.DefaultStreamingProvider != "bedrock_voxtral_mini_3b" {
		t.Errorf("Expected default streaming provider 'bedrock_voxtral_mini_3b'; got %s", cfg.DefaultStreamingProvider)
	}
	if cfg.DefaultLLMProvider != "cerebras_gpt_oss_120b" {
		t.Errorf("Expected default LLM provider 'cerebras_gpt_oss_120b'; got %s", cfg.DefaultLLMProvider)
	}

	pCfg := ProviderConfig{Models: cfg}
	batchOrder := pCfg.GetActiveBatchFallbackOrder()
	if len(batchOrder) < 3 || batchOrder[0] != "bedrock_voxtral_mini_3b" {
		t.Errorf("Expected batch order to start with 'bedrock_voxtral_mini_3b'; got %v", batchOrder)
	}

	streamingOrder := pCfg.GetActiveStreamingFallbackOrder()
	if len(streamingOrder) < 1 || streamingOrder[0] != "bedrock_voxtral_mini_3b" {
		t.Errorf("Expected streaming order to start with 'bedrock_voxtral_mini_3b'; got %v", streamingOrder)
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
		DefaultBatchProvider:     "deepinfra_whisper_large_v3_turbo",
		DefaultStreamingProvider: "deepinfra_nemotron_3_5",
		DefaultLLMProvider:       "deepinfra_deepseek_v4",
		BatchProviders: map[string]BatchProviderConfig{
			"bedrock_voxtral_mini_3b":           {Enabled: false, Priority: 1, Model: "mistral.voxtral-mini-3b-2507"},
			"bedrock_voxtral_small_24b":          {Enabled: false, Priority: 2, Model: "mistral.voxtral-small-24b-2507"},
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

	if active.Models.DefaultBatchProvider != "deepinfra_whisper_large_v3_turbo" {
		t.Errorf("Expected default batch 'deepinfra_whisper_large_v3_turbo'; got %s", active.Models.DefaultBatchProvider)
	}

	batchOrder := active.GetActiveBatchFallbackOrder()
	if len(batchOrder) != 2 || batchOrder[0] != "deepinfra_whisper_large_v3_turbo" || batchOrder[1] != "cloudflare_whisper_large_v3_turbo" {
		t.Errorf("Expected fallback order ['deepinfra_whisper_large_v3_turbo', 'cloudflare_whisper_large_v3_turbo']; got %v", batchOrder)
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

	if active.Models.DefaultBatchProvider != "bedrock_voxtral_mini_3b" {
		t.Errorf("Expected default batch 'bedrock_voxtral_mini_3b'; got %s", active.Models.DefaultBatchProvider)
	}
	if active.Models.DefaultStreamingProvider != "bedrock_voxtral_mini_3b" {
		t.Errorf("Expected default streaming 'bedrock_voxtral_mini_3b'; got %s", active.Models.DefaultStreamingProvider)
	}

	batchOrder := active.GetActiveBatchFallbackOrder()
	if len(batchOrder) < 3 || batchOrder[0] != "bedrock_voxtral_mini_3b" {
		t.Fatalf("Expected batch order to start with bedrock_voxtral_mini_3b; got %v", batchOrder)
	}
}
