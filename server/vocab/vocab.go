package vocab

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"speektome-server/db"
	"speektome-server/valkey"
)

// Match distance thresholds
const (
	SingleWordThreshold = 0.88
	TwoWordThreshold    = 0.84
	ThreeWordThreshold  = 0.80
	MultiWordThreshold  = 0.78
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
}

// UserVocabulary holds precompiled index structures for fast matching
type UserVocabulary struct {
	NormalizedLookup map[string]string
	TermsByCount     map[int][]CompiledTerm
	TermsByLength    map[int][]CompiledTerm
	TermsByPhonetic  map[string][]CompiledTerm
	Terms            []CompiledTerm
	MaxTokenCount    int
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

// ProcessText normalizes raw text using the user's custom vocabulary and logs matching benchmark stats.
func (vc *VocabCache) ProcessText(userID int64, text string, dbTerms []db.VocabularyTerm) (string, int, time.Duration) {
	if strings.TrimSpace(text) == "" || len(dbTerms) == 0 {
		return text, 0, 0
	}

	vocab := vc.GetOrCompile(userID, dbTerms)
	if vocab == nil || len(vocab.Terms) == 0 {
		return text, 0, 0
	}

	// Benchmark ONLY the actual matching step
	matchStart := time.Now()
	resultText, replacementCount, tokenCount := MatchAndReplace(text, vocab)
	matchDuration := time.Since(matchStart)

	log.Printf("[VOCAB BENCHMARK] User ID: %d | Match Time: %v (%d µs) | Input Words: %d | Total Vocab Terms: %d | Replacements: %d",
		userID, matchDuration, matchDuration.Microseconds(), tokenCount, len(vocab.Terms), replacementCount)

	return resultText, replacementCount, matchDuration
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

	isWordChar := func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '+' || r == '#'
	}
	isSpace := func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}

	i := 0
	for i < n {
		for i < n && isSpace(runes[i]) {
			i++
		}
		if i >= n {
			break
		}

		start := i
		for i < n && !isSpace(runes[i]) {
			i++
		}
		end := i

		firstWordIdx := -1
		lastWordIdx := -1
		for j := start; j < end; j++ {
			if isWordChar(runes[j]) {
				if firstWordIdx == -1 {
					firstWordIdx = j
				}
				lastWordIdx = j
			}
		}

		var tok Token
		if firstWordIdx == -1 {
			tok = Token{
				Original:   string(runes[start:end]),
				Prefix:     "",
				Clean:      "",
				Suffix:     string(runes[start:end]),
				Normalized: "",
			}
		} else {
			tok = Token{
				Original:   string(runes[start:end]),
				Prefix:     string(runes[start:firstWordIdx]),
				Clean:      string(runes[firstWordIdx : lastWordIdx+1]),
				Suffix:     string(runes[lastWordIdx+1 : end]),
				Normalized: NormalizeString(string(runes[firstWordIdx : lastWordIdx+1])),
			}
		}
		tokens = append(tokens, tok)
	}
	return tokens
}

// ExtractSubTokens splits terms on symbols and CamelCase boundaries
func ExtractSubTokens(term string) []string {
	var subTokens []string
	var current strings.Builder

	runes := []rune(term)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]
		if r == '.' || r == '-' || r == '_' || r == '/' || r == '@' || r == ':' || r == ';' {
			if current.Len() > 0 {
				subTokens = append(subTokens, NormalizeString(current.String()))
				current.Reset()
			}
			continue
		}

		if i > 0 && r >= 'A' && r <= 'Z' && runes[i-1] >= 'a' && runes[i-1] <= 'z' {
			if current.Len() > 0 {
				subTokens = append(subTokens, NormalizeString(current.String()))
				current.Reset()
			}
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		subTokens = append(subTokens, NormalizeString(current.String()))
	}

	var cleaned []string
	for _, st := range subTokens {
		if st != "" {
			cleaned = append(cleaned, st)
		}
	}
	return cleaned
}

