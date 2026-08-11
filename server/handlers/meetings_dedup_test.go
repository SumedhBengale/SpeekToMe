package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"speektome-server/services"
)

func TestDiarizeSegmentsWithEnergy(t *testing.T) {
	segments := []services.WhisperSegment{
		{Start: 0.0, End: 3.0, Text: "Hello everyone, welcome to the presentation."},
		{Start: 3.5, End: 6.0, Text: "Hi Sumedh, thanks for having me."},
		{Start: 6.5, End: 9.0, Text: "Let us review the Q3 roadmap."},
	}

	energy := []EnergySample{
		{Time: 0.5, Mic: 120, Lb: 5},
		{Time: 1.5, Mic: 150, Lb: 10},
		{Time: 2.5, Mic: 130, Lb: 2},
		{Time: 4.0, Mic: 15, Lb: 180},
		{Time: 5.0, Mic: 20, Lb: 210},
		{Time: 7.0, Mic: 140, Lb: 8},
		{Time: 8.0, Mic: 160, Lb: 12},
	}

	merged, text := diarizeSegmentsWithEnergy(segments, energy, "Sumedh Bengale")

	if len(merged) != 3 {
		t.Fatalf("Expected 3 merged segments, got %d", len(merged))
	}

	if merged[0].Speaker != "Sumedh Bengale" {
		t.Errorf("Expected first speaker to be Sumedh Bengale, got %s", merged[0].Speaker)
	}

	if merged[1].Speaker != "Remote Speaker" {
		t.Errorf("Expected second speaker to be Remote Speaker, got %s", merged[1].Speaker)
	}

	if merged[2].Speaker != "Sumedh Bengale" {
		t.Errorf("Expected third speaker to be Sumedh Bengale, got %s", merged[2].Speaker)
	}

	t.Logf("Formatted Diarized Transcript:\n%s", text)
}

func TestFlexibleActionItemsUnmarshaling(t *testing.T) {
	jsonPayload := `{
		"polished_transcript": "John: Hello",
		"summary": "Meeting summary text.",
		"action_items": [
			"- Review transcription quality",
			"- Test live conversation"
		]
	}`

	var analysis struct {
		PolishedTranscript string      `json:"polished_transcript"`
		Summary            string      `json:"summary"`
		ActionItems        interface{} `json:"action_items"`
	}

	err := json.Unmarshal([]byte(jsonPayload), &analysis)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON with array action_items: %v", err)
	}

	var actionItemsStr string
	if analysis.ActionItems != nil {
		switch v := analysis.ActionItems.(type) {
		case string:
			actionItemsStr = v
		case []interface{}:
			var items []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					items = append(items, s)
				}
			}
			actionItemsStr = strings.Join(items, "\n")
		}
	}

	if !strings.Contains(actionItemsStr, "Review transcription quality") {
		t.Errorf("Expected actionItemsStr to contain item text, got %s", actionItemsStr)
	}
}

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
