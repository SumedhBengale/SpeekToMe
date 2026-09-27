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

func TestFilterMeetingStreamTurns_BleedSuppression(t *testing.T) {
	turns := []ClientMeetingTurn{
		{
			ID:          "turn_lb_1",
			Channel:     "system",
			SpeakerID:   "spk_speaker_1",
			SpeakerName: "Speaker 1",
			StartTime:   25.0,
			EndTime:     45.0,
			Text:        "this is an easy base we can take",
			IsFinal:     true,
		},
		{
			// Phantom bleed mic turn #08
			ID:          "turn_mic_bleed_1",
			Channel:     "mic",
			SpeakerID:   "speaker_local",
			SpeakerName: "Sumedh Bengale",
			StartTime:   32.0,
			EndTime:     36.0,
			Text:        "This is a case begin to take",
			IsFinal:     true,
		},
		{
			// Phantom bleed mic turn #09 (word fragment)
			ID:          "turn_mic_bleed_2",
			Channel:     "mic",
			SpeakerID:   "speaker_local",
			SpeakerName: "Sumedh Bengale",
			StartTime:   41.0,
			EndTime:     44.0,
			Text:        "to take a person",
			IsFinal:     true,
		},
		{
			// Real user response after speaker finished
			ID:          "turn_mic_real",
			Channel:     "mic",
			SpeakerID:   "speaker_local",
			SpeakerName: "Sumedh Bengale",
			StartTime:   48.0,
			EndTime:     52.0,
			Text:        "Why is that the best option?",
			IsFinal:     true,
		},
	}

	filtered := filterMeetingStreamTurns(turns)

	if len(filtered) != 2 {
		t.Fatalf("Expected 2 turns after bleed filtering (Speaker 1 + real user speech), got %d: %+v", len(filtered), filtered)
	}

	if filtered[0].SpeakerName != "Speaker 1" {
		t.Errorf("Expected first turn to be Speaker 1, got %s", filtered[0].SpeakerName)
	}
	if filtered[0].Text != "this is an easy base we can take" {
		t.Errorf("Unexpected text for Speaker 1: %s", filtered[0].Text)
	}

	if filtered[1].SpeakerName != "Sumedh Bengale" {
		t.Errorf("Expected second turn to be Sumedh Bengale, got %s", filtered[1].SpeakerName)
	}
	if filtered[1].Text != "Why is that the best option?" {
		t.Errorf("Unexpected text for Sumedh Bengale: %s", filtered[1].Text)
	}
}

func TestFilterMeetingStreamTurns_MergeThresholdAndInterimPruning(t *testing.T) {
	turns := []ClientMeetingTurn{
		{
			// Loopback turn 1 (ends at 25.84)
			ID:          "turn_lb_1",
			Channel:     "system",
			SpeakerID:   "spk_speaker_1",
			SpeakerName: "Speaker 1",
			StartTime:   10.88,
			EndTime:     25.84,
			Text:        "First section of remote presentation.",
			IsFinal:     true,
		},
		{
			// Loopback turn 2 (starts at 26.48 -> pause = 0.64s > 0.35s, should NOT merge)
			ID:          "turn_lb_2",
			Channel:     "system",
			SpeakerID:   "spk_speaker_1",
			SpeakerName: "Speaker 1",
			StartTime:   26.48,
			EndTime:     30.16,
			Text:        "Second section after a pause.",
			IsFinal:     true,
		},
		{
			// Loopback turn 3 (starts at 30.30 -> pause = 0.14s <= 0.35s, SHOULD merge into turn 2)
			ID:          "turn_lb_3",
			Channel:     "system",
			SpeakerID:   "spk_speaker_1",
			SpeakerName: "Speaker 1",
			StartTime:   30.30,
			EndTime:     35.00,
			Text:        "Continuation without significant pause.",
			IsFinal:     true,
		},
		{
			// Interim non-final mic turn (must be pruned!)
			ID:          "turn_mic_interim",
			Channel:     "mic",
			SpeakerID:   "speaker_local",
			SpeakerName: "Sumedh Bengale",
			StartTime:   36.00,
			EndTime:     37.50,
			Text:        "Partial unfinished delta",
			IsFinal:     false,
		},
		{
			// Completed final mic turn
			ID:          "turn_mic_final",
			Channel:     "mic",
			SpeakerID:   "speaker_local",
			SpeakerName: "Sumedh Bengale",
			StartTime:   38.00,
			EndTime:     42.00,
			Text:        "Final verified local utterance.",
			IsFinal:     true,
		},
	}

	result := filterMeetingStreamTurns(turns)

	// Should result in 3 turns:
	// 1) turn_lb_1 (10.88-25.84)
	// 2) turn_lb_2 + turn_lb_3 merged (26.48-35.00)
	// 3) turn_mic_final (38.00-42.00)
	// turn_mic_interim MUST be dropped!
	if len(result) != 3 {
		t.Fatalf("Expected 3 turns, got %d: %+v", len(result), result)
	}

	if result[0].StartTime != 10.88 || result[0].EndTime != 25.84 {
		t.Errorf("Turn 1 timestamps incorrect: [%.2f-%.2f]", result[0].StartTime, result[0].EndTime)
	}

	if result[1].StartTime != 26.48 || result[1].EndTime != 35.00 {
		t.Errorf("Merged Turn 2 timestamps incorrect: [%.2f-%.2f]", result[1].StartTime, result[1].EndTime)
	}
	expectedMergedText := "Second section after a pause. Continuation without significant pause."
	if result[1].Text != expectedMergedText {
		t.Errorf("Expected merged text %q, got %q", expectedMergedText, result[1].Text)
	}

	if result[2].ID != "turn_mic_final" {
		t.Errorf("Expected 3rd turn to be turn_mic_final, got %s (%s)", result[2].ID, result[2].Text)
	}
}


