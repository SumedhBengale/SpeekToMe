// Package snippets finds where a dictation speaks the name of a saved snippet
// and decides, occurrence by occurrence, whether the speaker asked for the
// snippet or just said those words in a sentence.
package snippets

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Snippet is a saved text block the speaker inserts by saying its name. The
// content is not needed to find or decide occurrences, so it is loaded only
// for the snippets a dictation actually inserts.
type Snippet struct {
	ID   string
	Name string
}

// Occurrence is a spoken snippet name in a transcript: the byte range of its
// words, plus sentence punctuation the recognizer attached to the last word,
// which the snippet replaces when it is inserted. CommandStart is earlier than
// Start when a command word such as "insert" leads the name; an inserted
// snippet replaces that word too.
type Occurrence struct {
	SnippetID    string `json:"snippet_id"`
	Name         string `json:"-"`
	CommandStart int    `json:"command_start"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
}

// Key identifies an occurrence across growing versions of a transcript.
func (o Occurrence) Key() string {
	return o.SnippetID + ":" + strconv.Itoa(o.Start) + ":" + strconv.Itoa(o.End)
}

type word struct {
	start, end int
	norm       string
}

// words splits text into letter/number runs. Apostrophes inside a word stay in
// it, so "today's" is one word.
func words(text string) []word {
	var out []word
	start := -1
	for i, r := range text {
		inside := unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
		if !inside && (r == '\'' || r == '’') && start >= 0 {
			next, _ := utf8.DecodeRuneInString(text[i+utf8.RuneLen(r):])
			inside = unicode.IsLetter(next)
		}
		if inside && start < 0 {
			start = i
		}
		if !inside && start >= 0 {
			out = append(out, word{start, i, normalize(text[start:i])})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, word{start, len(text), normalize(text[start:])})
	}
	return out
}

func normalize(w string) string {
	return strings.ReplaceAll(strings.ToLower(w), "’", "'")
}

// Normalize returns a name's comparable form: its words, lowercased and joined
// by single spaces. Two snippets of one account may not share it.
func Normalize(name string) string {
	var parts []string
	for _, w := range words(name) {
		parts = append(parts, w.norm)
	}
	return strings.Join(parts, " ")
}

// WordCount is the number of words Normalize finds in a name.
func WordCount(name string) int { return len(words(name)) }

// Matcher finds snippet names in transcripts.
type Matcher struct {
	byFirst map[string][]entry
}

type entry struct {
	snippet Snippet
	words   []string
}

func NewMatcher(list []Snippet) *Matcher {
	m := &Matcher{byFirst: map[string][]entry{}}
	for _, s := range list {
		n := Normalize(s.Name)
		if n == "" {
			continue
		}
		parts := strings.Split(n, " ")
		m.byFirst[parts[0]] = append(m.byFirst[parts[0]], entry{s, parts})
	}
	for _, list := range m.byFirst {
		// Longer names first, so "my work address" wins over "my work".
		sort.SliceStable(list, func(a, b int) bool { return len(list[a].words) > len(list[b].words) })
	}
	return m
}

// Empty reports whether the matcher has no names to find.
func (m *Matcher) Empty() bool { return m == nil || len(m.byFirst) == 0 }

// Find returns the non-overlapping occurrences in text, left to right. A name
// matches whole words only, and never across a sentence end or line break.
func (m *Matcher) Find(text string) []Occurrence {
	if m.Empty() {
		return nil
	}
	ws := words(text)
	var out []Occurrence
	for i := 0; i < len(ws); i++ {
		for _, e := range m.byFirst[ws[i].norm] {
			if i+len(e.words) > len(ws) {
				continue
			}
			ok := true
			for k := 1; k < len(e.words) && ok; k++ {
				ok = ws[i+k].norm == e.words[k] && joinable(text[ws[i+k-1].end:ws[i+k].start])
			}
			if !ok {
				continue
			}
			last := ws[i+len(e.words)-1]
			end := last.end
			// A recognizer's full stop after the name belongs to it: "My address."
			for end < len(text) && strings.IndexByte(".!?", text[end]) >= 0 {
				end++
			}
			command := ws[i].start
			if i > 0 && commandWords[ws[i-1].norm] && joinable(text[ws[i-1].end:ws[i].start]) && (len(out) == 0 || out[len(out)-1].End <= ws[i-1].start) {
				command = ws[i-1].start
			}
			out = append(out, Occurrence{SnippetID: e.snippet.ID, Name: e.snippet.Name, CommandStart: command, Start: ws[i].start, End: end})
			i += len(e.words) - 1
			break
		}
	}
	return out
}

// Spoken right before a snippet's name, these words ask for the snippet.
var commandWords = map[string]bool{"insert": true, "paste": true, "snippet": true}

// joinable reports whether the text between two words keeps them in one
// spoken phrase.
func joinable(between string) bool {
	return !strings.ContainsAny(between, ".!?\n\r")
}

// Alone reports whether the occurrence is everything the speaker said, so
// there is no sentence it could be part of.
func Alone(text string, o Occurrence) bool {
	return len(words(text[:o.CommandStart])) == 0 && len(words(text[o.End:])) == 0
}

// Standalone reports whether the occurrence is a sentence of its own: nothing
// but sentence breaks separate it from the text around it.
func Standalone(text string, o Occurrence) bool {
	before := strings.TrimRightFunc(text[:o.CommandStart], unicode.IsSpace)
	if before != "" && !strings.ContainsAny(before[len(before)-1:], ".!?:\n") {
		return false
	}
	after := strings.TrimLeftFunc(text[o.End:], unicode.IsSpace)
	if after == "" || o.End > 0 && strings.IndexByte(".!?", text[o.End-1]) >= 0 {
		return true
	}
	return strings.ContainsAny(after[:1], ".!?\n")
}
