package config

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"

	"speektome-server/db"
)

var (
	GroqAPIKey          string
	DeepInfraAPIKey     string
	DeepSeekAPIKey      string
	CloudflareAPIToken  string
	CloudflareAccountID string
	GoogleClientID      string
	GoogleClientSecret  string
	BackendURL          string
	DatabaseURL         string

	GroqAPIURL       = "https://api.groq.com/openai/v1/audio/transcriptions"
	DeepInfraBaseURL = "https://api.deepinfra.com/v1/inference/"
	DeepSeekAPIURL   = "https://api.deepseek.com/chat/completions"
)

// Provider descriptor structs
type BatchProviderConfig struct {
	Enabled   bool   `json:"enabled"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type StreamingProviderConfig struct {
	Enabled   bool   `json:"enabled"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type LLMProviderConfig struct {
	Enabled   bool   `json:"enabled"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type ModelsConfig struct {
	DefaultBatchProvider     string                             `json:"default_batch_provider"`
	DefaultStreamingProvider string                             `json:"default_streaming_provider"`
	DefaultLLMProvider       string                             `json:"default_llm_provider"`
	BatchProviders           map[string]BatchProviderConfig     `json:"batch_providers"`
	StreamingProviders       map[string]StreamingProviderConfig `json:"streaming_providers"`
	LLMProviders             map[string]LLMProviderConfig       `json:"llm_providers"`
}

type ProviderConfig struct {
	Models                  ModelsConfig
	GroqRateLimitRPM        int
	DefaultUserRateLimitRPM int
	Notification            string
	WhisperBasePrompt       string
}

func DefaultModelsConfig() ModelsConfig {
	return ModelsConfig{
		DefaultBatchProvider:     "groq_whisper",
		DefaultStreamingProvider: "deepinfra_nemotron",
		DefaultLLMProvider:       "deepseek_v4",
		BatchProviders: map[string]BatchProviderConfig{
			"groq_whisper": {
				Enabled:   true,
				Model:     "whisper-large-v3-turbo",
				BaseURL:   GroqAPIURL,
				TimeoutMs: 10000,
			},
			"deepinfra_whisper": {
				Enabled:   true,
				Model:     "openai/whisper-large-v3-turbo",
				BaseURL:   DeepInfraBaseURL + "openai/whisper-large-v3-turbo",
				TimeoutMs: 10000,
			},
			"cloudflare_whisper": {
				Enabled:   true,
				Model:     "@cf/openai/whisper-large-v3-turbo",
				TimeoutMs: 15000,
			},
			"custom_batch": {
				Enabled:   false,
				Model:     "",
				BaseURL:   "",
				APIKey:    "",
				TimeoutMs: 10000,
			},
		},
		StreamingProviders: map[string]StreamingProviderConfig{
			"deepinfra_nemotron": {
				Enabled:   true,
				Model:     "nvidia/nemotron-3.5-asr-streaming-0.6b",
				BaseURL:   DeepInfraBaseURL + "nvidia/nemotron-3.5-asr-streaming-0.6b",
				TimeoutMs: 15000,
			},
			"custom_streaming": {
				Enabled:   false,
				Model:     "",
				BaseURL:   "",
				APIKey:    "",
				TimeoutMs: 15000,
			},
		},
		LLMProviders: map[string]LLMProviderConfig{
			"deepseek_v4": {
				Enabled:   true,
				Model:     "deepseek-v4-flash",
				BaseURL:   DeepSeekAPIURL,
				TimeoutMs: 30000,
			},
			"deepinfra_deepseek": {
				Enabled:   true,
				Model:     "deepseek-ai/DeepSeek-V4-Flash",
				BaseURL:   "https://api.deepinfra.com/v1/openai/chat/completions",
				TimeoutMs: 30000,
			},
			"custom_llm": {
				Enabled:   false,
				Model:     "",
				BaseURL:   "",
				APIKey:    "",
				TimeoutMs: 30000,
			},
		},
	}
}

// Helpers on ProviderConfig for fallback priority chains
func (p *ProviderConfig) GetActiveBatchFallbackOrder() []string {
	var order []string
	defaultProv := p.Models.DefaultBatchProvider
	if defaultProv != "" {
		if prov, ok := p.Models.BatchProviders[defaultProv]; ok && prov.Enabled {
			order = append(order, defaultProv)
		}
	}

	knownBatch := []string{"groq_whisper", "deepinfra_whisper", "cloudflare_whisper", "custom_batch"}
	for _, name := range knownBatch {
		if name == defaultProv {
			continue
		}
		if prov, ok := p.Models.BatchProviders[name]; ok && prov.Enabled {
			order = append(order, name)
		}
	}
	return order
}

func (p *ProviderConfig) GetActiveStreamingFallbackOrder() []string {
	var order []string
	defaultProv := p.Models.DefaultStreamingProvider
	if defaultProv != "" {
		if prov, ok := p.Models.StreamingProviders[defaultProv]; ok && prov.Enabled {
			order = append(order, defaultProv)
		}
	}

	knownStreaming := []string{"deepinfra_nemotron", "custom_streaming"}
	for _, name := range knownStreaming {
		if name == defaultProv {
			continue
		}
		if prov, ok := p.Models.StreamingProviders[name]; ok && prov.Enabled {
			order = append(order, name)
		}
	}
	return order
}

func (p *ProviderConfig) GetActiveLLMFallbackOrder() []string {
	var order []string
	defaultProv := p.Models.DefaultLLMProvider
	if defaultProv != "" {
		if prov, ok := p.Models.LLMProviders[defaultProv]; ok && prov.Enabled {
			order = append(order, defaultProv)
		}
	}

	knownLLM := []string{"deepseek_v4", "deepinfra_deepseek", "custom_llm"}
	for _, name := range knownLLM {
		if name == defaultProv {
			continue
		}
		if prov, ok := p.Models.LLMProviders[name]; ok && prov.Enabled {
			order = append(order, name)
		}
	}
	return order
}

type CachedConfigManager struct {
	mu         sync.RWMutex
	config     ProviderConfig
	rawConfigs map[string]string
}

func (m *CachedConfigManager) Get() ProviderConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *CachedConfigManager) GetRawAll() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copyMap := make(map[string]string, len(m.rawConfigs))
	for k, v := range m.rawConfigs {
		copyMap[k] = v
	}
	return copyMap
}

func (m *CachedConfigManager) Set(c ProviderConfig, raw map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = c
	m.rawConfigs = raw
}

var ConfigManager = &CachedConfigManager{
	config: ProviderConfig{
		Models:                  DefaultModelsConfig(),
		GroqRateLimitRPM:        20,
		DefaultUserRateLimitRPM: 10,
		Notification:            "",
		WhisperBasePrompt:       "Transcribe the spoken audio accurately with correct grammar, spelling, and punctuation.",
	},
	rawConfigs: make(map[string]string),
}

func SyncConfigsFromDB(database *db.Database, updateGroqLimitFunc func(limit int)) {
	if database == nil {
		return
	}
	dbConfigs, err := database.GetSaasConfig()
	if err != nil {
		log.Printf("[CONFIG SYNC ERROR] Failed to query saas_config: %v", err)
		return
	}

	models := DefaultModelsConfig()

	// Parse JSON models_config_json if present in saas_config
	if jsonStr, ok := dbConfigs["models_config_json"]; ok && strings.TrimSpace(jsonStr) != "" {
		var parsed ModelsConfig
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			if parsed.DefaultBatchProvider != "" {
				models.DefaultBatchProvider = parsed.DefaultBatchProvider
			}
			if parsed.DefaultStreamingProvider != "" {
				models.DefaultStreamingProvider = parsed.DefaultStreamingProvider
			}
			if parsed.DefaultLLMProvider != "" {
				models.DefaultLLMProvider = parsed.DefaultLLMProvider
			}
			for k, v := range parsed.BatchProviders {
				models.BatchProviders[k] = v
			}
			for k, v := range parsed.StreamingProviders {
				models.StreamingProviders[k] = v
			}
			for k, v := range parsed.LLMProviders {
				models.LLMProviders[k] = v
			}
		} else {
			log.Printf("[CONFIG SYNC WARNING] Failed to parse models_config_json: %v", err)
		}
	}

	newConfig := ProviderConfig{
		Models:                  models,
		GroqRateLimitRPM:        20,
		DefaultUserRateLimitRPM: 10,
		WhisperBasePrompt:       "Transcribe the spoken audio accurately with correct grammar, spelling, and punctuation.",
	}

	// Rate Limits
	if val, ok := dbConfigs["ratelimit_groq_rpm"]; ok {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			newConfig.GroqRateLimitRPM = limit
		}
	} else if val, ok := dbConfigs["groq_rate_limit_rpm"]; ok {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			newConfig.GroqRateLimitRPM = limit
		}
	}

	if val, ok := dbConfigs["ratelimit_default_user_rpm"]; ok {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			newConfig.DefaultUserRateLimitRPM = limit
		}
	} else if val, ok := dbConfigs["default_user_rate_limit_rpm"]; ok {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			newConfig.DefaultUserRateLimitRPM = limit
		}
	}

	// Notification Announcement String
	if val, ok := dbConfigs["notification"]; ok {
		newConfig.Notification = val
	}

	// Whisper Base Prompt String
	if val, ok := dbConfigs["whisper_base_prompt"]; ok && strings.TrimSpace(val) != "" {
		newConfig.WhisperBasePrompt = strings.TrimSpace(val)
	}

	// Re-serialize models_config_json into rawConfigs so the admin API always sees full representation
	if fullJSON, err := json.MarshalIndent(models, "", "  "); err == nil {
		dbConfigs["models_config_json"] = string(fullJSON)
	}

	ConfigManager.Set(newConfig, dbConfigs)

	if updateGroqLimitFunc != nil {
		updateGroqLimitFunc(newConfig.GroqRateLimitRPM)
	}
}

func normalizeProviderName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "groq", "groq_whisper", "whisper_groq":
		return "groq_whisper"
	case "deepinfra", "deepinfra_whisper", "whisper_deepinfra":
		return "deepinfra_whisper"
	case "cloudflare", "cloudflare_whisper", "whisper_cloudflare":
		return "cloudflare_whisper"
	case "custom", "custom_batch":
		return "custom_batch"
	case "nemotron", "deepinfra_nemotron", "nemotron_streaming":
		return "deepinfra_nemotron"
	case "custom_streaming":
		return "custom_streaming"
	case "deepseek", "deepseek_v4", "deepseek_chat":
		return "deepseek_v4"
	case "deepinfra_deepseek":
		return "deepinfra_deepseek"
	case "custom_llm":
		return "custom_llm"
	default:
		return lower
	}
}
