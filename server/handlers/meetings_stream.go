package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

// Unified NeMo-Speech.cpp + Sortformer streaming endpoint
const (
	DefaultUnifiedStreamURL = "ws://GPU_HOST_IP:80/v1/stream"
)

// MeetingStreamStartPayload sent by client upon WebSocket connection
type MeetingStreamStartPayload struct {
	Language   string `json:"language,omitempty"`
	Type       string `json:"type"` // "start"
	Title      string `json:"title"`
	SampleRate int    `json:"sample_rate"` // 16000
	Channels   int    `json:"channels"`    // 1
}

// ClientMeetingTurn represents a live transcript segment emitted to the client
type ClientMeetingTurn struct {
	ID          string  `json:"id"`
	Channel     string  `json:"channel"`      // "mic" (local user) | "system" (remote attendees)
	SpeakerID   string  `json:"speaker_id"`   // e.g. "speaker_local", "spk_1", "spk_2"
	SpeakerName string  `json:"speaker_name"` // Display name (e.g. "Sumedh", "Speaker 1")
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

// WordItem represents a timestamped & diarized word from NeMo-Speech.cpp / Sortformer
type WordItem struct {
	Word       string      `json:"word"`
	Start      float64     `json:"start"`
	End        float64     `json:"end"`
	Speaker    interface{} `json:"speaker"`
	Confidence float64     `json:"confidence"`
}

func (w WordItem) SpeakerID() int {
	if w.Speaker == nil {
		return 1
	}
	switch v := w.Speaker.(type) {
	case float64:
		if int(v) > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if int(v) > 0 {
			return int(v)
		}
	case string:
		if strings.Contains(v, "_") {
			parts := strings.Split(v, "_")
			if id, err := strconv.Atoi(parts[len(parts)-1]); err == nil && id > 0 {
				return id
			}
		}
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			return id
		}
	}
	return 1
}

// CompletedUtteranceEvent from NeMo-Speech.cpp runtime via SpeekToMe Gateway
type CompletedUtteranceEvent struct {
	EventID        string     `json:"event_id"`
	Type           string     `json:"type"`
	AudioProcessed float64    `json:"audio_processed"`
	Transcript     string     `json:"transcript"`
	Words          []WordItem `json:"words"`
}

