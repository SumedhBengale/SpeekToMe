package handlers

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The live meeting stream sends each run of finalized words to the desktop as a
// batch signed by this server. The desktop keeps the batches next to the audio
// and uploads them with it, so the finished meeting reuses the live transcript
// without the server holding it. Anything unsigned or altered is ignored, and
// the audio it should have covered is transcribed again as a gap.

const liveBatchVersion = 1

type liveWord struct {
	W string  `json:"w"`
	S float64 `json:"s"`
	E float64 `json:"e"`
}

// liveBatch confirms the recording range [From, To] of one channel. A batch
// with no words confirms silence.
type liveBatch struct {
	V       int        `json:"v"`
	KID     string     `json:"kid"`
	JobID   string     `json:"job_id"`
	Channel string     `json:"channel"`
	Session string     `json:"session"`
	Index   int        `json:"index"`
	From    float64    `json:"from"`
	To      float64    `json:"to"`
	Words   []liveWord `json:"words"`
}

// signedLiveEvent is the "words" stream event. The desktop stores each event
// line verbatim, so the signature covers the batch's exact bytes.
type signedLiveEvent struct {
	Type  string          `json:"type"`
	Batch json.RawMessage `json:"batch"`
	Sig   string          `json:"sig"`
}

type liveKey struct {
	id     string
	secret []byte
}

// liveKeys returns the signing key first, then a previous key still accepted
// for verification during rotation. Keys are 32 random bytes, base64-encoded.
func liveKeys() []liveKey {
	var keys []liveKey
	for _, name := range []string{"SPEEKTOME_LIVE_TRANSCRIPT_KEY", "SPEEKTOME_LIVE_TRANSCRIPT_KEY_PREVIOUS"} {
		secret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv(name)))
		if err != nil || len(secret) != 32 {
			if name == "SPEEKTOME_LIVE_TRANSCRIPT_KEY" {
				return nil
			}
			continue
		}
		sum := sha256.Sum256(secret)
		keys = append(keys, liveKey{hex.EncodeToString(sum[:4]), secret})
	}
	return keys
}

func liveMAC(secret []byte, user int64, batch []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("speektome-live-v1\n" + strconv.FormatInt(user, 10) + "\n"))
	mac.Write(batch)
	return mac.Sum(nil)
}

// signLiveBatch returns the event to send, or nil when no key is configured.
func signLiveBatch(user int64, batch liveBatch) []byte {
	keys := liveKeys()
	if len(keys) == 0 {
		return nil
	}
	batch.V, batch.KID = liveBatchVersion, keys[0].id
	if batch.Words == nil {
		batch.Words = []liveWord{}
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		return nil
	}
	event, err := json.Marshal(signedLiveEvent{Type: "words", Batch: raw, Sig: base64.StdEncoding.EncodeToString(liveMAC(keys[0].secret, user, raw))})
	if err != nil {
		return nil
	}
	return event
}

type timeSpan struct{ Start, End float64 }

// liveChannel is one channel's verified live transcript: its words, in time
// order, and the recording ranges those words cover.
type liveChannel struct {
	Words   []jobWord
	Covered []timeSpan
}

