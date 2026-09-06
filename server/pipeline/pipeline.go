package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	LLMModel    string // Tracks which exact model name was used (e.g. gpt-oss-120b, deepseek-chat)
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
	customPrompt   string
	model          string
	apiKey         string
	apiURL         string
	httpClient     *http.Client
	protectedVocab []string
}

func NewAIEnhancementTransform(customPrompt, model, apiKey, apiURL string, protectedVocab []string) *AIEnhancementTransform {
	return &AIEnhancementTransform{
		customPrompt:   customPrompt,
		model:          model,
		apiKey:         apiKey,
		apiURL:         apiURL,
		httpClient:     aiHTTPClient,
		protectedVocab: protectedVocab,
	}
}

func (t *AIEnhancementTransform) Name() string {
	return "AI Enhancement"
}

// ChunkCallback is invoked for each streaming text delta
type ChunkCallback func(delta, accumulated string) error

// callChatCompletionsAPIUnified executes a standard non-streaming HTTP chat completion request
func callChatCompletionsAPIUnified(
	ctx context.Context,
	client *http.Client,
	apiURL, apiKey string,
	extraHeaders map[string]string,
	payload map[string]interface{},
) (string, string, int, http.Header, error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed to marshal payload: %w", err)
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
		return "", "", 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed to make HTTP call: %w", err)
	}
	defer resp.Body.Close()

	routingLoc := getHTTPResponseRouting(resp, remoteIP)
	respHeader := resp.Header.Clone()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", routingLoc, resp.StatusCode, respHeader, fmt.Errorf("AI call failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", routingLoc, resp.StatusCode, respHeader, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", routingLoc, resp.StatusCode, respHeader, fmt.Errorf("AI returned empty choices")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), routingLoc, resp.StatusCode, respHeader, nil
}

