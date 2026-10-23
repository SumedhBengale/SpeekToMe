package audiospool

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

var ErrInvalidAudio = errors.New("recording is not a valid Ogg Opus stream")

var crcTable = func() [256]uint32 {
	var table [256]uint32
	for i := range table {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}()

func oggCRC(crc uint32, data []byte) uint32 {
	for _, b := range data {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^b]
	}
	return crc
}

// OggOpusDuration reads a whole Ogg Opus stream, verifying every page
// checksum, and returns its playable duration in seconds. It replaces probing
// a plaintext file with ffprobe, so recordings never need to touch disk.
func OggOpusDuration(r io.Reader) (float64, error) {
	in := bufio.NewReaderSize(r, 64<<10)
	header := make([]byte, 27)
	var serial uint32
	var preSkip uint16
	var granule uint64
	pages := 0
	for {
		if _, err := io.ReadFull(in, header); err != nil {
			if err == io.EOF && pages > 0 {
				break
			}
			return 0, ErrInvalidAudio
		}
		if string(header[:4]) != "OggS" || header[4] != 0 {
			return 0, ErrInvalidAudio
		}
		segments := make([]byte, header[26])
		if _, err := io.ReadFull(in, segments); err != nil {
			return 0, ErrInvalidAudio
		}
		size := 0
		for _, s := range segments {
			size += int(s)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(in, body); err != nil {
			return 0, ErrInvalidAudio
		}
		expected := binary.LittleEndian.Uint32(header[22:26])
		check := append([]byte(nil), header...)
		check[22], check[23], check[24], check[25] = 0, 0, 0, 0
		crc := oggCRC(0, check)
		crc = oggCRC(crc, segments)
		crc = oggCRC(crc, body)
		if crc != expected {
			return 0, ErrInvalidAudio
		}
		pageSerial := binary.LittleEndian.Uint32(header[14:18])
		if pages == 0 {
			if len(body) < 19 || string(body[:8]) != "OpusHead" {
				return 0, ErrInvalidAudio
			}
			serial = pageSerial
			preSkip = binary.LittleEndian.Uint16(body[10:12])
		} else if pageSerial != serial {
			return 0, ErrInvalidAudio
		}
		// A granule of all ones marks a page with no completed packet.
		if position := binary.LittleEndian.Uint64(header[6:14]); position != ^uint64(0) && position > granule {
			granule = position
		}
		pages++
	}
	if granule <= uint64(preSkip) {
		return 0, ErrInvalidAudio
	}
	return float64(granule-uint64(preSkip)) / 48000, nil
}
