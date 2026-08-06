package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"speektome-server/config"
	"speektome-server/db"
)

const (
	SingleWordThreshold = 0.88
	TwoWordThreshold    = 0.84
	ThreeWordThreshold  = 0.80
	MultiWordThreshold  = 0.78
)

type MatchMethod string

const (
	ExactMatch      MatchMethod = "exact"
	NormalizedMatch MatchMethod = "normalized"
	FuzzyMatch      MatchMethod = "fuzzy"
)

type Match struct {
	Original   string
	Canonical  string
	Method     MatchMethod
	Similarity float64
	Position   int
}

// PipelineContext tracks metadata and state through the transforms
type PipelineContext struct {
	UserID      int64
	Vocabulary  *UserVocabulary
	Matches     []Match
	Metrics     struct {
		ExactMatches      int
		NormalizedMatches int
		FuzzyMatches      int
	}
	Debug       bool
	LLMProvider string // Tracks which LLM provider was used
	LLMRouting  string // Tracks the routing/location of the LLM request
}

func NewPipelineContext(userID int64, vocab *UserVocabulary) *PipelineContext {
	return &PipelineContext{
		UserID:     userID,
		Vocabulary: vocab,
	}
}

// TextTransform defines a single step in our normalization pipeline
type TextTransform interface {
	Name() string
	Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error)
}

// CompiledTerm holds pre-tokenized target terms for greedy n-gram lookups
type CompiledTerm struct {
	Canonical     string
	Normalized    string
	TokenCount    int
	MinTokenCount int
	MaxTokenCount int
	SubTokens     []string
	PhoneticKey   string
}

// UserVocabulary contains pre-compiled lookup mappings
type UserVocabulary struct {
	NormalizedLookup map[string]string         // normalized exact -> canonical
	TermsByCount     map[int][]CompiledTerm    // group by TokenCount (including expanded sub-tokens)
	TermsByLength    map[int][]CompiledTerm    // group by normalized character length
	TermsByPhonetic  map[string][]CompiledTerm // group by Double Metaphone phonetic key
	Terms            []CompiledTerm            // sorted by TokenCount descending
	MaxTokenCount    int
}

// CachedVocabulary wraps compiled user vocabulary with metadata
type CachedVocabulary struct {
	Data       *UserVocabulary
	LastAccess int64 // atomic Unix timestamp
}

// VocabularyCache caches compiled user vocabularies using RWMutex
type VocabularyCache struct {
	mu     sync.RWMutex
	data   map[int64]*CachedVocabulary
	stopCh chan struct{}
}

func NewVocabularyCache() *VocabularyCache {
	vc := &VocabularyCache{
		data:   make(map[int64]*CachedVocabulary),
		stopCh: make(chan struct{}),
	}
	go vc.startEvictionWorker(10 * time.Minute)
	return vc
}

func (vc *VocabularyCache) Close() {
	close(vc.stopCh)
}

func (vc *VocabularyCache) Get(userID int64) *UserVocabulary {
	vc.mu.RLock()
	cached, ok := vc.data[userID]
	if ok {
		atomic.StoreInt64(&cached.LastAccess, time.Now().Unix())
		vc.mu.RUnlock()
		return cached.Data
	}
	vc.mu.RUnlock()
	return nil
}

func (vc *VocabularyCache) Set(userID int64, vocab *UserVocabulary) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.data[userID] = &CachedVocabulary{
		Data:       vocab,
		LastAccess: time.Now().Unix(),
	}
}

func (vc *VocabularyCache) Invalidate(userID int64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	delete(vc.data, userID)
}

func (vc *VocabularyCache) GetKeys() []int64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	keys := make([]int64, 0, len(vc.data))
	for k := range vc.data {
		keys = append(keys, k)
	}
	return keys
}

func (vc *VocabularyCache) InvalidateAll() {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.data = make(map[int64]*CachedVocabulary)
}

func (vc *VocabularyCache) startEvictionWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			vc.mu.Lock()
			now := time.Now().Unix()
			for id, cached := range vc.data {
				lastAccess := atomic.LoadInt64(&cached.LastAccess)
				if now-lastAccess > 3600 {
					delete(vc.data, id)
				}
			}
			vc.mu.Unlock()
		case <-vc.stopCh:
			return
		}
	}
}

