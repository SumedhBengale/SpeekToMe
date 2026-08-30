package vocab

import (
	"testing"

	"speektome-server/db"
)

func TestVocabMatching(t *testing.T) {
	terms := []db.VocabularyTerm{
		{Canonical: "PostgreSQL", Normalized: "postgresql"},
		{Canonical: "SpeekTo.Me", Normalized: "speektome"},
		{Canonical: "Kubernetes", Normalized: "kubernetes"},
		{Canonical: "C#", Normalized: "c#"},
		{Canonical: "Google DeepMind", Normalized: "googledeepmind"},
		{Canonical: "FastAPI", Normalized: "fastapi"},
		{Canonical: "tRPC", Normalized: "trpc"},
	}

	cache := NewVocabCache()

	tests := []struct {
		input       string
		expected    string
		expectRepls int
	}{
		{
			input:       "I am connecting to postgress and setting up kubernetes.",
			expected:    "I am connecting to PostgreSQL and setting up Kubernetes.",
			expectRepls: 2,
		},
		{
			input:       "Welcome to speek to me dictation with google deep mind.",
			expected:    "Welcome to SpeekTo.Me dictation with Google DeepMind.",
			expectRepls: 2,
		},
		{
			input:       "Writing c sharp code today.",
			expected:    "Writing C# code today.",
			expectRepls: 1,
		},
		{
			input:       "We use fast api and t r p c in backend.",
			expected:    "We use FastAPI and tRPC in backend.",
			expectRepls: 2,
		},
		{
			input:       "(testing postgress, speek to me!)",
			expected:    "(testing PostgreSQL, SpeekTo.Me!)",
			expectRepls: 2,
		},
	}

	for _, tt := range tests {
		result, repls, dur := cache.ProcessText(1, tt.input, terms)
		if len(repls) != tt.expectRepls {
			t.Errorf("For input %q, expected %d replacements, got %d (output: %q)", tt.input, tt.expectRepls, len(repls), result)
		}
		if result != tt.expected {
			t.Errorf("For input %q, expected %q, got %q", tt.input, tt.expected, result)
		}
		if dur < 0 {
			t.Errorf("Invalid duration: %v", dur)
		}
	}
}

func TestVocabCacheInvalidate(t *testing.T) {
	cache := NewVocabCache()
	terms1 := []db.VocabularyTerm{
		{Canonical: "AlphaOne", Normalized: "alphaone"},
	}
	res1, r1, _ := cache.ProcessText(42, "Testing alpha one here.", terms1)
	if res1 != "Testing AlphaOne here." || len(r1) != 1 {
		t.Fatalf("Expected AlphaOne, got %q", res1)
	}

	// Invalidate user 42
	cache.Invalidate(42)

	terms2 := []db.VocabularyTerm{
		{Canonical: "BetaTwo", Normalized: "betatwo"},
	}
	res2, r2, _ := cache.ProcessText(42, "Testing beta two here.", terms2)
	if res2 != "Testing BetaTwo here." || len(r2) != 1 {
		t.Fatalf("Expected BetaTwo, got %q", res2)
	}
}
