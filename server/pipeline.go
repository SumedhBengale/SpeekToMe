package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	UserID     int64
	Vocabulary *UserVocabulary
	Matches    []Match
	Metrics    struct {
		ExactMatches      int
		NormalizedMatches int
		FuzzyMatches      int
	}
	Debug      bool
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
	Canonical  string
	Normalized string
	TokenCount int
}

// UserVocabulary contains pre-compiled lookup mappings
type UserVocabulary struct {
	NormalizedLookup map[string]string      // normalized exact -> canonical
	TermsByCount     map[int][]CompiledTerm // group by TokenCount
	Terms            []CompiledTerm         // sorted by TokenCount descending
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

// CompileUserVocabulary compiles database terms into lookup maps
func CompileUserVocabulary(dbTerms []VocabularyTerm) *UserVocabulary {
	normalizedLookup := make(map[string]string)
	compiledTerms := make([]CompiledTerm, 0, len(dbTerms))
	termsByCount := make(map[int][]CompiledTerm)

	for _, vt := range dbTerms {
		tokens := Tokenize(vt.Canonical)
		normalizedTokens := make([]string, len(tokens))
		for i, tok := range tokens {
			normalizedTokens[i] = tok.Normalized
		}
		computedNormalized := strings.Join(normalizedTokens, "")
		
		normalizedLookup[computedNormalized] = vt.Canonical
		
		ct := CompiledTerm{
			Canonical:  vt.Canonical,
			Normalized: computedNormalized,
			TokenCount: len(normalizedTokens),
		}
		compiledTerms = append(compiledTerms, ct)
		termsByCount[ct.TokenCount] = append(termsByCount[ct.TokenCount], ct)
	}

	maxTokenCount := 0
	for _, term := range compiledTerms {
		if term.TokenCount > maxTokenCount {
			maxTokenCount = term.TokenCount
		}
	}

	return &UserVocabulary{
		NormalizedLookup: normalizedLookup,
		TermsByCount:     termsByCount,
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
		
		// 1. Try to match exact/normalized phrases greedily (longest phrase first)
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
					
					// Increment the appropriate metrics counter
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
		
		// 2. Try fuzzy matching on phrases/single words if enabled
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
					
					for _, term := range pctx.Vocabulary.TermsByCount[k] {
						lenDiff := len(joinedNorm) - len(term.Normalized)
						if lenDiff < 0 {
							lenDiff = -lenDiff
						}
						
						// Optimized pre-filter: check length difference AND up to the first 2 normalized characters
						matchPrefix := false
						if len(joinedNorm) >= 2 && len(term.Normalized) >= 2 {
							matchPrefix = joinedNorm[0] == term.Normalized[0] && joinedNorm[1] == term.Normalized[1]
						} else if len(joinedNorm) >= 1 && len(term.Normalized) >= 1 {
							matchPrefix = joinedNorm[0] == term.Normalized[0]
						}
						
						if lenDiff <= 2*k && matchPrefix {
							threshold := SingleWordThreshold
							if k == 2 {
								threshold = TwoWordThreshold
							} else if k == 3 {
								threshold = ThreeWordThreshold
							} else if k >= 4 {
								threshold = MultiWordThreshold
							}
							
							sim := jaroWinkler(joinedNorm, term.Normalized)
							
							if sim >= threshold {
								if pctx.Debug {
									fmt.Printf("    - Fuzzy Candidate (k=%d): %q vs %q -> JW Sim: %.4f (Passed threshold: %.2f, Diff: %d)\n",
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
					
					// Greedy decision: if we matched any terms of length `k`, stop searching shorter lengths
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
		
		// 3. Keep original
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

func (t *AIEnhancementTransform) Transform(ctx context.Context, text string, pctx *PipelineContext) (string, error) {
	if text == "" {
		return "", nil
	}
	if t.apiKey == "" {
		return "", fmt.Errorf("API key is not set")
	}

	systemPrompt := "You are an expert transcription editor. Enhance the provided transcription by correcting grammar, punctuation, and typos. Preserve all technical terms, proper names, and original meaning. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text."
	if strings.TrimSpace(t.customPrompt) != "" {
		systemPrompt = fmt.Sprintf(
			"You are an expert transcription editor. Enhance the provided transcription according to these custom instructions:\n%s\n\nPreserve the original meaning unless instructed otherwise. Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished text.",
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

	dsPayload := map[string]interface{}{
		"model": t.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
		"thinking": map[string]string{"type": "disabled"},
	}

	jsonPayload, err := json.Marshal(dsPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make HTTP call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("AI call failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("AI returned empty choices")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
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
		// Skip spaces
		for i < n && isSpace(runes[i]) {
			i++
		}
		if i >= n {
			break
		}
		
		// Find end of token
		start := i
		for i < n && !isSpace(runes[i]) {
			i++
		}
		end := i
		
		// Parse prefix, clean, suffix within the token
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
			// No word characters found (e.g. just commas or punctuation like ",,,")
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
