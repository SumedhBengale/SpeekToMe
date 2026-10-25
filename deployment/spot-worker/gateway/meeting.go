package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var jobIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type meetingWord struct {
	Word       string  `json:"word"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence,omitempty"`
}
type meetingASR struct {
	Text     string        `json:"text"`
	Words    []meetingWord `json:"words"`
	Duration float64       `json:"duration"`
}

// Separate from live admission: one meeting, exactly two ASR stages and one
// diarization stage, may share a worker with the configured live sessions.
func (g *gateway) admitMeeting(job, stage string) bool {
	g.admission.Lock()
	defer g.admission.Unlock()
	if !jobIDPattern.MatchString(job) || (stage != "mic" && stage != "system" && stage != "diar") {
		return false
	}
	if g.meetingExclusive && len(g.slots) > 0 {
		return false
	}
	if len(g.meetingStages) > 0 && g.meetingID != job {
		return false
	}
	if g.meetingStages == nil {
		g.meetingStages = map[string]bool{}
	}
	if g.meetingStages[stage] {
		return false
	}
	g.meetingID = job
	g.meetingStages[stage] = true
	return true
}
func (g *gateway) releaseMeeting(stage string) {
	g.admission.Lock()
	defer g.admission.Unlock()
	delete(g.meetingStages, stage)
	if len(g.meetingStages) == 0 {
		g.meetingID = ""
	}
}
func (g *gateway) meeting(w http.ResponseWriter, r *http.Request) {
	contexts, contextErr := queryContexts(r.URL.Query())
	if contextErr != nil {
		http.Error(w, contextErr.Error(), 400)
		return
	}
	stage := strings.TrimPrefix(r.URL.Path, "/v1/meeting/")
	job := r.Header.Get("X-SpeekToMe-Meeting-ID")
	if r.Method != "POST" || !jobIDPattern.MatchString(job) || (stage != "mic" && stage != "system" && stage != "diar") {
		http.Error(w, "invalid meeting request", 400)
		return
	}
	if !g.admitMeeting(job, stage) {
		w.Header().Set("X-SpeekToMe-Admission", "rejected")
		http.Error(w, "meeting capacity full", 429)
		return
	}
	defer g.releaseMeeting(stage)
	stat := g.stats.begin(r)
	g.stats.mu.Lock()
	stat.diar = stage == "diar"
	stat.asr = stage != "diar"
	g.stats.mu.Unlock()
	defer g.stats.end(stat)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	// Decode straight from the request into memory. Meeting audio is never
	// written to this worker's disk, not even as a temporary file.
	pcm, e := decodeMeetingAudio(ctx, http.MaxBytesReader(w, r.Body, 512<<20))
	defer clear(pcm)
	if errors.Is(e, errMeetingTooLong) {
		http.Error(w, "invalid audio or recording exceeds four hours", 422)
		return
	}
	if e != nil {
		http.Error(w, "cannot decode audio", 422)
		return
	}
	duration := float64(len(pcm)) / pcmBytesPerSecond
	if stage == "diar" {
		result, e := g.meetingBackend(ctx, "/v1/audio/diarizations", pcm, map[string]string{"mode": "streaming"})
		if e != nil {
			http.Error(w, "diarization failed: "+e.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
		return
	}
	ranges, e := parseMeetingRanges(r.URL.Query().Get("ranges"), duration)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	result, e := g.transcribeRanges(ctx, pcm, duration, ranges, r.URL.Query().Get("language"), contexts)
	if e != nil {
		http.Error(w, "transcription failed: "+e.Error(), 502)
		return
	}
	var words []string
	for _, word := range result.Words {
		words = append(words, word.Word)
	}
	result.Text = strings.Join(words, " ")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

type meetingRange struct{ start, end float64 }

// transcribeRanges runs recorded ASR over the given ranges of the decoded
// audio, in 30-second ownership windows with context on both sides. Word
// midpoints belong to exactly one window, and timestamps stay on the
// recording's timeline. A meeting whose live transcript already covers most
// of the recording asks only for its gaps, so only those reach the GPU.
func (g *gateway) transcribeRanges(ctx context.Context, pcm []byte, duration float64, ranges []meetingRange, language string, contexts []speechContext) (meetingASR, error) {
	result := meetingASR{Words: []meetingWord{}, Duration: duration}
	for _, span := range ranges {
		for start := span.start; start < span.end; start += 30 {
			end := math.Min(span.end, start+30)
			from := math.Max(0, start-4.48)
			to := math.Min(duration, end+4.48)
			section, e := g.meetingStreamingASR(ctx, bytes.NewReader(pcmSection(pcm, from, to)), language, contexts)
			if e != nil {
				return meetingASR{}, e
			}
			for _, word := range section.Words {
				word.Start += from
				word.End += from
				if mid := (word.Start + word.End) / 2; mid >= start && mid < end {
					result.Words = append(result.Words, word)
				}
			}
		}
	}
	return result, nil
}

// maxMeetingRanges matches the coordinator's bound on one gap request.
const maxMeetingRanges = 512

// parseMeetingRanges reads the optional ranges parameter, "12.40-75.10,…" in
// seconds: sorted, non-overlapping, and within the recording (a range may end
// up to a second past it; it is clipped). Without it, the whole recording is
// one range.
func parseMeetingRanges(raw string, duration float64) ([]meetingRange, error) {
	if raw == "" {
		return []meetingRange{{0, duration}}, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxMeetingRanges {
		return nil, errors.New("too many ranges")
	}
	ranges := make([]meetingRange, 0, len(parts))
	previous := 0.0
	for _, part := range parts {
		a, b, ok := strings.Cut(part, "-")
		start, startErr := strconv.ParseFloat(a, 64)
		end, endErr := strconv.ParseFloat(b, 64)
		if !ok || startErr != nil || endErr != nil || math.IsNaN(start) || math.IsNaN(end) || math.IsInf(end, 0) ||
			start < previous || end <= start || end > duration+1 {
			return nil, errors.New("invalid ranges")
		}
		ranges = append(ranges, meetingRange{start, math.Min(end, duration)})
		previous = end
	}
	return ranges, nil
}

func (g *gateway) meetingBackend(ctx context.Context, path string, pcm []byte, fields map[string]string) ([]byte, error) {
	input := io.MultiReader(bytes.NewReader(wavHeader(len(pcm))), bytes.NewReader(pcm))
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	finished := make(chan error, 1)
	go func() {
		p, e := form.CreateFormFile("file", "audio.wav")
		if e == nil {
			_, e = io.Copy(p, input)
		}
		if e == nil {
			for key, value := range fields {
				if e = form.WriteField(key, value); e != nil {
					break
				}
			}
		}
		if e == nil {
			e = form.Close()
		}
		writer.CloseWithError(e)
		finished <- e
	}()
	req, e := http.NewRequestWithContext(ctx, "POST", g.upstream.String()+path, reader)
	if e != nil {
		reader.Close()
		<-finished
		return nil, e
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		reader.Close()
		<-finished
		return nil, e
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	reader.Close()
	writeErr := <-finished
	if e != nil {
		return nil, e
	}
	if writeErr != nil {
		return nil, writeErr
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("runtime status %d", resp.StatusCode)
	}
	return data, nil
}

// Recorded audio uses the cache-aware encoder too. Whole-utterance HTTP ASR
// allocates attention tensors proportional to section length and can exhaust a
// fractional GPU when live streams share it. Each bounded section is a separate
// streaming session; the runtime consumes queued PCM at its own compute speed.
func (g *gateway) meetingStreamingASR(ctx context.Context, input io.Reader, language string, contexts ...[]speechContext) (meetingASR, error) {
	result := meetingASR{Words: []meetingWord{}}
	endpoint := *g.upstream
	endpoint.Scheme = "ws"
	if g.upstream.Scheme == "https" {
		endpoint.Scheme = "wss"
	}
	endpoint.Path = "/v1/audio/transcriptions/realtime"
	conn, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	defer conn.Close()
	conn.SetReadLimit(8 << 20)
	deadline := time.Now().Add(30 * time.Minute)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	conn.SetWriteDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	session := map[string]interface{}{"sample_rate": 16000, "word_timestamps": true, "speaker_diarization": false}
	if len(contexts) > 0 && len(contexts[0]) > 0 {
		session["speech_contexts"] = contexts[0]
	}
	if language != "" {
		session["language"] = language
	}
	if err = conn.WriteJSON(map[string]interface{}{"type": "session.update", "session": session}); err != nil {
		return result, err
	}
	sent := make(chan error, 1)
	go func() {
		var sendErr error
		defer func() {
			if sendErr != nil {
				conn.Close()
			}
			sent <- sendErr
		}()
		buf := make([]byte, 17920) // 560ms PCM16; bounded section is at most 39s.
		next := time.Now()
		for {
			n, e := input.Read(buf)
			if n > 0 {
				// Share spare capacity between the two background ASR stages.
				// At high live load, process recorded audio at real-time cadence
				// rather than dumping an entire section into the runtime queue.
				pace := meetingASRInterval(len(g.slots))
				if wait := time.Until(next); wait > 0 {
					timer := time.NewTimer(wait)
					select {
					case <-timer.C:
					case <-ctx.Done():
						timer.Stop()
						sendErr = ctx.Err()
						return
					}
				}
				next = time.Now().Add(pace)
				if we := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); we != nil {
					sendErr = we
					return
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				sendErr = e
				return
			}
		}
		sendErr = conn.WriteJSON(map[string]string{"type": "input_audio_buffer.commit"})
	}()
	// Ensure the sender exits before returning, including read failures.
	defer func() { conn.Close(); <-sent }()
	for {
		var event struct {
			Type       string          `json:"type"`
			Transcript string          `json:"transcript"`
			Words      []meetingWord   `json:"words"`
			Error      json.RawMessage `json:"error"`
		}
		if err = conn.ReadJSON(&event); err != nil {
			return result, err
		}
		switch event.Type {
		case "error":
			return result, fmt.Errorf("runtime streaming error: %s", event.Error)
		case "conversation.item.input_audio_transcription.completed":
			if strings.TrimSpace(event.Transcript) != "" && len(event.Words) == 0 {
				return result, fmt.Errorf("timestamped transcription unavailable")
			}
			result.Words = append(result.Words, event.Words...)
		case "input_audio_buffer.committed":
			return result, nil
		}
	}
}

func meetingASRInterval(live int) time.Duration {
	spare := max(2, 16-live)
	return time.Duration(float64(1120*time.Millisecond) / float64(spare))
}

const pcmBytesPerSecond = 16000 * 2

var errMeetingTooLong = errors.New("recording exceeds four hours")

// decodeMeetingAudio pipes an uploaded recording through ffmpeg and returns
// 16 kHz mono PCM16 held only in memory. Output is bounded to four hours.
func decodeMeetingAudio(ctx context.Context, body io.Reader) ([]byte, error) {
	limit := int64(14400 * pcmBytesPerSecond)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", "pipe:0", "-ac", "1", "-ar", "16000", "-f", "s16le", "-c:a", "pcm_s16le", "pipe:1")
	cmd.Stdin = body
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	pcm, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if int64(len(pcm)) > limit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		clear(pcm)
		return nil, errMeetingTooLong
	}
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil || len(pcm) < 2 {
		clear(pcm)
		return nil, errors.New("cannot decode audio")
	}
	return pcm[:len(pcm)&^1], nil
}

// pcmSection returns the PCM16 samples between two times without copying.
func pcmSection(pcm []byte, from, to float64) []byte {
	start := min(len(pcm), int(from*16000)*2)
	end := min(len(pcm), int(to*16000)*2)
	if end < start {
		end = start
	}
	return pcm[start:end]
}

// wavHeader describes 16 kHz mono PCM16 data of the given length.
func wavHeader(size int) []byte {
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+size))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], 16000)
	binary.LittleEndian.PutUint32(header[28:], pcmBytesPerSecond)
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(size))
	return header
}
