package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	log "speektome-server/privacylog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"speektome-server/accountlimits"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/services"
	"speektome-server/utils"
)

// MeetingStreamStartPayload is the first message of a live meeting session.
type MeetingStreamStartPayload struct {
	Language   string `json:"language,omitempty"`
	Type       string `json:"type"` // "start"
	Title      string `json:"title"`
	SampleRate int    `json:"sample_rate"` // 16000
	Channels   int    `json:"channels"`    // 1
}

// ClientMeetingTurn is one transcript turn shown to the desktop client.
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

// MeetingStreamEventMessage is sent to the client during a live session.
type MeetingStreamEventMessage struct {
	Type  string             `json:"type"` // "session_ready" | "turn" | "live_complete" | "error"
	Title string             `json:"title,omitempty"`
	Turn  *ClientMeetingTurn `json:"turn,omitempty"`
	Error string             `json:"error,omitempty"`
}

const (
	liveMeetingOperation = "meeting live transcript"
	// A pause this long in a channel's transcript starts a new live turn.
	liveTurnPause = 1500 * time.Millisecond
	// Long monologues are split so the live view stays readable.
	liveTurnMaxWords = 80
	liveUnavailable  = "Live transcription paused. Your recording continues and will be fully transcribed after the meeting."
)

// liveTurns turns one channel's cumulative streaming transcript into turns.
// Partial hypotheses can revise the tail, so turns are delimited by word
// counts rather than byte offsets.
type liveTurns struct {
	channel, speakerID, speakerName string
	committed                       int // words that belong to earlier turns
	turn                            int
	turnStart                       float64
	lastWords                       []string
	lastUpdate                      time.Time
}

func (l *liveTurns) current(words []string, elapsed float64, final bool) *ClientMeetingTurn {
	if l.committed > len(words) {
		l.committed = len(words)
	}
	text := strings.Join(words[l.committed:], " ")
	if text == "" {
		return nil
	}
	return &ClientMeetingTurn{ID: fmt.Sprintf("live_%s_%d", l.channel, l.turn), Channel: l.channel, SpeakerID: l.speakerID, SpeakerName: l.speakerName, StartTime: l.turnStart, EndTime: elapsed, Text: text, IsFinal: final}
}

// update returns the turns to emit for a new cumulative transcript: the
// closed previous turn (if a pause or length limit ended it) and the open one.
func (l *liveTurns) update(text string, now time.Time, elapsed float64) []ClientMeetingTurn {
	words := strings.Fields(text)
	var out []ClientMeetingTurn
	if !l.lastUpdate.IsZero() && len(l.lastWords) > l.committed && (now.Sub(l.lastUpdate) >= liveTurnPause || len(l.lastWords)-l.committed >= liveTurnMaxWords) {
		if closed := l.current(l.lastWords, elapsed, true); closed != nil {
			closed.EndTime = l.turnStart + now.Sub(l.lastUpdate).Seconds()
			out = append(out, *closed)
		}
		l.committed = len(l.lastWords)
		l.turn++
		l.turnStart = elapsed
	}
	if l.lastUpdate.IsZero() || len(l.lastWords) <= l.committed {
		l.turnStart = elapsed
	}
	l.lastWords = words
	l.lastUpdate = now
	if open := l.current(words, elapsed, false); open != nil {
		out = append(out, *open)
	}
	return out
}

func (l *liveTurns) finish(elapsed float64) *ClientMeetingTurn {
	return l.current(l.lastWords, elapsed, true)
}

