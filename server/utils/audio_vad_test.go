package utils

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func generateSineWavePCM(sampleRate, freqHz, durationMs int, amplitude int16) []byte {
	numSamples := (sampleRate * durationMs) / 1000
	buf := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := float64(amplitude) * math.Sin(2*math.Pi*float64(freqHz)*t)
		binary.LittleEndian.PutUint16(buf[i*2:(i+1)*2], uint16(int16(sample)))
	}
	return buf
}

func TestCalculateRMS(t *testing.T) {
	// 1. Pure silence (zeros)
	zeroBuf := make([]byte, 3200)
	rmsZero := CalculateRMS(zeroBuf)
	if rmsZero != 0.0 {
		t.Errorf("Expected RMS of 0.0 for zero buffer, got %f", rmsZero)
	}

	// 2. Sine wave with peak amplitude 10,000 (RMS of sine = amplitude / sqrt(2) ≈ 7071)
	sineBuf := generateSineWavePCM(16000, 440, 100, 10000)
	rmsSine := CalculateRMS(sineBuf)
	expectedRMS := 10000.0 / math.Sqrt(2)
	if math.Abs(rmsSine-expectedRMS) > 500 {
		t.Errorf("Expected RMS around %f for sine wave, got %f", expectedRMS, rmsSine)
	}
}

func TestIsSilencePCM(t *testing.T) {
	silence := make([]byte, 1600)
	if !IsSilencePCM(silence, 250.0) {
		t.Errorf("Expected zero buffer to be silence")
	}

	loudSine := generateSineWavePCM(16000, 440, 100, 5000)
	if IsSilencePCM(loudSine, 250.0) {
		t.Errorf("Expected loud sine wave to NOT be silence")
	}
}

func TestAudioChunkTracker(t *testing.T) {
	tracker := NewAudioChunkTracker(16000, 250.0)
	tracker.MinSpeechDuration = 200 * time.Millisecond
	tracker.SilenceDurationThreshold = 300 * time.Millisecond
	tracker.MaxChunkDuration = 2000 * time.Millisecond

	// Feed 300ms speech
	speechChunk := generateSineWavePCM(16000, 440, 300, 3000)
	isSilence, shouldCommit := tracker.ProcessChunk(speechChunk)
	if isSilence {
		t.Errorf("Expected speechChunk to be detected as speech")
	}
	if shouldCommit {
		t.Errorf("Expected shouldCommit to be false mid-speech")
	}

	// Feed 100ms silence
	silence100ms := make([]byte, 3200) // 100ms at 16kHz 16-bit = 3200 bytes
	isSilence, shouldCommit = tracker.ProcessChunk(silence100ms)
	if !isSilence {
		t.Errorf("Expected silence to be detected")
	}
	if shouldCommit {
		t.Errorf("Expected shouldCommit to be false after only 100ms silence (threshold is 300ms)")
	}

	// Feed another 250ms silence (total silence = 350ms >= 300ms)
	silence250ms := make([]byte, 8000) // 250ms at 16kHz 16-bit = 8000 bytes
	_, shouldCommit = tracker.ProcessChunk(silence250ms)
	if !shouldCommit {
		t.Errorf("Expected shouldCommit to be TRUE after speech followed by 350ms silence")
	}

	// Reset tracker
	tracker.Reset()
	if tracker.hasActiveSpeech || tracker.totalChunkBytes != 0 {
		t.Errorf("Expected tracker to be fully reset")
	}
}

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

func TestExtractStabilizedText(t *testing.T) {
	// If final or paused, full text is returned
	if res := ExtractStabilizedText("Welcome to the live", true); res != "Welcome to the live" {
		t.Errorf("Expected full text when isFinalOrPaused=true, got '%s'", res)
	}

	// Single word while active should hold back
	if res := ExtractStabilizedText("Welcome", false); res != "" {
		t.Errorf("Expected empty string for single word active speech, got '%s'", res)
	}

	// Multi-word active speech should hold back last word
	if res := ExtractStabilizedText("Welcome to the live", false); res != "Welcome to the" {
		t.Errorf("Expected 'Welcome to the', got '%s'", res)
	}

	if res := ExtractStabilizedText("Natural fast and completely effortless", false); res != "Natural fast and completely" {
		t.Errorf("Expected 'Natural fast and completely', got '%s'", res)
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
