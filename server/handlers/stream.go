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
	"sync"
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
	var activeChunkAudio bytes.Buffer
	var startPayload StreamStartPayload
	streamStartTime := time.Now()
	enhance := false
	customPrompt := ""

	vadTracker := utils.NewAudioChunkTracker(16000, 280.0)
	var committedText string
	var lastEmittedFullText string
	lastInterimTime := time.Now()
	var interimSeq uint64
	var latestEmittedSeq uint64
	var interimInProgress bool
	var interimMu sync.Mutex

	// Read audio loop
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
					if startPayload.SampleRate > 0 {
						vadTracker.SampleRate = startPayload.SampleRate
					}
					continue
				} else if t == "end_of_stream" || t == "stop" {
					break
				}
			}
		} else if msgType == websocket.MessageBinary {
			fullAudioAccumulator.Write(data)
			if fullAudioAccumulator.Len() > 15<<20 {
				_ = writeStreamJSON(ctx, conn, StreamEventMessage{Type: "error", Error: "Audio stream payload exceeded 15MB limit"})
				break
			}

			interimMu.Lock()
			activeChunkAudio.Write(data)

			// Update VAD tracker if format is PCM
			_, shouldCommitByVAD := false, false
			if startPayload.Format == "pcm" || startPayload.Format == "" {
				_, shouldCommitByVAD = vadTracker.ProcessChunk(data)
			}

			// Periodically perform interim transcription if 560ms of audio is buffered in the active chunk
			chunkLen := activeChunkAudio.Len()
			shouldRunInterim := chunkLen >= 17920 && // 560ms at 16kHz 16-bit mono (8960 samples * 2 bytes = 17920)
				time.Since(lastInterimTime) >= 480*time.Millisecond &&
				!interimInProgress

			if shouldRunInterim {
				interimInProgress = true
				interimSeq++
				currentSeq := interimSeq
				lastInterimTime = time.Now()

				// Take a snapshot of the active chunk audio only
				currentChunkBytes := make([]byte, chunkLen)
				copy(currentChunkBytes, activeChunkAudio.Bytes())
				format := startPayload.Format
				committedSnapshot := committedText
				interimMu.Unlock()

				go func(audioSnap []byte, fmtStr string, seq uint64, committedBase string, vadCommitFlag bool) {
					defer func() {
						interimMu.Lock()
						interimInProgress = false
						interimMu.Unlock()
					}()

					var toTranscribe []byte
					if fmtStr == "pcm" || fmtStr == "" {
						toTranscribe = utils.EncodePCMToWAV(audioSnap, 16000, 1)
					} else {
						toTranscribe = audioSnap
					}

					promptCtx := utils.GetPromptContext(committedBase, 150)
					rawChunkText, _, _, _, err := services.RunStreamingTranscribeWithPrompt(ctx, toTranscribe, user.Languages, promptCtx)
					if err == nil {
						rawChunkText = strings.TrimSpace(rawChunkText)

						interimMu.Lock()
						if seq >= latestEmittedSeq {
							latestEmittedSeq = seq

							// Check if this chunk should be committed:
							// 1. VAD detected silence pause after speech
							// 2. Or chunk text ends with sentence boundary punctuation (. ! ?) and audio is >= 1.5s (48000 bytes)
							// 3. Or chunk duration is >= 3.5s (112000 bytes)
							isSentenceEnd := utils.EndsWithSentenceBoundary(rawChunkText)
							hasEnoughSpeech := len(rawChunkText) > 0
							shouldCommit := hasEnoughSpeech && (vadCommitFlag || (isSentenceEnd && len(audioSnap) >= 48000) || len(audioSnap) >= 112000)

							var fullText string
							if shouldCommit {
								committedText = utils.MergeTranscripts(committedText, rawChunkText)

								// Retain trailing 200ms (6400 bytes at 16kHz 16-bit PCM) for acoustic lead-in continuity
								overlapBytes := 6400
								activeBytes := activeChunkAudio.Bytes()
								var retained []byte
								if len(activeBytes) > overlapBytes {
									retained = make([]byte, overlapBytes)
									copy(retained, activeBytes[len(activeBytes)-overlapBytes:])
								} else {
									retained = make([]byte, len(activeBytes))
									copy(retained, activeBytes)
								}
								activeChunkAudio.Reset()
								activeChunkAudio.Write(retained)
								vadTracker.Reset()
								fullText = committedText
							} else {
								// During continuous speech, hold back unstable trailing word(s) to avoid hypothesis flapping
								stabilizedChunk := utils.ExtractStabilizedText(rawChunkText, false)
								fullText = utils.MergeTranscripts(committedBase, stabilizedChunk)
							}

							if fullText != lastEmittedFullText && fullText != "" {
								lastEmittedFullText = fullText
								interimMu.Unlock()
								_ = writeStreamJSON(ctx, conn, StreamEventMessage{
									Type: "interim",
									Text: fullText,
								})
							} else {
								interimMu.Unlock()
							}
						} else {
							interimMu.Unlock()
						}
					}
				}(currentChunkBytes, format, currentSeq, committedSnapshot, shouldCommitByVAD)
			} else {
				interimMu.Unlock()
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

	// Transcribe any remaining tail audio in activeChunkAudio
	interimMu.Lock()
	tailChunkBytes := make([]byte, activeChunkAudio.Len())
	copy(tailChunkBytes, activeChunkAudio.Bytes())
	baseCommitted := committedText
	interimMu.Unlock()

	activeConfig := config.ConfigManager.Get()
	streamOrder := activeConfig.GetActiveStreamingFallbackOrder()
	selectedStreamingProvider := "deepinfra_nemotron"
	if len(streamOrder) > 0 {
		selectedStreamingProvider = streamOrder[0]
	}

	var finalRawTranscript string
	var asrProvider string
	var asrRouting string
	var asrErr error

	if len(tailChunkBytes) >= 3200 { // At least 100ms of audio
		var toTranscribe []byte
		if startPayload.Format == "pcm" || startPayload.Format == "" {
			toTranscribe = utils.EncodePCMToWAV(tailChunkBytes, 16000, 1)
		} else {
			toTranscribe = tailChunkBytes
		}
		var tailText string
		tailPromptCtx := utils.GetPromptContext(baseCommitted, 150)
		tailText, _, asrProvider, asrRouting, asrErr = services.RunStreamingTranscribeWithPrompt(ctx, toTranscribe, user.Languages, tailPromptCtx)
		if asrErr == nil {
			finalRawTranscript = utils.MergeTranscripts(baseCommitted, tailText)
		} else {
			finalRawTranscript = baseCommitted
		}
	} else {
		finalRawTranscript = baseCommitted
	}

	// Fallback to full audio if no committed text was produced
	if finalRawTranscript == "" {
		var toTranscribe []byte
		if startPayload.Format == "pcm" || startPayload.Format == "" {
			toTranscribe = utils.EncodePCMToWAV(audioRaw, 16000, 1)
		} else {
			toTranscribe = audioRaw
		}
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
