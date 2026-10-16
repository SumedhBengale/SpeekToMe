package vocabulary

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const (
	SingleWordThreshold = 0.88
	TwoWordThreshold    = 0.86
	ThreeWordThreshold  = 0.85
	MultiWordThreshold  = 0.84
)

// Token wraps individual words and split punctuation bounds
type Token struct {
	StartByte, EndByte int
	BoundaryBefore     bool
	Original           string
	Prefix             string
	Clean              string
	Suffix             string
	Normalized         string
}

// CompiledTerm holds compiled metadata for a vocabulary term
type CompiledTerm struct {
	Canonical     string
	Normalized    string
	TokenCount    int
	MinTokenCount int
	MaxTokenCount int
	SubTokens     []string
	PhoneticKey   string
	AcousticClass string
}

// TrieNode represents a node in the Aho-Corasick multi-word automaton
type TrieNode struct {
	Children map[string]*TrieNode
	Fail     *TrieNode
	Term     *CompiledTerm
	Depth    int
}

func newTrieNode(depth int) *TrieNode {
	return &TrieNode{
		Children: make(map[string]*TrieNode),
		Depth:    depth,
	}
}

// AhoCorasickTrie handles O(L) multi-pattern exact matching across token sequences
type AhoCorasickTrie struct {
	Root *TrieNode
}

func BuildAhoCorasick(terms []CompiledTerm) *AhoCorasickTrie {
	root := newTrieNode(0)

	// Step 1: Insert all token sequences into the Trie
	for i := range terms {
		term := &terms[i]
		tokens := term.SubTokens
		if len(tokens) == 0 {
			tokens = []string{term.Normalized}
		}

		curr := root
		for _, tok := range tokens {
			normTok := NormalizeString(tok)
			if normTok == "" {
				continue
			}
			if _, exists := curr.Children[normTok]; !exists {
				curr.Children[normTok] = newTrieNode(curr.Depth + 1)
			}
			curr = curr.Children[normTok]
		}
		curr.Term = term

		// If normalized single-string differs and is a single word, also insert as single token
		if len(tokens) > 1 && term.Normalized != "" && term.TokenCount == 1 {
			if _, exists := root.Children[term.Normalized]; !exists {
				root.Children[term.Normalized] = newTrieNode(1)
			}
			root.Children[term.Normalized].Term = term
		}
	}

	// Step 2: Build BFS failure links
	queue := make([]*TrieNode, 0)
	for _, child := range root.Children {
		child.Fail = root
		queue = append(queue, child)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for key, child := range curr.Children {
			queue = append(queue, child)

			failNode := curr.Fail
			for failNode != nil && failNode.Children[key] == nil {
				failNode = failNode.Fail
			}
			if failNode != nil {
				child.Fail = failNode.Children[key]
			} else {
				child.Fail = root
			}
		}
	}

	return &AhoCorasickTrie{Root: root}
}

// TrieMatchResult describes a span matched by the Aho-Corasick automaton
type TrieMatchResult struct {
	StartIdx  int
	EndIdx    int // inclusive
	Canonical string
}

