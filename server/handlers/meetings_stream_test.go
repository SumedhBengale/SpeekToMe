package handlers

import (
	"strings"
	"testing"
)

func TestFormatSpeakerDisplayName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"speaker_0", "Speaker 1"},
		{"speaker_1", "Speaker 1"},
		{"speaker_2", "Speaker 2"},
		{"speaker_3", "Speaker 3"},
		{"speaker_4", "Speaker 4"},
		{"spk_1", "Speaker 1"},
		{"spk_2", "Speaker 2"},
		{"speaker_5", "Speaker 5"},
		{"Alice", "Alice"},
	}

	for _, c := range cases {
		got := formatSpeakerDisplayName(c.input)
		if got != c.expected {
			t.Errorf("formatSpeakerDisplayName(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestUpsertMeetingTurn(t *testing.T) {
	var turns []ClientMeetingTurn

	turn1 := ClientMeetingTurn{
		ID:          "turn_1",
		Channel:     "mic",
		SpeakerID:   "speaker_local",
		SpeakerName: "Sumedh",
		StartTime:   0.0,
		EndTime:     1.12,
		Text:        "Hello everyone",
		IsFinal:     false,
	}

	upsertMeetingTurn(&turns, turn1)
	if len(turns) != 1 {
		t.Fatalf("Expected 1 turn, got %d", len(turns))
	}

	// Update the same turn
	turn1Updated := ClientMeetingTurn{
		ID:          "turn_1",
		Channel:     "mic",
		SpeakerID:   "speaker_local",
		SpeakerName: "Sumedh",
		StartTime:   0.0,
		EndTime:     2.24,
		Text:        "Hello everyone, welcome to the meeting.",
		IsFinal:     true,
	}

	upsertMeetingTurn(&turns, turn1Updated)
	if len(turns) != 1 {
		t.Fatalf("Expected still 1 turn after update, got %d", len(turns))
	}
	if turns[0].Text != "Hello everyone, welcome to the meeting." {
		t.Errorf("Expected updated text, got %q", turns[0].Text)
	}

	// Insert a second turn
	turn2 := ClientMeetingTurn{
		ID:          "turn_2",
		Channel:     "system",
		SpeakerID:   "spk_speaker_1",
		SpeakerName: "Speaker 1",
		StartTime:   2.5,
		EndTime:     4.0,
		Text:        "Thanks for having me.",
		IsFinal:     true,
	}

	upsertMeetingTurn(&turns, turn2)
	if len(turns) != 2 {
		t.Fatalf("Expected 2 turns, got %d", len(turns))
	}
}

func TestMathOverlapCalculation(t *testing.T) {
	// Phrase interval: [1.0, 3.0]
	// Diarizer segment A: [0.5, 2.5] -> overlap = min(3.0, 2.5) - max(1.0, 0.5) = 2.5 - 1.0 = 1.5
	// Diarizer segment B: [2.6, 5.0] -> overlap = min(3.0, 5.0) - max(1.0, 2.6) = 3.0 - 2.6 = 0.4

	phraseStart := 1.0
	phraseEnd := 3.0

	segAStart := 0.5
	segAEnd := 2.5
	overlapA := mathMax(0, mathMin(phraseEnd, segAEnd)-mathMax(phraseStart, segAStart))

	segBStart := 2.6
	segBEnd := 5.0
	overlapB := mathMax(0, mathMin(phraseEnd, segBEnd)-mathMax(phraseStart, segBStart))

	if overlapA != 1.5 {
		t.Errorf("Expected overlapA = 1.5, got %f", overlapA)
	}
	if overlapB < 0.39 || overlapB > 0.41 {
		t.Errorf("Expected overlapB = 0.4, got %f", overlapB)
	}

	if overlapA <= overlapB {
		t.Errorf("Expected overlapA > overlapB, so segment A is selected")
	}
}

func TestResolveMultiSpeakerDiarization(t *testing.T) {
	// Simulate Sortformer emitting segments for 3 distinct speakers
	segments := []DiarizationSegment{
		{Start: 0.0, End: 5.2, Speaker: "speaker_1"},
		{Start: 6.0, End: 10.5, Speaker: "speaker_2"},
		{Start: 11.0, End: 16.0, Speaker: "speaker_3"},
	}

	resolveSpeaker := func(startTime, endTime float64) (string, bool) {
		if len(segments) == 0 {
			return "Speaker 1", false
		}
		bestSpeaker := ""
		var maxOverlap float64 = 0.0

		for _, seg := range segments {
			overlap := mathMax(0, mathMin(endTime, seg.End)-mathMax(startTime, seg.Start))
			if overlap > maxOverlap {
				maxOverlap = overlap
				bestSpeaker = formatSpeakerDisplayName(seg.Speaker)
			}
		}
		if maxOverlap > 0.0 {
			return bestSpeaker, true
		}

		var minDistance float64 = 1e9
		for _, seg := range segments {
			var dist float64
			if endTime < seg.Start {
				dist = seg.Start - endTime
			} else if startTime > seg.End {
				dist = startTime - seg.End
			} else {
				dist = 0
			}
			if dist < minDistance {
				minDistance = dist
				bestSpeaker = formatSpeakerDisplayName(seg.Speaker)
			}
		}
		if bestSpeaker == "" {
			bestSpeaker = "Speaker 1"
		}
		return bestSpeaker, false
	}

	// Case 1: Utterance in [1.0, 4.0] should resolve to Speaker 1 with direct overlap
	spk1, hasOverlap1 := resolveSpeaker(1.0, 4.0)
	if spk1 != "Speaker 1" || !hasOverlap1 {
		t.Errorf("Expected Speaker 1 with overlap, got %q (hasOverlap=%v)", spk1, hasOverlap1)
	}

	// Case 2: Utterance in [7.0, 9.5] should resolve to Speaker 2 with direct overlap
	spk2, hasOverlap2 := resolveSpeaker(7.0, 9.5)
	if spk2 != "Speaker 2" || !hasOverlap2 {
		t.Errorf("Expected Speaker 2 with overlap, got %q (hasOverlap=%v)", spk2, hasOverlap2)
	}

	// Case 3: Utterance in [12.0, 15.0] should resolve to Speaker 3 with direct overlap
	spk3, hasOverlap3 := resolveSpeaker(12.0, 15.0)
	if spk3 != "Speaker 3" || !hasOverlap3 {
		t.Errorf("Expected Speaker 3 with overlap, got %q (hasOverlap=%v)", spk3, hasOverlap3)
	}

	// Case 4: Utterance in gap [5.3, 5.8] closest to Speaker 1 (dist = 0.1) vs Speaker 2 (dist = 0.2)
	spkGap, hasOverlapGap := resolveSpeaker(5.3, 5.8)
	if spkGap != "Speaker 1" || hasOverlapGap {
		t.Errorf("Expected Speaker 1 fallback without direct overlap, got %q (hasOverlap=%v)", spkGap, hasOverlapGap)
	}
}

func TestResolveUnifiedStreamURL(t *testing.T) {
	// 1. Default URL with diarize=true
	u1 := resolveUnifiedStreamURL("", true, "en", "session-1")
	if !strings.Contains(u1, "diarize=true") || !strings.Contains(u1, "lang=en") || !strings.Contains(u1, "stream_id=session-1") {
		t.Errorf("Unexpected default resolved URL: %s", u1)
	}

	// 2. HTTP URL converted to WS with legacy path normalized
	u2 := resolveUnifiedStreamURL("http://GPU_HOST_IP:8000/v1/realtime", false, "es", "session-2")
	if !strings.HasPrefix(u2, "ws://") || !strings.Contains(u2, "/v1/stream") || !strings.Contains(u2, "diarize=false") || !strings.Contains(u2, "lang=es") {
		t.Errorf("Unexpected normalized URL: %s", u2)
	}
}

func TestCollapseWordsToTurns(t *testing.T) {
	words := []WordItem{
		{Word: "Okay", Start: 1.28, End: 1.36, Speaker: 1, Confidence: 1.0},
		{Word: "right", Start: 1.92, End: 2.00, Speaker: 1, Confidence: 1.0},
		{Word: "we're", Start: 2.40, End: 2.48, Speaker: 1, Confidence: 1.0},
		{Word: "gonna", Start: 2.64, End: 2.80, Speaker: 1, Confidence: 1.0},
		{Word: "does", Start: 116.16, End: 116.24, Speaker: 3, Confidence: 1.0},
		{Word: "this", Start: 116.24, End: 116.32, Speaker: 3, Confidence: 1.0},
		{Word: "the", Start: 118.24, End: 118.40, Speaker: 1, Confidence: 1.0},
		{Word: "old", Start: 118.40, End: 118.72, Speaker: 1, Confidence: 1.0},
		{Word: "thank", Start: 120.96, End: 121.20, Speaker: 3, Confidence: 1.0},
		{Word: "you", Start: 121.20, End: 121.40, Speaker: 3, Confidence: 1.0},
	}

	turns := collapseWordsToTurns(words, "turn_test")
	if len(turns) != 4 {
		t.Fatalf("Expected 4 turns, got %d", len(turns))
	}

	// Turn 1: Speaker 1
	if turns[0].SpeakerName != "Speaker 1" || turns[0].Text != "Okay right we're gonna" {
		t.Errorf("Turn 0 mismatch: spk=%s text=%s", turns[0].SpeakerName, turns[0].Text)
	}
	if turns[0].StartTime != 1.28 || turns[0].EndTime != 2.80 {
		t.Errorf("Turn 0 time mismatch: %.2f - %.2f", turns[0].StartTime, turns[0].EndTime)
	}

	// Turn 2: Speaker 3
	if turns[1].SpeakerName != "Speaker 3" || turns[1].Text != "does this" {
		t.Errorf("Turn 1 mismatch: spk=%s text=%s", turns[1].SpeakerName, turns[1].Text)
	}

	// Turn 3: Speaker 1
	if turns[2].SpeakerName != "Speaker 1" || turns[2].Text != "the old" {
		t.Errorf("Turn 2 mismatch: spk=%s text=%s", turns[2].SpeakerName, turns[2].Text)
	}

	// Turn 4: Speaker 3
	if turns[3].SpeakerName != "Speaker 3" || turns[3].Text != "thank you" {
		t.Errorf("Turn 3 mismatch: spk=%s text=%s", turns[3].SpeakerName, turns[3].Text)
	}
}


