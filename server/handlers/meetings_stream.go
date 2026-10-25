package handlers

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
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
// Protocol 2 desktops number their audio chunks, reconnect with a resume
// point, and receive signed word batches to keep for the finished meeting.
type MeetingStreamStartPayload struct {
	Language   string                `json:"language,omitempty"`
	Type       string                `json:"type"` // "start"
	Title      string                `json:"title"`
	SampleRate int                   `json:"sample_rate"` // 16000
	Channels   int                   `json:"channels"`    // 1
	Protocol   int                   `json:"protocol,omitempty"`
	JobID      string                `json:"job_id,omitempty"`
	Resume     map[string]liveResume `json:"resume,omitempty"`
}

// liveResume is where a reconnecting desktop continues one channel: the next
// chunk it sends, and the end of the transcript it already holds. Chunks
// before CoveredTo are resent only as decoder context.
type liveResume struct {
	Seq       uint32  `json:"seq"`
	CoveredTo float64 `json:"covered_to"`
}

// Each protocol 2 chunk is 8,960 samples at 16 kHz, so chunk N starts at
// exactly N × 0.56 s on the recording's timeline.
const liveChunkSeconds = 0.56

// A reconnecting desktop replaces its own stale session for the same meeting
// instead of waiting for the old socket to time out.
var liveSessions = struct {
	sync.Mutex
	active map[string]*liveSessionHandle
}{active: map[string]*liveSessionHandle{}}

type liveSessionHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// claimLiveSession ends any earlier session for key, waits for it to release
// its account lease, and returns the function that releases this claim.
func claimLiveSession(key string, cancel context.CancelFunc) func() {
	handle := &liveSessionHandle{cancel: cancel, done: make(chan struct{})}
	liveSessions.Lock()
	previous := liveSessions.active[key]
	liveSessions.active[key] = handle
	liveSessions.Unlock()
	if previous != nil {
		previous.cancel()
		select {
		case <-previous.done:
		case <-time.After(15 * time.Second):
		}
	}
	return func() {
		liveSessions.Lock()
		if liveSessions.active[key] == handle {
			delete(liveSessions.active, key)
		}
		liveSessions.Unlock()
		close(handle.done)
	}
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

	msgType, data, err := conn.Read(ctx)
	var start MeetingStreamStartPayload
	if err != nil || msgType != websocket.MessageText || json.Unmarshal(data, &start) != nil || start.Type != "start" {
		fail("Expected a start message with one language string")
		return
	}
	numbered := start.Protocol == 2
	if numbered && !clientMeetingID.MatchString(start.JobID) {
		fail("Invalid meeting id")
		return
	}
	if numbered {
		defer claimLiveSession(jobKey(user.ID, start.JobID), cancel)()
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
	// Signed words are sent only when the desktop can keep them and a key exists.
	signed := numbered && len(liveKeys()) > 0
	open := func(channel string) (*services.UpstreamStreamingSession, error) {
		if !signed {
			return services.CreateUpstreamStreamingSession(ctx, provider, languages, "", vocabTerms)
		}
		return services.CreateMeetingStreamingSession(ctx, provider, languages, vocabTerms, float64(start.Resume[channel].Seq)*liveChunkSeconds)
	}
	mic, micErr := open("mic")
	system, systemErr := open("system")
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
	// words signs each confirmed batch for the desktop to keep. Audio before the
	// desktop's covered_to was resent only for decoder context, so its words are
	// already held and are not sent again.
	words := func(session *services.UpstreamStreamingSession, channel string) {
		defer relays.Done()
		coveredTo := start.Resume[channel].CoveredTo
		id := make([]byte, 8)
		rand.Read(id)
		out := liveBatch{JobID: start.JobID, Channel: channel, Session: hex.EncodeToString(id)}
		for batch := range session.Words {
			if batch.To <= coveredTo {
				continue
			}
			out.From, out.To, out.Words = max(batch.From, coveredTo), batch.To, nil
			for _, word := range batch.Words {
				if (word.Start+word.End)/2 >= coveredTo {
					out.Words = append(out.Words, liveWord{W: word.Word, S: word.Start, E: word.End})
				}
			}
			event := signLiveBatch(user.ID, out)
			if event == nil {
				continue
			}
			out.Index++
			writeMu.Lock()
			writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, event)
			stop()
			writeMu.Unlock()
		}
	}
	relays.Add(2)
	go relay(mic, &liveTurns{channel: "mic", speakerID: "speaker_local", speakerName: name})
	go relay(system, &liveTurns{channel: "system", speakerID: "live_system", speakerName: "Meeting audio"})
	for channel, session := range map[string]*services.UpstreamStreamingSession{"mic": mic, "system": system} {
		if session.Words != nil {
			relays.Add(1)
			go words(session, channel)
		}
	}
	next := map[byte]uint32{0x01: start.Resume["mic"].Seq, 0x02: start.Resume["system"].Seq}

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
		// Binary frames carry one channel tag followed by a short Ogg Opus chunk;
		// protocol 2 puts the chunk's big-endian sequence number in between.
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
		audio := frame[1:]
		if numbered {
			// The timeline is the chunk count, so a skipped or repeated chunk would
			// misplace every later word. The desktop reconnects to jump instead.
			if len(frame) < 6 || binary.BigEndian.Uint32(frame[1:5]) != next[frame[0]] {
				writeMu.Lock()
				fail("Live audio arrived out of order")
				writeMu.Unlock()
				return
			}
			next[frame[0]]++
			audio = frame[5:]
		}
		if e := target.SendAudio(ctx, audio); e != nil {
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
	flushCtx, stop := context.WithTimeout(ctx, 15*time.Second)
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