// FindMatches searches the token sequence in a single linear pass
func (t *AhoCorasickTrie) FindMatches(tokens []Token) []TrieMatchResult {
	if t == nil || t.Root == nil || len(tokens) == 0 {
		return nil
	}

	var rawMatches []TrieMatchResult
	curr := t.Root

	for i, tok := range tokens {
		if tok.BoundaryBefore {
			curr = t.Root
		}
		norm := tok.Normalized
		if norm == "" {
			continue
		}

		for curr != nil && curr.Children[norm] == nil {
			curr = curr.Fail
		}

		if curr == nil {
			curr = t.Root
			continue
		}

		curr = curr.Children[norm]

		// Check output matches
		check := curr
		for check != nil && check != t.Root {
			if check.Term != nil {
				start := i - check.Depth + 1
				if start >= 0 {
					rawMatches = append(rawMatches, TrieMatchResult{
						StartIdx:  start,
						EndIdx:    i,
						Canonical: check.Term.Canonical,
					})
				}
			}
			check = check.Fail
		}
	}

	if len(rawMatches) == 0 {
		return nil
	}

	// Resolve greedy longest non-overlapping matches
	sort.Slice(rawMatches, func(i, j int) bool {
		lenI := rawMatches[i].EndIdx - rawMatches[i].StartIdx
		lenJ := rawMatches[j].EndIdx - rawMatches[j].StartIdx
		if lenI != lenJ {
			return lenI > lenJ // longest match first
		}
		return rawMatches[i].StartIdx < rawMatches[j].StartIdx
	})

	covered := make([]bool, len(tokens))
	var finalMatches []TrieMatchResult

	for _, m := range rawMatches {
		overlap := false
		for k := m.StartIdx; k <= m.EndIdx; k++ {
			if covered[k] {
				overlap = true
				break
			}
		}
		if !overlap {
			for k := m.StartIdx; k <= m.EndIdx; k++ {
				covered[k] = true
			}
			finalMatches = append(finalMatches, m)
		}
	}

	sort.Slice(finalMatches, func(i, j int) bool {
		return finalMatches[i].StartIdx < finalMatches[j].StartIdx
	})

	return finalMatches
}

// PhoneticSymSpellBucket holds symmetric delete variants partitioned by acoustic class
type PhoneticSymSpellBucket struct {
	// Map: AcousticClass -> Map: Key -> List of candidate terms
	Buckets map[string]map[string][]CompiledTerm
}

func BuildPhoneticSymSpell(terms []CompiledTerm) *PhoneticSymSpellBucket {
	buckets := make(map[string]map[string][]CompiledTerm)

	for _, term := range terms {
		if term.PhoneticKey == "" {
			continue
		}

		acousticClass := getAcousticClass(term.PhoneticKey)
		if buckets[acousticClass] == nil {
			buckets[acousticClass] = make(map[string][]CompiledTerm)
		}

		// 1. Exact phonetic key
		buckets[acousticClass][term.PhoneticKey] = append(buckets[acousticClass][term.PhoneticKey], term)

		// 2. Symmetric Delete variants (edit distance 1 deletions)
		runes := []rune(term.PhoneticKey)
		if len(runes) > 1 {
			for i := 0; i < len(runes); i++ {
				delKey := string(runes[:i]) + string(runes[i+1:])
				buckets[acousticClass][delKey] = append(buckets[acousticClass][delKey], term)
			}
		}
	}

	return &PhoneticSymSpellBucket{Buckets: buckets}
}

func getAcousticClass(phoneticKey string) string {
	if phoneticKey == "" {
		return "OTHER"
	}
	r := rune(phoneticKey[0])
	switch r {
	case 'P', 'B', 'F', 'V', 'W':
		return "LABIAL"
	case 'K', 'C', 'G', 'Q':
		return "VELAR"
	case 'T', 'D', '0':
		return "DENTAL"
	case 'S', 'Z', 'X', 'J':
		return "SIBILANT"
	case 'M', 'N', 'L', 'R':
		return "NASAL_LIQUID"
	case 'H', 'Y':
		return "GLIDE"
	default:
		return "VOWEL"
	}
}

// UserVocabulary holds precompiled index structures for fast matching
type UserVocabulary struct {
	Trie          *AhoCorasickTrie
	PhoneticSym   *PhoneticSymSpellBucket
	NormalizedMap map[string]string
	Terms         []CompiledTerm
	MaxTokenCount int
}

func NormalizeString(s string) string {
	s = strings.ToLower(s)
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '+' || r == '#' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// Tokenize splits a string into word tokens in a single O(N) pass,
// extracting prefix, clean word, and suffix boundaries.
func Tokenize(text string) []Token {
	var out []Token
	start := -1
	add := func(end int) {
		t := parseWordBoundaries(text[start:end])
		t.StartByte = start + len(t.Prefix)
		t.EndByte = end - len(t.Suffix)
		if len(out) > 0 {
			t.BoundaryBefore = strings.ContainsAny(text[out[len(out)-1].EndByte:t.StartByte], ".!?;:\n\r")
		}
		out = append(out, t)
	}
	for i, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				add(i)
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		add(len(text))
	}
	return out
}

