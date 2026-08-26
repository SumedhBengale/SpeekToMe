package config

import (
	"encoding/json"
	"os"
	"testing"

	"speektome-server/db"
)

func TestModelsConfigDefaultAndFallbacks(t *testing.T) {
	cfg := DefaultModelsConfig()

	if cfg.DefaultBatchProvider != "deepinfra_whisper" {
		t.Errorf("Expected default batch provider 'deepinfra_whisper'; got %s", cfg.DefaultBatchProvider)
	}
	if cfg.DefaultStreamingProvider != "deepinfra_nemotron" {
		t.Errorf("Expected default streaming provider 'deepinfra_nemotron'; got %s", cfg.DefaultStreamingProvider)
	}
	if cfg.DefaultLLMProvider != "cerebras" {
		t.Errorf("Expected default LLM provider 'cerebras'; got %s", cfg.DefaultLLMProvider)
	}

	pCfg := ProviderConfig{Models: cfg}
	batchOrder := pCfg.GetActiveBatchFallbackOrder()
	if len(batchOrder) < 3 || batchOrder[0] != "deepinfra_whisper" {
		t.Errorf("Expected batch order to start with 'deepinfra_whisper'; got %v", batchOrder)
	}

	streamingOrder := pCfg.GetActiveStreamingFallbackOrder()
	if len(streamingOrder) < 1 || streamingOrder[0] != "deepinfra_nemotron" {
		t.Errorf("Expected streaming order to start with 'deepinfra_nemotron'; got %v", streamingOrder)
	}

	llmOrder := pCfg.GetActiveLLMFallbackOrder()
	if len(llmOrder) < 3 || llmOrder[0] != "cerebras" {
		t.Errorf("Expected LLM order to start with 'cerebras'; got %v", llmOrder)
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
		DefaultBatchProvider:     "deepinfra_whisper",
		DefaultStreamingProvider: "deepinfra_nemotron",
		DefaultLLMProvider:       "deepinfra_deepseek",
		BatchProviders: map[string]BatchProviderConfig{
			"deepinfra_whisper": {Enabled: true, Model: "openai/whisper-large-v3-turbo"},
			"groq_whisper":      {Enabled: false, Model: "whisper-large-v3-turbo"},
			"cloudflare_whisper": {Enabled: true, Model: "@cf/openai/whisper-large-v3-turbo"},
		},
		StreamingProviders: map[string]StreamingProviderConfig{
			"deepinfra_nemotron": {Enabled: true, Model: "nvidia/nemotron-3.5-asr-streaming-0.6b"},
		},
		LLMProviders: map[string]LLMProviderConfig{
			"deepinfra_deepseek": {Enabled: true, Model: "deepseek-ai/DeepSeek-V4-Flash"},
		},
	}

	jsonBytes, err := json.Marshal(customJSON)
	if err != nil {
		t.Fatal(err)
	}

	_ = database.UpdateSaasConfig("models_config_json", string(jsonBytes))

	SyncConfigsFromDB(database, nil)
	active := ConfigManager.Get()

	if active.Models.DefaultBatchProvider != "deepinfra_whisper" {
		t.Errorf("Expected default batch 'deepinfra_whisper'; got %s", active.Models.DefaultBatchProvider)
	}

	batchOrder := active.GetActiveBatchFallbackOrder()
	if len(batchOrder) != 2 || batchOrder[0] != "deepinfra_whisper" || batchOrder[1] != "cloudflare_whisper" {
		t.Errorf("Expected fallback order ['deepinfra_whisper', 'cloudflare_whisper']; got %v", batchOrder)
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

	if active.Models.DefaultBatchProvider != "groq_whisper" {
		t.Errorf("Expected default batch 'groq_whisper'; got %s", active.Models.DefaultBatchProvider)
	}
	if active.Models.DefaultStreamingProvider != "deepinfra_nemotron" {
		t.Errorf("Expected default streaming 'deepinfra_nemotron'; got %s", active.Models.DefaultStreamingProvider)
	}

	batchOrder := active.GetActiveBatchFallbackOrder()
	expectedOrder := []string{"groq_whisper", "deepinfra_whisper", "cloudflare_whisper"}
	if len(batchOrder) != len(expectedOrder) {
		t.Fatalf("Expected order length %d; got %d (%v)", len(expectedOrder), len(batchOrder), batchOrder)
	}
	for i, name := range expectedOrder {
		if batchOrder[i] != name {
			t.Errorf("Expected order[%d] to be %s; got %s", i, name, batchOrder[i])
		}
	}
}

