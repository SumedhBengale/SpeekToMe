package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/pipeline"
	"speektome-server/ratelimit"
	"speektome-server/utils"
)

// Server 1 default streaming endpoints
const (
	DefaultServer1ASROpusURL   = "ws://GPU_HOST_IP:8080/v1/realtime"
	DefaultServer1DiarizeURL   = "ws://GPU_HOST_IP:8080/v1/audio/diarize"
)

// MeetingStreamStartPayload sent by client upon WebSocket connection
type MeetingStreamStartPayload struct {
	Type       string `json:"type"` // "start"
	Title      string `json:"title"`
	SampleRate int    `json:"sample_rate"` // 16000
	Channels   int    `json:"channels"`    // 1
}

// ClientMeetingTurn represents a live transcript segment emitted to the client
type ClientMeetingTurn struct {
	ID          string  `json:"id"`
	Channel     string  `json:"channel"`      // "mic" (local user) | "system" (remote attendees)
	SpeakerID   string  `json:"speaker_id"`   // e.g. "speaker_local", "speaker_1", "speaker_2"
	SpeakerName string  `json:"speaker_name"` // Display name (e.g. "John Doe", "Speaker 1")
	StartTime   float64 `json:"start_time"`   // Seconds from session start
	EndTime     float64 `json:"end_time"`     // Seconds from session start
	Text        string  `json:"text"`         // Spoken words
	IsFinal     bool    `json:"is_final"`     // True if phrase is finalized
}

// MeetingStreamEventMessage sent to client
type MeetingStreamEventMessage struct {
	Type      string             `json:"type"` // "session_ready" | "turn" | "completed" | "error"
	MeetingID int64              `json:"meeting_id,omitempty"`
	Title     string             `json:"title,omitempty"`
	Turn      *ClientMeetingTurn `json:"turn,omitempty"`
	Meeting   *db.Meeting        `json:"meeting,omitempty"`
	Error     string             `json:"error,omitempty"`
}

// DiarizationSegment from Server 2 Sortformer
type DiarizationSegment struct {
	Type       string  `json:"type"` // "diarization.segment"
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Speaker    string  `json:"speaker"` // e.g. "speaker_1", "speaker_2"
	Confidence float64 `json:"confidence,omitempty"`
}

// UpstreamInferenceWS encapsulates a WebSocket connection to an inference worker
type UpstreamInferenceWS struct {
	Conn       *websocket.Conn
	URL        string
	Role       string
	CancelFunc context.CancelFunc
}

func (u *UpstreamInferenceWS) SendAudio(ctx context.Context, data []byte) error {
	if u == nil || u.Conn == nil {
		return fmt.Errorf("upstream connection not open")
	}
	return u.Conn.Write(ctx, websocket.MessageBinary, data)
}