// parseLiveTranscript verifies an uploaded transcript file. Lines that fail to
// parse, verify or match this meeting are skipped; a torn last line from a
// crash is just one more skipped line.
func parseLiveTranscript(data []byte, user int64, clientID string) map[string]*liveChannel {
	keys := liveKeys()
	result := map[string]*liveChannel{"mic": {}, "system": {}}
	if len(keys) == 0 {
		return result
	}
	batches := map[string][]liveBatch{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var event signedLiveEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != "words" {
			continue
		}
		var batch liveBatch
		if json.Unmarshal(event.Batch, &batch) != nil || !validLiveBatch(batch, clientID) {
			continue
		}
		sig, err := base64.StdEncoding.DecodeString(event.Sig)
		if err != nil {
			continue
		}
		verified := false
		for _, key := range keys {
			if key.id == batch.KID && hmac.Equal(sig, liveMAC(key.secret, user, event.Batch)) {
				verified = true
			}
		}
		if verified {
			batches[batch.Channel] = append(batches[batch.Channel], batch)
		}
	}
	for channel, list := range batches {
		sort.SliceStable(list, func(a, b int) bool { return list[a].From < list[b].From })
		out := result[channel]
		// Each moment belongs to the first batch that covers it, which drops the
		// overlap a reconnecting desktop resends for decoder context.
		for _, batch := range list {
			span := timeSpan{batch.From, batch.To}
			owned := subtractSpans([]timeSpan{span}, out.Covered)
			for _, word := range batch.Words {
				if mid := (word.S + word.E) / 2; insideSpans(owned, mid) {
					out.Words = append(out.Words, jobWord{Word: word.W, Start: word.S, End: word.E})
				}
			}
			out.Covered = unionSpans(append(out.Covered, span))
		}
		sort.SliceStable(out.Words, func(a, b int) bool { return out.Words[a].Start < out.Words[b].Start })
	}
	return result
}

func validLiveBatch(b liveBatch, clientID string) bool {
	if b.V != liveBatchVersion || b.JobID != clientID || (b.Channel != "mic" && b.Channel != "system") {
		return false
	}
	if !finite(b.From) || !finite(b.To) || b.From < 0 || b.To < b.From || b.To > 4*3600+60 {
		return false
	}
	for _, w := range b.Words {
		if !finite(w.S) || !finite(w.E) || w.E < w.S || w.S < b.From-1 || w.E > b.To+1 || w.W == "" || len(w.W) > 256 {
			return false
		}
	}
	return true
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// unionSpans sorts and merges overlapping or touching spans.
func unionSpans(spans []timeSpan) []timeSpan {
	sorted := append([]timeSpan(nil), spans...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Start < sorted[b].Start })
	var out []timeSpan
	for _, s := range sorted {
		if s.End <= s.Start {
			continue
		}
		if n := len(out); n > 0 && s.Start <= out[n-1].End {
			out[n-1].End = max(out[n-1].End, s.End)
			continue
		}
		out = append(out, s)
	}
	return out
}

// subtractSpans returns the parts of spans not inside the sorted, merged cut.
func subtractSpans(spans, cut []timeSpan) []timeSpan {
	var out []timeSpan
	for _, s := range unionSpans(spans) {
		start := s.Start
		for _, c := range cut {
			if c.End <= start || c.Start >= s.End {
				continue
			}
			if c.Start > start {
				out = append(out, timeSpan{start, c.Start})
			}
			start = max(start, c.End)
		}
		if start < s.End {
			out = append(out, timeSpan{start, s.End})
		}
	}
	return out
}

func insideSpans(spans []timeSpan, t float64) bool {
	for _, s := range spans {
		if t >= s.Start && t < s.End {
			return true
		}
	}
	return false
}

func spanSeconds(spans []timeSpan) float64 {
	total := 0.0
	for _, s := range spans {
		total += s.End - s.Start
	}
	return total
}

// Gaps closer than this are re-transcribed as one range, and shorter gaps are
// ignored: a decoder checkpoint can trail the audio by a few frames.
const (
	gapJoinSeconds = 1.0
	gapMinSeconds  = 0.3
)

// transcriptGaps returns the parts of [0, duration] the live transcript did not
// cover, tidied for re-transcription.
func transcriptGaps(covered []timeSpan, duration float64) []timeSpan {
	raw := subtractSpans([]timeSpan{{0, duration}}, unionSpans(covered))
	var joined []timeSpan
	for _, g := range raw {
		if n := len(joined); n > 0 && g.Start-joined[n-1].End < gapJoinSeconds {
			joined[n-1].End = g.End
			continue
		}
		joined = append(joined, g)
	}
	var out []timeSpan
	for _, g := range joined {
		if g.End-g.Start >= gapMinSeconds {
			out = append(out, g)
		}
	}
	return out
}