func parseWordBoundaries(raw string) Token {
	runes := []rune(raw)
	n := len(runes)
	if n == 0 {
		return Token{}
	}

	leadingIdx := 0
	for leadingIdx < n && isPunctuation(runes[leadingIdx]) {
		leadingIdx++
	}

	trailingIdx := n - 1
	for trailingIdx >= leadingIdx && isPunctuation(runes[trailingIdx]) {
		trailingIdx--
	}

	if leadingIdx > trailingIdx {
		return Token{
			Original:   raw,
			Prefix:     raw,
			Clean:      "",
			Suffix:     "",
			Normalized: "",
		}
	}

	prefix := string(runes[:leadingIdx])
	clean := string(runes[leadingIdx : trailingIdx+1])
	suffix := string(runes[trailingIdx+1:])

	return Token{
		Original:   raw,
		Prefix:     prefix,
		Clean:      clean,
		Suffix:     suffix,
		Normalized: NormalizeString(clean),
	}
}

func isPunctuation(r rune) bool {
	// Preserve specific symbols that can be part of identifiers (C#, C++)
	if r == '#' || r == '+' {
		return false
	}
	return unicode.IsPunct(r) || (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126)
}

// ExtractSubTokens splits camelCase, PascalCase, hyphenated, and snake_case terms
func ExtractSubTokens(s string) []string {
	var words []string
	var curr strings.Builder
	runes := []rune(s)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]

		if r == '-' || r == '_' || r == '.' || r == '/' || r == ' ' {
			if curr.Len() > 0 {
				words = append(words, curr.String())
				curr.Reset()
			}
			continue
		}

		if r >= 'A' && r <= 'Z' {
			if curr.Len() > 0 {
				prevIsLower := i > 0 && (runes[i-1] >= 'a' && runes[i-1] <= 'z')
				nextIsLower := i+1 < n && (runes[i+1] >= 'a' && runes[i+1] <= 'z')
				if prevIsLower || (curr.Len() > 1 && nextIsLower) {
					words = append(words, curr.String())
					curr.Reset()
				}
			}
		}

		curr.WriteRune(r)
	}

	if curr.Len() > 0 {
		words = append(words, curr.String())
	}

	if len(words) == 1 {
		tokens := Tokenize(s)
		if len(tokens) > 1 {
			out := make([]string, len(tokens))
			for idx, tok := range tokens {
				out[idx] = tok.Clean
			}
			return out
		}
	}

	return words
}

func isVowel(r rune) bool {
	return r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u' || r == 'y'
}

func isSafeAutoExpandSubWord(word string) bool {
	norm := NormalizeString(word)
	if len(norm) < 3 {
		return false
	}

	connectors := map[string]bool{
		"the": true, "and": true, "for": true, "with": true,
		"from": true, "that": true, "this": true, "have": true,
		"are": true, "was": true, "were": true, "been": true,
		"you": true, "your": true, "their": true, "there": true,
		"they": true, "what": true, "when": true, "where": true,
		"who": true, "how": true, "why": true, "which": true,
		"will": true, "would": true, "could": true, "should": true,
		"not": true, "but": true, "can": true, "all": true,
	}
	return !connectors[norm]
}