// ExtractSubTokens splits terms on punctuation and CamelCase / PascalCase transitions
func ExtractSubTokens(s string) []string {
	var subTokens []string
	var current strings.Builder

	runes := []rune(s)
	n := len(runes)

	isSymbol := func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '/' || r == '@' || r == ':' || r == ';'
	}

	for i := 0; i < n; i++ {
		r := runes[i]
		if isSymbol(r) || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				subTokens = append(subTokens, current.String())
				current.Reset()
			}
			continue
		}

		if i > 0 && current.Len() > 0 {
			prev := runes[i-1]
			// Check lowercase -> uppercase (e.g. 'k' -> 'T' in SpeekTo)
			if (prev >= 'a' && prev <= 'z') && (r >= 'A' && r <= 'Z') {
				subTokens = append(subTokens, current.String())
				current.Reset()
			} else if (prev >= 'A' && prev <= 'Z') && (r >= 'A' && r <= 'Z') && i+1 < n && (runes[i+1] >= 'a' && runes[i+1] <= 'z') {
				// Check acronym -> word transition (e.g. 'CMS' -> 'Enterprise')
				subTokens = append(subTokens, current.String())
				current.Reset()
			}
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		subTokens = append(subTokens, current.String())
	}

	var result []string
	for _, st := range subTokens {
		norm := NormalizeString(st)
		if norm != "" {
			result = append(result, norm)
		}
	}
	return result
}

