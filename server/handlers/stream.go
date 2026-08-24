package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/pipeline"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/utils"
)

type StreamStartPayload struct {
	Type        string `json:"type"` // "start"
	Format      string `json:"format"` // "opus" | "wav" | "pcm"
	SampleRate  int    `json:"sample_rate"`
	Channels    int    `json:"channels"`
	Enhance     bool   `json:"enhance"`
	Prompt      string `json:"prompt"`
}

type StreamEventMessage struct {
	Type       string  `json:"type"` // "interim" | "final" | "error"
	Text       string  `json:"text,omitempty"`
	LogID      int64   `json:"log_id,omitempty"`
	AckToken   string  `json:"ack_token,omitempty"`
	LatencyMs  int64   `json:"latency_ms,omitempty"`
	Provider   string  `json:"provider,omitempty"`
	Error      string  `json:"error,omitempty"`
}

func (a *App) HandleStreamTranscribe(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		OriginPatterns:     []string{"*"},
	})
	if err != nil {
		log.Printf("[STREAM ERROR] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "the session terminated")
	conn.SetReadLimit(32 << 20) // Allow reading up to 32MB frames (default was 32KB)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// 1. Authenticate user from Query Param, Cookie, or Authorization Header
	token := r.URL.Query().Get("token")
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if token == "" {
		if cookie, err := r.Cookie("token"); err == nil {
			token = cookie.Value
		}
	}

	if token == "" {
		_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Missing authentication token"})
		conn.Close(websocket.StatusPolicyViolation, "Unauthorized")
		return
	}

	user, err := a.DB.GetUserBySession(token)
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

	var fullAudioAccumulator bytes.Buffer
	var startPayload StreamStartPayload
	streamStartTime := time.Now()
	enhance := false
	customPrompt := ""

	var committedText string
	var chunkCount int

	// Read audio and control loop
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
				if t == "start" {
					_ = json.Unmarshal(data, &startPayload)
					enhance = startPayload.Enhance
					customPrompt = startPayload.Prompt
					log.Printf("[STREAM START] Session initialized | User ID: %d | Lang: %v | Enhance: %v | Prompt: %q",
						user.ID, user.Languages, enhance, customPrompt)
					continue
				} else if t == "end_of_stream" || t == "stop" {
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

			promptCtx := utils.GetPromptContext(committedText, 150)
			effectivePrompt := promptCtx
			if customPrompt != "" {
				if promptCtx != "" {
					effectivePrompt = customPrompt + " " + promptCtx
				} else {
					effectivePrompt = customPrompt
				}
			}

			audioDurationSec := utils.GetOggOpusDuration(toTranscribe)
			if audioDurationSec <= 0 && (startPayload.Format == "pcm" || startPayload.Format == "") {
				audioDurationSec = float64(len(data)) / 32000.0
			}

			log.Printf("[STREAM ASR REQUEST] Chunk: %d | User ID: %d | Audio: %d bytes (%.2fs) | Prompt: %q",
				currentChunk, user.ID, len(data), audioDurationSec, effectivePrompt)

			startASR := time.Now()
			rawChunkText, _, usedProvider, _, err := services.RunStreamingTranscribeWithPrompt(ctx, toTranscribe, user.Languages, effectivePrompt)
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

	activeConfig := config.ConfigManager.Get()
	streamOrder := activeConfig.GetActiveStreamingFallbackOrder()
	selectedStreamingProvider := "deepinfra_nemotron"
	if len(streamOrder) > 0 {
		selectedStreamingProvider = streamOrder[0]
	}

	var asrProvider string = selectedStreamingProvider

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
			_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: fmt.Sprintf("Transcription error: %v", asrErr)})
			conn.Close(websocket.StatusInternalError, "ASR Failed")
			return
		}
	}

	whisperLatency := time.Since(whisperStart).Seconds()

	currentText := finalRawTranscript
	var dsLatency float64
	pctx := pipeline.NewPipelineContext(user.ID)

	if enhance && finalRawTranscript != "" {
		tstart := time.Now()
		enhancer := pipeline.NewAIEnhancementTransform(customPrompt, "deepseek-v4-flash", config.DeepSeekAPIKey, config.DeepSeekAPIURL)
		enhancedText, terr := enhancer.Transform(ctx, finalRawTranscript, pctx)
		if terr != nil {
			log.Printf("[STREAM LLM ERROR] %v", terr)
		} else if enhancedText != "" {
			currentText = enhancedText
		}
		dsLatency = time.Since(tstart).Seconds()
	}

	totalLatencyMs := time.Since(streamStartTime).Milliseconds()

	// Log usage asynchronously
	finalOutput := currentText
	status := "success"
	if finalRawTranscript == "" {
		status = "success_empty"
	}
	loggedModel := ""
	if enhance {
		loggedModel = "deepseek-v4-flash"
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
		pctx.LLMProvider,
	)
	if err != nil {
		log.Printf("[STREAM LOG ERROR] Failed to log usage/quota: %v", err)
	}

	ackToken := auth.GenerateLogAckToken(user.ID, logID)

	_ = writeStreamJSON(ctx, conn, StreamEventMessage{
		Type:      "final",
		Text:      finalOutput,
		LogID:     logID,
		AckToken:  ackToken,
		LatencyMs: totalLatencyMs,
		Provider:  selectedStreamingProvider,
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