// DiarizationSegment preserved for backward compatibility with testing suites
type DiarizationSegment struct {
	Type       string  `json:"type"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Speaker    string  `json:"speaker"`
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
	dialCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
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

// resolveUnifiedStreamURL normalizes and configures the streaming WebSocket URL
func resolveUnifiedStreamURL(baseURL string, diarize bool, lang string, streamID string, phrases ...[]string) string {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = DefaultUnifiedStreamURL
	}
	if strings.HasPrefix(base, "http://") {
		base = "ws://" + strings.TrimPrefix(base, "http://")
	} else if strings.HasPrefix(base, "https://") {
		base = "wss://" + strings.TrimPrefix(base, "https://")
	} else if !strings.HasPrefix(base, "ws://") && !strings.HasPrefix(base, "wss://") {
		base = "ws://" + base
	}

	u, err := url.Parse(base)
	if err != nil {
		return fmt.Sprintf("%s?diarize=%t&lang=%s&stream_id=%s", base, diarize, lang, streamID)
	}
	if u.Path == "" || u.Path == "/" || strings.HasSuffix(u.Path, "/v1/realtime") || strings.HasSuffix(u.Path, "/v1/audio/diarize") {
		u.Path = "/v1/stream"
	}
	q := u.Query()
	q.Set("diarize", fmt.Sprintf("%t", diarize))
	if lang != "" {
		q.Set("lang", lang)
	}
	if streamID != "" {
		q.Set("stream_id", streamID)
	}
	if len(phrases) > 0 && len(phrases[0]) > 0 {
		q.Set("boosted_words", strings.Join(phrases[0], ","))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// collapseWordsToTurns collapses an array of diarized words into speaker turns
func collapseWordsToTurns(words []WordItem, baseIDPrefix string) []ClientMeetingTurn {
	if len(words) == 0 {
		return nil
	}
	var turns []ClientMeetingTurn
	curSpk := words[0].SpeakerID()
	startT := words[0].Start
	endT := words[0].End
	var curWords []string

	for _, w := range words {
		spk := w.SpeakerID()
		if spk == curSpk {
			curWords = append(curWords, w.Word)
			if w.End > endT {
				endT = w.End
			}
		} else {
			if len(curWords) > 0 {
				spkName := formatSpeakerNumber(curSpk)
				turns = append(turns, ClientMeetingTurn{
					ID:          fmt.Sprintf("%s_%d_%d", baseIDPrefix, curSpk, int64(startT*1000)),
					Channel:     "system",
					SpeakerID:   sanitizeSpeakerID(spkName),
					SpeakerName: spkName,
					StartTime:   startT,
					EndTime:     endT,
					Text:        strings.Join(curWords, " "),
					IsFinal:     true,
				})
			}
			curSpk = spk
			startT = w.Start
			endT = w.End
			curWords = []string{w.Word}
		}
	}
	if len(curWords) > 0 {
		spkName := formatSpeakerNumber(curSpk)
		turns = append(turns, ClientMeetingTurn{
			ID:          fmt.Sprintf("%s_%d_%d", baseIDPrefix, curSpk, int64(startT*1000)),
			Channel:     "system",
			SpeakerID:   sanitizeSpeakerID(spkName),
			SpeakerName: spkName,
			StartTime:   startT,
			EndTime:     endT,
			Text:        strings.Join(curWords, " "),
			IsFinal:     true,
		})
	}
	return turns
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
	if msgType != websocket.MessageText || json.Unmarshal(data, &startPayload) != nil || startPayload.Type != "start" {
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: "Expected a start message with one language string"})
		return
	}

	language, languageErr := utils.ResolveSpeakingLanguage(startPayload.Language, user.Languages)
	if languageErr != nil {
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: languageErr.Error()})
		return
	}
	requestUser := *user
	requestUser.Languages = []string{language}
	user = &requestUser

	title := strings.TrimSpace(startPayload.Title)
	if title == "" {
		title = fmt.Sprintf("Meeting on %s", time.Now().Format("2006-01-02 15:04"))
	}

	userName := user.Name
	if userName == "" {
		userName = "Local Speaker"
	}

	log.Printf("[MEETING STREAM START] User %s (ID: %d) started meeting session %q", user.Email, user.ID, title)

	// 4. Resolve Upstream Unified Inference URL
	streamBaseURL := DefaultUnifiedStreamURL
	if envURL := os.Getenv("SPEEKTOME_INFERENCE_URL"); envURL != "" {
		streamBaseURL = envURL
	} else if envURL := os.Getenv("SERVER1_STREAM_URL"); envURL != "" {
		streamBaseURL = envURL
	} else {
		rawCfg := config.ConfigManager.GetRawAll()
		if u, ok := rawCfg["meeting_stream_url"]; ok && strings.TrimSpace(u) != "" {
			streamBaseURL = strings.TrimSpace(u)
		} else if u, ok := rawCfg["unified_stream_url"]; ok && strings.TrimSpace(u) != "" {
			streamBaseURL = strings.TrimSpace(u)
		} else {
			mCfg := config.ConfigManager.Get().Models
			if prov, ok := mCfg.StreamingProviders["self_hosted_nemotron_3_5"]; ok && prov.BaseURL != "" {
				streamBaseURL = prov.BaseURL
			}
		}
	}

	nowNs := time.Now().UnixNano()
	var vocabPhrases []string
	for _, term := range vocabTerms {
		tTrimmed := strings.TrimSpace(term.Canonical)
		if tTrimmed != "" {
			vocabPhrases = append(vocabPhrases, tTrimmed)
		}
	}
	micLang := utils.ResolveNemotronLanguage(user.Languages)
	micURL := resolveUnifiedStreamURL(streamBaseURL, false, micLang, fmt.Sprintf("meeting-%d-%d-mic", user.ID, nowNs))
	lbURL := resolveUnifiedStreamURL(streamBaseURL, true, micLang, fmt.Sprintf("meeting-%d-%d-lb", user.ID, nowNs))

	log.Printf("[MEETING STREAM UPSTREAM] Connecting Mic stream to: %s", micURL)
	log.Printf("[MEETING STREAM UPSTREAM] Connecting Loopback stream to: %s", lbURL)

	// Connect 2 upstream inference sessions to the unified NeMo-Speech.cpp runtime
	// Stream 1: Mic ASR (Local user) -> low-latency streaming deltas
	s1MicASR, errMic := dialUpstreamInferenceWS(ctx, micURL, "Unified Mic ASR")
	if errMic != nil {
		log.Printf("[MEETING STREAM WARNING] Mic ASR connect failed (%s): %v", micURL, errMic)
	}

	// Stream 2: Loopback ASR + Sortformer Diarization (Remote meeting attendees)
	s1Loopback, errLB := dialUpstreamInferenceWS(ctx, lbURL, "Unified Loopback Diarize+ASR")
	if errLB != nil {
		log.Printf("[MEETING STREAM WARNING] Loopback Diarize+ASR connect failed (%s): %v", lbURL, errLB)
	}

	defer func() {
		if s1MicASR != nil {
			s1MicASR.Close(websocket.StatusNormalClosure, "meeting finished")
		}
		if s1Loopback != nil {
			s1Loopback.Close(websocket.StatusNormalClosure, "meeting finished")
		}
	}()

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

	var isMeetingEnding atomic.Bool

	// Monotonic Session Clock Tracking (560ms chunks)
	var micChunkCount int64
	var loopbackChunkCount int64
	var chunkCountMu sync.Mutex

	// Loopback stream completion coordination
	var lbDoneChan = make(chan struct{})
	var closeLBDoneOnce sync.Once
	closeLBDone := func() {
		closeLBDoneOnce.Do(func() {
			close(lbDoneChan)
		})
	}

	var lastEmittedLBWordEnd float64 = 0.0
	var lbWordsMu sync.Mutex

	// -------------------------------------------------------------------
	// 5. Goroutine A: Listen to Unified Loopback Stream (Diarized Utterances)
	// -------------------------------------------------------------------
	if s1Loopback != nil {
		go func() {
			defer closeLBDone()
			for {
				msgType, data, err := s1Loopback.Conn.Read(ctx)
				if err != nil {
					break
				}
				if msgType == websocket.MessageText {
					var raw map[string]interface{}
					if json.Unmarshal(data, &raw) != nil {
						continue
					}
					evtType, _ := raw["type"].(string)

					switch evtType {
					case "conversation.item.input_audio_transcription.completed":
						var completedEvt CompletedUtteranceEvent
						if err := json.Unmarshal(data, &completedEvt); err != nil {
							continue
						}
						log.Printf("[SERVER LOOPBACK COMPLETED] words=%d audio_processed=%.2f transcript=%q",
							len(completedEvt.Words), completedEvt.AudioProcessed, completedEvt.Transcript)

						lbWordsMu.Lock()
						var newWords []WordItem
						for _, w := range completedEvt.Words {
							if w.End > lastEmittedLBWordEnd+0.05 {
								newWords = append(newWords, w)
							}
						}

						var generatedTurns []ClientMeetingTurn
						if len(newWords) > 0 {
							generatedTurns = collapseWordsToTurns(newWords, "turn_lb")
							for _, w := range newWords {
								if w.End > lastEmittedLBWordEnd {
									lastEmittedLBWordEnd = w.End
								}
							}
						} else if len(completedEvt.Words) == 0 && strings.TrimSpace(completedEvt.Transcript) != "" && completedEvt.AudioProcessed > lastEmittedLBWordEnd+0.2 {
							cleanText := strings.TrimSpace(completedEvt.Transcript)
							wordCount := len(strings.Fields(cleanText))
							estimatedDuration := mathMax(1.0, float64(wordCount)/2.6)
							startSec := mathMax(lastEmittedLBWordEnd, completedEvt.AudioProcessed-estimatedDuration)
							endSec := mathMax(startSec+0.5, completedEvt.AudioProcessed)
							generatedTurns = append(generatedTurns, ClientMeetingTurn{
								ID:          fmt.Sprintf("turn_lb_1_%d", int64(startSec*1000)),
								Channel:     "system",
								SpeakerID:   "spk_speaker_1",
								SpeakerName: "Speaker 1",
								StartTime:   startSec,
								EndTime:     endSec,
								Text:        cleanText,
								IsFinal:     true,
							})
							lastEmittedLBWordEnd = endSec
						}
						lbWordsMu.Unlock()

						for _, turn := range generatedTurns {
							turnCopy := turn
							turnsMu.Lock()
							upsertMeetingTurn(&activeTurns, turnCopy)
							turnsMu.Unlock()

							log.Printf("[SERVER EMIT TURN] ID=%s [%.2f-%.2f] %s: %q",
								turnCopy.ID, turnCopy.StartTime, turnCopy.EndTime, turnCopy.SpeakerName, turnCopy.Text)

							_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
								Type: "turn",
								Turn: &turnCopy,
							})
						}

					case "input_audio_buffer.committed":
						log.Printf("[SERVER LOOPBACK] input_audio_buffer.committed confirmed by inference engine")
						if isMeetingEnding.Load() {
							// Allow 250ms grace period to ensure all flushed completion events are received
							time.Sleep(250 * time.Millisecond)
							return
						}

					case "error":
						log.Printf("[SERVER LOOPBACK INFERENCE ERROR] %v", raw["error"])
					}
				}
			}
		}()
	} else {
		closeLBDone()
	}

	// -------------------------------------------------------------------
	// 5. Goroutine B: Listen to Unified Mic Stream (Local User Speech)
	// -------------------------------------------------------------------
	var micDoneChan = make(chan struct{})
	var closeMicDoneOnce sync.Once
	closeMicDone := func() {
		closeMicDoneOnce.Do(func() {
			close(micDoneChan)
		})
	}

	if s1MicASR != nil {
		go func() {
			defer closeMicDone()
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
					var raw map[string]interface{}
					if json.Unmarshal(data, &raw) != nil {
						continue
					}
					evtType, _ := raw["type"].(string)

					switch evtType {
					case "conversation.item.input_audio_transcription.delta":
						if delta, ok := raw["delta"].(string); ok && delta != "" {
							audioProc, _ := raw["audio_processed"].(float64)
							now := time.Now()

							if currentTurnText == "" || now.Sub(lastDeltaTime) > 800*time.Millisecond {
								turnStartTime = mathMax(0, audioProc-1.90)
								turnID = fmt.Sprintf("turn_mic_%d", now.UnixNano())
								currentTurnText = ""
							}
							lastDeltaTime = now
							currentTurnText += delta
						}

					case "conversation.item.input_audio_transcription.completed":
						audioProc, _ := raw["audio_processed"].(float64)
						transcript, _ := raw["transcript"].(string)
						cleanText := strings.TrimSpace(transcript)
						if cleanText != "" {
							turnDuration := mathMax(1.0, float64(len(strings.Fields(cleanText)))/2.6)
							effectiveStart := mathMax(0, mathMin(turnStartTime, audioProc-turnDuration))
							turn := ClientMeetingTurn{
								ID:          turnID,
								Channel:     "mic",
								SpeakerID:   "speaker_local",
								SpeakerName: userName,
								StartTime:   effectiveStart,
								EndTime:     audioProc,
								Text:        cleanText,
								IsFinal:     true,
							}
							turnsMu.Lock()
							upsertMeetingTurn(&activeTurns, turn)
							turnsMu.Unlock()

							log.Printf("[SERVER EMIT MIC TURN] ID=%s [%.2f-%.2f] %s: %q",
								turn.ID, turn.StartTime, turn.EndTime, turn.SpeakerName, turn.Text)

							_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{
								Type: "turn",
								Turn: &turn,
							})
						}
						currentTurnText = ""

					case "input_audio_buffer.committed":
						log.Printf("[SERVER MIC] input_audio_buffer.committed confirmed by inference engine")
						if isMeetingEnding.Load() {
							// Allow 250ms grace period to ensure all flushed completion events are received
							time.Sleep(250 * time.Millisecond)
							return
						}

					case "error":
						log.Printf("[SERVER MIC INFERENCE ERROR] %v", raw["error"])
					}
				}
			}
		}()
	} else {
		closeMicDone()
	}

	// -------------------------------------------------------------------
	// Main Receive Loop: Read dual-channel 560ms packets from Windows Client
	// -------------------------------------------------------------------
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

				if s1Loopback != nil {
					_ = s1Loopback.SendAudio(ctx, opusData)
				}
			}
			chunkCountMu.Unlock()
		}
	}

	// -------------------------------------------------------------------
	// 6. Finalize session & Commit upstream engines
	// -------------------------------------------------------------------
	isMeetingEnding.Store(true)
	commitMsg := map[string]interface{}{"type": "input_audio_buffer.commit"}
	if s1MicASR != nil {
		_ = s1MicASR.SendControl(ctx, commitMsg)
	}
	if s1Loopback != nil {
		_ = s1Loopback.SendControl(ctx, commitMsg)
	}

	// Allow brief window for final utterance flushes
	flushTimer := time.NewTimer(5 * time.Second)
	select {
	case <-lbDoneChan:
		log.Printf("[SERVER INFERENCE] Loopback upstream completed.")
	case <-flushTimer.C:
		log.Printf("[SERVER INFERENCE WARNING] Timed out waiting for loopback flush. Proceeding.")
	}
	flushTimer.Stop()

	micTimer := time.NewTimer(3 * time.Second)
	select {
	case <-micDoneChan:
		log.Printf("[SERVER INFERENCE] Mic upstream completed.")
	case <-micTimer.C:
		log.Printf("[SERVER INFERENCE WARNING] Timed out waiting for mic flush. Proceeding.")
	}
	micTimer.Stop()

	// 7. Calculate Audio Duration strictly from processed audio chunks
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

	// -------------------------------------------------------------------
	// 8. Assemble Final Diarized Dialogue & JSON Segments
	// -------------------------------------------------------------------
	type FinalSegment struct {
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		Text    string  `json:"text"`
		Speaker string  `json:"speaker"`
	}

	var finalSegments []FinalSegment
	turnsMu.Lock()
	activeTurns = filterMeetingStreamTurns(activeTurns)
	for _, turn := range activeTurns {
		trimmed := strings.TrimSpace(turn.Text)
		if trimmed == "" {
			continue
		}
		finalSegments = append(finalSegments, FinalSegment{
			Start:   turn.StartTime,
			End:     turn.EndTime,
			Text:    trimmed,
			Speaker: turn.SpeakerName,
		})
	}
	turnsMu.Unlock()

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

	// -------------------------------------------------------------------
	// 9. Run AI Summarization & Action Item Generation
	// -------------------------------------------------------------------
	pctx := pipeline.NewPipelineContext(user.ID)
	var polishedTranscript string = rawTranscriptText
	var summaryText string = "No speech detected in this recording."
	var actionItemsText string = "No action items extracted."

	audit := finalizeVocabulary(ctx, rawTranscriptText, vocabTerms, nil)
	polishedTranscript = audit.Resolved
	if strings.TrimSpace(rawTranscriptText) != "" {
		input, restore := audit.Protect()
		pPolished, pSummary, pActions, errAI := generateMeetingAIAnalysis(ctx, input, pctx)
		if errAI == nil {
			polishedTranscript = restore(pPolished)
			summaryText = audit.RestoreMentions(pSummary)
			actionItemsText = audit.RestoreMentions(pActions)
		} else {
			log.Printf("[MEETING AI SUMMARY ERROR] %v", errAI)
		}
	}

	log.Printf("[SERVER FINAL POLISHED TRANSCRIPT]\n%s", polishedTranscript)
	log.Printf("[SERVER FINAL SUMMARY]\n%s", summaryText)
	log.Printf("[SERVER FINAL ACTION ITEMS]\n%s", actionItemsText)

	// -------------------------------------------------------------------
	// 10. Persist Meeting to Database
	// -------------------------------------------------------------------
	meetingID, err := a.DB.CreateMeeting(user.ID, title, sessionDuration, audioPath)
	if err != nil {
		log.Printf("[DB ERROR] Failed to record live meeting for user %d: %v", user.ID, err)
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: "Failed to persist meeting record to database"})
		return
	}

	_ = a.DB.UpdateMeetingResult(meetingID, polishedTranscript, summaryText, actionItemsText, segmentsJSON, auditJSON(audit))

	audioFileSize := 0
	if fi, errStat := os.Stat(audioPath); errStat == nil {
		audioFileSize = int(fi.Size())
	}

	// -------------------------------------------------------------------
	// 11. Log Usage & Quota
	// -------------------------------------------------------------------
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

	// -------------------------------------------------------------------
	// 12. Return Final Completed Meeting Object to Client
	// -------------------------------------------------------------------
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

func formatSpeakerNumber(speakerNum int) string {
	if speakerNum <= 0 {
		return "Speaker 1"
	}
	return fmt.Sprintf("Speaker %d", speakerNum)
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

// filterMeetingStreamTurns eliminates acoustic bleed turns where remote speaker sound was picked up faintly
// by the microphone and mis-transcribed as phantom local speaker turns.
func filterMeetingStreamTurns(turns []ClientMeetingTurn) []ClientMeetingTurn {
	var lbTurns []ClientMeetingTurn
	var micTurns []ClientMeetingTurn

	for _, t := range turns {
		if !t.IsFinal {
			continue // Drop non-final interim turns from final timeline
		}
		clean := strings.TrimSpace(t.Text)
		if clean == "" {
			continue
		}
		t.Text = clean
		if t.Channel == "system" {
			lbTurns = append(lbTurns, t)
		} else {
			micTurns = append(micTurns, t)
		}
	}

	var filteredTurns []ClientMeetingTurn
	// Always retain legitimate system / loopback turns
	filteredTurns = append(filteredTurns, lbTurns...)

	for _, m := range micTurns {
		isBleed := false
		mWords := tokenizeString(m.Text)

		for _, lb := range lbTurns {
			// Check temporal overlap (within +/- 2.0s tolerance)
			timeOverlap := !(m.EndTime < lb.StartTime-2.0 || m.StartTime > lb.EndTime+2.0)
			if !timeOverlap {
				continue
			}

			// 1. Text Jaccard similarity check
			similarity := calculateTextSimilarity(m.Text, lb.Text)
			if similarity >= 0.30 {
				log.Printf("[BLEED FILTER] Dropped acoustic bleed turn %s [%.2f-%.2f] %q (overlapped with %s [%.2f-%.2f] %q, sim=%.2f)",
					m.ID, m.StartTime, m.EndTime, m.Text, lb.ID, lb.StartTime, lb.EndTime, lb.Text, similarity)
				isBleed = true
				break
			}

			// 2. Short fragment containment check: if mic turn is completely submerged within an active loopback turn
			// and shares words with the remote speaker (ratio >= 0.25)
			isSubmerged := m.StartTime >= lb.StartTime-0.5 && m.EndTime <= lb.EndTime+0.5
			if isSubmerged && len(mWords) > 0 && len(mWords) <= 8 {
				lbWords := tokenizeString(lb.Text)
				lbSet := make(map[string]bool)
				for _, w := range lbWords {
					lbSet[w] = true
				}
				common := 0
				for _, w := range mWords {
					if lbSet[w] {
						common++
					}
				}
				if float64(common)/float64(len(mWords)) >= 0.25 {
					log.Printf("[BLEED FILTER] Dropped acoustic bleed fragment turn %s [%.2f-%.2f] %q (words matched in loopback turn [%.2f-%.2f], ratio=%.2f)",
						m.ID, m.StartTime, m.EndTime, m.Text, lb.StartTime, lb.EndTime, float64(common)/float64(len(mWords)))
					isBleed = true
					break
				}
			}
		}

		if !isBleed {
			filteredTurns = append(filteredTurns, m)
		}
	}

	// Sort chronologically by start time
	sort.Slice(filteredTurns, func(i, j int) bool {
		if filteredTurns[i].StartTime == filteredTurns[j].StartTime {
			return filteredTurns[i].EndTime < filteredTurns[j].EndTime
		}
		return filteredTurns[i].StartTime < filteredTurns[j].StartTime
	})

	// Merge adjacent turns from the same speaker and same channel if within 0.35 seconds
	var merged []ClientMeetingTurn
	for _, t := range filteredTurns {
		if len(merged) == 0 {
			merged = append(merged, t)
			continue
		}
		last := &merged[len(merged)-1]
		if last.Channel == t.Channel && last.SpeakerID == t.SpeakerID && t.StartTime <= last.EndTime+0.35 {
			last.Text = strings.TrimSpace(last.Text + " " + t.Text)
			if t.EndTime > last.EndTime {
				last.EndTime = t.EndTime
			}
		} else {
			merged = append(merged, t)
		}
	}

	return merged
}