func doubleMetaphone(s string) string {
	s = NormalizeString(s)
	if s == "" {
		return ""
	}

	runes := []rune(s)
	n := len(runes)
	var sb strings.Builder

	for i := 0; i < n; i++ {
		r := runes[i]
		switch r {
		case 'b':
			sb.WriteRune('P')
			if i+1 < n && runes[i+1] == 'b' {
				i++
			}
		case 'c':
			if i+1 < n && (runes[i+1] == 'e' || runes[i+1] == 'i' || runes[i+1] == 'y') {
				sb.WriteRune('S')
			} else if i+1 < n && runes[i+1] == 'h' {
				sb.WriteRune('X')
				i++
			} else {
				sb.WriteRune('K')
			}
			if i+1 < n && runes[i+1] == 'c' {
				i++
			}
		case 'd':
			if i+1 < n && runes[i+1] == 'g' {
				sb.WriteRune('J')
				i++
			} else {
				sb.WriteRune('T')
			}
			if i+1 < n && runes[i+1] == 'd' {
				i++
			}
		case 'f':
			sb.WriteRune('F')
			if i+1 < n && runes[i+1] == 'f' {
				i++
			}
		case 'g':
			if i+1 < n && (runes[i+1] == 'e' || runes[i+1] == 'i' || runes[i+1] == 'y') {
				sb.WriteRune('J')
			} else if i+1 < n && runes[i+1] == 'h' {
				i++
			} else {
				sb.WriteRune('K')
			}
			if i+1 < n && runes[i+1] == 'g' {
				i++
			}
		case 'j':
			sb.WriteRune('J')
		case 'k':
			sb.WriteRune('K')
			if i+1 < n && runes[i+1] == 'k' {
				i++
			}
		case 'l':
			sb.WriteRune('L')
			if i+1 < n && runes[i+1] == 'l' {
				i++
			}
		case 'm':
			sb.WriteRune('M')
			if i+1 < n && runes[i+1] == 'm' {
				i++
			}
		case 'n':
			sb.WriteRune('N')
			if i+1 < n && runes[i+1] == 'n' {
				i++
			}
		case 'p':
			if i+1 < n && runes[i+1] == 'h' {
				sb.WriteRune('F')
				i++
			} else {
				sb.WriteRune('P')
			}
			if i+1 < n && runes[i+1] == 'p' {
				i++
			}
		case 'q':
			sb.WriteRune('K')
		case 'r':
			sb.WriteRune('R')
			if i+1 < n && runes[i+1] == 'r' {
				i++
			}
		case 's':
			if i+1 < n && runes[i+1] == 'h' {
				sb.WriteRune('X')
				i++
			} else {
				sb.WriteRune('S')
			}
			if i+1 < n && runes[i+1] == 's' {
				i++
			}
		case 't':
			if i+1 < n && runes[i+1] == 'h' {
				sb.WriteRune('0')
				i++
			} else if i+1 < n && runes[i+1] == 'c' && i+2 < n && runes[i+2] == 'h' {
				sb.WriteRune('X')
				i += 2
			} else {
				sb.WriteRune('T')
			}
			if i+1 < n && runes[i+1] == 't' {
				i++
			}
		case 'v':
			sb.WriteRune('F')
		case 'w':
			if i+1 < n && isVowel(runes[i+1]) {
				sb.WriteRune('W')
			}
		case 'x':
			sb.WriteString("KS")
		case 'y':
			if i+1 < n && isVowel(runes[i+1]) {
				sb.WriteRune('Y')
			}
		case 'z':
			sb.WriteRune('S')
		}
	}

	return sb.String()
}

