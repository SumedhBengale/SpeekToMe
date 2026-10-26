package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	log "speektome-server/privacylog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/modelresult"
	"speektome-server/pipeline"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/utils"
	"speektome-server/vocabularydecision"
)

type StreamStartPayload struct {
	Language     string   `json:"language,omitempty"`
	Type         string   `json:"type"`   // "start"
	Format       string   `json:"format"` // "opus" | "wav" | "pcm" | "ogg_opus"
	SampleRate   int      `json:"sample_rate"`
	Channels     int      `json:"channels"`
	Enhance      bool     `json:"enhance"`
	Prompt       string   `json:"prompt"`
	Capabilities []string `json:"capabilities,omitempty"` // e.g. ["continuous_stream", "chunked_vad"]
}

type StreamEventMessage struct {
	Code       string                    `json:"code,omitempty"`
	Stage      string                    `json:"stage,omitempty"`
	Warning    string                    `json:"warning,omitempty"`
	Type       string                    `json:"type"`           // "session_ready" | "interim" | "final" | "error"
	Mode       string                    `json:"mode,omitempty"` // "continuous_stream" | "chunked_vad"
	Text       string                    `json:"text,omitempty"`
	LogID      int64                     `json:"log_id,omitempty"`
	AckToken   string                    `json:"ack_token,omitempty"`
	LatencyMs  int64                     `json:"latency_ms,omitempty"`
	Provider   string                    `json:"provider,omitempty"`
	Error      string                    `json:"error,omitempty"`
	Vocabulary *vocabularydecision.Audit `json:"vocabulary,omitempty"`
	// Replacements locates live vocabulary decisions in an interim's Text.
	Replacements []vocabularydecision.Span `json:"replacements,omitempty"`
}