// doubleMetaphone computes a simplified phonetic representation of a normalized string
func doubleMetaphone(s string) string {
	s = NormalizeString(s)
	if s == "" {
		return ""
	}

	var sb strings.Builder
	runes := []rune(s)
	n := len(runes)

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
			if i+1 < n && (runes[i+1] == 'c' || runes[i+1] == 'k') {
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
			} else {
				sb.WriteRune('K')
			}
			if i+1 < n && (runes[i+1] == 'g' || runes[i+1] == 'h') {
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

// isSafeAutoExpandSubWord checks if an extracted sub-word is safe to auto-expand into an independent single-word vocabulary entry
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
	if connectors[norm] {
		return false
	}

	return true
}

// CompileUserVocabulary compiles database terms into lookup maps
func CompileUserVocabulary(dbTerms []db.VocabularyTerm) *UserVocabulary {
	normalizedLookup := make(map[string]string)
	compiledTerms := make([]CompiledTerm, 0, len(dbTerms))
	termsByCount := make(map[int][]CompiledTerm)
	termsByLength := make(map[int][]CompiledTerm)
	termsByPhonetic := make(map[string][]CompiledTerm)

	for _, vt := range dbTerms {
		tokens := Tokenize(vt.Canonical)
		normalizedTokens := make([]string, len(tokens))
		for i, tok := range tokens {
			normalizedTokens[i] = tok.Normalized
		}
		computedNormalized := strings.Join(normalizedTokens, "")

		// Sub-token decomposition for CamelCase and symbols
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

		// Register term in termsByCount ONLY for exact valid token counts (space tokens & sub-tokens)
		countsToRegister := map[int]bool{
			len(normalizedTokens): true,
		}
		if len(subTokens) > 0 {
			countsToRegister[len(subTokens)] = true
		}

		for c := range countsToRegister {
			termsByCount[c] = append(termsByCount[c], ct)
		}

		// Register term by normalized length
		termLen := len(computedNormalized)
		termsByLength[termLen] = append(termsByLength[termLen], ct)
		if len(joinedSubTokens) > 0 && len(joinedSubTokens) != termLen {
			termsByLength[len(joinedSubTokens)] = append(termsByLength[len(joinedSubTokens)], ct)
		}

		// Register term by phonetic key
		if phoneticKey != "" {
			termsByPhonetic[phoneticKey] = append(termsByPhonetic[phoneticKey], ct)
		}

		// Approach 3: Smart Sub-Word Auto-Expansion for Multi-Word Entries (space-separated)
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

// VocabNormalizerTransform replaces raw speech terms with vocabulary entries
type VocabNormalizerTransform struct {
	name           string
	fuzzyEnabled   bool
	fuzzyThreshold float64
}

func NewVocabNormalizerTransform(name string, fuzzy bool, threshold float64) *VocabNormalizerTransform {
	return &VocabNormalizerTransform{
		name:           name,
		fuzzyEnabled:   fuzzy,
		fuzzyThreshold: threshold,
	}
}

func (t *VocabNormalizerTransform) Name() string {
	return t.name
}

func (t *VocabNormalizerTransform) Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error) {
	if pctx.Vocabulary == nil || len(pctx.Vocabulary.Terms) == 0 {
		return text, nil
	}

	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return text, nil
	}

	n := len(tokens)
	var resultTokens []Token
	
	maxK := pctx.Vocabulary.MaxTokenCount

	if pctx.Debug {
		fmt.Printf("\n--- [%s] Matching Engine Diagnostic Log ---\n", t.name)
		fmt.Printf("Input text: %q\n", text)
		fmt.Printf("Vocabulary size: %d terms, lookahead horizon: %d words\n", len(pctx.Vocabulary.Terms), maxK)
	}

	i := 0
	for i < n {
		matched := false
		if pctx.Debug {
			fmt.Printf("  * Token at index %d: Original=%q, Clean=%q, Normalized=%q\n", i, tokens[i].Original, tokens[i].Clean, tokens[i].Normalized)
		}
		
		for k := maxK; k >= 1; k-- {
			if i+k <= n {
				var sb strings.Builder
				var cleanParts []string
				for j := 0; j < k; j++ {
					sb.WriteString(tokens[i+j].Normalized)
					cleanParts = append(cleanParts, tokens[i+j].Clean)
				}
				joinedNorm := sb.String()
				joinedClean := strings.Join(cleanParts, " ")
				
				if canonical, exists := pctx.Vocabulary.NormalizedLookup[joinedNorm]; exists {
					if pctx.Debug {
						fmt.Printf("    -> HIT Exact/Normalized! Joined %q (norm: %q) matches canonical %q\n", joinedClean, joinedNorm, canonical)
					}
					
					method := ExactMatch
					if joinedClean != canonical {
						method = NormalizedMatch
					}
					
					if method == ExactMatch {
						pctx.Metrics.ExactMatches++
					} else {
						pctx.Metrics.NormalizedMatches++
					}
					
					pctx.Matches = append(pctx.Matches, Match{
						Original:   joinedClean,
						Canonical:  canonical,
						Method:     method,
						Similarity: 1.0,
						Position:   i,
					})
					
					replacedToken := Token{
						Prefix:     tokens[i].Prefix,
						Clean:      canonical,
						Suffix:     tokens[i+k-1].Suffix,
						Normalized: joinedNorm,
					}
					replacedToken.Original = replacedToken.Prefix + replacedToken.Clean + replacedToken.Suffix
					
					resultTokens = append(resultTokens, replacedToken)
					i += k
					matched = true
					break
				}
			}
		}
		
		if matched {
			continue
		}
		
		if t.fuzzyEnabled {
			bestSim := 0.0
			var bestCanonical string
			bestK := 0

			for k := maxK; k >= 1; k-- {
				if i+k <= n {
					var sb strings.Builder
					var cleanParts []string
					for j := 0; j < k; j++ {
						sb.WriteString(tokens[i+j].Normalized)
						cleanParts = append(cleanParts, tokens[i+j].Clean)
					}
					joinedNorm := sb.String()
					joinedClean := strings.Join(cleanParts, " ")
					if joinedNorm == "" {
						continue
					}

					threshold := SingleWordThreshold
					if k == 2 {
						threshold = TwoWordThreshold
					} else if k == 3 {
						threshold = ThreeWordThreshold
					} else if k >= 4 {
						threshold = MultiWordThreshold
					}

					candidates := pctx.Vocabulary.TermsByCount[k]
					for _, term := range candidates {
						lenDiff := len(joinedNorm) - len(term.Normalized)
						if lenDiff < 0 {
							lenDiff = -lenDiff
						}

						matchPrefix := false
						if len(joinedNorm) >= 2 && len(term.Normalized) >= 2 {
							matchPrefix = joinedNorm[0] == term.Normalized[0] && joinedNorm[1] == term.Normalized[1]
						} else if len(joinedNorm) >= 1 && len(term.Normalized) >= 1 {
							matchPrefix = joinedNorm[0] == term.Normalized[0]
						}

						subTokenMatch := true
						if k >= 2 && len(term.SubTokens) == k {
							for j := 0; j < k; j++ {
								tokSim := jaroWinkler(tokens[i+j].Normalized, term.SubTokens[j])
								minTokSim := 0.65
								if len(term.SubTokens[j]) <= 2 {
									minTokSim = 0.85
								}
								if tokSim < minTokSim {
									subTokenMatch = false
									break
								}
							}
						}

						if lenDiff <= 2*k && matchPrefix && subTokenMatch {
							sim := jaroWinkler(joinedNorm, term.Normalized)
							if sim >= threshold {
								if pctx.Debug {
									fmt.Printf("    - Fuzzy Stage 2 Hit (k=%d): %q vs %q -> JW Sim: %.4f (Passed threshold: %.2f, Diff: %d)\n",
										k, joinedClean, term.Canonical, sim, threshold, lenDiff)
								}
								if sim > bestSim {
									bestSim = sim
									bestCanonical = term.Canonical
									bestK = k
								}
							}
						}
					}

					if bestCanonical == "" && pctx.Vocabulary.TermsByLength != nil {
						normLen := len(joinedNorm)
						for delta := -2; delta <= 2; delta++ {
							targetLen := normLen + delta
							if targetLen <= 0 {
								continue
							}
							for _, term := range pctx.Vocabulary.TermsByLength[targetLen] {
								if k > term.MaxTokenCount {
									continue
								}
								lenDiff := len(joinedNorm) - len(term.Normalized)
								if lenDiff < 0 {
									lenDiff = -lenDiff
								}
								maxLen := len(joinedNorm)
								if len(term.Normalized) > maxLen {
									maxLen = len(term.Normalized)
								}
								lenDiffRatio := float64(lenDiff) / float64(maxLen)

								matchPrefix := false
								if len(joinedNorm) >= 1 && len(term.Normalized) >= 1 {
									matchPrefix = joinedNorm[0] == term.Normalized[0]
								}

								subTokenMatch := true
								if k >= 2 && len(term.SubTokens) == k {
									for j := 0; j < k; j++ {
										tokSim := jaroWinkler(tokens[i+j].Normalized, term.SubTokens[j])
										minTokSim := 0.65
										if len(term.SubTokens[j]) <= 2 {
											minTokSim = 0.85
										}
										if tokSim < minTokSim {
											subTokenMatch = false
											break
										}
									}
								}

								if matchPrefix && lenDiffRatio <= 0.15 && subTokenMatch {
									sim := jaroWinkler(joinedNorm, term.Normalized)
									if sim >= threshold {
										if pctx.Debug {
											fmt.Printf("    - Fuzzy Stage 3 Length Hit (k=%d): %q vs %q -> JW Sim: %.4f\n",
												k, joinedClean, term.Canonical, sim)
										}
										if sim > bestSim {
											bestSim = sim
											bestCanonical = term.Canonical
											bestK = k
										}
									}
								}
							}
						}
					}

					if bestCanonical == "" && pctx.Vocabulary.TermsByPhonetic != nil {
						windowPhonetic := doubleMetaphone(joinedNorm)
						if windowPhonetic != "" {
							if phonCandidates, ok := pctx.Vocabulary.TermsByPhonetic[windowPhonetic]; ok {
								for _, term := range phonCandidates {
									if k > term.MaxTokenCount {
										continue
									}
									lenDiff := len(joinedNorm) - len(term.Normalized)
									if lenDiff < 0 {
										lenDiff = -lenDiff
									}
									maxLen := len(joinedNorm)
									if len(term.Normalized) > maxLen {
										maxLen = len(term.Normalized)
									}
									lenDiffRatio := float64(lenDiff) / float64(maxLen)

									matchPrefix := false
									if len(joinedNorm) >= 1 && len(term.Normalized) >= 1 {
										matchPrefix = joinedNorm[0] == term.Normalized[0]
									}

									subTokenMatch := true
									if k >= 2 && len(term.SubTokens) == k {
										for j := 0; j < k; j++ {
											tokSim := jaroWinkler(tokens[i+j].Normalized, term.SubTokens[j])
											minTokSim := 0.65
											if len(term.SubTokens[j]) <= 2 {
												minTokSim = 0.85
											}
											if tokSim < minTokSim {
												subTokenMatch = false
												break
											}
										}
									}

									if matchPrefix && lenDiffRatio <= 0.15 && subTokenMatch {
										sim := jaroWinkler(joinedNorm, term.Normalized)
										if sim >= 0.75 || sim >= threshold-0.05 {
											if pctx.Debug {
												fmt.Printf("    - Fuzzy Stage 4 Phonetic Hit (k=%d): %q vs %q -> JW Sim: %.4f (Phonetic: %s)\n",
													k, joinedClean, term.Canonical, sim, windowPhonetic)
											}
											if sim > bestSim {
												bestSim = sim
												bestCanonical = term.Canonical
												bestK = k
											}
										}
									}
								}
							}
						}
					}

					if bestCanonical != "" {
						break
					}
				}
			}
			
			if bestCanonical != "" {
				if pctx.Debug {
					fmt.Printf("    -> HIT Fuzzy! Replaced %d words starting at index %d with %q (sim: %.4f)\n", bestK, i, bestCanonical, bestSim)
				}
				
				var originalParts []string
				for j := 0; j < bestK; j++ {
					originalParts = append(originalParts, tokens[i+j].Clean)
				}
				joinedOriginal := strings.Join(originalParts, " ")
				
				pctx.Matches = append(pctx.Matches, Match{
					Original:   joinedOriginal,
					Canonical:  bestCanonical,
					Method:     FuzzyMatch,
					Similarity: bestSim,
					Position:   i,
				})
				
				pctx.Metrics.FuzzyMatches++
				
				replacedToken := Token{
					Prefix:     tokens[i].Prefix,
					Clean:      bestCanonical,
					Suffix:     tokens[i+bestK-1].Suffix,
					Normalized: NormalizeString(bestCanonical),
				}
				replacedToken.Original = replacedToken.Prefix + replacedToken.Clean + replacedToken.Suffix
				
				resultTokens = append(resultTokens, replacedToken)
				i += bestK
				continue
			}
		}
		
		if pctx.Debug {
			fmt.Printf("    -> Keep Original: %q\n", tokens[i].Original)
		}
		resultTokens = append(resultTokens, tokens[i])
		i++
	}

	var sb strings.Builder
	for idx, tok := range resultTokens {
		if idx > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(tok.Original)
	}
	if pctx.Debug {
		fmt.Printf("Engine Output: %q\n---------------------------------------------\n", sb.String())
	}
	return sb.String(), nil
}

var aiHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// AIEnhancementTransform cleans transcripts using LLMs, injecting only matched vocabulary terms
type AIEnhancementTransform struct {
	customPrompt string
	model        string
	apiKey       string
	apiURL       string
	httpClient   *http.Client
}

func NewAIEnhancementTransform(customPrompt, model, apiKey, apiURL string) *AIEnhancementTransform {
	return &AIEnhancementTransform{
		customPrompt: customPrompt,
		model:        model,
		apiKey:       apiKey,
		apiURL:       apiURL,
		httpClient:   aiHTTPClient,
	}
}

func (t *AIEnhancementTransform) Name() string {
	return "AI Enhancement"
}

func callChatCompletionsAPI(ctx context.Context, client *http.Client, apiURL, apiKey, model, systemPrompt, text string, includeThinking bool) (string, string, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
	}
	if includeThinking {
		payload["thinking"] = map[string]string{"type": "disabled"}
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	var remoteIP string
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			if host, _, err := net.SplitHostPort(addr); err == nil {
				remoteIP = host
			} else {
				remoteIP = addr
			}
		},
	}
	ctxTrace := httptrace.WithClientTrace(ctx, trace)

	req, err := http.NewRequestWithContext(ctxTrace, "POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to make HTTP call: %w", err)
	}
	defer resp.Body.Close()

	routingLoc := getHTTPResponseRouting(resp, remoteIP)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("AI call failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", "", fmt.Errorf("AI returned empty choices")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), routingLoc, nil
}

