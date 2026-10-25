package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"speektome-server/db"
	"speektome-server/utils"
	"speektome/vocabulary"
)

type replayChunk struct {
	data       []byte
	start, end float64
}
type replayCommand struct {
	audio   []byte
	control map[string]interface{}
	ack     chan error
}
type replayEvent struct {
	data  []byte
	err   error
	epoch int
}
type fleetReplay struct {
	commands chan replayCommand
	cancel   context.CancelFunc
	done     <-chan struct{}
}

func (f *fleetReplay) send(ctx context.Context, command replayCommand) error {
	command.ack = make(chan error, 1)
	select {
	case f.commands <- command:
	case <-f.done:
		return errors.New("stream recovery session closed")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case e := <-command.ack:
		return e
	case <-f.done:
		return errors.New("stream recovery session closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TimedWord is one finalized word on the stream's audio timeline, in seconds.
type TimedWord struct {
	Word       string
	Start, End float64
}

// WordBatch confirms the audio range [From, To]. Consecutive batches of one
// stream are contiguous; a batch with no words confirms silence.
type WordBatch struct {
	From, To float64
	Words    []TimedWord
}

// CreateMeetingStreamingSession opens a streaming session whose timeline starts
// at offset seconds, publishing confirmed words on Words. Only the fleet emits
// timestamped finals; any other provider returns a session with nil Words.
func CreateMeetingStreamingSession(ctx context.Context, provider string, languages []string, terms []db.VocabularyTerm, offset float64) (*UpstreamStreamingSession, error) {
	if provider == "self_hosted_nemotron_3_5" && os.Getenv("SPEEKTOME_FLEET_URL") != "" {
		return createFleetSession(ctx, provider, languages, terms, offset, true)
	}
	return CreateUpstreamStreamingSession(ctx, provider, languages, "", terms)
}

// Journal audio through finalized word positions, retaining acoustic overlap.
// Replay recreates a fresh model state; it never claims exact decoder migration.
func createFleetStreamingSession(parent context.Context, provider string, languages []string, prompt string, terms []db.VocabularyTerm) (*UpstreamStreamingSession, error) {
	// User prompts belong to the grammar LLM, never decoder bias.
	_ = prompt
	return createFleetSession(parent, provider, languages, terms, 0, false)
}

func createFleetSession(parent context.Context, provider string, languages []string, terms []db.VocabularyTerm, offset float64, words bool) (*UpstreamStreamingSession, error) {
	tokenPath := os.Getenv("SPEEKTOME_WORKER_TOKEN_FILE")
	if tokenPath == "" {
		tokenPath = "/etc/speektome-fleet/worker.token"
	}
	token, e := os.ReadFile(tokenPath)
	if e != nil {
		return nil, e
	}
	base := strings.TrimRight(os.Getenv("SPEEKTOME_FLEET_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8095"
	}
	target := strings.Replace(base, "http", "ws", 1) + "/v1/realtime/opus"
	ctx, cancel := context.WithCancel(parent)
	stream := &UpstreamStreamingSession{Provider: provider, CancelFunc: cancel, TextChan: make(chan string, 100), ErrChan: make(chan error, 1), DoneChan: make(chan struct{})}
	stream.replay = &fleetReplay{commands: make(chan replayCommand, 32), cancel: cancel, done: ctx.Done()}
	if words {
		stream.Words = make(chan WordBatch, 64)
	}
	session := map[string]any{"sample_rate": 16000, "language": utils.ResolveNemotronLanguage(languages), "word_timestamps": true, "speaker_diarization": false, "endpointing_ms": 1200}
	var phrases []string
	for _, term := range terms {
		phrases = append(phrases, term.Canonical)
	}
	// Existing accounts can contain old, oversized entries. Do not let an
	// invalid dictionary make otherwise valid audio fail at worker admission.
	if !validWorkerVocabulary(phrases) {
		phrases = nil
	}
	if len(phrases) > 0 {
		session["vocabulary_matching"] = vocabulary.Config{Terms: phrases}
	}
	dial := func(prefix string, confirmed, offset float64) (*websocket.Conn, error) {
		if len(phrases) > 0 {
			session["vocabulary_matching"] = vocabulary.Config{Terms: phrases, Prefix: prefix, Confirmed: confirmed, AudioOffset: offset}
		}
		dialCtx, stop := context.WithTimeout(ctx, 6*time.Second)
		defer stop()
		connection, _, e := websocket.Dial(dialCtx, target, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + strings.TrimSpace(string(token))}}})
		if e != nil {
			return nil, e
		}
		connection.SetReadLimit(8 << 20)
		update, _ := json.Marshal(map[string]any{"type": "session.update", "session": session})
		if e = connection.Write(dialCtx, websocket.MessageText, update); e != nil {
			connection.CloseNow()
			return nil, e
		}
		return connection, nil
	}
	connection, e := dial("", 0, 0)
	if e != nil {
		cancel()
		return nil, e
	}
	go func() {
		defer close(stream.DoneChan)
		defer close(stream.TextChan)
		if stream.Words != nil {
			defer close(stream.Words)
		}
		defer cancel()
		defer func() {
			if connection != nil {
				connection.CloseNow()
			}
		}()
		events := make(chan replayEvent, 64)
		epoch := 0
		startReader := func(conn *websocket.Conn, generation int) {
			go func() {
				for {
					kind, data, e := conn.Read(ctx)
					if e != nil {
						select {
						case events <- replayEvent{err: e, epoch: generation}:
						case <-ctx.Done():
						}
						return
					}
					if kind == websocket.MessageText {
						select {
						case events <- replayEvent{data: data, epoch: generation}:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
		}
		startReader(connection, epoch)
		overlap := 4.48
		if configured, e := strconv.ParseFloat(os.Getenv("SPEEKTOME_REPLAY_CONTEXT_SECONDS"), 64); e == nil && configured >= 4.48 && configured <= 30 {
			overlap = configured
		}
		var journal []replayChunk
		total, confirmed, baseOffset := offset, offset, offset
		history := ""
		partial := ""
		suppressReplayPreview := false
		committing := false
		var vocabularyDeadline <-chan time.Time
		retry := time.NewTicker(time.Second)
		defer retry.Stop()
		disconnectedAt := time.Time{}
		fail := func(e error) {
			select {
			case stream.ErrChan <- e:
			default:
			}
		}
		publish := func(text string) {
			select {
			case stream.TextChan <- text:
			case <-ctx.Done():
			}
		}
		confirm := func(from float64, words []TimedWord) {
			if stream.Words == nil || (confirmed <= from && len(words) == 0) {
				return
			}
			select {
			case stream.Words <- WordBatch{From: from, To: confirmed, Words: words}:
			case <-ctx.Done():
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-vocabularyDeadline:
				publish(history)
				return // Old worker or matcher failure: main uses a local final-pass fallback.
			case command := <-stream.replay.commands:
				var sendErr error
				if command.audio != nil {
					duration := utils.GetOggOpusDecodedDuration(command.audio)
					if duration <= 0 || duration > 5 {
						sendErr = errors.New("fleet streaming requires complete short Ogg Opus frames")
					} else {
						journal = append(journal, replayChunk{append([]byte(nil), command.audio...), total, total + duration})
						total += duration
						// Bound outage/backlog memory. Report failure rather than dropping audio.
						if total-confirmed > 120 {
							sendErr = errors.New("unconfirmed audio exceeded two-minute recovery buffer")
						} else if connection != nil {
							sendErr = connection.Write(ctx, websocket.MessageBinary, command.audio)
							if sendErr != nil {
								connection.CloseNow()
								connection = nil
								disconnectedAt = time.Now()
								sendErr = nil
							}
						}
					}
				} else if command.control != nil {
					kind, _ := command.control["type"].(string)
					if kind == "stop" || kind == "end_of_stream" || kind == "input_audio_buffer.commit" {
						committing = true
						if connection != nil {
							sendErr = connection.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.commit"}`))
							if sendErr != nil {
								connection.CloseNow()
								connection = nil
								disconnectedAt = time.Now()
								sendErr = nil
							}
						}
					}
				}
				command.ack <- sendErr
				if sendErr != nil {
					fail(sendErr)
					return
				}
			case event := <-events:
				if event.epoch != epoch {
					continue
				}
				if event.err != nil {
					if connection != nil {
						connection.CloseNow()
						connection = nil
						disconnectedAt = time.Now()
					}
					continue
				}
				var payload struct {
					Type           string  `json:"type"`
					Transcript     string  `json:"transcript"`
					Delta          string  `json:"delta"`
					AudioProcessed float64 `json:"audio_processed"`
					Words          []struct {
						Word  string  `json:"word"`
						Start float64 `json:"start"`
						End   float64 `json:"end"`
					} `json:"words"`
				}
				if json.Unmarshal(event.data, &payload) != nil {
					continue
				}
				if payload.Type == "vocabulary.final" && committing {
					var result vocabulary.Result
					if json.Unmarshal(event.data, &result) == nil && result.Validate(history, phrases) == nil {
						stream.vocabularyResult = &result
					}
					publish(history)
					return
				}
				if payload.Type == "error" {
					fail(errors.New("inference runtime reported an error"))
					return
				}
				// Partial hypotheses are UI previews only. Never advance confirmed or
				// discard journal audio until timestamped final words arrive.
				if payload.Type == "conversation.item.input_audio_transcription.delta" && payload.Delta != "" {
					partial += payload.Delta
					if !suppressReplayPreview {
						publish(strings.TrimSpace(history + " " + strings.TrimSpace(partial)))
					}
				}
				if strings.HasSuffix(payload.Type, ".completed") {
					partial = ""
					var fresh []string
					var timed []TimedWord
					from := confirmed
					last := confirmed
					for _, word := range payload.Words {
						start, end := word.Start+baseOffset, word.End+baseOffset
						if (start+end)/2 > confirmed {
							fresh = append(fresh, word.Word)
							timed = append(timed, TimedWord{word.Word, start, end})
							if end > last {
								last = end
							}
						}
					}
					if len(payload.Words) == 0 && payload.Transcript != "" {
						fail(errors.New("timestamped streaming output required for safe replay"))
						return
					}
					if len(fresh) > 0 {
						suppressReplayPreview = false
						if history != "" {
							history += " "
						}
						history += strings.Join(fresh, " ")
						confirmed = last
						publish(history)
					}
					// Silence-only finalized events may advance the checkpoint, but generic
					// audio_processed progress never acknowledges committed transcription.
					if payload.Transcript == "" && len(payload.Words) == 0 && payload.AudioProcessed > 0 {
						confirmed = max(confirmed, payload.AudioProcessed+baseOffset)
					}
					confirm(from, timed)
					cut := 0
					for cut < len(journal) && journal[cut].end < confirmed-overlap {
						cut++
					}
					journal = journal[cut:]
				}
				if payload.Type == "input_audio_buffer.committed" && committing {
					// The worker has finalized everything it was sent, so the
					// trailing silence after the last word is confirmed too.
					if from := confirmed; total > confirmed {
						confirmed = total
						confirm(from, nil)
					}
					if len(phrases) > 0 {
						vocabularyDeadline = time.After(1500 * time.Millisecond)
					} else {
						publish(history)
						return
					}
				}
			case <-retry.C:
				if connection != nil {
					continue
				}
				if !disconnectedAt.IsZero() && time.Since(disconnectedAt) > 90*time.Second {
					fail(errors.New("no worker available during stream recovery"))
					return
				}
				baseOffset = total
				if len(journal) > 0 {
					baseOffset = journal[0].start
				}
				newConn, e := dial(history, confirmed, baseOffset)
				if e != nil {
					continue
				}
				epoch++
				baseOffset = total
				if len(journal) > 0 {
					baseOffset = journal[0].start
				}
				// New decoder hypotheses include replayed context. Suppress them
				// until fresh timestamped finals cross the existing checkpoint.
				partial = ""
				suppressReplayPreview = baseOffset < confirmed
				connection = newConn
				startReader(connection, epoch)
				for _, chunk := range journal {
					if e = connection.Write(ctx, websocket.MessageBinary, chunk.data); e != nil {
						break
					}
				}
				if e == nil && committing {
					e = connection.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.commit"}`))
				}
				if e != nil {
					connection.CloseNow()
					connection = nil
				}
			}
		}
	}()
	return stream, nil
}

// Mirrors the protocol input bounds without compiling an index on the main
// server. Invalid snapshots fall through to raw ASR and an explicit audit error.
func validWorkerVocabulary(terms []string) bool {
	if len(terms) > vocabulary.MaxTerms {
		return false
	}
	size := 0
	for _, term := range terms {
		size += len(term)
		if len(term) > 256 || size > 128<<10 || !utf8.ValidString(term) || strings.ContainsAny(term, "\n\r\x00") {
			return false
		}
	}
	return true
}