func (a *App) HandleStreamTranscribe(w http.ResponseWriter, r *http.Request) {
	// Default origin checks reject cross-site browser pages; the desktop client
	// sends no Origin header.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("[STREAM ERROR] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "the session terminated")
	conn.SetReadLimit(32 << 20) // Allow reading up to 32MB frames (default was 32KB)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// 1. Authenticate from the Authorization header. Desktop builds released
	// before header authentication still send ?token=; nginx drops query
	// strings from access logs. Remove the fallback once those builds expire.
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}

	if token == "" {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Missing authentication token"})
		conn.Close(websocket.StatusPolicyViolation, "Unauthorized")
		return
	}

	user, vocabTerms, err := a.DB.GetUserAndVocabularyBySession(token)
	if err != nil || user == nil {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Invalid or expired session token"})
		conn.Close(websocket.StatusPolicyViolation, "Unauthorized")
		return
	}

	// Rate limiting & quota check
	isPaid := user.PlanActive == 1 && user.PlanTier == "pro"
	if !isPaid {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{
			Type:  "error",
			Error: "Ultra-Fast Streaming ASR is an exclusive Pro feature. Please upgrade your plan in settings.",
		})
		conn.Close(websocket.StatusPolicyViolation, "Pro plan required")
		return
	}

	rpmLimit := 15
	monthlyLimitSeconds := 180000.0
	cfg := config.ConfigManager.GetRawAll()
	if val, ok := cfg["ratelimit_paid_rpm"]; ok {
		if limit, err := strconv.Atoi(val); err == nil {
			rpmLimit = limit
		}
	}
	if val, ok := cfg["quota_paid_monthly_seconds"]; ok {
		if limit, err := strconv.ParseFloat(val, 64); err == nil {
			monthlyLimitSeconds = limit
		}
	}

	if !ratelimit.GlobalUserRateLimiter.Allow(user.ID, rpmLimit) {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: fmt.Sprintf("Rate limit exceeded. Maximum %d requests per minute.", rpmLimit)})
		conn.Close(websocket.StatusPolicyViolation, "Rate limit exceeded")
		return
	}

	if user.MonthlyUsageSeconds >= monthlyLimitSeconds {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Monthly usage quota exceeded."})
		conn.Close(websocket.StatusPolicyViolation, "Monthly limit exceeded")
		return
	}

	streamStartTime := time.Now()

	// 2. Read initial handshake message from client
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		log.Printf("[STREAM ERROR] Failed to read initial handshake frame: %v", err)
		return
	}

	var startPayload StreamStartPayload
	if msgType != websocket.MessageText || json.Unmarshal(data, &startPayload) != nil || startPayload.Type != "start" {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Expected a start message with one language string"})
		return
	}

	startPayload.Prompt = strings.TrimSpace(startPayload.Prompt)
	if len(startPayload.Prompt) > pipeline.MaxCustomPromptChars {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: fmt.Sprintf("Custom prompt is too long (maximum %d characters)", pipeline.MaxCustomPromptChars)})
		return
	}

	language, languageErr := utils.ResolveSpeakingLanguage(startPayload.Language, user.Languages)
	if languageErr != nil {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: languageErr.Error()})
		return
	}
	requestUser := *user
	requestUser.Languages = []string{language}
	user = &requestUser

	log.Printf("[STREAM START] Session initialized | User ID: %d | Lang: %v | Enhance: %v | Capabilities: %v | Vocab: %d terms",
		user.ID, user.Languages, startPayload.Enhance, startPayload.Capabilities, len(vocabTerms))

	// 3. Resolve best available streaming provider
	activeConfig := config.ConfigManager.Get()
	streamOrder := activeConfig.GetActiveStreamingFallbackOrder()
	var selectedProvider string
	var selectedType string

	for _, p := range streamOrder {
		if activeConfig.IsStreamingLanguageSupported(p, "") {
			selectedProvider = p
			selectedType = activeConfig.GetStreamingProviderType(p)
			break
		}
	}
	if selectedProvider == "" {
		selectedProvider = "deepinfra_nemotron"
		selectedType = "pseudo"
	}

	// 4. Branch: Real Streaming Proxy vs Pseudo-Streaming (Chunked VAD)
	if selectedType == "real" {
		upstreamSess, uerr := services.CreateUpstreamStreamingSession(ctx, selectedProvider, user.Languages, startPayload.Prompt, vocabTerms)
		if uerr == nil && upstreamSess != nil {
			log.Printf("[STREAM NEGOTIATION] Selected REAL streaming provider: %s", selectedProvider)
			_ = writeStreamJSON(ctx, conn, StreamEventMessage{
				Type:     "session_ready",
				Mode:     "continuous_stream",
				Provider: selectedProvider,
			})
			a.handleRealStreamingSession(ctx, conn, user, vocabTerms, upstreamSess, startPayload, selectedProvider, streamStartTime)
			return
		}
		log.Printf("[STREAM REAL FAILOVER] Upstream connection to %s failed (%v). Falling back to pseudo-streaming.", selectedProvider, uerr)

		// Pick next active pseudo-streaming provider in fallback order
		for _, p := range streamOrder {
			if p != selectedProvider && activeConfig.IsStreamingLanguageSupported(p, "") {
				selectedProvider = p
				break
			}
		}
	}

	// Pseudo-Streaming (Chunked VAD)
	log.Printf("[STREAM NEGOTIATION] Selected PSEUDO streaming provider: %s", selectedProvider)
	_ = writeStreamJSON(ctx, conn, StreamEventMessage{
		Type:     "session_ready",
		Mode:     "chunked_vad",
		Provider: selectedProvider,
	})
	a.handlePseudoStreamingSession(ctx, conn, user, vocabTerms, startPayload, selectedProvider, streamStartTime)
}

