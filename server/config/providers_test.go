package config

import (
	"testing"
)

func TestNormalizeLLMProvider(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"deepinfra", ProviderDeepInfra},
		{"DeepInfra_Fast", ProviderDeepInfra},
		{"deepseek", ProviderDeepSeek},
		{"deepseek_v4_flash", ProviderDeepSeek},
		{"bedrock", ProviderBedrock},
		{"aws_bedrock_mantle", ProviderBedrock},
		{"openrouter", ProviderOpenRouter},
		{"OpenRouter_AI", ProviderOpenRouter},
		{"wafer", ProviderWafer},
		{"wafer_deepseek", ProviderWafer},
		{"cerebras", ProviderCerebras},
		{"cerebras_gpt_oss", ProviderCerebras},
		{"groq", ProviderGroq},
		{"groq_llama_3_3", ProviderGroq},
		{"unknown_provider", "unknown_provider"},
	}

	for _, tc := range tests {
		got := NormalizeLLMProvider(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeLLMProvider(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestLLMProvidersRegistryCoverage(t *testing.T) {
	expectedProviders := []string{
		ProviderDeepInfra,
		ProviderDeepSeek,
		ProviderBedrock,
		ProviderOpenRouter,
		ProviderWafer,
		ProviderCerebras,
		ProviderGroq,
	}

	for _, pID := range expectedProviders {
		prov, ok := GetLLMProvider(pID)
		if !ok {
			t.Fatalf("Expected provider %s to be registered", pID)
		}
		if prov.ID != pID {
			t.Errorf("Provider ID mismatch: got %s, want %s", prov.ID, pID)
		}
		if prov.DefaultBaseURL == "" {
			t.Errorf("Provider %s has empty DefaultBaseURL", pID)
		}
		if prov.EnvKeyName == "" {
			t.Errorf("Provider %s has empty EnvKeyName", pID)
		}
		if prov.GetAPIKey == nil {
			t.Errorf("Provider %s has nil GetAPIKey", pID)
		}
		if prov.FormatPayload == nil {
			t.Errorf("Provider %s has nil FormatPayload", pID)
		}
		if prov.ValidateModelSpec == nil {
			t.Errorf("Provider %s has nil ValidateModelSpec", pID)
		}
	}
}

func TestDeepInfraPayloadFormatting(t *testing.T) {
	prov, ok := GetLLMProvider(ProviderDeepInfra)
	if !ok {
		t.Fatal("DeepInfra provider not found")
	}

	spec := ModelSpec{
		Provider:    ProviderDeepInfra,
		Model:       "deepseek-ai/DeepSeek-V4-Flash",
		FailFast:    true,
		ServiceTier: "priority",
		Reasoning:   "disabled",
		Stream:      true,
	}

	payload, err := prov.FormatPayload(spec, "System Prompt", "User Message")
	if err != nil {
		t.Fatalf("FormatPayload failed: %v", err)
	}

	if payload["model"] != "deepseek-ai/DeepSeek-V4-Flash" {
		t.Errorf("Unexpected model: %v", payload["model"])
	}
	if payload["fail_fast"] != true {
		t.Errorf("Expected fail_fast=true, got %v", payload["fail_fast"])
	}
	if payload["priority"] != true {
		t.Errorf("Expected priority=true, got %v", payload["priority"])
	}
	if payload["service_tier"] != "priority" {
		t.Errorf("Expected service_tier='priority', got %v", payload["service_tier"])
	}
	if payload["stream"] != true {
		t.Errorf("Expected stream=true, got %v", payload["stream"])
	}
	thinking, ok := payload["thinking"].(map[string]string)
	if !ok || thinking["type"] != "disabled" {
		t.Errorf("Expected thinking.type=disabled, got %v", payload["thinking"])
	}
}

func TestOpenRouterPayloadAndHeaders(t *testing.T) {
	prov, ok := GetLLMProvider(ProviderOpenRouter)
	if !ok {
		t.Fatal("OpenRouter provider not found")
	}

	spec := ModelSpec{
		Provider:  ProviderOpenRouter,
		Model:     "anthropic/claude-3.5-haiku",
		Reasoning: "disabled",
	}

	payload, err := prov.FormatPayload(spec, "System Prompt", "User Message")
	if err != nil {
		t.Fatalf("FormatPayload failed: %v", err)
	}

	reasoning, ok := payload["reasoning"].(map[string]interface{})
	if !ok || reasoning["exclude"] != true {
		t.Errorf("Expected reasoning.exclude=true, got %v", payload["reasoning"])
	}

	if prov.ExtraHeaders == nil {
		t.Fatal("Expected ExtraHeaders to be defined for OpenRouter")
	}
	headers := prov.ExtraHeaders("test-key")
	if headers["HTTP-Referer"] != "https://speekto.me" {
		t.Errorf("Expected HTTP-Referer header, got %v", headers["HTTP-Referer"])
	}
	if headers["X-Title"] != "SpeekToMe" {
		t.Errorf("Expected X-Title header, got %v", headers["X-Title"])
	}
}

func TestStrictOpenAIProvidersPayloadCleanliness(t *testing.T) {
	strictProviders := []string{ProviderCerebras, ProviderBedrock, ProviderGroq}

	for _, pID := range strictProviders {
		prov, ok := GetLLMProvider(pID)
		if !ok {
			t.Fatalf("Provider %s not found", pID)
		}

		spec := ModelSpec{
			Provider:    pID,
			Model:       "test-model",
			FailFast:    true,       // Should be ignored
			ServiceTier: "priority", // Should be ignored
			Reasoning:   "disabled", // Should be ignored
		}

		payload, err := prov.FormatPayload(spec, "System Prompt", "User Message")
		if err != nil {
			t.Fatalf("FormatPayload failed for %s: %v", pID, err)
		}

		if _, exists := payload["fail_fast"]; exists {
			t.Errorf("Provider %s leaked fail_fast parameter", pID)
		}
		if _, exists := payload["service_tier"]; exists {
			t.Errorf("Provider %s leaked service_tier parameter", pID)
		}
		if _, exists := payload["thinking"]; exists {
			t.Errorf("Provider %s leaked thinking parameter", pID)
		}
		if _, exists := payload["reasoning"]; exists {
			t.Errorf("Provider %s leaked reasoning parameter", pID)
		}
	}
}

func TestCerebrasGPTOSSReasoningEffort(t *testing.T) {
	prov, ok := GetLLMProvider(ProviderCerebras)
	if !ok {
		t.Fatal("Cerebras provider not found")
	}

	cases := []struct {
		reasoning string
		want      string
	}{
		{"", "low"},
		{"disabled", "low"},
		{"low", "low"},
		{"medium", "medium"},
		{"high", "high"},
	}
	for _, tc := range cases {
		spec := ModelSpec{Provider: ProviderCerebras, Model: "gpt-oss-120b", Reasoning: tc.reasoning}
		payload, err := prov.FormatPayload(spec, "System Prompt", "User Message")
		if err != nil {
			t.Fatalf("FormatPayload failed: %v", err)
		}
		if got := payload["reasoning_effort"]; got != tc.want {
			t.Errorf("Reasoning %q: reasoning_effort = %v, want %q", tc.reasoning, got, tc.want)
		}
	}
}

func TestModelSpecValidation(t *testing.T) {
	prov, _ := GetLLMProvider(ProviderGroq)

	err := prov.ValidateModelSpec(ModelSpec{Provider: ProviderGroq, Model: ""})
	if err == nil {
		t.Error("Expected validation error for empty model, got nil")
	}

	err = prov.ValidateModelSpec(ModelSpec{Provider: ProviderGroq, Model: "llama-3.3-70b-versatile"})
	if err != nil {
		t.Errorf("Expected valid model to pass, got %v", err)
	}
}
