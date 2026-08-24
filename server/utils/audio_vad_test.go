package utils

import (
	"testing"
)

func TestMergeTranscripts(t *testing.T) {
	if res := MergeTranscripts("", "hello"); res != "hello" {
		t.Errorf("Expected 'hello', got '%s'", res)
	}
	if res := MergeTranscripts("hello", ""); res != "hello" {
		t.Errorf("Expected 'hello', got '%s'", res)
	}
	if res := MergeTranscripts("hello", "world"); res != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", res)
	}
	if res := MergeTranscripts(" hello ", " world. "); res != "hello world." {
		t.Errorf("Expected 'hello world.', got '%s'", res)
	}
}

func TestEndsWithSentenceBoundary(t *testing.T) {
	if !EndsWithSentenceBoundary("Hello world.") {
		t.Errorf("Expected '.' to be sentence boundary")
	}
	if !EndsWithSentenceBoundary("Is this a test?") {
		t.Errorf("Expected '?' to be sentence boundary")
	}
	if !EndsWithSentenceBoundary("Look out!") {
		t.Errorf("Expected '!' to be sentence boundary")
	}
	if EndsWithSentenceBoundary("Hello world") {
		t.Errorf("Expected 'Hello world' without punctuation to NOT be sentence boundary")
	}
}

func TestGetPromptContext(t *testing.T) {
	// Short text within maxChars
	if res := GetPromptContext("Hello world", 150); res != "Hello world" {
		t.Errorf("Expected 'Hello world', got '%s'", res)
	}

	// Long text sliced at word boundary
	longText := "This is a long sentence with many words that exceeds the character limit for prompt context"
	res := GetPromptContext(longText, 30)
	if len(res) > 30 {
		t.Errorf("Expected length <= 30, got %d ('%s')", len(res), res)
	}
	if res == "" {
		t.Errorf("Expected non-empty prompt context")
	}
}