func (a *App) handleRealStreamingSession(
	ctx context.Context,
	conn *websocket.Conn,
	user *db.UserProfile,
	vocabTerms []db.VocabularyTerm,
	upstream *services.UpstreamStreamingSession,
	startPayload StreamStartPayload,
	provider string,
	startTime time.Time,
) {
	defer upstream.Close(websocket.StatusNormalClosure, "client stream closed")

	var fullAudioAccumulator bytes.Buffer
	var committedText string
	var textMutex sync.Mutex

	var lastChunkSentTime time.Time
	var chunkCount int
	var timingMutex sync.Mutex

	// The preview shows each vocabulary proposal as the ASR words until Jev
	// decides it, then the decision, so the end of the stream has less to do.
	var previewMutex sync.Mutex
	var lastPreview string
	var live *vocabularydecision.Live
	sendPreview := func() {
		previewMutex.Lock()
		defer previewMutex.Unlock()
		preview, replacements := live.Preview()
		if preview == "" || preview == lastPreview {
			return
		}
		lastPreview = preview
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{
			Type:         "interim",
			Text:         preview,
			Provider:     provider,
			Replacements: replacements,
		})
	}
	live = vocabularydecision.NewLive(vocabularydecision.Default(), sendPreview)
	defer live.Close()
	if upstream.Proposals != nil {
		go func() {
			for proposal := range upstream.Proposals {
				live.Propose(proposal)
			}
		}()
	}

	// Goroutine to receive text tokens from upstream real-time model and forward to client
	textReceiverDone := make(chan struct{})
	go func() {
		defer close(textReceiverDone)
		for text := range upstream.TextChan {
			normText := text

			timingMutex.Lock()
			rtt := time.Duration(0)
			if !lastChunkSentTime.IsZero() {
				rtt = time.Since(lastChunkSentTime)
			}
			currChunk := chunkCount
			timingMutex.Unlock()

			textMutex.Lock()
			committedText = normText
			textMutex.Unlock()

			if rtt > 0 {
				log.Printf("[STREAM REAL LATENCY] Chunk #%d -> Upstream GPU RTT: %v | Words: %d", currChunk, rtt.Round(time.Millisecond), len(strings.Fields(normText)))
			}

			live.Text(normText)
			sendPreview()
		}
	}()

	// Main loop: forward binary audio frames from client to upstream model
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if msgType == websocket.MessageText {
			var control map[string]interface{}
			if err := json.Unmarshal(data, &control); err == nil {
				t, _ := control["type"].(string)
				if t == "end_of_stream" || t == "stop" {
					log.Printf("[STREAM REAL] Received %s from user ID: %d", t, user.ID)
					_ = upstream.SendControl(ctx, control)
					break
				}
			}
		} else if msgType == websocket.MessageBinary {
			fullAudioAccumulator.Write(data)
			if fullAudioAccumulator.Len() > 15<<20 {
				_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Audio stream payload exceeded limit"})
				break
			}

			timingMutex.Lock()
			lastChunkSentTime = time.Now()
			chunkCount++
			timingMutex.Unlock()

			if err := upstream.SendAudio(ctx, data); err != nil {
				log.Printf("[STREAM REAL ERROR] Failed to forward audio to upstream: %v", err)
				_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Transcription recovery failed; please retry.", Code: "asr_failed", Stage: "asr"})
				return
			}
		}
	}

	// Acknowledge commit before saving successful output. Failure is explicit.
	flushCtx, flushCancel := context.WithTimeout(ctx, 15*time.Second)
	flushError := upstream.AwaitFinal(flushCtx)
	flushCancel()
	if upstream.UsesFleet() {
		select {
		case <-textReceiverDone:
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Transcription output could not be saved; please retry.", Code: "asr_failed", Stage: "asr"})
			return
		}
	}
	if flushError != nil {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Transcription could not be finalized; please retry.", Code: "asr_failed", Stage: "asr"})
		return
	}

	textMutex.Lock()
	finalRawTranscript := committedText
	textMutex.Unlock()

	audit := finalizeVocabulary(ctx, finalRawTranscript, vocabTerms, upstream.VocabularyResult(), live)
	grammarInput, restoreGrammar := audit.Protect()

	audioRaw := fullAudioAccumulator.Bytes()
	audioSizeBytes := len(audioRaw)
	audioDuration := utils.GetOggOpusDuration(audioRaw)
	if audioDuration <= 0 && audioSizeBytes > 0 {
		audioDuration = float64(audioSizeBytes) / 32000.0
	}

	currentText := audit.Resolved
	var dsLatency float64
	var enhancementWarning string
	pctx := pipeline.NewPipelineContext(user.ID)

	if finalRawTranscript != "" {
		tstart := time.Now()
		enhancer := pipeline.NewGrammarEnhancementTransform(startPayload.Prompt, config.DeepSeekAPIKey, config.DeepSeekAPIURL)
		enhancedText, terr := enhancer.Transform(ctx, grammarInput, pctx)
		dsLatency = time.Since(tstart).Seconds()
		if terr != nil {
			enhancementWarning = modelresult.New("enhancement_failed", "enhancement").Message
			log.Printf("[STREAM REAL ENHANCE ERROR] %v", terr)
		} else if enhancedText != "" {
			currentText = restoreGrammar(enhancedText)
			log.Printf("[STREAM REAL ENHANCE] LLM Post-Processing completed in %.3fs (model: %s, provider: %s, protected vocab: %d terms)", dsLatency, pctx.LLMModel, pctx.LLMProvider, len(audit.Decisions))
		}
	}

	totalLatencyMs := time.Since(startTime).Milliseconds()
	finalOutput := currentText
	status := "success"
	if finalRawTranscript == "" {
		status = "success_empty"
	}
	loggedModel := ""
	{
		loggedModel = pctx.LLMModel
		if loggedModel == "" {
			loggedModel = pctx.LLMProvider
		}
	}

	logID, err := a.DB.LogUsageAndUpdateQuota(
		user.ID,
		audioDuration,
		float64(totalLatencyMs)/1000.0,
		dsLatency,
		float64(totalLatencyMs)/1000.0,
		true,
		loggedModel,
		finalRawTranscript,
		finalOutput,
		status,
		audioSizeBytes,
		provider,
		pctx.LLMProvider, auditJSON(audit),
	)
	if err != nil {
		log.Printf("[STREAM LOG ERROR] Failed to log usage: %v", err)
	}

	ackToken := auth.GenerateLogAckToken(user.ID, logID)

	_ = writeStreamJSON(ctx, conn, StreamEventMessage{
		Type:       "final",
		Warning:    enhancementWarning,
		Text:       finalOutput,
		LogID:      logID,
		AckToken:   ackToken,
		LatencyMs:  totalLatencyMs,
		Provider:   provider,
		Vocabulary: &audit,
	})

	conn.Close(websocket.StatusNormalClosure, "Transcription complete")
}

