package utils

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"
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

var continuationWords = map[string]bool{
	// Coordinating and subordinating conjunctions
	"and": true, "but": true, "or": true, "nor": true, "so": true, "because": true,
	"which": true, "that": true, "while": true, "as": true, "although": true,
	"since": true, "though": true, "unless": true, "whereas": true, "if": true,
	"when": true, "where": true, "after": true, "before": true,

	// Prepositions and continuation particles
	"to": true, "into": true, "onto": true, "for": true, "from": true, "with": true,
	"without": true, "about": true, "between": true, "through": true, "under": true,
	"over": true, "by": true, "at": true, "in": true, "on": true, "of": true,
	"out": true, "off": true, "up": true, "down": true,

	// Determiners, pronouns, and adverbs continuing a clause
	"my": true, "your": true, "our": true, "their": true, "his": true, "her": true,
	"its": true, "the": true, "a": true, "an": true, "this": true, "these": true,
	"those": true, "me": true, "him": true, "them": true, "us": true, "you": true,
	"also": true, "even": true, "just": true, "only": true, "already": true,
	"still": true, "then": true, "too": true, "automatically": true, "completely": true,
	"really": true, "actually": true, "simply": true,

	// Auxiliary / modal verbs
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"being": true, "have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "can": true, "could": true, "will": true, "would": true, "shall": true,
	"should": true, "may": true, "might": true, "must": true,
}

var incompleteEndings = map[string]bool{
	"to": true, "into": true, "onto": true, "for": true, "from": true, "with": true,
	"without": true, "about": true, "at": true, "in": true, "on": true, "of": true,
	"by": true, "and": true, "or": true, "but": true, "that": true, "which": true,
	"because": true, "as": true, "while": true, "organizes": true, "captures": true,
	"is": true, "are": true, "was": true, "were": true, "have": true, "has": true,
	"had": true, "makes": true, "takes": true, "gives": true, "includes": true,
	"requires": true, "contains": true, "shows": true, "connect": true, "connects": true,
	"integrate": true, "integrates": true, "build": true, "builds": true,
	"create": true, "creates": true, "handle": true, "handles": true,
	"support": true, "supports": true, "provide": true, "provides": true,
	"allow": true, "allows": true, "need": true, "needs": true,
	"want": true, "wants": true, "like": true, "likes": true,
}

// isProperOrAcronym checks if a word should preserve uppercase (e.g. "I", "I'm", "API", "SpeekTo.Me")
func isProperOrAcronym(word string) bool {
	if word == "I" || strings.HasPrefix(word, "I'") || word == "I’m" || word == "I’ve" || word == "I’ll" || word == "I’d" {
		return true
	}
	// Check if all uppercase acronym (len >= 2)
	allUpper := true
	hasLetter := false
	for _, r := range word {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				allUpper = false
				break
			}
		}
	}
	if hasLetter && allUpper && len(word) >= 2 {
		return true
	}
	// Check if PascalCase or contains internal dots
	if strings.Contains(word, ".") || strings.Contains(word, "-") {
		return true
	}
	return false
}

// MergeTranscripts combines committed text with current active chunk transcript cleanly using seam stitching and overlap deduplication
func MergeTranscripts(committed, current string) string {
	cTrimmed := strings.TrimSpace(committed)
	curTrimmed := strings.TrimSpace(current)
	if cTrimmed == "" {
		return curTrimmed
	}
	if curTrimmed == "" {
		return cTrimmed
	}

	cWords := strings.Fields(cTrimmed)
	curWords := strings.Fields(curTrimmed)
	if len(curWords) == 0 {
		return cTrimmed
	}
	if len(cWords) == 0 {
		return curTrimmed
	}

	// 1. Check for suffix-prefix word overlap between committed text and new chunk
	maxOverlap := len(curWords)
	if len(cWords) < maxOverlap {
		maxOverlap = len(cWords)
	}

	overlapCount := 0
	for k := maxOverlap; k >= 1; k-- {
		cTail := cWords[len(cWords)-k:]
		curHead := curWords[:k]
		match := true
		for i := 0; i < k; i++ {
			cleanCTail := strings.ToLower(strings.Trim(cTail[i], ".,!?:;\"'()[]{}"))
			cleanCurHead := strings.ToLower(strings.Trim(curHead[i], ".,!?:;\"'()[]{}"))
			if cleanCTail != cleanCurHead {
				match = false
				break
			}
		}
		if match {
			overlapCount = k
			break
		}
	}

	// If overlap found, slice off the duplicated prefix words from the new chunk
	if overlapCount > 0 {
		curWords = curWords[overlapCount:]
		if len(curWords) == 0 {
			return cTrimmed
		}
		curTrimmed = strings.Join(curWords, " ")
	}

	// Extract first word of current chunk
	firstWordClean := strings.ToLower(strings.Trim(curWords[0], ".,!?:;\"'()[]{}"))

	// Extract last word of committed text
	lastWordClean := strings.ToLower(strings.Trim(cWords[len(cWords)-1], ".,!?:;\"'()[]{}"))

	isContinuation := continuationWords[firstWordClean]
	isIncomplete := incompleteEndings[lastWordClean]

	// If the seam represents a continuation or connects an incomplete predicate
	if isContinuation || isIncomplete {
		// Strip trailing punctuation from committed text
		cTrimmed = strings.TrimRight(cTrimmed, ".,;:!?")

		// Adjust casing of first word if not a proper noun / acronym
		if !isProperOrAcronym(curWords[0]) && len(curWords[0]) > 0 {
			runes := []rune(curWords[0])
			runes[0] = unicode.ToLower(runes[0])
			curWords[0] = string(runes)
			curTrimmed = strings.Join(curWords, " ")
		}
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
