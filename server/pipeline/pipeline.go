package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"speektome-server/config"
)

// PipelineContext tracks metadata and state through AI transformations
type PipelineContext struct {
	UserID      int64
	Debug       bool
	LLMProvider string // Tracks which LLM provider was used
	LLMRouting  string // Tracks the routing/location of the LLM request
}

func NewPipelineContext(userID int64) *PipelineContext {
	return &PipelineContext{
		UserID: userID,
	}
}

// TextTransform defines a single step in the transformation pipeline
type TextTransform interface {
	Name() string
	Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error)
}

var aiHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// AIEnhancementTransform cleans transcripts using LLMs
type AIEnhancementTransform struct {
	customPrompt string
	model        string
	apiKey       string
	apiURL       string
	httpClient   *http.Client
}

func NewAIEnhancementTransform(customPrompt, model, apiKey, apiURL string) *AIEnhancementTransform {
	return &AIEnhancementTransform{
		customPrompt: customPrompt,
		model:        model,
		apiKey:       apiKey,
		apiURL:       apiURL,
		httpClient:   aiHTTPClient,
	}
}

func (t *AIEnhancementTransform) Name() string {
	return "AI Enhancement"
}

func callChatCompletionsAPI(ctx context.Context, client *http.Client, apiURL, apiKey, model, systemPrompt, text string, includeThinking bool) (string, string, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
	}
	if includeThinking {
		payload["thinking"] = map[string]string{"type": "disabled"}
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	var remoteIP string
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			if host, _, err := net.SplitHostPort(addr); err == nil {
				remoteIP = host
			} else {
				remoteIP = addr
			}
		},
	}
	ctxTrace := httptrace.WithClientTrace(ctx, trace)

	req, err := http.NewRequestWithContext(ctxTrace, "POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to make HTTP call: %w", err)
	}
	defer resp.Body.Close()

	routingLoc := getHTTPResponseRouting(resp, remoteIP)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("AI call failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", "", fmt.Errorf("AI returned empty choices")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), routingLoc, nil
}

func getHTTPResponseRouting(resp *http.Response, remoteIP string) string {
	if resp == nil {
		return "Unknown Location (No response)"
	}
	if groqReg := resp.Header.Get("x-groq-region"); groqReg != "" {
		return fmt.Sprintf("Groq (%s)", groqReg)
	}
	if cfRay := resp.Header.Get("Cf-Ray"); cfRay != "" {
		parts := strings.Split(cfRay, "-")
		if len(parts) > 1 {
			return fmt.Sprintf("Cloudflare (%s)", parts[len(parts)-1])
		}
		return fmt.Sprintf("Cloudflare (%s)", cfRay)
	}
	if server := resp.Header.Get("Server"); server != "" {
		return fmt.Sprintf("Server (%s)", server)
	}
	return "Unknown Location (No routing headers)"
}

func RunDeepSeekChatCompletion(ctx context.Context, client *http.Client, model, systemPrompt, userPrompt string, pctx *PipelineContext) (string, error) {
	cfg := config.ConfigManager.Get()
	fallbackOrder := cfg.GetActiveLLMFallbackOrder()
	if len(fallbackOrder) == 0 {
		fallbackOrder = []string{"deepseek_v4", "deepinfra_deepseek"}
	}

	var lastErr error
	for _, provider := range fallbackOrder {
		switch provider {
		case "deepseek_v4", "deepseek":
			if config.DeepSeekAPIKey != "" {
				provCfg := cfg.Models.LLMProviders["deepseek_v4"]
				useModel := "deepseek-v4-flash"
				if provCfg.Model != "" {
					useModel = provCfg.Model
				} else if model != "" {
					useModel = model
				}
				apiURL := config.DeepSeekAPIURL
				if provCfg.BaseURL != "" {
					apiURL = provCfg.BaseURL
				}
				res, loc, err := callChatCompletionsAPI(ctx, client, apiURL, config.DeepSeekAPIKey, useModel, systemPrompt, userPrompt, true)
				if err == nil {
					if pctx != nil {
						pctx.LLMProvider = "deepseek"
						pctx.LLMRouting = loc
					}
					return res, nil
				}
				lastErr = err
			}
		case "deepinfra_deepseek", "deepinfra":
			if config.DeepInfraAPIKey != "" {
				provCfg := cfg.Models.LLMProviders["deepinfra_deepseek"]
				diURL := "https://api.deepinfra.com/v1/openai/chat/completions"
				if provCfg.BaseURL != "" {
					diURL = provCfg.BaseURL
				}
				diModel := "deepseek-ai/DeepSeek-V4-Flash"
				if provCfg.Model != "" {
					diModel = provCfg.Model
				}
				res, loc, err := callChatCompletionsAPI(ctx, client, diURL, config.DeepInfraAPIKey, diModel, systemPrompt, userPrompt, false)
				if err == nil {
					if pctx != nil {
						pctx.LLMProvider = "deepinfra"
						pctx.LLMRouting = loc
					}
					return res, nil
				}
				lastErr = err
			}
		case "custom_llm", "custom":
			provCfg := cfg.Models.LLMProviders["custom_llm"]
			if provCfg.BaseURL != "" {
				apiKey := provCfg.APIKey
				res, loc, err := callChatCompletionsAPI(ctx, client, provCfg.BaseURL, apiKey, provCfg.Model, systemPrompt, userPrompt, false)
				if err == nil {
					if pctx != nil {
						pctx.LLMProvider = "custom_llm"
						pctx.LLMRouting = loc
					}
					return res, nil
				}
				lastErr = err
			}
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("all enabled LLM providers failed. Last error: %w", lastErr)
	}
	return "", fmt.Errorf("neither DeepSeekAPIKey nor DeepInfraAPIKey is configured or enabled")
}

func (t *AIEnhancementTransform) Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error) {
	if text == "" {
		return "", nil
	}

	systemPrompt := "You are an expert transcription editor. Refine the provided voice transcription by fixing grammar, punctuation (adding missing commas, periods, and question marks), and capitalization. Remove verbal disfluencies (such as 'uh', 'um', stutters, and repeated filler words). Preserve all technical terms, proper names, and original meaning intact. Do not translate into other languages. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text."
	if strings.TrimSpace(t.customPrompt) != "" {
		systemPrompt = fmt.Sprintf(
			"You are an expert transcription editor. Enhance the provided transcription according to these custom instructions:\n%s\n\nFix grammar, punctuation, and remove verbal disfluencies while preserving original meaning unless instructed otherwise. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text.",
			strings.TrimSpace(t.customPrompt),
		)
	}

	return RunDeepSeekChatCompletion(ctx, t.httpClient, t.model, systemPrompt, text, pctx)
}
