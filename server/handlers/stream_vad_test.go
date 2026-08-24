package handlers

import (
	"testing"

	"speektome-server/utils"
)

func TestStreamTranscriptComposition(t *testing.T) {
	committed := ""
	chunk1 := "Hello world"
	full1 := utils.MergeTranscripts(committed, chunk1)
	if full1 != "Hello world" {
		t.Errorf("Expected 'Hello world', got '%s'", full1)
	}

	// Commit chunk1
	committed = utils.MergeTranscripts(committed, chunk1)
	if committed != "Hello world" {
		t.Errorf("Expected 'Hello world', got '%s'", committed)
	}

	// Next chunk arrives
	chunk2 := "how are you today"
	full2 := utils.MergeTranscripts(committed, chunk2)
	if full2 != "Hello world how are you today" {
		t.Errorf("Expected 'Hello world how are you today', got '%s'", full2)
	}

	// Commit chunk2
	committed = utils.MergeTranscripts(committed, chunk2)

	// Tail chunk arrives
	chunk3 := "thank you."
	full3 := utils.MergeTranscripts(committed, chunk3)
	if full3 != "Hello world how are you today thank you." {
		t.Errorf("Expected 'Hello world how are you today thank you.', got '%s'", full3)
	}
}

func TestVADSentenceBoundaryTrigger(t *testing.T) {
	testCases := []struct {
		text     string
		expected bool
	}{
		{"I am speaking", false},
		{"I am speaking.", true},
		{"What is this?", true},
		{"Amazing!", true},
		{"First; second;", true},
		{"trailing space. ", true},
		{"", false},
	}

	for _, tc := range testCases {
		res := utils.EndsWithSentenceBoundary(tc.text)
		if res != tc.expected {
			t.Errorf("EndsWithSentenceBoundary(%q) = %v, expected %v", tc.text, res, tc.expected)
		}
	}
}
