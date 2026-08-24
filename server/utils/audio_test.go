package utils

import (
	"testing"
)

func TestMergeTranscriptsSeamStitching(t *testing.T) {
	tests := []struct {
		name      string
		committed string
		current   string
		expected  string
	}{
		{
			name:      "Separate distinct sentences",
			committed: "Welcome to the live test of SpeekTo.Me.",
			current:   "Voice dictation should feel natural, fast, and completely effortless.",
			expected:  "Welcome to the live test of SpeekTo.Me. Voice dictation should feel natural, fast, and completely effortless.",
		},
		{
			name:      "Coordinating conjunction joining",
			committed: "As I speak, the streaming engine captures each phrase in real time.",
			current:   "And automatically organizes my thoughts into clean polished sentences.",
			expected:  "As I speak, the streaming engine captures each phrase in real time and automatically organizes my thoughts into clean polished sentences.",
		},
		{
			name:      "Transitive verb ending to pronoun continuation",
			committed: "In real time and automatically organizes.",
			current:   "My thoughts into.",
			expected:  "In real time and automatically organizes my thoughts into.",
		},
		{
			name:      "Preposition ending to noun continuation",
			committed: "My thoughts into.",
			current:   "Polished sentences.",
			expected:  "My thoughts into polished sentences.",
		},
		{
			name:      "Preserve pronoun I uppercase",
			committed: "Voice dictation feels effortless.",
			current:   "As I speak, the engine works.",
			expected:  "Voice dictation feels effortless as I speak, the engine works.",
		},
		{
			name:      "Preserve acronym uppercase",
			committed: "We can connect.",
			current:   "API endpoints easily.",
			expected:  "We can connect API endpoints easily.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeTranscripts(tc.committed, tc.current)
			if got != tc.expected {
				t.Errorf("\nGot:      %q\nExpected: %q", got, tc.expected)
			}
		})
	}
}
