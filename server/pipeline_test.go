package main

import (
	"context"
	"testing"
)

func hasMatch(pctx *PipelineContext, canonical string) bool {
	for _, m := range pctx.Matches {
		if m.Canonical == canonical {
			return true
		}
	}
	return false
}

func TestNormalizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"DeepSeek", "deepseek"},
		{"Node-JS", "nodejs"},
		{"C++", "c++"},
		{"C#", "c#"},
		{"Postgre SQL", "postgresql"},
		{"Hello, World!", "helloworld"},
	}

	for _, tc := range tests {
		got := NormalizeString(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeString(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input  string
		tokens []Token
	}{
		{
			"Hello,",
			[]Token{
				{Original: "Hello,", Prefix: "", Clean: "Hello", Suffix: ",", Normalized: "hello"},
			},
		},
		{
			"(Node.js)",
			[]Token{
				{Original: "(Node.js)", Prefix: "(", Clean: "Node.js", Suffix: ")", Normalized: "nodejs"},
			},
		},
		{
			"we are using deep seek",
			[]Token{
				{Original: "we", Prefix: "", Clean: "we", Suffix: "", Normalized: "we"},
				{Original: "are", Prefix: "", Clean: "are", Suffix: "", Normalized: "are"},
				{Original: "using", Prefix: "", Clean: "using", Suffix: "", Normalized: "using"},
				{Original: "deep", Prefix: "", Clean: "deep", Suffix: "", Normalized: "deep"},
				{Original: "seek", Prefix: "", Clean: "seek", Suffix: "", Normalized: "seek"},
			},
		},
	}

	for _, tc := range tests {
		got := Tokenize(tc.input)
		if len(got) != len(tc.tokens) {
			t.Fatalf("Tokenize(%q) returned %d tokens; want %d", tc.input, len(got), len(tc.tokens))
		}
		for i, tok := range got {
			want := tc.tokens[i]
			if tok.Original != want.Original || tok.Prefix != want.Prefix || tok.Clean != want.Clean || tok.Suffix != want.Suffix || tok.Normalized != want.Normalized {
				t.Errorf("Tokenize(%q)[%d] = %+v; want %+v", tc.input, i, tok, want)
			}
		}
	}
}

func TestJaroWinkler(t *testing.T) {
	tests := []struct {
		s1       string
		s2       string
		minScore float64
	}{
		{"speaktome", "speektome", 0.88},
		{"speakto.me", "speektome", 0.88},
		{"sumed", "sumedh", 0.94},
	}

	for _, tc := range tests {
		score := jaroWinkler(tc.s1, tc.s2)
		if score < tc.minScore {
			t.Errorf("jaroWinkler(%q, %q) = %f; want >= %f", tc.s1, tc.s2, score, tc.minScore)
		}
	}
}

func TestPipelineVocabMatching(t *testing.T) {
	dbTerms := []VocabularyTerm{
		{"SpeekTo.Me", "speektome"},
		{"DeepSeek", "deepseek"},
		{"Payload CMS Enterprise", "payloadcmsenterprise"},
		{"Payload CMS", "payloadcms"},
	}

	vocab := CompileUserVocabulary(dbTerms)
	pctx := NewPipelineContext(1, vocab)
	pctx.Debug = true
	
	// Test Pass 1 normalizer (exact + normalized exact)
	p1 := NewVocabNormalizerTransform("Pass 1", false, 0.0)
	
	input := "we are using deep seek with payload cms enterprise and speaktome"
	got, err := p1.Transform(context.Background(), input, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected := "we are using DeepSeek with Payload CMS Enterprise and speaktome"
	if got != expected {
		t.Errorf("Pass 1 match output = %q; want %q", got, expected)
	}

	if !hasMatch(pctx, "DeepSeek") {
		t.Error("DeepSeek should be in Matches")
	}
	if !hasMatch(pctx, "Payload CMS Enterprise") {
		t.Error("Payload CMS Enterprise should be in Matches")
	}
	if hasMatch(pctx, "SpeekTo.Me") {
		t.Error("SpeekTo.Me should not be in Matches")
	}

	// Verify metrics after Pass 1 (should have 2 normalized matches, 0 exact matches)
	if pctx.Metrics.ExactMatches != 0 {
		t.Errorf("Expected 0 ExactMatches; got %d", pctx.Metrics.ExactMatches)
	}
	if pctx.Metrics.NormalizedMatches != 2 {
		t.Errorf("Expected 2 NormalizedMatches; got %d", pctx.Metrics.NormalizedMatches)
	}

	// Test Pass 2 normalizer (fuzzy)
	p2 := NewVocabNormalizerTransform("Pass 2", true, 0.88)
	got2, err := p2.Transform(context.Background(), got, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected2 := "we are using DeepSeek with Payload CMS Enterprise and SpeekTo.Me"
	if got2 != expected2 {
		t.Errorf("Pass 2 match output = %q; want %q", got2, expected2)
	}

	if !hasMatch(pctx, "SpeekTo.Me") {
		t.Error("SpeekTo.Me should be in Matches after Pass 2 fuzzy match")
	}

	// Verify metrics after Pass 2 (should have 1 fuzzy match)
	if pctx.Metrics.FuzzyMatches != 1 {
		t.Errorf("Expected 1 FuzzyMatches; got %d", pctx.Metrics.FuzzyMatches)
	}
}

func TestPipelineDeepSeekEdgeCase(t *testing.T) {
	dbTerms := []VocabularyTerm{
		{"DeepSeek", "deepseek"},
		{"Payload CMS", "payloadcms"}, // Ensures MaxTokenCount >= 2 for the lookahead horizon test
	}

	vocab := CompileUserVocabulary(dbTerms)
	pctx := NewPipelineContext(1, vocab)
	pctx.Debug = true

	p1 := NewVocabNormalizerTransform("Pass 1", false, 0.0)
	input := "I'm trying out the deep seek API"
	got, err := p1.Transform(context.Background(), input, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected := "I'm trying out the DeepSeek API"
	if got != expected {
		t.Errorf("Pass 1 DeepSeek match output = %q; want %q", got, expected)
	}

	pctx2 := NewPipelineContext(2, vocab)
	pctx2.Debug = true
	p2 := NewVocabNormalizerTransform("Pass 2", true, 0.88)
	input2 := "the ocean is very deep today"
	got2, err := p2.Transform(context.Background(), input2, pctx2)
	if err != nil {
		t.Fatal(err)
	}

	expected2 := "the ocean is very deep today"
	if got2 != expected2 {
		t.Errorf("Pass 2 deep fuzzy matched inappropriately: got = %q; want %q", got2, expected2)
	}
}

func TestPipelineMultiWordFuzzyMatching(t *testing.T) {
	dbTerms := []VocabularyTerm{
		{"Sumedh Sanjay Bengale", "sumedhsanjaybengale"},
	}

	vocab := CompileUserVocabulary(dbTerms)
	pctx := NewPipelineContext(1, vocab)
	pctx.Debug = true

	p2 := NewVocabNormalizerTransform("Pass 2", true, 0.88)
	input := "Sumed Sanjay Bengli"
	got, err := p2.Transform(context.Background(), input, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Sumedh Sanjay Bengale"
	if got != expected {
		t.Errorf("Multi-word fuzzy match output = %q; want %q", got, expected)
	}

	if !hasMatch(pctx, "Sumedh Sanjay Bengale") {
		t.Error("Sumedh Sanjay Bengale should be in Matches after multi-word fuzzy match")
	}
}

func TestPipelineSumitSenjoyBengli(t *testing.T) {
	dbTerms := []VocabularyTerm{
		{"Sumedh Sanjay Bengale", "sumedhsanjaybengale"},
	}

	vocab := CompileUserVocabulary(dbTerms)
	pctx := NewPipelineContext(1, vocab)
	pctx.Debug = true

	p2 := NewVocabNormalizerTransform("Pass 2", true, 0.88)
	input := "Sumit Senjoy Bengli"
	got, err := p2.Transform(context.Background(), input, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Sumedh Sanjay Bengale"
	if got != expected {
		t.Errorf("Multi-word typo fuzzy match output = %q; want %q", got, expected)
	}

	if !hasMatch(pctx, "Sumedh Sanjay Bengale") {
		t.Error("Sumedh Sanjay Bengale should be in Matches after dynamic threshold fuzzy match")
	}
}

func TestVocabularyCacheLifecycle(t *testing.T) {
	cache := NewVocabularyCache()
	defer cache.Close()

	dbTerms := []VocabularyTerm{
		{"SpeekTo.Me", "speektome"},
	}
	vocab := CompileUserVocabulary(dbTerms)
	cache.Set(10, vocab)

	retrieved := cache.Get(10)
	if retrieved == nil || len(retrieved.Terms) == 0 {
		t.Fatal("Failed to retrieve cached vocabulary")
	}

	cache.Invalidate(10)
	if cache.Get(10) != nil {
		t.Fatal("Expected cached vocabulary to be invalidated")
	}
}

func TestExtractSubTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"SpeekTo.Me", []string{"speek", "to", "me"}},
		{"Node.js", []string{"node", "js"}},
		{"PostgreSQL", []string{"postgre", "sql"}},
		{"Payload CMS Enterprise", []string{"payload", "cms", "enterprise"}},
	}

	for _, tc := range tests {
		got := ExtractSubTokens(tc.input)
		if len(got) != len(tc.expected) {
			t.Fatalf("ExtractSubTokens(%q) returned %d sub-tokens; want %d", tc.input, len(got), len(tc.expected))
		}
		for i, tok := range got {
			if tok != tc.expected[i] {
				t.Errorf("ExtractSubTokens(%q)[%d] = %q; want %q", tc.input, i, tok, tc.expected[i])
			}
		}
	}
}

func TestSpeekToMeSubTokenMatching(t *testing.T) {
	dbTerms := []VocabularyTerm{
		{"SpeekTo.Me", "speektome"},
		{"Node.js", "nodejs"},
		{"PostgreSQL", "postgresql"},
	}

	vocab := CompileUserVocabulary(dbTerms)
	pctx := NewPipelineContext(1, vocab)
	pctx.Debug = true

	p := NewVocabNormalizerTransform("Hybrid Normalizer", true, 0.88)
	
	// Test 1: "speak to me" -> "SpeekTo.Me"
	input1 := "Welcome and speak to me please"
	got1, err := p.Transform(context.Background(), input1, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected1 := "Welcome and SpeekTo.Me please"
	if got1 != expected1 {
		t.Errorf("Sub-token SpeekTo.Me output = %q; want %q", got1, expected1)
	}

	// Test 2: "node js" -> "Node.js"
	input2 := "we build our backend using node js"
	got2, err := p.Transform(context.Background(), input2, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected2 := "we build our backend using Node.js"
	if got2 != expected2 {
		t.Errorf("Sub-token Node.js output = %q; want %q", got2, expected2)
	}

	// Test 3: "postgre sql" -> "PostgreSQL"
	input3 := "connecting to postgre sql database"
	got3, err := p.Transform(context.Background(), input3, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected3 := "connecting to PostgreSQL database"
	if got3 != expected3 {
		t.Errorf("Sub-token PostgreSQL output = %q; want %q", got3, expected3)
	}
}

func TestPartialPhraseNoMatch(t *testing.T) {
	dbTerms := []VocabularyTerm{
		{"SpeekTo.Me", "speektome"},
	}

	vocab := CompileUserVocabulary(dbTerms)
	pctx := NewPipelineContext(1, vocab)
	pctx.Debug = true

	p := NewVocabNormalizerTransform("Hybrid Normalizer", true, 0.88)

	// Partial phrase "speak to" (missing "me") should NOT match SpeekTo.Me
	input := "he went and decided to speak to the principal"
	got, err := p.Transform(context.Background(), input, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expected := "he went and decided to speak to the principal"
	if got != expected {
		t.Errorf("Partial phrase 'speak to' inappropriately replaced: got %q; want %q", got, expected)
	}

	// Full phrase "speak to me" SHOULD match SpeekTo.Me
	inputFull := "he went and decided to speak to me the principal"
	gotFull, err := p.Transform(context.Background(), inputFull, pctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedFull := "he went and decided to SpeekTo.Me the principal"
	if gotFull != expectedFull {
		t.Errorf("Full phrase 'speak to me' failed to replace: got %q; want %q", gotFull, expectedFull)
	}
}


