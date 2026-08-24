package utils

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// Calculate audio duration from standard WAV header
func GetWavDuration(data []byte) float64 {
	if len(data) < 44 {
		return 0
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0
	}
	channels := uint32(binary.LittleEndian.Uint16(data[22:24]))
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	bitsPerSample := uint32(binary.LittleEndian.Uint16(data[34:36]))

	bytesPerSample := bitsPerSample / 8
	if channels == 0 || sampleRate == 0 || bytesPerSample == 0 {
		return 0
	}
	dataSize := len(data) - 44
	numSamples := uint32(dataSize) / (channels * bytesPerSample)
	return float64(numSamples) / float64(sampleRate)
}

// GetOggOpusDuration parses an Ogg/Opus file header and returns its duration in seconds
func GetOggOpusDuration(data []byte) float64 {
	const signature = "OggS"
	var lastGranule int64 = 0
	offset := 0
	for offset+27 <= len(data) {
		if string(data[offset:offset+4]) != signature {
			break
		}
		// Read granule position at offset + 6 (8 bytes, little endian)
		granule := int64(binary.LittleEndian.Uint64(data[offset+6 : offset+14]))
		if granule > 0 {
			lastGranule = granule
		}
		// Read segments count at offset 26
		segmentsCount := int(data[offset+26])
		if offset+27+segmentsCount > len(data) {
			break
		}
		// Sum segment sizes
		var pageDataSize int
		for i := 0; i < segmentsCount; i++ {
			pageDataSize += int(data[offset+27+i])
		}
		// Jump to next page (27 bytes header + segment table + data size)
		offset += 27 + segmentsCount + pageDataSize
	}
	return float64(lastGranule) / 48000.0
}

// FormatSize converts bytes to human readable format (KB or MB)
func FormatSize(bytes int) string {
	const kb = 1024
	const mb = 1024 * 1024
	if bytes >= mb {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(mb))
	} else if bytes >= kb {
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(kb))
	}
	return fmt.Sprintf("%d Bytes", bytes)
}

// EncodePCMToWAV wraps raw 16-bit PCM samples into a valid WAV container
func EncodePCMToWAV(pcmData []byte, sampleRate int, channels int) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	bitsPerSample := 16
	byteRate := sampleRate * channels * (bitsPerSample / 8)
	blockAlign := channels * (bitsPerSample / 8)
	dataSize := len(pcmData)
	chunkSize := 36 + dataSize

	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(chunkSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // Subchunk1Size (16 for PCM)
	binary.LittleEndian.PutUint16(header[20:22], 1)  // AudioFormat (1 for PCM)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitsPerSample))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	return append(header, pcmData...)
}

// CalculateRMS computes the Root Mean Square energy level for 16-bit signed PCM audio samples
func CalculateRMS(pcmData []byte) float64 {
	if len(pcmData) < 2 {
		return 0
	}
	var sumSquares float64
	sampleCount := len(pcmData) / 2
	for i := 0; i < len(pcmData)-1; i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcmData[i : i+2]))
		sumSquares += float64(sample) * float64(sample)
	}
	if sampleCount == 0 {
		return 0
	}
	return math.Sqrt(sumSquares / float64(sampleCount))
}

// IsSilencePCM determines whether a PCM audio slice is silence based on an RMS threshold
func IsSilencePCM(pcmData []byte, thresholdRMS float64) bool {
	if thresholdRMS <= 0 {
		thresholdRMS = 250.0 // Default baseline silence threshold for 16-bit PCM
	}
	return CalculateRMS(pcmData) < thresholdRMS
}

// AudioChunkTracker tracks real-time Voice Activity Detection (VAD) and natural voice pause boundaries
type AudioChunkTracker struct {
	SampleRate               int
	Channels                 int
	ThresholdRMS             float64
	NoiseFloorRMS            float64
	SilenceDurationThreshold time.Duration
	MaxChunkDuration         time.Duration
	MinSpeechDuration        time.Duration
	hasActiveSpeech          bool
	consecutiveSilenceBytes  int
	totalChunkBytes          int
}