// callChatCompletionsAPIStreamUnified executes a streaming SSE HTTP chat completion request
func callChatCompletionsAPIStreamUnified(
	ctx context.Context,
	client *http.Client,
	apiURL, apiKey string,
	extraHeaders map[string]string,
	payload map[string]interface{},
	onChunk ChunkCallback,
) (string, string, int, http.Header, error) {
	payload["stream"] = true

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed to marshal payload: %w", err)
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
		return "", "", 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed to make HTTP call: %w", err)
	}
	defer resp.Body.Close()

	routingLoc := getHTTPResponseRouting(resp, remoteIP)
	respHeader := resp.Header.Clone()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", routingLoc, resp.StatusCode, respHeader, fmt.Errorf("AI stream call failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	reader := bufio.NewReader(resp.Body)
	var fullText strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fullText.String(), routingLoc, resp.StatusCode, respHeader, fmt.Errorf("error reading stream: %w", err)
		}

		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if data == "[DONE]" {
				break
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content,omitempty"`
					} `json:"delta"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta.Content
					if delta != "" {
						fullText.WriteString(delta)
						if onChunk != nil {
							if cbErr := onChunk(delta, fullText.String()); cbErr != nil {
								return fullText.String(), routingLoc, resp.StatusCode, respHeader, cbErr
							}
						}
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	return fullText.String(), routingLoc, resp.StatusCode, respHeader, nil
}

// Backward-compatible wrappers for direct unit tests
func callChatCompletionsAPI(
	ctx context.Context,
	client *http.Client,
	apiURL, apiKey, model, systemPrompt, text string,
	includeThinking bool,
	failFast bool,
	priorityFlag bool,
) (string, string, error) {
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
	if failFast {
		payload["fail_fast"] = true
	}
	if priorityFlag {
		payload["priority"] = true
	}

	content, routing, _, _, err := callChatCompletionsAPIUnified(ctx, client, apiURL, apiKey, nil, payload)
	return content, routing, err
}

func callChatCompletionsAPIStream(
	ctx context.Context,
	client *http.Client,
	apiURL, apiKey, model, systemPrompt, text string,
	includeThinking bool,
	failFast bool,
	priorityFlag bool,
	onChunk ChunkCallback,
) (string, string, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
		"stream": true,
	}
	if includeThinking {
		payload["thinking"] = map[string]string{"type": "disabled"}
	}
	if failFast {
		payload["fail_fast"] = true
	}
	if priorityFlag {
		payload["priority"] = true
	}

	content, routing, _, _, err := callChatCompletionsAPIStreamUnified(ctx, client, apiURL, apiKey, nil, payload, onChunk)
	return content, routing, err
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

// executeModelAttempt performs a single model execution attempt with circuit breaking,
// provider resolution, and error/cooldown feedback.
func executeModelAttempt(
	ctx context.Context,
	client *http.Client,
	spec config.ModelSpec,
	systemPrompt, userPrompt string,
	onChunk ChunkCallback,
) (res string, routing string, err error) {
	canExec, done := GlobalCircuitBreaker.CanExecute(spec.Provider, spec.Model)
	if !canExec {
		return "", "", fmt.Errorf("circuit breaker open / fast-failing for %s/%s", spec.Provider, spec.Model)
	}

	provDef, ok := config.GetLLMProvider(spec.Provider)
	if !ok {
		err := fmt.Errorf("unsupported LLM provider: %s", spec.Provider)
		if done != nil {
			done(false, 0, err, nil)
		}
		return "", "", err
	}

	apiKey := provDef.GetAPIKey()
	if apiKey == "" {
		err := fmt.Errorf("missing API key for LLM provider %s (check .env %s)", spec.Provider, provDef.EnvKeyName)
		if done != nil {
			done(false, 0, err, nil)
		}
		return "", "", err
	}

	apiURL := provDef.GetBaseURL(spec.EndpointOverride)
	if apiURL == "" {
		err := fmt.Errorf("missing API endpoint URL for LLM provider %s", spec.Provider)
		if done != nil {
			done(false, 0, err, nil)
		}
		return "", "", err
	}

	// Model-level prompt override if configured in spec
	effectiveSystemPrompt := systemPrompt
	if strings.TrimSpace(spec.SystemPrompt) != "" {
		effectiveSystemPrompt = strings.TrimSpace(spec.SystemPrompt)
	}

	payload, err := provDef.FormatPayload(spec, effectiveSystemPrompt, userPrompt)
	if err != nil {
		if done != nil {
			done(false, 0, err, nil)
		}
		return "", "", fmt.Errorf("failed to format payload for %s/%s: %w", spec.Provider, spec.Model, err)
	}

	timeout := time.Duration(spec.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var extraHeaders map[string]string
	if provDef.ExtraHeaders != nil {
		extraHeaders = provDef.ExtraHeaders(apiKey)
	}

	var statusCode int
	var respHeader http.Header

	if spec.Stream {
		res, routing, statusCode, respHeader, err = callChatCompletionsAPIStreamUnified(
			ctxTimeout, client, apiURL, apiKey, extraHeaders, payload, onChunk,
		)
	} else {
		res, routing, statusCode, respHeader, err = callChatCompletionsAPIUnified(
			ctxTimeout, client, apiURL, apiKey, extraHeaders, payload,
		)
	}

	if done != nil {
		done(err == nil, statusCode, err, respHeader)
	}

	return res, routing, err
}

// RunDeepSeekChatCompletion executes chat completions using the decoupled enhancement_models priority list.
func RunDeepSeekChatCompletion(ctx context.Context, client *http.Client, model, systemPrompt, userPrompt string, pctx *PipelineContext) (string, error) {
	if client == nil {
		client = aiHTTPClient
	}
	cfg := config.ConfigManager.Get()
	activeCandidates := cfg.GetActiveEnhancementModels()
	if len(activeCandidates) == 0 {
		return "", fmt.Errorf("no active enhancement models configured")
	}

	var lastErr error
	for _, spec := range activeCandidates {
		if spec.Model == "" && model != "" {
			spec.Model = model
		}

		log.Printf("[LLM ENHANCE] Attempting candidate provider=%s model=%s (stream=%v timeout=%dms)",
			spec.Provider, spec.Model, spec.Stream, spec.TimeoutMs)

		res, routing, err := executeModelAttempt(ctx, client, spec, systemPrompt, userPrompt, nil)
		if err == nil {
			if pctx != nil {
				pctx.LLMProvider = spec.Provider
				pctx.LLMModel = spec.Model
				pctx.LLMRouting = routing
			}
			return res, nil
		}

		log.Printf("[LLM ENHANCE FALLBACK] Candidate %s/%s failed: %v. Falling back to next candidate in order.",
			spec.Provider, spec.Model, err)
		lastErr = err
	}

	if lastErr != nil {
		return "", fmt.Errorf("all configured enhancement models failed. Last error: %w", lastErr)
	}
	return "", fmt.Errorf("no enhancement models succeeded")
}

// RunCommandTransformation executes command transforms synchronously
func RunCommandTransformation(ctx context.Context, client *http.Client, selectedText, command string, pctx *PipelineContext) (string, error) {
	return RunCommandTransformationStream(ctx, client, selectedText, command, pctx, nil)
}

// RunCommandTransformationStream executes command transformations using the decoupled command_models priority list,
// streaming SSE deltas to onChunk with atomic [RESET] signaling if mid-stream fallback occurs.
func RunCommandTransformationStream(
	ctx context.Context,
	client *http.Client,
	selectedText, command string,
	pctx *PipelineContext,
	onChunk ChunkCallback,
) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	cfg := config.ConfigManager.Get()
	activeCandidates := cfg.GetActiveCommandModels()
	if len(activeCandidates) == 0 {
		return "", fmt.Errorf("no active command models configured")
	}

	hasTarget := strings.TrimSpace(selectedText) != ""
	var defaultSystemPrompt string
	if hasTarget {
		defaultSystemPrompt = config.DefaultCommandLLMSystemPrompt
	} else {
		defaultSystemPrompt = config.DefaultCommandGenerationSystemPrompt
	}

	var userMessage string
	if hasTarget {
		userMessage = fmt.Sprintf("Target Text:\n\"\"\"\n%s\n\"\"\"\n\nInstruction:\n%s", selectedText, command)
	} else {
		userMessage = fmt.Sprintf("Instruction:\n%s", command)
	}

	var lastErr error
	for _, spec := range activeCandidates {
		systemPrompt := defaultSystemPrompt
		if strings.TrimSpace(spec.SystemPrompt) != "" {
			systemPrompt = strings.TrimSpace(spec.SystemPrompt)
		}

		log.Printf("[COMMAND LLM] Attempting candidate provider=%s model=%s (stream=%v timeout=%dms)",
			spec.Provider, spec.Model, spec.Stream, spec.TimeoutMs)

		var providerEmittedChunks bool
		provChunkHandler := func(delta, accumulated string) error {
			providerEmittedChunks = true
			if onChunk != nil {
				return onChunk(delta, accumulated)
			}
			return nil
		}

		var res string
		var routing string
		var err error

		if spec.Stream && onChunk != nil {
			res, routing, err = executeModelAttempt(ctx, client, spec, systemPrompt, userMessage, provChunkHandler)
		} else {
			res, routing, err = executeModelAttempt(ctx, client, spec, systemPrompt, userMessage, nil)
			if err == nil && onChunk != nil && res != "" {
				_ = onChunk(res, res)
			}
		}

		if err == nil {
			if pctx != nil {
				pctx.LLMProvider = spec.Provider
				pctx.LLMModel = spec.Model
				pctx.LLMRouting = routing
			}
			return res, nil
		}

		log.Printf("[COMMAND LLM FALLBACK] Candidate %s/%s failed: %v. Trying next candidate.",
			spec.Provider, spec.Model, err)

		// Mid-stream reset recovery: If this candidate already emitted partial chunks before failing,
		// notify the client to clear accumulated output before falling back to the next candidate
		if providerEmittedChunks && onChunk != nil {
			log.Printf("[COMMAND LLM FALLBACK] Candidate %s/%s emitted partial chunks before failing. Signaling [RESET].",
				spec.Provider, spec.Model)
			_ = onChunk("[RESET]", "")
		}

		lastErr = err
	}

	if lastErr != nil {
		return "", fmt.Errorf("all configured command models failed. Last error: %w", lastErr)
	}
	return "", fmt.Errorf("no command models succeeded")
}

func (t *AIEnhancementTransform) Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error) {
	if text == "" {
		return "", nil
	}

	cfg := config.ConfigManager.Get()
	baseSystemPrompt := config.DefaultUniversalLLMSystemPrompt
	if len(cfg.Models.EnhancementModels) > 0 && strings.TrimSpace(cfg.Models.EnhancementModels[0].SystemPrompt) != "" {
		baseSystemPrompt = strings.TrimSpace(cfg.Models.EnhancementModels[0].SystemPrompt)
	}

	var sb strings.Builder
	sb.WriteString(baseSystemPrompt)

	if len(t.protectedVocab) > 0 {
		sb.WriteString("\n\nCRITICAL - PRESERVED DOMAIN & VOCABULARY TERMS:\n")
		sb.WriteString("The following terms are verified domain-specific vocabulary and user terms. You MUST NOT modify, autocorrect, rephrase, or remove these exact terms under any circumstances. Preserve their exact spelling, capitalization, and formatting:\n")
		for _, term := range t.protectedVocab {
			if strings.TrimSpace(term) != "" {
				sb.WriteString(fmt.Sprintf("- %s\n", strings.TrimSpace(term)))
			}
		}
	}

	if strings.TrimSpace(t.customPrompt) != "" {
		sb.WriteString(fmt.Sprintf("\n\nAdditional User Instructions:\n%s", strings.TrimSpace(t.customPrompt)))
	}

	sb.WriteString("\n\nDo not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished transcription.")

	systemPrompt := sb.String()
	userPrompt := fmt.Sprintf("<raw_transcript>\n%s\n</raw_transcript>", text)
	res, err := RunDeepSeekChatCompletion(ctx, t.httpClient, t.model, systemPrompt, userPrompt, pctx)
	if err != nil {
		return "", err
	}
	res = strings.TrimPrefix(res, "<raw_transcript>")
	res = strings.TrimSuffix(res, "</raw_transcript>")
	res = strings.TrimSpace(res)

	// Truncation safeguard: If the LLM returned significantly fewer words than the input
	// (e.g. dropped more than 40% of words on transcripts with >= 6 words), reject the truncation
	// and fall back to verbatim input text.
	inputWords := len(strings.Fields(text))
	outputWords := len(strings.Fields(res))
	if inputWords >= 6 && outputWords < (inputWords*6)/10 {
		log.Printf("[AI ENHANCEMENT TRUNCATION REJECTED] LLM output words (%d) < 60%% of input words (%d). Discarding truncated output:\n%q\nFalling back to raw text:\n%q",
			outputWords, inputWords, res, text)
		return text, nil
	}

	return res, nil
}