// CompileUserVocabulary compiles database terms into Trie + Phonetic SymSpell maps
func CompileUserVocabulary(dbTerms []Term) *UserVocabulary {
	normalizedMap := make(map[string]string)
	compiledTerms := make([]CompiledTerm, 0, len(dbTerms))
	maxTokenCount := 1

	for _, vt := range dbTerms {
		if strings.TrimSpace(vt.Canonical) == "" {
			continue
		}
		tokens := Tokenize(vt.Canonical)
		normalizedTokens := make([]string, len(tokens))
		for i, tok := range tokens {
			normalizedTokens[i] = tok.Normalized
		}
		computedNormalized := strings.Join(normalizedTokens, "")

		subTokens := ExtractSubTokens(vt.Canonical)
		joinedSubTokens := strings.Join(subTokens, "")

		normalizedMap[computedNormalized] = vt.Canonical
		if joinedSubTokens != "" && joinedSubTokens != computedNormalized {
			normalizedMap[joinedSubTokens] = vt.Canonical
		}

		minCount := len(normalizedTokens)
		maxCount := len(normalizedTokens)
		if len(subTokens) > maxCount {
			maxCount = len(subTokens)
		}
		if len(subTokens) < minCount && len(subTokens) > 0 {
			minCount = len(subTokens)
		}

		// Support spell-out acronym token sequences (e.g. "t r p c" for "tRPC" or "g p u" for "GPU")
		hasUpper := false
		for _, r := range vt.Canonical {
			if r >= 'A' && r <= 'Z' {
				hasUpper = true
				break
			}
		}
		if hasUpper && len(computedNormalized) <= 6 && len(computedNormalized) > maxCount {
			maxCount = len(computedNormalized)
		}

		phoneticKey := doubleMetaphone(computedNormalized)
		acousticClass := getAcousticClass(phoneticKey)

		ct := CompiledTerm{
			Canonical:     vt.Canonical,
			Normalized:    computedNormalized,
			TokenCount:    len(normalizedTokens),
			MinTokenCount: minCount,
			MaxTokenCount: maxCount,
			SubTokens:     subTokens,
			PhoneticKey:   phoneticKey,
			AcousticClass: acousticClass,
		}
		compiledTerms = append(compiledTerms, ct)

		// Index compound word stems (len >= 5) so phonetic variants like "postgres" match "PostgreSQL"
		if ct.TokenCount == 1 && len(subTokens) > 1 {
			for _, st := range subTokens {
				if len(st) >= 5 {
					stNorm := NormalizeString(st)
					if stNorm != "" && stNorm != computedNormalized {
						stPhonetic := doubleMetaphone(stNorm)
						stCt := CompiledTerm{
							Canonical:     vt.Canonical,
							Normalized:    computedNormalized,
							TokenCount:    1,
							MinTokenCount: 1,
							MaxTokenCount: 1,
							SubTokens:     []string{st},
							PhoneticKey:   stPhonetic,
							AcousticClass: getAcousticClass(stPhonetic),
						}
						compiledTerms = append(compiledTerms, stCt)
					}
				}
			}
		}

		// Support symbol expansions (e.g. C# -> c sharp, C++ -> c plus plus)
		if strings.Contains(computedNormalized, "#") {
			sharpNorm := strings.ReplaceAll(computedNormalized, "#", "sharp")
			normalizedMap[sharpNorm] = vt.Canonical
			sharpCt := CompiledTerm{
				Canonical:     vt.Canonical,
				Normalized:    sharpNorm,
				TokenCount:    2,
				MinTokenCount: 1,
				MaxTokenCount: 2,
				SubTokens:     []string{"c", "sharp"},
				PhoneticKey:   doubleMetaphone(sharpNorm),
				AcousticClass: getAcousticClass(doubleMetaphone(sharpNorm)),
			}
			compiledTerms = append(compiledTerms, sharpCt)
		}
		if strings.Contains(computedNormalized, "++") {
			plusNorm := strings.ReplaceAll(computedNormalized, "++", "plusplus")
			normalizedMap[plusNorm] = vt.Canonical
			plusCt := CompiledTerm{
				Canonical:     vt.Canonical,
				Normalized:    plusNorm,
				TokenCount:    3,
				MinTokenCount: 1,
				MaxTokenCount: 3,
				SubTokens:     []string{"c", "plus", "plus"},
				PhoneticKey:   doubleMetaphone(plusNorm),
				AcousticClass: getAcousticClass(doubleMetaphone(plusNorm)),
			}
			compiledTerms = append(compiledTerms, plusCt)
		}

		if maxCount > maxTokenCount {
			maxTokenCount = maxCount
		}
	}

	trie := BuildAhoCorasick(compiledTerms)
	phoneticSym := BuildPhoneticSymSpell(compiledTerms)

	return &UserVocabulary{
		Trie:          trie,
		PhoneticSym:   phoneticSym,
		NormalizedMap: normalizedMap,
		Terms:         compiledTerms,
		MaxTokenCount: maxTokenCount,
	}
}

