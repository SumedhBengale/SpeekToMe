package utils

import (
	"encoding/binary"
	"fmt"
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
