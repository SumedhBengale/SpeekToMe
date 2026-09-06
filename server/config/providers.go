package config

import (
	"fmt"
	"strings"
)

// Standard Provider Identifier Constants
const (
	ProviderDeepInfra  = "deepinfra"
	ProviderDeepSeek   = "deepseek"
	ProviderBedrock    = "bedrock"
	ProviderOpenRouter = "openrouter"
	ProviderWafer      = "wafer"
	ProviderCerebras   = "cerebras"
	ProviderGroq       = "groq"
)

// ModelSpec defines a decoupled model configuration entry.
// Zero credentials: all secrets resolve strictly from environment variables.
type ModelSpec struct {
	Provider         string `json:"provider"`                    // "deepinfra" | "deepseek" | "bedrock" | "openrouter" | "wafer" | "cerebras" | "groq"
	Model            string `json:"model"`                       // e.g. "gpt-oss-120b", "deepseek-ai/DeepSeek-V4-Flash"
	Enabled          bool   `json:"enabled"`                     // Quick toggle without deleting
	TimeoutMs        int    `json:"timeout_ms,omitempty"`        // Per-model timeout (default 15000ms)
	Stream           bool   `json:"stream,omitempty"`            // SSE streaming requested
	Reasoning        string `json:"reasoning,omitempty"`         // "disabled" | "low" | "medium" | "high"
	ServiceTier      string `json:"service_tier,omitempty"`      // "priority" | "default" | "flex" (DeepInfra)
	FailFast         bool   `json:"fail_fast,omitempty"`         // (DeepInfra: fast 429 when busy)
	EndpointOverride string `json:"endpoint_override,omitempty"` // Optional URL override (e.g. for staging/local vLLM)
	SystemPrompt     string `json:"system_prompt,omitempty"`     // Model-level prompt override
}

// ProviderDefinition encapsulates all static, transport, and formatting metadata for a provider.
type ProviderDefinition struct {
	ID                   string
	DisplayName          string
	DefaultBaseURL       string
	EnvKeyName           string
	SupportsFailFast     bool
	SupportsServiceTier  bool
	SupportsReasoning    bool
	GetAPIKey            func() string
	GetBaseURL           func(customURL string) string
	ExtraHeaders         func(apiKey string) map[string]string
	FormatPayload        func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error)
	ValidateModelSpec    func(spec ModelSpec) error
}

// NormalizeLLMProvider normalizes any legacy or variant provider string into standard canonical ID.
func NormalizeLLMProvider(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "deepinfra"):
		return ProviderDeepInfra
	case strings.Contains(lower, "openrouter"):
		return ProviderOpenRouter
	case strings.Contains(lower, "cerebras"):
		return ProviderCerebras
	case strings.Contains(lower, "groq"):
		return ProviderGroq
	case strings.Contains(lower, "wafer"):
		return ProviderWafer
	case strings.Contains(lower, "bedrock") || strings.Contains(lower, "mantle"):
		return ProviderBedrock
	case strings.Contains(lower, "deepseek"):
		return ProviderDeepSeek
	default:
		return lower
	}
}

func defaultBasePayload(spec ModelSpec, systemPrompt, userMessage string) map[string]interface{} {
	payload := map[string]interface{}{
		"model": spec.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMessage},
		},
	}
	if spec.Stream {
		payload["stream"] = true
	}
	return payload
}