// MatchAndReplace executes:
// Phase 1: O(L) Aho-Corasick Multi-Pattern Trie exact matching.
// Phase 2: O(1) Phonetic SymSpell with Acoustic Super-Bucketing on trailing window tokens.
func propose(ctx context.Context, text string, vocab *UserVocabulary) ([]Choice, error) {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return nil, nil
	}

	n := len(tokens)
	var choices []Choice
	add := func(start, end int, replacement string, score float64, kind string) {
		left, right := tokens[start].StartByte, tokens[end].EndByte
		if right > left {
			choices = append(choices, Choice{ID: fmt.Sprintf("v%d", len(choices)), StartByte: left, EndByte: right, Original: text[left:right], Replacement: replacement, Score: score, MatchType: kind})
		}
	}

	// Step 1: Phase 1 Aho-Corasick Exact Multi-Word Matching in O(L)
	trieMatches := vocab.Trie.FindMatches(tokens)
	matchedSpans := make(map[int]TrieMatchResult) // Key: StartIdx -> Result
	coveredIndices := make(map[int]bool)

	for _, m := range trieMatches {
		matchedSpans[m.StartIdx] = m
		for k := m.StartIdx; k <= m.EndIdx; k++ {
			coveredIndices[k] = true
		}
	}

	prefixThreshold := 0
	i := 0

	for i < n {
		if tokens[i].Normalized == "" {
			i++
			continue
		}
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		// If index is covered by exact Trie match
		if m, exists := matchedSpans[i]; exists {
			add(m.StartIdx, m.EndIdx, m.Canonical, 1, "exact")
			i = m.EndIdx + 1
			continue
		}

		if coveredIndices[i] {
			// Part of an ongoing multi-word match handled by StartIdx
			i++
			continue
		}

		// Fast Exact / Normalized Map check
		if canonical, ok := vocab.NormalizedMap[tokens[i].Normalized]; ok {
			add(i, i, canonical, 1, "normalized")
			i++
			continue
		}

		// If in stable prefix zone, skip heavy fuzzy checking
		if i < prefixThreshold {
			i++
			continue
		}

		// Phase 2: Phonetic SymSpell matching on window tokens (testing 1-word, 2-word, 3-word spans)
		bestCanonical := ""
		bestScore := 0.0
		bestK := 0

		maxK := vocab.MaxTokenCount
		if maxK > 4 {
			maxK = 4
		}

		for k := maxK; k >= 1; k-- {
			if i+k <= n {
				overlaps := strings.ContainsAny(text[tokens[i].StartByte:tokens[i+k-1].EndByte], "\n\r")
				for j := i; j < i+k; j++ {
					if coveredIndices[j] || tokens[j].Normalized == "" || (j > i && tokens[j].BoundaryBefore) {
						overlaps = true
					}
				}
				if overlaps {
					continue
				}
				var sbNorm strings.Builder
				for j := 0; j < k; j++ {
					sbNorm.WriteString(tokens[i+j].Normalized)
				}
				joinedNorm := sbNorm.String()
				if len(joinedNorm) < 2 {
					continue
				}

				// Check exact normalized map for joined multi-tokens (e.g. "csharp" or "fastapi")
				if canonical, ok := vocab.NormalizedMap[joinedNorm]; ok {
					bestCanonical = canonical
					bestScore = 1
					bestK = k
					break
				}

				// Phonetic SymSpell Bucket Lookup
				phoneticQuery := doubleMetaphone(joinedNorm)
				if phoneticQuery != "" {
					acousticClass := getAcousticClass(phoneticQuery)
					if bucket, ok := vocab.PhoneticSym.Buckets[acousticClass]; ok {
						threshold := SingleWordThreshold
						if k == 2 {
							threshold = TwoWordThreshold
						} else if k == 3 {
							threshold = ThreeWordThreshold
						} else if k >= 4 {
							threshold = MultiWordThreshold
						}

						candidates := append([]CompiledTerm(nil), bucket[phoneticQuery]...)
						qRunes := []rune(phoneticQuery)
						if len(qRunes) > 1 {
							for d := 0; d < len(qRunes); d++ {
								delQ := string(qRunes[:d]) + string(qRunes[d+1:])
								if delMatches, hasDel := bucket[delQ]; hasDel {
									candidates = append(candidates, delMatches...)
								}
							}
						}

						// Evaluate candidates
						for _, cand := range candidates {
							if absVal(cand.TokenCount-k) > 1 && absVal(len(cand.SubTokens)-k) > 1 {
								continue
							}

							// Check length ratio to prevent wild length discrepancies
							minLen := minVal(len(joinedNorm), len(cand.Normalized))
							maxLen := maxVal(len(joinedNorm), len(cand.Normalized))
							if maxLen > 0 && float64(minLen)/float64(maxLen) < 0.70 {
								continue
							}

							// Phonetic distance check
							pDist := levenshteinDistance(phoneticQuery, cand.PhoneticKey)
							maxAllowedPDist := 1
							if len(qRunes) >= 6 || len([]rune(cand.PhoneticKey)) >= 6 {
								maxAllowedPDist = 2
							}
							if pDist > maxAllowedPDist {
								continue
							}

							sim := jaroWinkler(joinedNorm, cand.Normalized)
							if sim >= threshold && sim > bestScore {
								bestScore = sim
								bestCanonical = cand.Canonical
								bestK = k
							}
						}
					}
				}
			}
		}

		// Avoid swallowing a context word when a suffix is a better match to
		// the same candidate (e.g. "we postgrsq" -> only "postgrsq").
		if bestK > 1 && bestScore < 1 {
			for skip := 1; skip < bestK; skip++ {
				var suffix strings.Builder
				for j := i + skip; j < i+bestK; j++ {
					suffix.WriteString(tokens[j].Normalized)
				}
				sim := jaroWinkler(suffix.String(), NormalizeString(bestCanonical))
				if sim >= SingleWordThreshold && sim > bestScore {
					bestCanonical = ""
					break
				}
			}
		}
		if bestCanonical != "" && bestK > 0 {
			add(i, i+bestK-1, bestCanonical, bestScore, "phonetic")
			i += bestK
		} else {
			i++
		}
	}

	return choices, nil
}