func hasRecordingPermit(ctx context.Context, database *db.Database, user int64) bool {
	var n int
	err := database.GetDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM account_leases WHERE user_id=? AND operation='meeting recording' AND expires>?`, user, time.Now().Unix()).Scan(&n)
	return err == nil && n > 0
}

func liveStreamingProvider() string {
	active := config.ConfigManager.Get()
	for _, provider := range active.GetActiveStreamingFallbackOrder() {
		if active.GetStreamingProviderType(provider) == "real" && active.IsStreamingLanguageSupported(provider, "") {
			return provider
		}
	}
	return ""
}

// HandleMeetingStream relays a recording's microphone and system channels to
// the streaming ASR so the desktop can show a live transcript. It stores
// nothing: audio and text exist only in memory for the session. The saved
// transcript and speaker identities come from the uploaded meeting job after
// the meeting ends.
func (a *App) HandleMeetingStream(w http.ResponseWriter, r *http.Request) {
	// Authentication is a header, never a URL or cookie, so the token stays out
	// of access logs and other sites cannot ride a browser session.
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || token == "" {
		http.Error(w, "Sign in to transcribe meetings live", http.StatusUnauthorized)
		return
	}
	user, vocabTerms, err := a.DB.GetUserAndVocabularyBySession(token)
	if err != nil || user == nil {
		http.Error(w, "Invalid or expired session token", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "session terminated")
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Hour+5*time.Minute)
	defer cancel()
	fail := func(message string) {
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "error", Error: message})
		conn.Close(websocket.StatusPolicyViolation, "live transcript unavailable")
	}
	if user.PlanTier != "pro" || user.PlanActive != 1 {
		fail("Meeting recording requires an active Pro subscription")
		return
	}
	if e := a.limits().Rate(ctx, user.ID, "meeting_live", 10); e != nil {
		fail(e.Error())
		return
	}
	// Live transcription accompanies a permitted recording; it never starts one.
	if !hasRecordingPermit(ctx, a.DB, user.ID) {
		fail("Start the meeting recording before live transcription")
		return
	}
	owner, e := a.limits().Acquire(ctx, user.ID, liveMeetingOperation, 4*time.Hour+5*time.Minute)
	if e != nil {
		message := "Live transcription is unavailable right now"
		if failure, ok := e.(*accountlimits.Failure); ok {
			message = failure.Message
		}
		fail(message)
		return
	}
	defer a.limits().Release(user.ID, liveMeetingOperation, owner)

	msgType, data, err := conn.Read(ctx)
	var start MeetingStreamStartPayload
	if err != nil || msgType != websocket.MessageText || json.Unmarshal(data, &start) != nil || start.Type != "start" {
		fail("Expected a start message with one language string")
		return
	}
	language, languageErr := utils.ResolveSpeakingLanguage(start.Language, user.Languages)
	if languageErr != nil {
		fail(languageErr.Error())
		return
	}
	provider := liveStreamingProvider()
	if provider == "" {
		fail(liveUnavailable)
		return
	}
	languages := []string{language}
	mic, micErr := services.CreateUpstreamStreamingSession(ctx, provider, languages, "", vocabTerms)
	system, systemErr := services.CreateUpstreamStreamingSession(ctx, provider, languages, "", vocabTerms)
	if micErr != nil || systemErr != nil {
		if mic != nil {
			mic.Close(websocket.StatusNormalClosure, "unavailable")
		}
		if system != nil {
			system.Close(websocket.StatusNormalClosure, "unavailable")
		}
		log.Printf("[MEETING LIVE] streaming ASR unavailable for provider %s", provider)
		fail(liveUnavailable)
		return
	}
	defer mic.Close(websocket.StatusNormalClosure, "meeting finished")
	defer system.Close(websocket.StatusNormalClosure, "meeting finished")

	name := user.Name
	if name == "" {
		name = "You"
	}
	began := time.Now()
	var writeMu sync.Mutex
	emit := func(turn ClientMeetingTurn) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "turn", Turn: &turn})
	}
	upstreamFailed := make(chan struct{}, 2)
	var relays sync.WaitGroup
	relay := func(session *services.UpstreamStreamingSession, turns *liveTurns) {
		defer relays.Done()
		for {
			select {
			case text, ok := <-session.TextChan:
				if !ok {
					if last := turns.finish(time.Since(began).Seconds()); last != nil {
						emit(*last)
					}
					return
				}
				now := time.Now()
				for _, turn := range turns.update(text, now, now.Sub(began).Seconds()) {
					emit(turn)
				}
			case <-session.ErrChan:
				upstreamFailed <- struct{}{}
				return
			case <-ctx.Done():
				return
			}
		}
	}
	relays.Add(2)
	go relay(mic, &liveTurns{channel: "mic", speakerID: "speaker_local", speakerName: name})
	go relay(system, &liveTurns{channel: "system", speakerID: "live_system", speakerName: "Meeting audio"})

	writeMu.Lock()
	_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "session_ready", Title: strings.TrimSpace(start.Title)})
	writeMu.Unlock()

	for {
		select {
		case <-upstreamFailed:
			writeMu.Lock()
			fail(liveUnavailable)
			writeMu.Unlock()
			return
		default:
		}
		kind, frame, err := conn.Read(ctx)
		if err != nil {
			return // The desktop keeps recording; nothing here needs saving.
		}
		if kind == websocket.MessageText {
			var control struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(frame, &control) == nil && (control.Type == "end_of_meeting" || control.Type == "stop") {
				break
			}
			continue
		}
		// Binary frames carry one channel tag followed by a short Ogg Opus chunk.
		if len(frame) < 2 {
			continue
		}
		var target *services.UpstreamStreamingSession
		switch frame[0] {
		case 0x01:
			target = mic
		case 0x02:
			target = system
		default:
			continue
		}
		if e := target.SendAudio(ctx, frame[1:]); e != nil {
			writeMu.Lock()
			fail(liveUnavailable)
			writeMu.Unlock()
			return
		}
	}

	// Flush both decoders, then report completion. The finished meeting's
	// transcript is produced by the uploaded job, not by this preview.
	commit := map[string]interface{}{"type": "input_audio_buffer.commit"}
	_ = mic.SendControl(ctx, commit)
	_ = system.SendControl(ctx, commit)
	flushCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	var flush sync.WaitGroup
	for _, session := range []*services.UpstreamStreamingSession{mic, system} {
		flush.Add(1)
		go func(session *services.UpstreamStreamingSession) {
			defer flush.Done()
			_ = session.AwaitFinal(flushCtx)
		}(session)
	}
	flush.Wait()
	stop()
	mic.Close(websocket.StatusNormalClosure, "meeting finished")
	system.Close(websocket.StatusNormalClosure, "meeting finished")
	relays.Wait()
	writeMu.Lock()
	_ = writeMeetingEventJSON(ctx, conn, MeetingStreamEventMessage{Type: "live_complete"})
	writeMu.Unlock()
	conn.Close(websocket.StatusNormalClosure, "meeting finished")
}

func writeMeetingEventJSON(ctx context.Context, conn *websocket.Conn, msg MeetingStreamEventMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}