func (u *UpstreamInferenceWS) SendControl(ctx context.Context, msg map[string]interface{}) error {
	if u == nil || u.Conn == nil {
		return fmt.Errorf("upstream connection not open")
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return u.Conn.Write(ctx, websocket.MessageText, bytes)
}

func (u *UpstreamInferenceWS) Close(code websocket.StatusCode, reason string) {
	if u == nil {
		return
	}
	if u.CancelFunc != nil {
		u.CancelFunc()
	}
	if u.Conn != nil {
		_ = u.Conn.Close(code, reason)
	}
}

func dialUpstreamInferenceWS(ctx context.Context, rawURL, role string) (*UpstreamInferenceWS, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, rawURL, &websocket.DialOptions{
		Subprotocols: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to dial upstream %s (%s): %w", role, rawURL, err)
	}

	_, sessCancel := context.WithCancel(ctx)
	return &UpstreamInferenceWS{
		Conn:       conn,
		URL:        rawURL,
		Role:       role,
		CancelFunc: sessCancel,
	}, nil
}

// HandleMeetingStream handles live real-time bidirectional meeting transcription & diarization
func (a *App) HandleMeetingStream(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		OriginPatterns:     []string{"*"},
	})
	if err != nil {
		log.Printf("[MEETING STREAM ERROR] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "session terminated")
	conn.SetReadLimit(64 << 20) // Allow reading up to 64MB total

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Hour) // Max 4 hour meeting session
	defer cancel()

	// 1. Authenticate user
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
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: "Missing authentication token"})
		conn.Close(websocket.StatusPolicyViolation, "Unauthorized")
		return
	}

	user, vocabTerms, err := a.DB.GetUserAndVocabularyBySession(token)
	if err != nil || user == nil {
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: "Invalid or expired session token"})
		conn.Close(websocket.StatusPolicyViolation, "Unauthorized")
		return
	}

	limit := config.ConfigManager.Get().DefaultUserRateLimitRPM
	if user.RateLimitRPM.Valid {
		userOverride := int(user.RateLimitRPM.Int64)
		if userOverride < limit {
			limit = userOverride
		}
	}
	userLimiter := ratelimit.UserRateLimiterReg.GetLimiter(user.ID, limit)
	if !userLimiter.Allow() {
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: "Rate limit exceeded. Please try again later."})
		conn.Close(websocket.StatusPolicyViolation, "Rate limit exceeded")
		return
	}

	// 2. Validate user quotas
	isPaid := user.PlanActive == 1 && user.PlanTier == "pro"
	monthlyLimitSeconds := 3600.0
	cfg := config.ConfigManager.GetRawAll()
	if isPaid {
		monthlyLimitSeconds = 180000.0
		if val, ok := cfg["quota_paid_monthly_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	} else {
		if val, ok := cfg["quota_free_monthly_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	}

	if user.MonthlyUsageSeconds >= monthlyLimitSeconds {
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
			Type:  "error",
			Error: fmt.Sprintf("Monthly usage limit reached (%.1f / %.1f hours). Please upgrade in Settings.", user.MonthlyUsageSeconds/3600.0, monthlyLimitSeconds/3600.0),
		})
		conn.Close(websocket.StatusPolicyViolation, "Monthly limit exceeded")
		return
	}

	// 3. Read initial handshake message
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		log.Printf("[MEETING STREAM ERROR] Failed to read start handshake: %v", err)
		return
	}

	var startPayload MeetingStreamStartPayload
	if msgType == websocket.MessageText {
		_ = json.Unmarshal(data, &startPayload)
	}

	title := strings.TrimSpace(startPayload.Title)
	if title == "" {
		title = fmt.Sprintf("Meeting on %s", time.Now().Format("2006-01-02 15:04"))
	}

	userName := user.Name
	if userName == "" {
		userName = "Local Speaker"
	}

	log.Printf("[MEETING STREAM START] User %s (ID: %d) started meeting session %q", user.Email, user.ID, title)

	// 4. Resolve Upstream Inference URLs
	s1ASRURL := DefaultServer1ASROpusURL
	s1DiarizeURL := DefaultServer1DiarizeURL

	// Check if configured dynamically in saas_config
	mCfg := config.ConfigManager.Get().Models
	if prov, ok := mCfg.StreamingProviders["self_hosted_nemotron_3_5"]; ok && prov.BaseURL != "" {
		s1ASRURL = prov.BaseURL
	}

	// 5. Connect 3 upstream inference streams (All on Server 1)
	// Stream 1: Mic ASR (Local user)
	s1MicASR, err := dialUpstreamInferenceWS(ctx, s1ASRURL, "Server 1 Mic ASR")
	if err != nil {
		log.Printf("[MEETING STREAM WARNING] Server 1 Mic ASR connect failed: %v", err)
	}
	// Stream 2: Loopback ASR (Remote meeting attendees)
	s1LoopbackASR, err := dialUpstreamInferenceWS(ctx, s1ASRURL, "Server 1 Loopback ASR")
	if err != nil {
		log.Printf("[MEETING STREAM WARNING] Server 1 Loopback ASR connect failed: %v", err)
	}
	// Stream 3: Loopback Diarization (Sortformer v2.1)
	s1LoopbackDiarize, err := dialUpstreamInferenceWS(ctx, s1DiarizeURL, "Server 1 Loopback Diarization")
	if err != nil {
		log.Printf("[MEETING STREAM WARNING] Server 1 Loopback Diarization connect failed: %v", err)
	}

	defer func() {
		if s1MicASR != nil {
			s1MicASR.Close(websocket.StatusNormalClosure, "meeting finished")
		}
		if s1LoopbackASR != nil {
			s1LoopbackASR.Close(websocket.StatusNormalClosure, "meeting finished")
		}
		if s1LoopbackDiarize != nil {
			s1LoopbackDiarize.Close(websocket.StatusNormalClosure, "meeting finished")
		}
	}()

	// Send initial session setup message to upstreams
	micLang := utils.ResolveNemotronLanguage(user.Languages)
	sessionInitMsgMicASR := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"sample_rate":           16000,
			"automatic_punctuation": true,
			"language":              micLang,
			"model":                 "nemotron-3.5-asr-streaming-0.6b.planar.q8_0",
		},
	}
	if s1MicASR != nil {
		_ = s1MicASR.SendControl(ctx, sessionInitMsgMicASR)
	}

	sessionInitMsgLoopbackASR := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"sample_rate":           16000,
			"automatic_punctuation": true,
			"language":              "auto",
			"model":                 "nemotron-3.5-asr-streaming-0.6b.planar.q8_0",
		},
	}
	if s1LoopbackASR != nil {
		_ = s1LoopbackASR.SendControl(ctx, sessionInitMsgLoopbackASR)
	}

	sessionInitMsgDiarize := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"sample_rate": 16000,
			"model":       "diarization",
		},
	}
	if s1LoopbackDiarize != nil {
		_ = s1LoopbackDiarize.SendControl(ctx, sessionInitMsgDiarize)
	}

	_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
		Type:  "session_ready",
		Title: title,
	})

	// Session State tracking
	var turnsMu sync.Mutex
	var activeTurns []ClientMeetingTurn
	var fullAudioMixed bytes.Buffer

	var micAudioAccumulator bytes.Buffer
	var loopbackAudioAccumulator bytes.Buffer

	// Diarization segment cache for remote loopback speakers
	var remoteSegmentsMu sync.Mutex
	var remoteSegments []DiarizationSegment
	var incomingBatchSegs []DiarizationSegment
	var diarizeInFlight atomic.Bool
	var isMeetingEnding atomic.Bool
	diarDoneChan := make(chan struct{})
	var closeDiarOnce sync.Once
	closeDiarDone := func() {
		closeDiarOnce.Do(func() {
			close(diarDoneChan)
		})
	}

	// Monotonic Session Clock Tracking (560ms chunks)
	var micChunkCount int64
	var loopbackChunkCount int64
	var chunkCountMu sync.Mutex

	// Trigger non-blocking diarization commit pass to upstream Sortformer
	triggerDiarizeCommit := func() {
		if s1LoopbackDiarize == nil || isMeetingEnding.Load() {
			return
		}
		if diarizeInFlight.CompareAndSwap(false, true) {
			commitMsg := map[string]interface{}{"type": "input_audio_buffer.commit"}
			if err := s1LoopbackDiarize.SendControl(ctx, commitMsg); err != nil {
				diarizeInFlight.Store(false)
			}
		}
	}

	// Helper function to resolve speaker for a phrase timestamp window with exact overlap check
	resolveRemoteSpeakerWithOverlap := func(startTime, endTime float64) (string, bool) {
		remoteSegmentsMu.Lock()
		defer remoteSegmentsMu.Unlock()

		if len(remoteSegments) == 0 {
			return "Speaker 1", false
		}

		bestSpeaker := ""
		var maxOverlap float64 = 0.0

		for _, seg := range remoteSegments {
			// Overlap formula: max(0, min(wEnd, dEnd) - max(wStart, dStart))
			overlap := mathMax(0, mathMin(endTime, seg.End)-mathMax(startTime, seg.Start))
			if overlap > maxOverlap {
				maxOverlap = overlap
				bestSpeaker = formatSpeakerDisplayName(seg.Speaker)
			}
		}

		if maxOverlap > 0.0 {
			return bestSpeaker, true
		}

		// Fallback to nearest segment in time if no direct temporal overlap
		var minDistance float64 = 1e9
		for _, seg := range remoteSegments {
			var dist float64
			if endTime < seg.Start {
				dist = seg.Start - endTime
			} else if startTime > seg.End {
				dist = startTime - seg.End
			} else {
				dist = 0
			}
			if dist < minDistance {
				minDistance = dist
				bestSpeaker = formatSpeakerDisplayName(seg.Speaker)
			}
		}

		if bestSpeaker == "" {
			bestSpeaker = "Speaker 1"
		}
		return bestSpeaker, false
	}

	resolveRemoteSpeaker := func(startTime, endTime float64) string {
		spk, _ := resolveRemoteSpeakerWithOverlap(startTime, endTime)
		return spk
	}

	// Goroutine to listen to Server 1 Loopback Diarizer (Sortformer)
	if s1LoopbackDiarize != nil {
		go func() {
			defer closeDiarDone()
			for {
				msgType, data, err := s1LoopbackDiarize.Conn.Read(ctx)
				if err != nil {
					break
				}
				if msgType == websocket.MessageText {
					log.Printf("[SERVER RAW DIARIZE LOOPBACK] %s", string(data))
					var event map[string]interface{}
					if json.Unmarshal(data, &event) == nil {
						evtType, _ := event["type"].(string)
						if evtType == "diarization.segment" {
							start, _ := event["start"].(float64)
							end, _ := event["end"].(float64)
							spk, _ := event["speaker"].(string)
							conf, _ := event["confidence"].(float64)
							if spk != "" {
								incomingBatchSegs = append(incomingBatchSegs, DiarizationSegment{
									Type:       evtType,
									Start:      start,
									End:        end,
									Speaker:    spk,
									Confidence: conf,
								})
							}
						} else if evtType == "diarization.completed" {
							log.Printf("[SERVER DIARIZE] Sortformer completed batch. Segments in batch: %d", len(incomingBatchSegs))
							remoteSegmentsMu.Lock()
							remoteSegments = make([]DiarizationSegment, len(incomingBatchSegs))
							copy(remoteSegments, incomingBatchSegs)
							remoteSegmentsMu.Unlock()
							incomingBatchSegs = nil
							diarizeInFlight.Store(false)

							// Retroactive speaker re-attribution on recent loopback turns
							if !isMeetingEnding.Load() {
								var turnsToUpdate []ClientMeetingTurn
								turnsMu.Lock()
								for i := range activeTurns {
									if activeTurns[i].Channel == "system" {
										resolved := resolveRemoteSpeaker(activeTurns[i].StartTime, activeTurns[i].EndTime)
										if resolved != "" && resolved != activeTurns[i].SpeakerName {
											log.Printf("[SERVER DIARIZE RETROACTIVE] Turn %s updated: %s -> %s",
												activeTurns[i].ID, activeTurns[i].SpeakerName, resolved)
											activeTurns[i].SpeakerName = resolved
											activeTurns[i].SpeakerID = sanitizeSpeakerID(resolved)
											turnsToUpdate = append(turnsToUpdate, activeTurns[i])
										}
									}
								}
								turnsMu.Unlock()

								for _, turn := range turnsToUpdate {
									turnCopy := turn
									_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
										Type: "turn",
										Turn: &turnCopy,
									})
								}
							} else {
								// Final meeting diarization pass complete
								closeDiarDone()
								return
							}
						} else if evtType == "error" {
							log.Printf("[SERVER DIARIZE ERROR] Upstream error: %v", event["error"])
							diarizeInFlight.Store(false)
						}
					}
				}
			}
		}()
	} else {
		closeDiarDone()
	}

	// Periodic background ticker to keep Sortformer diarization refreshed during active speech
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		var lastCheckChunks int64 = 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if isMeetingEnding.Load() {
					return
				}
				chunkCountMu.Lock()
				currentChunks := loopbackChunkCount
				chunkCountMu.Unlock()
				if currentChunks > lastCheckChunks+3 {
					lastCheckChunks = currentChunks
					triggerDiarizeCommit()
				}
			}
		}
	}()

	type TimedWordToken struct {
		Text      string
		StartTime float64
		EndTime   float64
	}

	var micTokens []TimedWordToken
	var micTokensMu sync.Mutex

	var loopbackTokens []TimedWordToken
	var loopbackTokensMu sync.Mutex

	// Goroutine to listen to Server 1 Mic ASR (Local user)
	if s1MicASR != nil {
		go func() {
			var currentTurnText string
			var turnStartTime float64 = 0.0
			var turnID string = fmt.Sprintf("turn_mic_%d", time.Now().UnixNano())
			var lastDeltaTime time.Time = time.Now()

			for {
				msgType, data, err := s1MicASR.Conn.Read(ctx)
				if err != nil {
					break
				}
				if msgType == websocket.MessageText {
					var event map[string]interface{}
					if json.Unmarshal(data, &event) == nil {
						// Case A: Delta token
						if delta, ok := event["delta"].(string); ok && delta != "" {
							audioProc, _ := event["audio_processed"].(float64)
							now := time.Now()

							// If starting a new utterance after pause / silence (> 800ms)
							var tokenStart float64
							if currentTurnText == "" || now.Sub(lastDeltaTime) > 800*time.Millisecond {
								// Acoustic lookahead: Speech onset begins in the prior chunk before RNN-T word commit
								// An RNN-T token commits ~1.90s after acoustic speech onset (560ms chunks + right-context + word beam)
								turnStartTime = mathMax(0, audioProc-1.90)
								tokenStart = turnStartTime
								turnID = fmt.Sprintf("turn_mic_%d", now.UnixNano())
								currentTurnText = ""
							} else {
								tokenStart = mathMax(0, audioProc-1.12)
							}
							tokenEnd := audioProc
							lastDeltaTime = now

							micTokensMu.Lock()
							micTokens = append(micTokens, TimedWordToken{
								Text:      delta,
								StartTime: tokenStart,
								EndTime:   tokenEnd,
							})
							micTokensMu.Unlock()

							currentTurnText += delta
							cleanText := strings.TrimSpace(currentTurnText)
							if cleanText != "" {
								turn := ClientMeetingTurn{
									ID:          turnID,
									Channel:     "mic",
									SpeakerID:   "speaker_local",
									SpeakerName: userName,
									StartTime:   turnStartTime,
									EndTime:     audioProc,
									Text:        cleanText,
									IsFinal:     false,
								}

								turnsMu.Lock()
								upsertMeetingTurn(&activeTurns, turn)
								turnsMu.Unlock()

								_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
									Type: "turn",
									Turn: &turn,
								})
							}
						}

						// Case B: Completed utterance
						if transcript, ok := event["transcript"].(string); ok && transcript != "" {
							audioProc, _ := event["audio_processed"].(float64)
							cleanText := strings.TrimSpace(transcript)
							if cleanText != "" {
								turn := ClientMeetingTurn{
									ID:          turnID,
									Channel:     "mic",
									SpeakerID:   "speaker_local",
									SpeakerName: userName,
									StartTime:   turnStartTime,
									EndTime:     audioProc,
									Text:        cleanText,
									IsFinal:     true,
								}

								turnsMu.Lock()
								upsertMeetingTurn(&activeTurns, turn)
								turnsMu.Unlock()

								_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
									Type: "turn",
									Turn: &turn,
								})
							}
							currentTurnText = ""
						}
					}
				}
			}
		}()
	}

	// Goroutine to listen to Server 1 Loopback ASR (Remote meeting attendees)
	if s1LoopbackASR != nil {
		go func() {
			var currentTurnText string
			var turnStartTime float64 = 0.0
			var turnID string = fmt.Sprintf("turn_lb_%d", time.Now().UnixNano())
			var lastDeltaTime time.Time = time.Now()
			var currentTurnSpeaker string = ""

			for {
				msgType, data, err := s1LoopbackASR.Conn.Read(ctx)
				if err != nil {
					break
				}
				if msgType == websocket.MessageText {
					var event map[string]interface{}
					if json.Unmarshal(data, &event) == nil {
						// Case A: Delta token
						if delta, ok := event["delta"].(string); ok && delta != "" {
							audioProc, _ := event["audio_processed"].(float64)
							now := time.Now()

							var tokenStart float64
							isNewUtterance := currentTurnText == "" || now.Sub(lastDeltaTime) > 800*time.Millisecond
							if isNewUtterance {
								turnStartTime = mathMax(0, audioProc-1.90)
								tokenStart = turnStartTime
								turnID = fmt.Sprintf("turn_lb_%d", now.UnixNano())
								currentTurnText = ""
								currentTurnSpeaker = ""
							} else {
								tokenStart = mathMax(0, audioProc-1.12)
							}
							tokenEnd := audioProc
							lastDeltaTime = now

							loopbackTokensMu.Lock()
							loopbackTokens = append(loopbackTokens, TimedWordToken{
								Text:      delta,
								StartTime: tokenStart,
								EndTime:   tokenEnd,
							})
							loopbackTokensMu.Unlock()

							spkName, hasDirectOverlap := resolveRemoteSpeakerWithOverlap(turnStartTime, audioProc)
							if spkName == "" {
								spkName = "Speaker 1"
							}

							// Turn split detection: If a known speaker transition occurs during speech
							if !isNewUtterance && currentTurnSpeaker != "" && hasDirectOverlap && spkName != currentTurnSpeaker {
								cleanPrev := strings.TrimSpace(currentTurnText)
								if cleanPrev != "" {
									prevTurn := ClientMeetingTurn{
										ID:          turnID,
										Channel:     "system",
										SpeakerID:   sanitizeSpeakerID(currentTurnSpeaker),
										SpeakerName: currentTurnSpeaker,
										StartTime:   turnStartTime,
										EndTime:     tokenStart,
										Text:        cleanPrev,
										IsFinal:     true,
									}
									turnsMu.Lock()
									upsertMeetingTurn(&activeTurns, prevTurn)
									turnsMu.Unlock()

									_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
										Type: "turn",
										Turn: &prevTurn,
									})
								}
								// Start new turn for the transitioning speaker
								turnStartTime = tokenStart
								turnID = fmt.Sprintf("turn_lb_%d", now.UnixNano())
								currentTurnText = ""
							}

							currentTurnSpeaker = spkName
							currentTurnText += delta
							cleanText := strings.TrimSpace(currentTurnText)
							if cleanText != "" {
								spkID := sanitizeSpeakerID(spkName)

								turn := ClientMeetingTurn{
									ID:          turnID,
									Channel:     "system",
									SpeakerID:   spkID,
									SpeakerName: spkName,
									StartTime:   turnStartTime,
									EndTime:     audioProc,
									Text:        cleanText,
									IsFinal:     false,
								}

								turnsMu.Lock()
								upsertMeetingTurn(&activeTurns, turn)
								turnsMu.Unlock()

								_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
									Type: "turn",
									Turn: &turn,
								})
							}
						}

						// Case B: Completed utterance
						if transcript, ok := event["transcript"].(string); ok && transcript != "" {
							audioProc, _ := event["audio_processed"].(float64)
							cleanText := strings.TrimSpace(transcript)
							if cleanText != "" {
								spkName := resolveRemoteSpeaker(turnStartTime, audioProc)
								spkID := sanitizeSpeakerID(spkName)

								turn := ClientMeetingTurn{
									ID:          turnID,
									Channel:     "system",
									SpeakerID:   spkID,
									SpeakerName: spkName,
									StartTime:   turnStartTime,
									EndTime:     audioProc,
									Text:        cleanText,
									IsFinal:     true,
								}

								turnsMu.Lock()
								upsertMeetingTurn(&activeTurns, turn)
								turnsMu.Unlock()

								_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
									Type: "turn",
									Turn: &turn,
								})
							}
							currentTurnText = ""
							currentTurnSpeaker = ""
							// Trigger diarization commit to catch up with newly completed utterance
							go triggerDiarizeCommit()
						}
					}
				}
			}
		}()
	}

	// Main receive loop: Read dual-channel 560ms packets from Windows Client
	sessionStartTime := time.Now()
	for {
		msgType, frame, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure || websocket.CloseStatus(err) == websocket.StatusGoingAway {
				break
			}
			log.Printf("[MEETING STREAM INFO] Client connection ended: %v", err)
			break
		}

		if msgType == websocket.MessageText {
			var control map[string]interface{}
			if json.Unmarshal(frame, &control) == nil {
				t, _ := control["type"].(string)
				if t == "end_of_meeting" || t == "stop" {
					log.Printf("[MEETING STREAM STOP] Received %s from user ID: %d", t, user.ID)
					break
				}
			}
		} else if msgType == websocket.MessageBinary {
			if len(frame) < 2 {
				continue
			}

			channelTag := frame[0] // 0x01 = Mic, 0x02 = Loopback
			opusData := frame[1:]

			chunkCountMu.Lock()
			if channelTag == 0x01 {
				micChunkCount++
				micAudioAccumulator.Write(opusData)
				fullAudioMixed.Write(opusData)

				if s1MicASR != nil {
					_ = s1MicASR.SendAudio(ctx, opusData)
				}
			} else if channelTag == 0x02 {
				loopbackChunkCount++
				loopbackAudioAccumulator.Write(opusData)
				fullAudioMixed.Write(opusData)

				if s1LoopbackASR != nil {
					_ = s1LoopbackASR.SendAudio(ctx, opusData)
				}
				if s1LoopbackDiarize != nil {
					_ = s1LoopbackDiarize.SendAudio(ctx, opusData)
				}
			}
			chunkCountMu.Unlock()
		}
	}

	// 6. Finalize session & Commit upstream engines
	isMeetingEnding.Store(true)
	commitMsg := map[string]interface{}{"type": "input_audio_buffer.commit"}
	if s1MicASR != nil {
		_ = s1MicASR.SendControl(ctx, commitMsg)
		_ = s1MicASR.SendControl(ctx, map[string]interface{}{"type": "end_of_stream"})
	}
	if s1LoopbackASR != nil {
		_ = s1LoopbackASR.SendControl(ctx, commitMsg)
		_ = s1LoopbackASR.SendControl(ctx, map[string]interface{}{"type": "end_of_stream"})
	}
	if s1LoopbackDiarize != nil {
		_ = s1LoopbackDiarize.SendControl(ctx, map[string]interface{}{"type": "end_of_meeting"})
	}

	// Wait for Sortformer diarization completion (up to 30 seconds)
	select {
	case <-diarDoneChan:
		log.Printf("[SERVER DIARIZE SUCCESS] Received all Sortformer segments before assembling meeting transcript.")
	case <-time.After(30 * time.Second):
		log.Printf("[SERVER DIARIZE WARNING] Timed out waiting for Sortformer segments. Proceeding with collected segments.")
	}

	// 7. Calculate Ground-Truth Audio Duration strictly from processed audio chunks
	chunkCountMu.Lock()
	maxChunks := mathMax(float64(micChunkCount), float64(loopbackChunkCount))
	chunkCountMu.Unlock()
	sessionDuration := maxChunks * 0.560
	if sessionDuration <= 0 {
		sessionDuration = time.Since(sessionStartTime).Seconds()
	}

	dir := "uploads/meetings"
	_ = os.MkdirAll(dir, 0700)
	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("meeting_%d_%d.ogg", user.ID, timestamp)
	audioPath := filepath.Join(dir, filename)

	micFilename := fmt.Sprintf("meeting_%d_%d_mic.ogg", user.ID, timestamp)
	micPath := filepath.Join(dir, micFilename)

	lbFilename := fmt.Sprintf("meeting_%d_%d_lb.ogg", user.ID, timestamp)
	lbPath := filepath.Join(dir, lbFilename)

	hasMic := micAudioAccumulator.Len() > 0
	hasLb := loopbackAudioAccumulator.Len() > 0

	if hasMic {
		_ = os.WriteFile(micPath, micAudioAccumulator.Bytes(), 0600)
	}
	if hasLb {
		_ = os.WriteFile(lbPath, loopbackAudioAccumulator.Bytes(), 0600)
	}

	if hasMic && hasLb {
		log.Printf("[AUDIO MIXING] Combining mic and loopback audio into single ground-truth audio: %s", audioPath)
		cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
			"-i", micPath,
			"-i", lbPath,
			"-filter_complex", "[0:a][1:a]amix=inputs=2:duration=longest:dropout_transition=0[aout]",
			"-map", "[aout]",
			"-c:a", "libopus",
			"-b:a", "64k",
			audioPath,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[AUDIO MIXING WARNING] FFmpeg mix failed: %v, output: %s. Falling back to mic audio.", err, string(output))
			_ = os.WriteFile(audioPath, micAudioAccumulator.Bytes(), 0600)
		} else {
			log.Printf("[AUDIO MIXING SUCCESS] Successfully generated mixed audio file: %s", audioPath)
		}
	} else if hasMic {
		log.Printf("[AUDIO MIXING] Only mic audio present. Saving mic as audio file: %s", audioPath)
		_ = os.WriteFile(audioPath, micAudioAccumulator.Bytes(), 0600)
	} else if hasLb {
		log.Printf("[AUDIO MIXING] Only loopback audio present. Saving loopback as audio file: %s", audioPath)
		_ = os.WriteFile(audioPath, loopbackAudioAccumulator.Bytes(), 0600)
	} else {
		_ = os.WriteFile(audioPath, []byte{}, 0600)
	}

	// 8. Assemble Final Diarized Dialogue & JSON Segments
	type FinalSegment struct {
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		Text    string  `json:"text"`
		Speaker string  `json:"speaker"`
	}

	var finalSegments []FinalSegment

	// A. Process Mic Tokens (Local User)
	micTokensMu.Lock()
	if len(micTokens) > 0 {
		var curText strings.Builder
		var curStart float64 = micTokens[0].StartTime
		var curEnd float64 = micTokens[0].EndTime

		for i, tok := range micTokens {
			if i > 0 && (tok.StartTime-curEnd > 1.8) {
				trimmed := strings.TrimSpace(curText.String())
				if trimmed != "" {
					finalSegments = append(finalSegments, FinalSegment{
						Start:   curStart,
						End:     curEnd + 0.08,
						Text:    trimmed,
						Speaker: userName,
					})
				}
				curText.Reset()
				curStart = tok.StartTime
			}
			curText.WriteString(tok.Text)
			curEnd = tok.EndTime
		}
		trimmed := strings.TrimSpace(curText.String())
		if trimmed != "" {
			finalSegments = append(finalSegments, FinalSegment{
				Start:   curStart,
				End:     curEnd + 0.08,
				Text:    trimmed,
				Speaker: userName,
			})
		}
	}
	micTokensMu.Unlock()

	// B. Process Loopback Tokens (Diarization-Anchored on Server Sortformer Timestamps)
	remoteSegmentsMu.Lock()
	sortformerSegs := make([]DiarizationSegment, len(remoteSegments))
	copy(sortformerSegs, remoteSegments)
	remoteSegmentsMu.Unlock()

	loopbackTokensMu.Lock()
	lbTokens := make([]TimedWordToken, len(loopbackTokens))
	copy(lbTokens, loopbackTokens)
	loopbackTokensMu.Unlock()

	if len(sortformerSegs) > 0 && len(lbTokens) > 0 {
		// 1. Merge adjacent Sortformer segments of the same speaker if silence gap < 0.8s
		type MergedSortformerTurn struct {
			Start   float64
			End     float64
			Speaker string
			Words   strings.Builder
		}

		var mergedTurns []MergedSortformerTurn
		for _, seg := range sortformerSegs {
			spkName := formatSpeakerDisplayName(seg.Speaker)
			if len(mergedTurns) > 0 {
				lastIdx := len(mergedTurns) - 1
				if mergedTurns[lastIdx].Speaker == spkName && (seg.Start-mergedTurns[lastIdx].End <= 0.8) {
					// Extend current turn
					if seg.End > mergedTurns[lastIdx].End {
						mergedTurns[lastIdx].End = seg.End
					}
					continue
				}
			}
			mergedTurns = append(mergedTurns, MergedSortformerTurn{
				Start:   seg.Start,
				End:     seg.End,
				Speaker: spkName,
			})
		}

		// 2. Distribute Server 1 word tokens into the merged Sortformer turn containers
		for _, tok := range lbTokens {
			tokMid := (tok.StartTime + tok.EndTime) / 2.0
			bestTurnIdx := -1
			var bestOverlap float64 = 0.0

			for idx, turn := range mergedTurns {
				overlap := mathMax(0, mathMin(tok.EndTime, turn.End)-mathMax(tok.StartTime, turn.Start))
				if overlap > bestOverlap {
					bestOverlap = overlap
					bestTurnIdx = idx
				}
			}

			// Fallback: If no direct overlap, find closest turn within 0.8s
			if bestTurnIdx == -1 {
				var minDistance float64 = 1e9
				for idx, turn := range mergedTurns {
					var dist float64
					if tokMid < turn.Start {
						dist = turn.Start - tokMid
					} else if tokMid > turn.End {
						dist = tokMid - turn.End
					} else {
						dist = 0
					}
					if dist < minDistance && dist <= 0.8 {
						minDistance = dist
						bestTurnIdx = idx
					}
				}
			}

			if bestTurnIdx != -1 {
				mergedTurns[bestTurnIdx].Words.WriteString(tok.Text)
			}
		}

		// 3. Emit final segments for all Sortformer turns that received speech
		for _, turn := range mergedTurns {
			text := strings.TrimSpace(turn.Words.String())
			if text != "" {
				finalSegments = append(finalSegments, FinalSegment{
					Start:   mathMax(0, turn.Start-0.22),
					End:     turn.End + 0.08,
					Text:    text,
					Speaker: turn.Speaker,
				})
			}
		}
	} else if len(lbTokens) > 0 {
		// Fallback: If Sortformer was unavailable, group by pause
		var curText strings.Builder
		var curStart float64 = lbTokens[0].StartTime
		var curEnd float64 = lbTokens[0].EndTime
		for i, tok := range lbTokens {
			if i > 0 && (tok.StartTime-curEnd > 1.8) {
				trimmed := strings.TrimSpace(curText.String())
				if trimmed != "" {
					finalSegments = append(finalSegments, FinalSegment{
						Start:   curStart,
						End:     curEnd,
						Text:    trimmed,
						Speaker: "Speaker 1",
					})
				}
				curText.Reset()
				curStart = tok.StartTime
			}
			curText.WriteString(tok.Text)
			curEnd = tok.EndTime
		}
		trimmed := strings.TrimSpace(curText.String())
		if trimmed != "" {
			finalSegments = append(finalSegments, FinalSegment{
				Start:   curStart,
				End:     curEnd,
				Text:    trimmed,
				Speaker: "Speaker 1",
			})
		}
	}

	// Fallback to activeTurns if token arrays are empty
	if len(finalSegments) == 0 {
		turnsMu.Lock()
		for _, turn := range activeTurns {
			if strings.TrimSpace(turn.Text) == "" {
				continue
			}
			spkName := turn.SpeakerName
			if turn.Channel == "system" {
				spkName = resolveRemoteSpeaker(turn.StartTime, turn.EndTime)
			}

			finalSegments = append(finalSegments, FinalSegment{
				Start:   turn.StartTime,
				End:     turn.EndTime,
				Text:    turn.Text,
				Speaker: spkName,
			})
		}
		turnsMu.Unlock()
	}

	// Chronologically sort all combined dialogue turns across all speakers
	sort.Slice(finalSegments, func(i, j int) bool {
		return finalSegments[i].Start < finalSegments[j].Start
	})

	var rawTranscriptBuilder strings.Builder
	for _, seg := range finalSegments {
		rawTranscriptBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", seg.Speaker, seg.Text))
	}

	segmentsBytes, _ := json.Marshal(finalSegments)
	segmentsJSON := string(segmentsBytes)
	rawTranscriptText := rawTranscriptBuilder.String()

	log.Printf("[SERVER FINAL REVIEW] Total Active Turns: %d | Final Segments: %d", len(activeTurns), len(finalSegments))
	log.Printf("[SERVER FINAL RAW DIARIZED TRANSCRIPT]\n%s", rawTranscriptText)
	log.Printf("[SERVER FINAL SEGMENTS JSON]\n%s", segmentsJSON)

	// 9. Run AI Summarization & Action Item Generation
	pctx := pipeline.NewPipelineContext(user.ID)
	var polishedTranscript string = rawTranscriptText
	var summaryText string = "No speech detected in this recording."
	var actionItemsText string = "No action items extracted."

	if strings.TrimSpace(rawTranscriptText) != "" {
		if a.VocabCache != nil && len(vocabTerms) > 0 {
			rawTranscriptText, _, _ = a.VocabCache.ProcessText(user.ID, rawTranscriptText, vocabTerms)
		}
		pPolished, pSummary, pActions, errAI := generateMeetingAIAnalysis(ctx, rawTranscriptText, pctx)
		if errAI == nil {
			polishedTranscript = pPolished
			summaryText = pSummary
			actionItemsText = pActions
		} else {
			log.Printf("[MEETING AI SUMMARY ERROR] %v", errAI)
			summaryText = fmt.Sprintf("AI Summary generation failed: %v", errAI)
		}
	}

	log.Printf("[SERVER FINAL POLISHED TRANSCRIPT]\n%s", polishedTranscript)
	log.Printf("[SERVER FINAL SUMMARY]\n%s", summaryText)
	log.Printf("[SERVER FINAL ACTION ITEMS]\n%s", actionItemsText)

	// 10. Persist Meeting to Database
	meetingID, err := a.DB.CreateMeeting(user.ID, title, sessionDuration, audioPath)
	if err != nil {
		log.Printf("[DB ERROR] Failed to record live meeting for user %d: %v", user.ID, err)
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: "Failed to persist meeting record to database"})
		return
	}

	_ = a.DB.UpdateMeetingResult(meetingID, polishedTranscript, summaryText, actionItemsText, segmentsJSON)

	audioFileSize := 0
	if fi, errStat := os.Stat(audioPath); errStat == nil {
		audioFileSize = int(fi.Size())
	}

	// 11. Log Usage & Quota
	_, _ = a.DB.LogUsageAndUpdateQuota(
		user.ID,
		sessionDuration,
		0,
		0,
		sessionDuration,
		false,
		"meeting-sortformer-nemotron",
		rawTranscriptText,
		polishedTranscript,
		"success",
		audioFileSize,
		"self_hosted_sortformer_nemotron",
		pctx.LLMProvider,
	)

	// 12. Return Final Completed Meeting Object to Client
	finalMeeting, err := a.DB.GetMeeting(meetingID)
	if err == nil && finalMeeting != nil {
		finalData, _ := json.Marshal(finalMeeting)
		log.Printf("[SERVER SENDING COMPLETED EVENT PAYLOAD] %s", string(finalData))
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
			Type:    "completed",
			Meeting: finalMeeting,
		})
	}

	log.Printf("[MEETING STREAM COMPLETE] Session finished successfully | User ID: %d | Meeting ID: %d | Duration: %.2fs | Segments: %d",
		user.ID, meetingID, sessionDuration, len(finalSegments))

	conn.Close(websocket.StatusNormalClosure, "Meeting session completed")
}

func writeMeetingEventJSON(ctx context.Context, conn *websocket.Conn, msg MeetingStreamEventMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func upsertMeetingTurn(turns *[]ClientMeetingTurn, turn ClientMeetingTurn) {
	for i := range *turns {
		if (*turns)[i].ID == turn.ID {
			(*turns)[i] = turn
			return
		}
	}
	*turns = append(*turns, turn)
}

func formatSpeakerDisplayName(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch lower {
	case "speaker_1", "spk_1", "1":
		return "Speaker 1"
	case "speaker_2", "spk_2", "2":
		return "Speaker 2"
	case "speaker_3", "spk_3", "3":
		return "Speaker 3"
	case "speaker_4", "spk_4", "4":
		return "Speaker 4"
	case "speaker_5", "spk_5", "5":
		return "Speaker 5"
	case "speaker_0", "spk_0", "0":
		return "Speaker 1"
	default:
		if strings.HasPrefix(lower, "speaker_") {
			numStr := strings.TrimPrefix(lower, "speaker_")
			return "Speaker " + numStr
		}
		return raw
	}
}

func sanitizeSpeakerID(name string) string {
	return "spk_" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "_"))
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}