func absVal(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func jaroWinkler(s1, s2 string) float64 {
	r1 := []rune(s1)
	r2 := []rune(s2)
	l1 := len(r1)
	l2 := len(r2)
	if l1 == 0 && l2 == 0 {
		return 1.0
	}
	if l1 == 0 || l2 == 0 {
		return 0.0
	}
	matchWindow := maxVal(l1, l2)/2 - 1
	if matchWindow < 0 {
		matchWindow = 0
	}
	m1 := make([]bool, l1)
	m2 := make([]bool, l2)
	matches := 0
	for i := 0; i < l1; i++ {
		start := maxVal(0, i-matchWindow)
		end := minVal(l2-1, i+matchWindow)
		for j := start; j <= end; j++ {
			if !m2[j] && r1[i] == r2[j] {
				m1[i] = true
				m2[j] = true
				matches++
				break
			}
		}
	}
	if matches == 0 {
		return 0.0
	}
	t := 0
	k := 0
	for i := 0; i < l1; i++ {
		if m1[i] {
			for !m2[k] {
				k++
			}
			if r1[i] != r2[k] {
				t++
			}
			k++
		}
	}
	transpositions := t / 2
	m := float64(matches)
	jaro := (1.0 / 3.0) * (m/float64(l1) + m/float64(l2) + (m-float64(transpositions))/m)
	prefixLen := 0
	maxPrefix := minVal(4, minVal(l1, l2))
	for i := 0; i < maxPrefix; i++ {
		if r1[i] == r2[i] {
			prefixLen++
		} else {
			break
		}
	}
	p := 0.1
	return jaro + float64(prefixLen)*p*(1.0-jaro)
}

func minVal(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxVal(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	n, m := len(r1), len(r2)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	dp := make([]int, m+1)
	for j := 0; j <= m; j++ {
		dp[j] = j
	}

	for i := 1; i <= n; i++ {
		prev := dp[0]
		dp[0] = i
		for j := 1; j <= m; j++ {
			temp := dp[j]
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			dp[j] = minVal(dp[j]+1, minVal(dp[j-1]+1, prev+cost))
			prev = temp
		}
	}
	return dp[m]
}
