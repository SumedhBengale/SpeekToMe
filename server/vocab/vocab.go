package vocab

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"speektome-server/db"
	"speektome-server/valkey"
)

// Match distance thresholds
const (
	SingleWordThreshold = 0.82
	TwoWordThreshold    = 0.78
	ThreeWordThreshold  = 0.75
	MultiWordThreshold  = 0.72
)

// Token wraps individual words and split punctuation bounds
type Token struct {
	Original   string
	Prefix     string
	Clean      string
	Suffix     string
	Normalized string
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

		// If normalized single-string differs, also insert as single token
		if len(tokens) > 1 && term.Normalized != "" {
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

		// 2. Phonetic prefix keys (length 2 and 3)
		runes := []rune(term.PhoneticKey)
		if len(runes) >= 2 {
			p2 := string(runes[:2])
			buckets[acousticClass][p2] = append(buckets[acousticClass][p2], term)
		}
		if len(runes) >= 3 {
			p3 := string(runes[:3])
			buckets[acousticClass][p3] = append(buckets[acousticClass][p3], term)
		}

		// 3. Symmetric Delete variants (edit distance 1 deletions)
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
	Trie            *AhoCorasickTrie
	PhoneticSym     *PhoneticSymSpellBucket
	NormalizedMap   map[string]string
	Terms           []CompiledTerm
	MaxTokenCount   int
}

// VocabCache provides thread-safe caching of compiled user vocabularies
type VocabCache struct {
	mu    sync.RWMutex
	items map[int64]*UserVocabulary
}

func NewVocabCache() *VocabCache {
	return &VocabCache{
		items: make(map[int64]*UserVocabulary),
	}
}

func (vc *VocabCache) Invalidate(userID int64) {
	vc.mu.Lock()
	delete(vc.items, userID)
	vc.mu.Unlock()

	if valkey.GlobalClient != nil && valkey.GlobalClient.IsConnected() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = valkey.GlobalClient.InvalidateCachedVocabulary(ctx, userID)
		_ = valkey.GlobalClient.PublishVocabInvalidate(ctx, userID)
	}
}

func (vc *VocabCache) Clear() {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.items = make(map[int64]*UserVocabulary)
}

func (vc *VocabCache) GetOrCompile(userID int64, dbTerms []db.VocabularyTerm) *UserVocabulary {
	vc.mu.RLock()
	cached, ok := vc.items[userID]
	vc.mu.RUnlock()
	if ok && cached != nil {
		return cached
	}

	compiled := CompileUserVocabulary(dbTerms)

	vc.mu.Lock()
	vc.items[userID] = compiled
	vc.mu.Unlock()

	return compiled
}

// ProcessText normalizes raw text using the user's custom vocabulary, logs matching benchmark stats,
// and returns the transformed text, a slice of replaced canonical terms, and matching duration.
func (vc *VocabCache) ProcessText(userID int64, text string, dbTerms []db.VocabularyTerm) (string, []string, time.Duration) {
	if strings.TrimSpace(text) == "" || len(dbTerms) == 0 {
		return text, nil, 0
	}

	vocab := vc.GetOrCompile(userID, dbTerms)
	if vocab == nil || len(vocab.Terms) == 0 {
		return text, nil, 0
	}

	// Benchmark ONLY the actual matching step
	matchStart := time.Now()
	resultText, replacedTerms, tokenCount := MatchAndReplace(text, vocab)
	matchDuration := time.Since(matchStart)

	log.Printf("[VOCAB BENCHMARK] User ID: %d | Match Time: %v (%d µs) | Input Words: %d (Full) | Total Vocab Terms: %d | Replacements: %d",
		userID, matchDuration, matchDuration.Microseconds(), tokenCount, len(vocab.Terms), len(replacedTerms))

	return resultText, replacedTerms, matchDuration
}

// ProcessStreamingText runs sliding-window vocabulary matching on streaming transcripts.
// It slices only the trailing window of words (e.g. last 12 words) for tokenization and fuzzy matching,
// preserving the already-processed stable prefix with zero re-computation.
func (vc *VocabCache) ProcessStreamingText(userID int64, text string, dbTerms []db.VocabularyTerm) (string, []string, time.Duration) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(dbTerms) == 0 {
		return text, nil, 0
	}

	vocab := vc.GetOrCompile(userID, dbTerms)
	if vocab == nil || len(vocab.Terms) == 0 {
		return text, nil, 0
	}

	windowWords := 12
	if vocab.MaxTokenCount+4 > windowWords {
		windowWords = vocab.MaxTokenCount + 4
	}

	// Find the start of the trailing window by scanning backwards for word boundaries
	runes := []rune(text)
	n := len(runes)
	wordCount := 0
	splitIdx := 0

	inWord := false
	for i := n - 1; i >= 0; i-- {
		r := runes[i]
		isSpace := r == ' ' || r == '\t' || r == '\n' || r == '\r'
		if !isSpace && !inWord {
			inWord = true
			wordCount++
			if wordCount > windowWords {
				splitIdx = i + 1
				break
			}
		} else if isSpace {
			inWord = false
		}
	}

	if splitIdx <= 0 {
		// Entire text is within the window (short text <= 12 words)
		matchStart := time.Now()
		resultText, replacedTerms, tokenCount := MatchAndReplace(text, vocab)
		matchDuration := time.Since(matchStart)

		log.Printf("[VOCAB BENCHMARK] User ID: %d | Match Time: %v (%d µs) | Input Words: %d (Window Tail) | Total Vocab Terms: %d | Replacements: %d",
			userID, matchDuration, matchDuration.Microseconds(), tokenCount, len(vocab.Terms), len(replacedTerms))

		return resultText, replacedTerms, matchDuration
	}

	prefix := string(runes[:splitIdx])
	tail := string(runes[splitIdx:])

	matchStart := time.Now()
	matchedTail, replacedTerms, tailTokens := MatchAndReplace(tail, vocab)
	matchDuration := time.Since(matchStart)

	log.Printf("[VOCAB BENCHMARK] User ID: %d | Match Time: %v (%d µs) | Input Words: %d (Window Tail) | Total Vocab Terms: %d | Replacements: %d",
		userID, matchDuration, matchDuration.Microseconds(), tailTokens, len(vocab.Terms), len(replacedTerms))

	return prefix + matchedTail, replacedTerms, matchDuration
}