// LLMProvidersRegistry contains the hardcoded registry for the 7 official LLM providers.
var LLMProvidersRegistry = map[string]ProviderDefinition{
	ProviderDeepInfra: {
		ID:                  ProviderDeepInfra,
		DisplayName:         "DeepInfra",
		DefaultBaseURL:      DeepInfraChatAPIURL,
		EnvKeyName:          "DEEPINFRA_API_KEY",
		SupportsFailFast:    true,
		SupportsServiceTier: true,
		SupportsReasoning:   true,
		GetAPIKey: func() string {
			return DeepInfraAPIKey
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return DeepInfraChatAPIURL
		},
		ExtraHeaders: nil,
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			if spec.FailFast {
				payload["fail_fast"] = true
			}
			if spec.ServiceTier != "" {
				payload["service_tier"] = spec.ServiceTier
				if spec.ServiceTier == "priority" {
					payload["priority"] = true
				}
			}
			// DeepSeek models on DeepInfra accept thinking: disabled
			isDeepSeek := strings.Contains(strings.ToLower(spec.Model), "deepseek")
			if spec.Reasoning == "disabled" || (spec.Reasoning == "" && isDeepSeek) {
				payload["thinking"] = map[string]string{"type": "disabled"}
			} else if spec.Reasoning != "" {
				payload["reasoning_effort"] = spec.Reasoning
			}
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderDeepInfra)
			}
			return nil
		},
	},

	ProviderDeepSeek: {
		ID:                  ProviderDeepSeek,
		DisplayName:         "DeepSeek Direct",
		DefaultBaseURL:      DeepSeekAPIURL,
		EnvKeyName:          "DEEPSEEK_API_KEY",
		SupportsFailFast:    false,
		SupportsServiceTier: false,
		SupportsReasoning:   true,
		GetAPIKey: func() string {
			return DeepSeekAPIKey
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return DeepSeekAPIURL
		},
		ExtraHeaders: nil,
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			if spec.Reasoning == "disabled" || spec.Reasoning == "" {
				payload["thinking"] = map[string]string{"type": "disabled"}
			} else {
				payload["reasoning_effort"] = spec.Reasoning
			}
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderDeepSeek)
			}
			return nil
		},
	},

	ProviderBedrock: {
		ID:                  ProviderBedrock,
		DisplayName:         "AWS Bedrock (Mantle)",
		DefaultBaseURL:      AWSBedrockMantleURL,
		EnvKeyName:          "AWS_BEARER_TOKEN_BEDROCK",
		SupportsFailFast:    false,
		SupportsServiceTier: false,
		SupportsReasoning:   false,
		GetAPIKey: func() string {
			return AWSBearerTokenBedrock
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return AWSBedrockMantleURL
		},
		ExtraHeaders: nil,
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			// Strict OpenAI format for Bedrock Mantle
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderBedrock)
			}
			return nil
		},
	},

	ProviderOpenRouter: {
		ID:                  ProviderOpenRouter,
		DisplayName:         "OpenRouter",
		DefaultBaseURL:      OpenRouterAPIURL,
		EnvKeyName:          "OPENROUTER_API_KEY",
		SupportsFailFast:    false,
		SupportsServiceTier: false,
		SupportsReasoning:   true,
		GetAPIKey: func() string {
			return OpenRouterAPIKey
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return OpenRouterAPIURL
		},
		ExtraHeaders: func(apiKey string) map[string]string {
			return map[string]string{
				"HTTP-Referer": "https://speekto.me",
				"X-Title":      "SpeekToMe",
			}
		},
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			if spec.Reasoning == "disabled" {
				payload["reasoning"] = map[string]interface{}{
					"effort":  "low",
					"exclude": true,
				}
			} else if spec.Reasoning != "" {
				payload["reasoning"] = map[string]interface{}{
					"effort": spec.Reasoning,
				}
			}
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderOpenRouter)
			}
			return nil
		},
	},

	ProviderWafer: {
		ID:                  ProviderWafer,
		DisplayName:         "Wafer AI",
		DefaultBaseURL:      WaferAPIURL,
		EnvKeyName:          "WAFER_API_KEY",
		SupportsFailFast:    false,
		SupportsServiceTier: false,
		SupportsReasoning:   true,
		GetAPIKey: func() string {
			return WaferAPIKey
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return WaferAPIURL
		},
		ExtraHeaders: nil,
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			if spec.Reasoning == "disabled" || spec.Reasoning == "" {
				payload["thinking"] = map[string]string{"type": "disabled"}
			} else {
				payload["reasoning_effort"] = spec.Reasoning
			}
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderWafer)
			}
			return nil
		},
	},

	ProviderCerebras: {
		ID:                  ProviderCerebras,
		DisplayName:         "Cerebras",
		DefaultBaseURL:      CerebrasAPIURL,
		EnvKeyName:          "CEREBRAS_API_KEY",
		SupportsFailFast:    false,
		SupportsServiceTier: false,
		SupportsReasoning:   false,
		GetAPIKey: func() string {
			return CerebrasAPIKey
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return CerebrasAPIURL
		},
		ExtraHeaders: nil,
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			// Strict OpenAI format; Cerebras rejects extra custom fields
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderCerebras)
			}
			return nil
		},
	},

	ProviderGroq: {
		ID:                  ProviderGroq,
		DisplayName:         "Groq Chat",
		DefaultBaseURL:      GroqChatAPIURL,
		EnvKeyName:          "GROQ_API_KEY",
		SupportsFailFast:    false,
		SupportsServiceTier: false,
		SupportsReasoning:   false,
		GetAPIKey: func() string {
			return GroqAPIKey
		},
		GetBaseURL: func(customURL string) string {
			if strings.TrimSpace(customURL) != "" {
				return customURL
			}
			return GroqChatAPIURL
		},
		ExtraHeaders: nil,
		FormatPayload: func(spec ModelSpec, systemPrompt, userMessage string) (map[string]interface{}, error) {
			// Strict OpenAI format for Groq
			payload := defaultBasePayload(spec, systemPrompt, userMessage)
			return payload, nil
		},
		ValidateModelSpec: func(spec ModelSpec) error {
			if strings.TrimSpace(spec.Model) == "" {
				return fmt.Errorf("model identifier cannot be empty for provider %s", ProviderGroq)
			}
			return nil
		},
	},
}

// GetLLMProvider retrieves a provider definition by name/alias.
func GetLLMProvider(name string) (ProviderDefinition, bool) {
	norm := NormalizeLLMProvider(name)
	prov, ok := LLMProvidersRegistry[norm]
	return prov, ok
}

// IsValidLLMProvider checks if a provider name is registered.
func IsValidLLMProvider(name string) bool {
	norm := NormalizeLLMProvider(name)
	_, ok := LLMProvidersRegistry[norm]
	return ok
}

// ListSupportedLLMProviders returns a slice of all supported provider IDs.
func ListSupportedLLMProviders() []string {
	return []string{
		ProviderDeepInfra,
		ProviderDeepSeek,
		ProviderBedrock,
		ProviderOpenRouter,
		ProviderWafer,
		ProviderCerebras,
		ProviderGroq,
	}
}