// doubleMetaphone computes a simplified phonetic representation for fuzzy matching
func doubleMetaphone(s string) string {
	s = strings.ToLower(s)
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return ""
	}

	var sb strings.Builder

	isVowel := func(r rune) bool {
		return r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u' || r == 'y'
	}

	for i := 0; i < n; i++ {
		r := runes[i]
		if i == 0 && isVowel(r) {
			sb.WriteRune('A')
			continue
		}

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
			} else {
				sb.WriteRune('K')
			}
			if i+1 < n && runes[i+1] == 'g' {
				i++
			}
		case 'h':
			if i > 0 && !isVowel(runes[i-1]) {
				// silent h after consonant
			} else if i+1 < n && isVowel(runes[i+1]) {
				sb.WriteRune('H')
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

// CompileUserVocabulary compiles database terms into lookup maps
func CompileUserVocabulary(dbTerms []db.VocabularyTerm) *UserVocabulary {
	normalizedLookup := make(map[string]string)
	compiledTerms := make([]CompiledTerm, 0, len(dbTerms))
	termsByCount := make(map[int][]CompiledTerm)
	termsByLength := make(map[int][]CompiledTerm)
	termsByPhonetic := make(map[string][]CompiledTerm)

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

		normalizedLookup[computedNormalized] = vt.Canonical
		if joinedSubTokens != "" && joinedSubTokens != computedNormalized {
			normalizedLookup[joinedSubTokens] = vt.Canonical
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

		ct := CompiledTerm{
			Canonical:     vt.Canonical,
			Normalized:    computedNormalized,
			TokenCount:    len(normalizedTokens),
			MinTokenCount: minCount,
			MaxTokenCount: maxCount,
			SubTokens:     subTokens,
			PhoneticKey:   phoneticKey,
		}
		compiledTerms = append(compiledTerms, ct)

		countsToRegister := map[int]bool{
			len(normalizedTokens): true,
		}
		if len(subTokens) > 0 {
			countsToRegister[len(subTokens)] = true
		}
		if maxCount > len(normalizedTokens) {
			countsToRegister[maxCount] = true
		}

		for c := range countsToRegister {
			termsByCount[c] = append(termsByCount[c], ct)
		}

		termLen := len(computedNormalized)
		termsByLength[termLen] = append(termsByLength[termLen], ct)
		if len(joinedSubTokens) > 0 && len(joinedSubTokens) != termLen {
			termsByLength[len(joinedSubTokens)] = append(termsByLength[len(joinedSubTokens)], ct)
		}

		// Support symbol expansions (e.g. C# -> c sharp, C++ -> c plus plus)
		if strings.Contains(computedNormalized, "#") {
			sharpNorm := strings.ReplaceAll(computedNormalized, "#", "sharp")
			normalizedLookup[sharpNorm] = vt.Canonical
			sharpCt := CompiledTerm{
				Canonical:     vt.Canonical,
				Normalized:    sharpNorm,
				TokenCount:    2,
				MinTokenCount: 1,
				MaxTokenCount: 2,
				SubTokens:     []string{"c", "sharp"},
				PhoneticKey:   doubleMetaphone(sharpNorm),
			}
			compiledTerms = append(compiledTerms, sharpCt)
			termsByCount[2] = append(termsByCount[2], sharpCt)
			termsByLength[len(sharpNorm)] = append(termsByLength[len(sharpNorm)], sharpCt)
		}
		if strings.Contains(computedNormalized, "++") {
			plusNorm := strings.ReplaceAll(computedNormalized, "++", "plusplus")
			normalizedLookup[plusNorm] = vt.Canonical
			plusCt := CompiledTerm{
				Canonical:     vt.Canonical,
				Normalized:    plusNorm,
				TokenCount:    3,
				MinTokenCount: 1,
				MaxTokenCount: 3,
				SubTokens:     []string{"c", "plus", "plus"},
				PhoneticKey:   doubleMetaphone(plusNorm),
			}
			compiledTerms = append(compiledTerms, plusCt)
			termsByCount[3] = append(termsByCount[3], plusCt)
			termsByLength[len(plusNorm)] = append(termsByLength[len(plusNorm)], plusCt)
		}

		// Smart sub-word auto-expansion for multi-word entries ONLY
		if len(normalizedTokens) > 1 {
			var candidateWords []string
			for _, tok := range tokens {
				if tok.Clean != "" {
					candidateWords = append(candidateWords, tok.Clean)
				}
			}

			for _, word := range candidateWords {
				if isSafeAutoExpandSubWord(word) {
					subNorm := NormalizeString(word)
					if subNorm != "" {
						if _, exists := normalizedLookup[subNorm]; !exists {
							runes := []rune(word)
							if len(runes) > 0 && runes[0] >= 'a' && runes[0] <= 'z' {
								runes[0] = runes[0] - 'a' + 'A'
							}
							subCanonical := string(runes)
							normalizedLookup[subNorm] = subCanonical

							subPhonetic := doubleMetaphone(subNorm)
							subCt := CompiledTerm{
								Canonical:     subCanonical,
								Normalized:    subNorm,
								TokenCount:    1,
								MinTokenCount: 1,
								MaxTokenCount: 1,
								SubTokens:     []string{subNorm},
								PhoneticKey:   subPhonetic,
							}
							compiledTerms = append(compiledTerms, subCt)
							termsByCount[1] = append(termsByCount[1], subCt)
							termsByLength[len(subNorm)] = append(termsByLength[len(subNorm)], subCt)
							if subPhonetic != "" {
								termsByPhonetic[subPhonetic] = append(termsByPhonetic[subPhonetic], subCt)
							}
						}
					}
				}
			}
		}
	}

	maxTokenCount := 0
	for _, term := range compiledTerms {
		if term.MaxTokenCount > maxTokenCount {
			maxTokenCount = term.MaxTokenCount
		}
	}

	return &UserVocabulary{
		NormalizedLookup: normalizedLookup,
		TermsByCount:     termsByCount,
		TermsByLength:    termsByLength,
		TermsByPhonetic:  termsByPhonetic,
		Terms:            compiledTerms,
		MaxTokenCount:    maxTokenCount,
	}
}

// MatchAndReplace executes greedy longest-match lookup and fuzzy phonetic replacement
func MatchAndReplace(text string, vocab *UserVocabulary) (string, int, int) {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return text, 0, 0
	}

	n := len(tokens)
	var resultTokens []Token
	maxK := vocab.MaxTokenCount
	if maxK <= 0 {
		maxK = 1
	}

	replacements := 0
	i := 0

	for i < n {
		matched := false

		// Stage 1: Exact and Normalized N-Gram Lookup
		for k := maxK; k >= 1; k-- {
			if i+k <= n {
				var sb strings.Builder
				for j := 0; j < k; j++ {
					sb.WriteString(tokens[i+j].Normalized)
				}
				joinedNorm := sb.String()

				if canonical, ok := vocab.NormalizedLookup[joinedNorm]; ok {
					leadTok := tokens[i]
					trailTok := tokens[i+k-1]

					resultTokens = append(resultTokens, Token{
						Original:   canonical,
						Prefix:     leadTok.Prefix,
						Clean:      canonical,
						Suffix:     trailTok.Suffix,
						Normalized: joinedNorm,
					})
					i += k
					matched = true
					replacements++
					break
				}
			}
		}

		if matched {
			continue
		}

		// Stage 2: Multi-stage Fuzzy / Phonetic matching
		var bestCanonical string
		var bestScore float64
		var bestK int

		for k := maxK; k >= 1; k-- {
			if i+k <= n {
				var sb strings.Builder
				for j := 0; j < k; j++ {
					sb.WriteString(tokens[i+j].Normalized)
				}
				joinedNorm := sb.String()
				if joinedNorm == "" {
					continue
				}

				threshold := MultiWordThreshold
				if k == 1 {
					threshold = SingleWordThreshold
				} else if k == 2 {
					threshold = TwoWordThreshold
				} else if k == 3 {
					threshold = ThreeWordThreshold
				}

				// Check terms with token count k
				if candidates, ok := vocab.TermsByCount[k]; ok {
					for _, term := range candidates {
						if len(joinedNorm) <= 2 && joinedNorm != term.Normalized {
							continue
						}
						lenDiff := absVal(len(joinedNorm) - len(term.Normalized))
						matchPrefix := false
						if len(joinedNorm) >= 2 && len(term.Normalized) >= 2 {
							matchPrefix = joinedNorm[:2] == term.Normalized[:2]
						} else if len(joinedNorm) >= 1 && len(term.Normalized) >= 1 {
							matchPrefix = joinedNorm[:1] == term.Normalized[:1]
						}

						subTokenMatch := true
						if k >= 2 {
							if len(term.SubTokens) == k {
								for j := 0; j < k; j++ {
									tokSim := jaroWinkler(tokens[i+j].Normalized, term.SubTokens[j])
									minTokSim := 0.70
									if len(term.SubTokens[j]) <= 2 {
										minTokSim = 0.88
									}
									if tokSim < minTokSim {
										subTokenMatch = false
										break
									}
								}
							} else {
								subTokenMatch = false
							}
						} else {
							subTokenMatch = (term.TokenCount == 1)
						}

						if lenDiff <= 2*k && matchPrefix && subTokenMatch {
							sim := jaroWinkler(joinedNorm, term.Normalized)
							if sim >= threshold && sim > bestScore {
								bestScore = sim
								bestCanonical = term.Canonical
								bestK = k
							}
						}
					}
				}

				// Check Phonetic Pool
				if bestCanonical == "" {
					windowPhonetic := doubleMetaphone(joinedNorm)
					if windowPhonetic != "" {
						if candidates, ok := vocab.TermsByPhonetic[windowPhonetic]; ok {
							for _, term := range candidates {
								if k > term.MaxTokenCount {
									continue
								}
								subTokenMatch := true
								if k >= 2 {
									if len(term.SubTokens) == k {
										for j := 0; j < k; j++ {
											tokSim := jaroWinkler(tokens[i+j].Normalized, term.SubTokens[j])
											if tokSim < 0.70 {
												subTokenMatch = false
												break
											}
										}
									} else {
										subTokenMatch = false
									}
								} else {
									subTokenMatch = (term.TokenCount == 1)
								}

								matchPrefix := false
								if len(joinedNorm) >= 1 && len(term.Normalized) >= 1 {
									matchPrefix = joinedNorm[:1] == term.Normalized[:1]
								}

								if matchPrefix && subTokenMatch {
									sim := jaroWinkler(joinedNorm, term.Normalized)
									if sim >= threshold-0.05 && sim > bestScore {
										bestScore = sim
										bestCanonical = term.Canonical
										bestK = k
									}
								}
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
			i += bestK
			replacements++
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

	return out.String(), replacements, n
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