func getHTTPResponseRouting(resp *http.Response, remoteIP string) string {
	if resp == nil {
		return "Unknown Location (No response)"
	}
	if groqReg := resp.Header.Get("x-groq-region"); groqReg != "" {
		return fmt.Sprintf("Groq (%s)", groqReg)
	}
	if cfRay := resp.Header.Get("Cf-Ray"); cfRay != "" {
		parts := strings.Split(cfRay, "-")
		if len(parts) > 1 {
			return fmt.Sprintf("Cloudflare (%s)", parts[len(parts)-1])
		}
		return fmt.Sprintf("Cloudflare (%s)", cfRay)
	}
	if server := resp.Header.Get("Server"); server != "" {
		return fmt.Sprintf("Server (%s)", server)
	}
	return "Unknown Location (No routing headers)"
}

func RunDeepSeekChatCompletion(ctx context.Context, client *http.Client, model, systemPrompt, userPrompt string, pctx *PipelineContext) (string, error) {
	if config.DeepSeekAPIKey != "" {
		res, loc, err := callChatCompletionsAPI(ctx, client, config.DeepSeekAPIURL, config.DeepSeekAPIKey, model, systemPrompt, userPrompt, true)
		if err == nil {
			if pctx != nil {
				pctx.LLMProvider = "deepseek"
				pctx.LLMRouting = loc
			}
			return res, nil
		}
	}

	if config.DeepInfraAPIKey != "" {
		diURL := "https://api.deepinfra.com/v1/openai/chat/completions"
		diModel := "deepseek-ai/DeepSeek-V4-Flash"
		res, loc, err := callChatCompletionsAPI(ctx, client, diURL, config.DeepInfraAPIKey, diModel, systemPrompt, userPrompt, false)
		if err == nil {
			if pctx != nil {
				pctx.LLMProvider = "deepinfra"
				pctx.LLMRouting = loc
			}
			return res, nil
		}
	}

	return "", fmt.Errorf("neither DeepSeekAPIKey nor DeepInfraAPIKey is configured or succeeded")
}

