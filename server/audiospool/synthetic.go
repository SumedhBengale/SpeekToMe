package audiospool

import "encoding/binary"

func oggPage(serial uint32, sequence uint32, granule uint64, flags byte, body []byte) []byte {
	header := make([]byte, 27)
	copy(header, "OggS")
	header[5] = flags
	binary.LittleEndian.PutUint64(header[6:14], granule)
	binary.LittleEndian.PutUint32(header[14:18], serial)
	binary.LittleEndian.PutUint32(header[18:22], sequence)
	var lacing []byte
	remaining := len(body)
	for remaining >= 255 {
		lacing = append(lacing, 255)
		remaining -= 255
	}
	lacing = append(lacing, byte(remaining))
	header[26] = byte(len(lacing))
	page := append(append(header, lacing...), body...)
	crc := oggCRC(0, page)
	binary.LittleEndian.PutUint32(page[22:26], crc)
	return page
}

// SyntheticOggOpus builds a minimal valid Ogg Opus page sequence of the given
// duration. Tests use it in place of recorded audio.
func SyntheticOggOpus(seconds float64) []byte {
	head := []byte("OpusHead\x01\x01\x38\x01\x80\x3e\x00\x00\x00\x00\x00")
	tags := []byte("OpusTags\x00\x00\x00\x00\x00\x00\x00\x00")
	stream := oggPage(9, 0, 0, 2, head)
	stream = append(stream, oggPage(9, 1, 0, 0, tags)...)
	stream = append(stream, oggPage(9, 2, uint64(seconds*48000)+312, 4, []byte{0xfc, 0xff, 0xfe})...)
	return stream
}
