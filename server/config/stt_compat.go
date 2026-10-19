package config

import "strings"

// Legacy provider maps and model arrays are two schemas for the same STT
// configuration. Resolve maps once at load time; an explicitly empty or disabled
// chain must not silently restore the built-in providers.
func legacySTTProvider(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, provider := range []string{"deepinfra", "groq", "cloudflare", "bedrock", "self_hosted", "custom"} {
		if name == provider || strings.HasPrefix(name, provider+"_") {
			return provider
		}
	}
	return name
}

func legacyBatchSTTModels(providers map[string]BatchProviderConfig) []STTModelSpec {
	cfg := ProviderConfig{Models: ModelsConfig{BatchProviders: providers}}
	models := make([]STTModelSpec, 0, len(providers))
	for _, name := range cfg.GetActiveBatchFallbackOrder() {
		p := providers[name]
		models = append(models, STTModelSpec{
			Provider: legacySTTProvider(name), Model: p.Model, Enabled: p.Enabled,
			TimeoutMs: p.TimeoutMs, FailFast: p.FailFast, PriorityFlag: p.PriorityFlag,
			SupportedLanguages: p.SupportedLanguages, InitialPrompt: p.InitialPrompt,
			EndpointOverride: p.BaseURL,
		})
	}
	return models
}

func legacyStreamingSTTModels(providers map[string]StreamingProviderConfig) []STTStreamingModelSpec {
	cfg := ProviderConfig{Models: ModelsConfig{StreamingProviders: providers}}
	models := make([]STTStreamingModelSpec, 0, len(providers))
	for _, name := range cfg.GetActiveStreamingFallbackOrder() {
		p := providers[name]
		models = append(models, STTStreamingModelSpec{
			STTModelSpec: STTModelSpec{
				Provider: legacySTTProvider(name), Model: p.Model, Enabled: p.Enabled,
				TimeoutMs: p.TimeoutMs, FailFast: p.FailFast, PriorityFlag: p.PriorityFlag,
				SupportedLanguages: p.SupportedLanguages, InitialPrompt: p.InitialPrompt,
				EndpointOverride: p.BaseURL,
			},
			Type: cfg.GetStreamingProviderType(name),
		})
	}
	return models
}