func (t *AIEnhancementTransform) Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error) {
	if text == "" {
		return "", nil
	}

	systemPrompt := "You are an expert transcription editor. Refine the provided voice transcription by fixing grammar, punctuation (adding missing commas, periods, and question marks), and capitalization. Remove verbal disfluencies (such as 'uh', 'um', stutters, and repeated filler words). Preserve all technical terms, proper names, and original meaning intact. Do not translate into other languages. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text."
	if strings.TrimSpace(t.customPrompt) != "" {
		systemPrompt = fmt.Sprintf(
			"You are an expert transcription editor. Enhance the provided transcription according to these custom instructions:\n%s\n\nFix grammar, punctuation, and remove verbal disfluencies while preserving original meaning unless instructed otherwise. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text.",
			strings.TrimSpace(t.customPrompt),
		)
	}

	var matchedTerms []string
	uniqueMatched := make(map[string]bool)
	for _, m := range pctx.Matches {
		uniqueMatched[m.Canonical] = true
	}
	for term := range uniqueMatched {
		matchedTerms = append(matchedTerms, term)
	}
	sort.Strings(matchedTerms)
	
	if len(matchedTerms) > 0 {
		vocabLines := make([]string, len(matchedTerms))
		for i, term := range matchedTerms {
			vocabLines[i] = fmt.Sprintf("- %s", term)
		}
		systemPrompt += "\n\nThe following user-specific vocabulary terms are present in the text and MUST remain unchanged and spelled exactly as shown:\n" + strings.Join(vocabLines, "\n")
	}

	return RunDeepSeekChatCompletion(ctx, t.httpClient, t.model, systemPrompt, text, pctx)
}

// Token wraps individual words and split punctuation bounds
type Token struct {
	Original   string
	Prefix     string
	Clean      string
	Suffix     string
	Normalized string
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

// jaroWinkler calculates string matching distance metric
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
