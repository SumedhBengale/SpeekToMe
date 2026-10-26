// Package vocabulary proposes text edits. It never biases ASR decoding.
package vocabulary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const Version = "phonetic-symspell-jaro-v2"
const MaxTerms = 1000
const MaxTranscriptBytes = 2 << 20

type Term struct {
	Canonical string `json:"canonical"`
}
type Config struct {
	Terms       []string `json:"terms"`
	Prefix      string   `json:"confirmed_prefix,omitempty"`
	Confirmed   float64  `json:"confirmed_seconds,omitempty"`
	AudioOffset float64  `json:"audio_offset_seconds,omitempty"`
	// Live asks the worker for an UpdateType event whenever newly confirmed
	// words change the proposals, so decisions can start before the stream ends.
	Live bool `json:"live,omitempty"`
}

const (
	FinalType  = "vocabulary.final"
	UpdateType = "vocabulary.update"
)
type Choice struct {
	ID          string  `json:"id"`
	StartByte   int     `json:"start_byte"`
	EndByte     int     `json:"end_byte"`
	Original    string  `json:"original"`
	Replacement string  `json:"replacement"`
	Score       float64 `json:"score"`
	MatchType   string  `json:"match_type"`
}
type Result struct {
	Type           string   `json:"type"`
	SchemaVersion  int      `json:"schema_version"`
	Raw            string   `json:"raw_transcript"`
	Enhanced       string   `json:"vocabulary_transcript"`
	TranscriptSHA  string   `json:"transcript_sha256"`
	VocabularySHA  string   `json:"vocabulary_sha256"`
	MatcherVersion string   `json:"matcher_version"`
	Choices        []Choice `json:"choices"`
	Error          string   `json:"matcher_error,omitempty"`
	MatchMillis    float64  `json:"match_ms"`
}

func Hash(s string) string            { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func TermsHash(terms []string) string { b, _ := json.Marshal(terms); return Hash(string(b)) }
func Compile(terms []string) (*UserVocabulary, error) {
	if len(terms) > MaxTerms {
		return nil, errors.New("vocabulary exceeds 1000 terms")
	}
	var clean []Term
	seen := map[string]bool{}
	size := 0
	for _, t := range terms {
		size += len(t)
		if !utf8.ValidString(t) || strings.ContainsAny(t, "\n\r\x00") || len(t) > 256 || size > 128<<10 {
			return nil, errors.New("invalid or oversized vocabulary")
		}
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			clean = append(clean, Term{t})
		}
	}
	sort.Slice(clean, func(i, j int) bool { return clean[i].Canonical < clean[j].Canonical })
	return CompileUserVocabulary(clean), nil
}
func Propose(ctx context.Context, raw string, compiled *UserVocabulary, termsSHA string) Result {
	r := Result{Type: FinalType, SchemaVersion: 1, Raw: raw, Enhanced: raw, TranscriptSHA: Hash(raw), VocabularySHA: termsSHA, MatcherVersion: Version, Choices: []Choice{}}
	if len(raw) > MaxTranscriptBytes || !utf8.ValidString(raw) {
		r.Error = "transcript_limit"
		return r
	}
	var err error
	r.Choices, err = propose(ctx, raw, compiled)
	if err != nil {
		r.Error = "matcher_timeout"
		r.Choices = []Choice{}
		return r
	}
	if r.Choices == nil {
		r.Choices = []Choice{}
	}
	r.Enhanced, err = Apply(raw, r.Choices, nil)
	if err != nil {
		r.Error = "invalid_candidates"
		r.Enhanced = raw
		r.Choices = []Choice{}
	}
	return r
}

// Apply uses UTF-8 byte offsets in the immutable raw transcript. nil selects all.
func Apply(raw string, choices []Choice, use map[string]bool) (string, error) {
	var b strings.Builder
	pos := 0
	seen := map[string]bool{}
	for _, c := range choices {
		if c.ID == "" || seen[c.ID] || c.StartByte < pos || c.EndByte <= c.StartByte || c.EndByte > len(raw) || raw[c.StartByte:c.EndByte] != c.Original || !utf8.ValidString(c.Original) || !utf8.ValidString(c.Replacement) || !utf8.ValidString(raw[:c.StartByte]) || !utf8.ValidString(raw[:c.EndByte]) {
			return "", errors.New("invalid vocabulary edit")
		}
		seen[c.ID] = true
		b.WriteString(raw[pos:c.StartByte])
		if use == nil || use[c.ID] {
			b.WriteString(c.Replacement)
		} else {
			b.WriteString(c.Original)
		}
		pos = c.EndByte
	}
	b.WriteString(raw[pos:])
	return b.String(), nil
}
func (r Result) Validate(raw string, terms []string) error {
	return r.ValidateAs(FinalType, raw, terms)
}

// ValidateAs checks a result of the given event type against the transcript
// it claims to describe.
func (r Result) ValidateAs(kind, raw string, terms []string) error {
	if r.Type != kind || r.SchemaVersion != 1 || r.Raw != raw || r.TranscriptSHA != Hash(raw) || r.VocabularySHA != TermsHash(terms) || r.MatcherVersion != Version {
		return errors.New("vocabulary result identity mismatch")
	}
	allowed := map[string]bool{}
	for _, t := range terms {
		allowed[strings.TrimSpace(t)] = true
	}
	for _, c := range r.Choices {
		if !allowed[c.Replacement] {
			return errors.New("replacement absent from vocabulary")
		}
	}
	text, e := Apply(raw, r.Choices, nil)
	if e != nil {
		return e
	}
	if text != r.Enhanced {
		return errors.New("vocabulary result text mismatch")
	}
	return nil
}

// Request is also used for finalized legacy and durable meeting transcripts.
// Native streaming sessions build this text on the worker and do not round-trip it.
type Request struct {
	Raw   string   `json:"raw_transcript"`
	Terms []string `json:"terms"`
}