func (a *App) handlePseudoStreamingSession(
	ctx context.Context,
	conn *websocket.Conn,
	user *db.UserProfile,
	vocabTerms []db.VocabularyTerm,
	startPayload StreamStartPayload,
	provider string,
	startTime time.Time,
) {
	var fullAudioAccumulator bytes.Buffer
	enhance := true
	customPrompt := startPayload.Prompt

	var committedText string
	var chunkCount int

	// Read audio chunks loop (VAD pause chunks)
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				break
			}
			log.Printf("[STREAM INFO] Connection closed by client: %v", err)
			break
		}

		if msgType == websocket.MessageText {
			var control map[string]interface{}
			if err := json.Unmarshal(data, &control); err == nil {
				t, _ := control["type"].(string)
				if t == "end_of_stream" || t == "stop" {
					log.Printf("[STREAM CLIENT] Received %s signal from user ID: %d", t, user.ID)
					break
				}
			}
		} else if msgType == websocket.MessageBinary {
			fullAudioAccumulator.Write(data)
			if fullAudioAccumulator.Len() > 15<<20 {
				_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Audio stream payload exceeded 15MB limit"})
				break
			}

			chunkCount++
			currentChunk := chunkCount

			var toTranscribe []byte
			if startPayload.Format == "pcm" || startPayload.Format == "" {
				toTranscribe = utils.EncodePCMToWAV(data, 16000, 1)
			} else {
				toTranscribe = data
			}

			// User prompts belong to the grammar LLM, never decoder bias.
			promptCtx := utils.GetPromptContext(committedText, 150)

			audioDurationSec := utils.GetOggOpusDuration(toTranscribe)
			if audioDurationSec <= 0 && (startPayload.Format == "pcm" || startPayload.Format == "") {
				audioDurationSec = float64(len(data)) / 32000.0
			}

			// Ignore tiny trailing noise or empty header packets
			if (len(data) < 500 && audioDurationSec > 0 && audioDurationSec < 0.35) || len(data) < 250 {
				continue
			}

			log.Printf("[STREAM ASR REQUEST] Chunk: %d | User ID: %d | Audio: %d bytes (%.2fs)",
				currentChunk, user.ID, len(data), audioDurationSec)

			startASR := time.Now()
			rawChunkText, _, usedProvider, _, err := services.RunStreamingTranscribeWithPrompt(ctx, toTranscribe, user.Languages, promptCtx)
			asrDuration := time.Since(startASR)

			if err != nil {
				log.Printf("[STREAM ASR ERROR] Chunk: %d | User ID: %d | Error: %v | Latency: %v", currentChunk, user.ID, err, asrDuration)
				continue
			}

			rawChunkText = strings.TrimSpace(rawChunkText)
			log.Printf("[STREAM ASR RESPONSE] Chunk: %d | User ID: %d | Provider: %s | Result: %q | Latency: %v",
				currentChunk, user.ID, usedProvider, rawChunkText, asrDuration)

			if rawChunkText != "" {

				previousCommitted := committedText
				committedText = utils.MergeTranscripts(committedText, rawChunkText)
				log.Printf("[STREAM ASR MERGE] Chunk: %d | Prev: %q | New: %q -> Merged: %q",
					currentChunk, previousCommitted, rawChunkText, committedText)

				_ = writeStreamJSON(ctx, conn, StreamEventMessage{
					Type: "interim",
					Text: committedText,
				})
			}
		}
	}

	audioRaw := fullAudioAccumulator.Bytes()
	audioSizeBytes := len(audioRaw)
	if audioSizeBytes == 0 {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "final", Text: ""})
		conn.Close(websocket.StatusNormalClosure, "Empty audio stream")
		return
	}

	var audioDuration float64
	if startPayload.Format == "pcm" || startPayload.Format == "" {
		audioDuration = float64(len(audioRaw)) / (16000.0 * 2.0)
	} else {
		audioDuration = utils.GetOggOpusDuration(audioRaw)
	}
	whisperStart := time.Now()

	finalRawTranscript := committedText
	var asrProvider string = provider

	// Fallback to full audio if no committed text was produced
	if finalRawTranscript == "" {
		var toTranscribe []byte
		if startPayload.Format == "pcm" || startPayload.Format == "" {
			toTranscribe = utils.EncodePCMToWAV(audioRaw, 16000, 1)
		} else {
			toTranscribe = audioRaw
		}
		var asrRouting string
		var asrErr error
		finalRawTranscript, _, asrProvider, asrRouting, asrErr = services.RunStreamingTranscribe(ctx, toTranscribe, user.Languages)
		if asrErr != nil {
			log.Printf("[STREAM TRANSCRIBE ERROR] %v (routing: %s)", asrErr, asrRouting)
			_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: modelresult.New("asr_failed", "asr").Message, Code: "asr_failed", Stage: "asr"})
			conn.Close(websocket.StatusInternalError, "ASR Failed")
			return
		}
	}

	audit := finalizeVocabulary(ctx, finalRawTranscript, vocabTerms, nil, nil)
	grammarInput, restoreGrammar := audit.Protect()

	whisperLatency := time.Since(whisperStart).Seconds()

	currentText := audit.Resolved
	var dsLatency float64
	var enhancementWarning string
	pctx := pipeline.NewPipelineContext(user.ID)

	if enhance && finalRawTranscript != "" {
		tstart := time.Now()
		log.Printf("[STREAM AI ENHANCE START] User ID: %d | Protected Vocab: %d terms", user.ID, len(audit.Decisions))
		enhancer := pipeline.NewGrammarEnhancementTransform(customPrompt, config.DeepSeekAPIKey, config.DeepSeekAPIURL)
		enhancedText, terr := enhancer.Transform(ctx, grammarInput, pctx)
		dsLatency = time.Since(tstart).Seconds()
		if terr != nil {
			enhancementWarning = modelresult.New("enhancement_failed", "enhancement").Message
			log.Printf("[STREAM AI ENHANCE ERROR] User ID: %d | Error: %v | Latency: %.2fs", user.ID, terr, dsLatency)
		} else if enhancedText != "" {
			log.Printf("[STREAM AI ENHANCE SUCCESS] User ID: %d | Latency: %.2fs (model: %s, provider: %s)", user.ID, dsLatency, pctx.LLMModel, pctx.LLMProvider)
			currentText = restoreGrammar(enhancedText)
		}
	}

	totalLatencyMs := time.Since(startTime).Milliseconds()

	// Log usage asynchronously
	finalOutput := currentText
	status := "success"
	if finalRawTranscript == "" {
		status = "success_empty"
	}
	loggedModel := ""
	if enhance {
		loggedModel = pctx.LLMModel
		if loggedModel == "" {
			loggedModel = pctx.LLMProvider
		}
	}

	logID, err := a.DB.LogUsageAndUpdateQuota(
		user.ID,
		audioDuration,
		whisperLatency,
		dsLatency,
		float64(totalLatencyMs)/1000.0,
		enhance,
		loggedModel,
		finalRawTranscript,
		finalOutput,
		status,
		audioSizeBytes,
		asrProvider,
		pctx.LLMProvider, auditJSON(audit),
	)
	if err != nil {
		log.Printf("[STREAM LOG ERROR] Failed to log usage/quota: %v", err)
	}

	ackToken := auth.GenerateLogAckToken(user.ID, logID)

	_ = writeStreamJSON(ctx, conn, StreamEventMessage{
		Type:       "final",
		Warning:    enhancementWarning,
		Text:       finalOutput,
		LogID:      logID,
		AckToken:   ackToken,
		LatencyMs:  totalLatencyMs,
		Provider:   asrProvider,
		Vocabulary: &audit,
	})

	conn.Close(websocket.StatusNormalClosure, "Transcription complete")
}

func writeStreamJSON(ctx context.Context, conn *websocket.Conn, msg StreamEventMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