// NewAudioChunkTracker creates a new VAD audio chunk tracker with natural speech pause defaults
func NewAudioChunkTracker(sampleRate int, thresholdRMS float64) *AudioChunkTracker {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if thresholdRMS <= 0 {
		thresholdRMS = 280.0
	}
	return &AudioChunkTracker{
		SampleRate:               sampleRate,
		Channels:                 1,
		ThresholdRMS:             thresholdRMS,
		NoiseFloorRMS:            120.0,
		SilenceDurationThreshold: 450 * time.Millisecond,
		MaxChunkDuration:         10000 * time.Millisecond, // 10s max window before safety commit
		MinSpeechDuration:        500 * time.Millisecond,
	}
}

// ProcessChunk evaluates incoming PCM data, updates internal state, and signals if a natural voice break occurred
func (t *AudioChunkTracker) ProcessChunk(pcmChunk []byte) (isSilence bool, shouldCommit bool) {
	if len(pcmChunk) == 0 {
		return true, false
	}
	t.totalChunkBytes += len(pcmChunk)
	bytesPerSec := t.SampleRate * t.Channels * 2 // 16-bit = 2 bytes per sample

	rms := CalculateRMS(pcmChunk)
	dynamicThreshold := math.Max(t.ThresholdRMS, t.NoiseFloorRMS*2.0)
	isSilence = rms < dynamicThreshold

	if !isSilence {
		t.hasActiveSpeech = true
		t.consecutiveSilenceBytes = 0
	} else {
		t.consecutiveSilenceBytes += len(pcmChunk)
		if rms > 10.0 && rms < t.ThresholdRMS {
			t.NoiseFloorRMS = 0.95*t.NoiseFloorRMS + 0.05*rms
		}
	}

	chunkDuration := time.Duration(float64(t.totalChunkBytes)/float64(bytesPerSec)*1000) * time.Millisecond
	silenceDuration := time.Duration(float64(t.consecutiveSilenceBytes)/float64(bytesPerSec)*1000) * time.Millisecond

	// Trigger commit condition ONLY on true voice breaks/pauses:
	// Had active speech of >= 500ms, followed by >= 450ms silence pause
	if t.hasActiveSpeech && chunkDuration >= t.MinSpeechDuration && silenceDuration >= t.SilenceDurationThreshold {
		return isSilence, true
	}

	// Safety fallback if someone speaks for 10s without any pause
	if chunkDuration >= t.MaxChunkDuration {
		return isSilence, true
	}

	return isSilence, false
}

// Reset clears the active chunk VAD state for the next chunk
func (t *AudioChunkTracker) Reset() {
	t.hasActiveSpeech = false
	t.consecutiveSilenceBytes = 0
	t.totalChunkBytes = 0
}

// MergeTranscripts combines committed text with current active chunk transcript cleanly
func MergeTranscripts(committed, current string) string {
	cTrimmed := strings.TrimSpace(committed)
	curTrimmed := strings.TrimSpace(current)
	if cTrimmed == "" {
		return curTrimmed
	}
	if curTrimmed == "" {
		return cTrimmed
	}
	return cTrimmed + " " + curTrimmed
}

// EndsWithSentenceBoundary checks if a transcript ends with definitive sentence punctuation
func EndsWithSentenceBoundary(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) == 0 {
		return false
	}
	lastChar := t[len(t)-1]
	return lastChar == '.' || lastChar == '?' || lastChar == '!' || lastChar == ';'
}

// ExtractStabilizedText holds back tentative in-flight trailing words during active continuous speech
// to prevent premature output of half-baked phonetic hypotheses.
func ExtractStabilizedText(rawText string, isFinalOrPaused bool) string {
	raw := strings.TrimSpace(rawText)
	if raw == "" || isFinalOrPaused {
		return raw
	}
	words := strings.Fields(raw)
	if len(words) <= 1 {
		return ""
	}
	// Hold back the trailing 1 word while speech is still active
	return strings.Join(words[:len(words)-1], " ")
}

// GetPromptContext extracts the trailing character context (up to maxChars) from committed text
// aligned to a clean word boundary for ASR model prompt conditioning.
func GetPromptContext(committedText string, maxChars int) string {
	c := strings.TrimSpace(committedText)
	if maxChars <= 0 {
		maxChars = 150
	}
	if len(c) <= maxChars {
		return c
	}
	sub := c[len(c)-maxChars:]
	if idx := strings.Index(sub, " "); idx != -1 {
		return strings.TrimSpace(sub[idx+1:])
	}
	return strings.TrimSpace(sub)
}
