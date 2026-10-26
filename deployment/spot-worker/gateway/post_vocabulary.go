package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"speektome/vocabulary"
	"strings"
	"sync"
	"time"
)

// One instance belongs to one authenticated stream. Only final words are retained;
// partial hypotheses never enter the final matcher. Recovery seeds the checkpoint.
type postVocabulary struct {
	mu                sync.Mutex
	config            *vocabulary.Config
	compiled          *vocabulary.UserVocabulary
	raw               string
	confirmed         float64
	started, finished bool
	liveKeys          string // proposals last sent live, to skip unchanged updates
}

func (p *postVocabulary) control(data []byte) ([]byte, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(data, &event) != nil {
		return data, nil
	}
	var kind string
	_ = json.Unmarshal(event["type"], &kind)
	if kind != "session.update" && kind != "start" && kind != "config" {
		return data, nil
	}
	var s map[string]json.RawMessage
	if json.Unmarshal(event["session"], &s) != nil {
		s = event
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if raw, ok := s["vocabulary_matching"]; ok {
		if p.started || p.config != nil {
			return nil, errors.New("vocabulary must be configured once before audio")
		}
		var cfg vocabulary.Config
		if json.Unmarshal(raw, &cfg) != nil || len(cfg.Prefix) > vocabulary.MaxTranscriptBytes || cfg.Confirmed < 0 || cfg.AudioOffset < 0 || math.IsNaN(cfg.Confirmed) || math.IsInf(cfg.Confirmed, 0) || math.IsNaN(cfg.AudioOffset) || math.IsInf(cfg.AudioOffset, 0) {
			return nil, errors.New("invalid vocabulary session")
		}
		compiled, err := vocabulary.Compile(cfg.Terms)
		if err != nil {
			return nil, err
		}
		p.config = &cfg
		p.compiled = compiled
		p.raw = cfg.Prefix
		p.confirmed = cfg.Confirmed
	}
	delete(s, "vocabulary_matching")
	if p.config != nil {
		// The model gets ordinary audio, with no decoder vocabulary or prompt bias.
		delete(s, "speech_contexts")
		delete(s, "boosted_words")
		delete(s, "prompt")
		s["speech_contexts"] = json.RawMessage(`[]`)
		s["word_timestamps"] = json.RawMessage(`true`)
	}
	if _, ok := event["session"]; ok {
		event["session"], _ = json.Marshal(s)
	} else {
		delete(s, "type")
		event = map[string]json.RawMessage{"session": nil}
		event["session"], _ = json.Marshal(s)
	}
	event["type"], _ = json.Marshal("session.update")
	return json.Marshal(event)
}
func (p *postVocabulary) audio() { p.mu.Lock(); p.started = true; p.mu.Unlock() }

// observe returns the vocabulary event to send after this backend event, if
// any, and whether it is the final result that ends the stream.
func (p *postVocabulary) observe(data []byte) ([]byte, bool) {
	var e struct {
		Type           string  `json:"type"`
		Transcript     string  `json:"transcript"`
		AudioProcessed float64 `json:"audio_processed"`
		Words          []struct {
			Word  string  `json:"word"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"words"`
	}
	if json.Unmarshal(data, &e) != nil {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.config == nil || p.finished {
		return nil, false
	}
	if e.Type == "conversation.item.input_audio_transcription.completed" {
		var words []string
		last := p.confirmed
		for _, w := range e.Words {
			start, end := w.Start+p.config.AudioOffset, w.End+p.config.AudioOffset
			if (start+end)/2 > p.confirmed {
				words = append(words, w.Word)
				last = max(last, end)
			}
		}
		if len(words) > 0 {
			p.raw = strings.TrimSpace(p.raw + " " + strings.Join(words, " "))
			p.confirmed = last
		}
		if e.Transcript == "" && len(e.Words) == 0 && e.AudioProcessed > 0 {
			p.confirmed = max(p.confirmed, e.AudioProcessed+p.config.AudioOffset)
		}
		if len(words) > 0 && p.config.Live {
			return p.liveUpdate(), false
		}
	}
	if e.Type != "input_audio_buffer.committed" {
		return nil, false
	}
	p.finished = true
	r := p.propose(time.Second)
	encoded, _ := json.Marshal(r)
	return encoded, true
}

func (p *postVocabulary) propose(deadline time.Duration) vocabulary.Result {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	r := vocabulary.Propose(ctx, p.raw, p.compiled, vocabulary.TermsHash(p.config.Terms))
	r.MatchMillis = float64(time.Since(start).Microseconds()) / 1000
	return r
}

// liveUpdate matches the confirmed transcript so far. Confirmed text only
// grows, so earlier offsets stay valid; unchanged proposals are not resent.
func (p *postVocabulary) liveUpdate() []byte {
	r := p.propose(250 * time.Millisecond)
	if r.Error != "" {
		return nil
	}
	var keys strings.Builder
	for _, c := range r.Choices {
		fmt.Fprintf(&keys, "%d:%d:%s\n", c.StartByte, c.EndByte, c.Replacement)
	}
	if keys.String() == p.liveKeys {
		return nil
	}
	p.liveKeys = keys.String()
	r.Type = vocabulary.UpdateType
	encoded, _ := json.Marshal(r)
	return encoded
}

// Legacy providers and merged meeting channels use the same worker-side matcher
// after the entire ASR job is complete. No audio or GPU job is launched here.
func (g *gateway) matchVocabulary(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input vocabulary.Request
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 3<<20)).Decode(&input) != nil || len(input.Raw) > vocabulary.MaxTranscriptBytes {
		http.Error(w, "invalid vocabulary request", 400)
		return
	}
	compiled, err := vocabulary.Compile(input.Terms)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	start := time.Now()
	result := vocabulary.Propose(ctx, input.Raw, compiled, vocabulary.TermsHash(input.Terms))
	result.MatchMillis = float64(time.Since(start).Microseconds()) / 1000
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
