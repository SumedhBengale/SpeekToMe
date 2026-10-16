package vocab

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"speektome-server/db"
)

// ConservativeNormalizer is an opt-in alternative to the existing phonetic
// matcher. Production request handlers continue using VocabCache unchanged.
type ConservativeNormalizer struct {
	forms     map[string]string
	fullTerms map[string]string
	maxWords  int
	guarded   bool
}

// Keep non-ASCII letters intact so a partial ASCII spelling cannot match a
// multilingual canonical term after its other letters have been discarded.
func conservativeKey(text string) string {
	var key strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#' {
			key.WriteRune(r)
		}
	}
	return key.String()
}

// NewConservativeNormalizer indexes complete canonical terms, full CamelCase
// expansions, symbol expansions, and caller-supplied aliases. It deliberately
// excludes the legacy compiler's partial stems (e.g. torch -> PyTorch).
func NewConservativeNormalizer(terms []db.VocabularyTerm, aliases map[string]string, guarded bool) (*ConservativeNormalizer, error) {
	n := &ConservativeNormalizer{forms: map[string]string{}, fullTerms: map[string]string{}, maxWords: 1, guarded: guarded}
	allowed := map[string]bool{}
	add := func(parts []string, canonical string) {
		var normalized []string
		for _, p := range parts {
			if s := conservativeKey(p); s != "" {
				normalized = append(normalized, s)
			} else {
				return
			}
		}
		if len(normalized) == 0 {
			return
		}
		key := strings.Join(normalized, "\x00")
		if old, ok := n.forms[key]; ok && old != canonical {
			n.forms[key] = ""
		} else if !ok {
			n.forms[key] = canonical
		}
		if len(normalized) > n.maxWords {
			n.maxWords = len(normalized)
		}
	}
	for _, t := range terms {
		canonical := strings.TrimSpace(t.Canonical)
		if canonical == "" {
			continue
		}
		allowed[canonical] = true
		var parts []string
		for _, tok := range Tokenize(canonical) {
			parts = append(parts, tok.Clean)
		}
		add(parts, canonical)
		add(ExtractSubTokens(canonical), canonical)
		if strings.Contains(canonical, "#") {
			add(strings.Fields(strings.ReplaceAll(canonical, "#", " sharp")), canonical)
		}
		if strings.Contains(canonical, "++") {
			add(strings.Fields(strings.ReplaceAll(canonical, "++", " plus plus")), canonical)
		}
		// Guarded typo correction only considers complete long single-token names.
		if len(parts) == 1 {
			key := conservativeKey(canonical)
			if len([]rune(key)) >= 8 {
				if old, ok := n.fullTerms[key]; ok && old != canonical {
					n.fullTerms[key] = ""
				} else if !ok {
					n.fullTerms[key] = canonical
				}
			}
		}
	}
	for alias, canonical := range aliases {
		if !allowed[canonical] {
			return nil, fmt.Errorf("alias target %q is absent from vocabulary", canonical)
		}
		if strings.TrimSpace(alias) == "" {
			return nil, fmt.Errorf("alias must not be empty")
		}
		var parts []string
		for _, tok := range Tokenize(alias) {
			parts = append(parts, tok.Clean)
		}
		add(parts, canonical)
	}
	return n, nil
}

// Normalize preserves original whitespace and punctuation around replaced
// spans. Guarded mode permits one edit in a long, single-token name only when
// exactly one canonical candidate qualifies; ambiguous candidates are untouched.
func (n *ConservativeNormalizer) Normalize(text string) (string, []string) {
	type span struct {
		start, end int
		token      Token
	}
	var spans []span
	start := -1
	for i, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				token := parseWordBoundaries(text[start:i])
				token.Normalized = conservativeKey(token.Clean)
				spans = append(spans, span{start, i, token})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		token := parseWordBoundaries(text[start:])
		token.Normalized = conservativeKey(token.Clean)
		spans = append(spans, span{start, len(text), token})
	}
	var out strings.Builder
	cursor := 0
	replaced := map[string]bool{}
	for i := 0; i < len(spans); {
		canonical := ""
		count := 0
		var key []string
		for k := 1; k <= n.maxWords && i+k <= len(spans); k++ {
			tok := spans[i+k-1].token
			if tok.Normalized == "" || (k > 1 && (spans[i+k-2].token.Suffix != "" || tok.Prefix != "")) {
				break
			}
			key = append(key, tok.Normalized)
			if c := n.forms[strings.Join(key, "\x00")]; c != "" {
				canonical = c
				count = k
			}
		}
		if canonical == "" && n.guarded && len([]rune(spans[i].token.Normalized)) >= 7 {
			matches := 0
			for full, c := range n.fullTerms {
				if c != "" && levenshteinDistance(spans[i].token.Normalized, full) == 1 {
					canonical = c
					matches++
				}
			}
			if matches == 1 {
				count = 1
			} else {
				canonical = ""
			}
		}
		if count == 0 {
			i++
			continue
		}
		first, last := spans[i], spans[i+count-1]
		out.WriteString(text[cursor:first.start])
		out.WriteString(first.token.Prefix)
		out.WriteString(canonical)
		out.WriteString(last.token.Suffix)
		cursor = last.end
		replaced[canonical] = true
		i += count
	}
	out.WriteString(text[cursor:])
	var terms []string
	for t := range replaced {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	return out.String(), terms
}