// NormalizeString converts string to lowercase, keeps alphanumeric and specific chars (+, #)
func NormalizeString(s string) string {
	s = strings.ToLower(s)
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '#' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// Tokenize splits a string into word tokens in a single O(N) pass,
// extracting prefix, clean word, and suffix boundaries.
func Tokenize(text string) []Token {
	var tokens []Token
	runes := []rune(text)
	n := len(runes)
	i := 0

	for i < n {
		// Skip whitespace
		for i < n && (runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n' || runes[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}

		start := i
		for i < n && !(runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n' || runes[i] == '\r') {
			i++
		}
		end := i

		rawWord := string(runes[start:end])
		tok := parseWordBoundaries(rawWord)
		tokens = append(tokens, tok)
	}

	return tokens
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
	return (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126)
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
func CompileUserVocabulary(dbTerms []db.VocabularyTerm) *UserVocabulary {
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

		// Register distinctive sub-tokens (e.g. "Postgre" for "PostgreSQL")
		for _, st := range subTokens {
			if len(st) >= 3 && isSafeAutoExpandSubWord(st) {
				stNorm := NormalizeString(st)
				if stNorm != "" && stNorm != computedNormalized {
					stPhonetic := doubleMetaphone(stNorm)
					stCt := CompiledTerm{
						Canonical:     vt.Canonical,
						Normalized:    stNorm,
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
func MatchAndReplace(text string, vocab *UserVocabulary) (string, []string, int) {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return text, nil, 0
	}

	n := len(tokens)
	replacedMap := make(map[string]bool)

	// Step 1: Phase 1 Aho-Corasick Exact Multi-Word Matching in O(L)
	trieMatches := vocab.Trie.FindMatches(tokens)
	matchedSpans := make(map[int]TrieMatchResult) // Key: StartIdx -> Result
	coveredIndices := make(map[int]bool)

	for _, m := range trieMatches {
		matchedSpans[m.StartIdx] = m
		for k := m.StartIdx; k <= m.EndIdx; k++ {
			coveredIndices[k] = true
		}
		replacedMap[m.Canonical] = true
	}

	// Step 2: Phase 2 Phonetic SymSpell on uncovered tokens in trailing window
	windowWords := 12
	if vocab.MaxTokenCount+4 > windowWords {
		windowWords = vocab.MaxTokenCount + 4
	}
	prefixThreshold := n - windowWords
	if prefixThreshold < 0 {
		prefixThreshold = 0
	}

	var resultTokens []Token
	i := 0

	for i < n {
		// If index is covered by exact Trie match
		if m, exists := matchedSpans[i]; exists {
			leadTok := tokens[m.StartIdx]
			trailTok := tokens[m.EndIdx]
			resultTokens = append(resultTokens, Token{
				Original:   m.Canonical,
				Prefix:     leadTok.Prefix,
				Clean:      m.Canonical,
				Suffix:     trailTok.Suffix,
				Normalized: m.Canonical,
			})
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
			leadTok := tokens[i]
			resultTokens = append(resultTokens, Token{
				Original:   canonical,
				Prefix:     leadTok.Prefix,
				Clean:      canonical,
				Suffix:     leadTok.Suffix,
				Normalized: canonical,
			})
			replacedMap[canonical] = true
			i++
			continue
		}

		// If in stable prefix zone, skip heavy fuzzy checking
		if i < prefixThreshold {
			resultTokens = append(resultTokens, tokens[i])
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

						candidates := bucket[phoneticQuery]
						qRunes := []rune(phoneticQuery)
						if len(qRunes) >= 2 {
							p2 := string(qRunes[:2])
							if pMatches, ok := bucket[p2]; ok {
								candidates = append(candidates, pMatches...)
							}
						}
						if len(qRunes) >= 3 {
							p3 := string(qRunes[:3])
							if pMatches, ok := bucket[p3]; ok {
								candidates = append(candidates, pMatches...)
							}
						}
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

		if bestCanonical != "" && bestK > 0 {
			leadTok := tokens[i]
			trailTok := tokens[i+bestK-1]
			resultTokens = append(resultTokens, Token{
				Original:   bestCanonical,
				Prefix:     leadTok.Prefix,
				Clean:      bestCanonical,
				Suffix:     trailTok.Suffix,
				Normalized: bestCanonical,
			})
			replacedMap[bestCanonical] = true
			i += bestK
		} else {
			resultTokens = append(resultTokens, tokens[i])
			i++
		}
	}

	// Reconstruct formatted string
	var out strings.Builder
	for idx, tok := range resultTokens {
		if idx > 0 {
			out.WriteString(" ")
		}
		out.WriteString(tok.Prefix)
		out.WriteString(tok.Clean)
		out.WriteString(tok.Suffix)
	}

	var replacedList []string
	for term := range replacedMap {
		replacedList = append(replacedList, term)
	}
	sort.Strings(replacedList)

	return out.String(), replacedList, n
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
