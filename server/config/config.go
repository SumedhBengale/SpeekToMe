package config

import (
	"log"
	"strconv"
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

type ProviderConfig struct {
	DefaultProvider         string
	GroqEnabled             bool
	DeepInfraEnabled        bool
	CloudflareEnabled       bool
	GroqRateLimitRPM        int
	DefaultUserRateLimitRPM int
	Notification            string
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
		DefaultProvider:         "deepinfra",
		GroqEnabled:             true,
		DeepInfraEnabled:        true,
		CloudflareEnabled:       true,
		GroqRateLimitRPM:        20,
		DefaultUserRateLimitRPM: 10,
		Notification:            "",
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

	newConfig := ProviderConfig{
		DefaultProvider:         "deepinfra",
		GroqEnabled:             true,
		DeepInfraEnabled:        true,
		CloudflareEnabled:       true,
		GroqRateLimitRPM:        20,
		DefaultUserRateLimitRPM: 10,
	}

	// STT Provider Routing
	if val, ok := dbConfigs["provider_default_stt"]; ok && val != "" {
		newConfig.DefaultProvider = val
	} else if val, ok := dbConfigs["whisper_default_provider"]; ok && val != "" {
		newConfig.DefaultProvider = val
	}

	if val, ok := dbConfigs["provider_groq_enabled"]; ok {
		newConfig.GroqEnabled = val == "true"
	} else if val, ok := dbConfigs["groq_enabled"]; ok {
		newConfig.GroqEnabled = val == "true"
	}

	if val, ok := dbConfigs["provider_deepinfra_enabled"]; ok {
		newConfig.DeepInfraEnabled = val == "true"
	} else if val, ok := dbConfigs["deepinfra_enabled"]; ok {
		newConfig.DeepInfraEnabled = val == "true"
	}

	if val, ok := dbConfigs["provider_cloudflare_enabled"]; ok {
		newConfig.CloudflareEnabled = val == "true"
	} else if val, ok := dbConfigs["cloudflare_enabled"]; ok {
		newConfig.CloudflareEnabled = val == "true"
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

	ConfigManager.Set(newConfig, dbConfigs)

	if updateGroqLimitFunc != nil {
		updateGroqLimitFunc(newConfig.GroqRateLimitRPM)
	}
}
