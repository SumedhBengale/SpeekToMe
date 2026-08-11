package handlers

import (
	"testing"

	"speektome-server/services"
)

func TestDeduplicateMeetingSegments_BleedRemoval(t *testing.T) {
	micSegs := []services.WhisperSegment{
		{Start: 0.5, End: 3.0, Text: "Hello, can everyone see my shared screen?"},
		{Start: 4.0, End: 6.0, Text: "Yes, I can see it clearly."},
	}

	lbSegs := []services.WhisperSegment{
		{Start: 0.4, End: 2.9, Text: "Hello, can everyone see my shared screen?"},
	}

	merged, text := deduplicateMeetingSegments(micSegs, lbSegs, "Alice")

	if len(merged) != 2 {
		t.Fatalf("Expected 2 merged segments, got %d", len(merged))
	}

	if merged[0].Speaker != "Remote Speaker" {
		t.Errorf("Expected first speaker to be Remote Speaker, got %s", merged[0].Speaker)
	}

	if merged[1].Speaker != "Alice" {
		t.Errorf("Expected second speaker to be Alice, got %s", merged[1].Speaker)
	}

	if merged[1].Text != "Yes, I can see it clearly." {
		t.Errorf("Unexpected text for Alice: %s", merged[1].Text)
	}

	t.Logf("Formatted Output:\n%s", text)
}

func TestDeduplicateMeetingSegments_SameSpeakerMerging(t *testing.T) {
	micSegs := []services.WhisperSegment{
		{Start: 1.0, End: 2.5, Text: "Let us start the meeting."},
		{Start: 2.8, End: 4.5, Text: "First topic is Q3 budget overview."},
	}

	lbSegs := []services.WhisperSegment{}

	merged, _ := deduplicateMeetingSegments(micSegs, lbSegs, "Bob")

	if len(merged) != 1 {
		t.Fatalf("Expected consecutive segments from Bob to merge into 1, got %d", len(merged))
	}

	expectedText := "Let us start the meeting. First topic is Q3 budget overview."
	if merged[0].Text != expectedText {
		t.Errorf("Expected merged text '%s', got '%s'", expectedText, merged[0].Text)
	}
}

func TestCalculateTextSimilarity(t *testing.T) {
	sim1 := calculateTextSimilarity("Can everyone see my screen?", "Can everyone see my screen")
	if sim1 < 0.9 {
		t.Errorf("Expected high similarity for near identical strings, got %f", sim1)
	}

	sim2 := calculateTextSimilarity("The project status is good.", "I disagree with that proposal completely.")
	if sim2 > 0.2 {
		t.Errorf("Expected low similarity for distinct strings, got %f", sim2)
	}
}
