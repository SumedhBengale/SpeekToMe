package handlers

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"time"
)

// Diarization envelope v1: the coordinator seals each recording to the external
// diarizer's long-term X25519 public key, so the provider's input store, proxy
// and logs only hold ciphertext. Mirrors deployment/modal-diarization/envelope.py.
//
//	request  = "SDE1" | key_id[8] | ephemeral_pub[32] | issued_at u64 | job_id[16]
//	response = "SDR1" | job_id[16]
//	record   = u32 length | AES-256-GCM(<= 1 MiB), nonce = 0^4 | u64 index,
//	           AAD = header | u64 index | final flag (last record = 1)
const (
	envelopeRecord         = 1 << 20
	envelopeTag            = 16
	envelopeRequestHeader  = 68
	envelopeResponseHeader = 20
	envelopeMaxRecords     = 4096
)

var errEnvelope = errors.New("diarization envelope rejected")

type diarizationRecipient struct {
	key *ecdh.PublicKey
	id  [8]byte
}

func parseDiarizationRecipient(encoded string) (*diarizationRecipient, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	key, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, err
	}
	r := &diarizationRecipient{key: key}
	sum := sha256.Sum256(append([]byte("speektome-diarization-key/v1"), raw...))
	copy(r.id[:], sum[:8])
	return r, nil
}

func envelopeKeys(shared, ephemeral, recipient []byte) (request, response []byte, err error) {
	salt := append(append([]byte{}, ephemeral...), recipient...)
	if request, err = hkdf.Key(sha256.New, shared, salt, "speektome-diarization/v1/request", 32); err != nil {
		return nil, nil, err
	}
	response, err = hkdf.Key(sha256.New, shared, salt, "speektome-diarization/v1/response", 32)
	return request, response, err
}

func envelopeGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func envelopeNonce(index uint64) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], index)
	return nonce
}

func envelopeAAD(header []byte, index uint64, final bool) []byte {
	aad := binary.BigEndian.AppendUint64(append([]byte{}, header...), index)
	if final {
		return append(aad, 1)
	}
	return append(aad, 0)
}

func envelopeRecords(size int64) int64 {
	return max(1, (size+envelopeRecord-1)/envelopeRecord)
}

// sealedRequest streams a sealed recording without buffering it. Its length is
// known up front, so the endpoint can reject oversized uploads before reading.
type sealedRequest struct {
	src           io.Reader
	aead          cipher.AEAD
	header        []byte
	pending       bytes.Buffer
	index, total  uint64
	remaining     int64
	plain         []byte
	size          int64
	headerWritten bool
}

func sealDiarizationRequest(src io.Reader, size int64, to *diarizationRecipient, jobID string, now time.Time, ephemeral *ecdh.PrivateKey) (*sealedRequest, []byte, error) {
	id, err := hex.DecodeString(jobID)
	if err != nil || len(id) != 16 || size < 0 {
		return nil, nil, errors.New("invalid diarization job identity")
	}
	if ephemeral == nil {
		if ephemeral, err = ecdh.X25519().GenerateKey(rand.Reader); err != nil {
			return nil, nil, err
		}
	}
	shared, err := ephemeral.ECDH(to.key)
	if err != nil {
		return nil, nil, err
	}
	ephemeralPub := ephemeral.PublicKey().Bytes()
	requestKey, responseKey, err := envelopeKeys(shared, ephemeralPub, to.key.Bytes())
	if err != nil {
		return nil, nil, err
	}
	aead, err := envelopeGCM(requestKey)
	if err != nil {
		return nil, nil, err
	}
	header := append([]byte("SDE1"), to.id[:]...)
	header = append(header, ephemeralPub...)
	header = binary.BigEndian.AppendUint64(header, uint64(now.Unix()))
	header = append(header, id...)
	records := envelopeRecords(size)
	return &sealedRequest{
		src: src, aead: aead, header: header, total: uint64(records), remaining: size,
		plain: make([]byte, envelopeRecord), size: envelopeRequestHeader + records*(4+envelopeTag) + size,
	}, responseKey, nil
}

func (s *sealedRequest) Len() int64 { return s.size }

func (s *sealedRequest) Read(p []byte) (int, error) {
	for s.pending.Len() == 0 {
		if !s.headerWritten {
			s.pending.Write(s.header)
			s.headerWritten = true
			break
		}
		if s.index == s.total {
			return 0, io.EOF
		}
		n := min(int64(envelopeRecord), s.remaining)
		if _, err := io.ReadFull(s.src, s.plain[:n]); err != nil {
			return 0, errors.New("diarization audio shorter than declared")
		}
		s.remaining -= n
		final := s.index == s.total-1
		sealed := s.aead.Seal(nil, envelopeNonce(s.index), s.plain[:n], envelopeAAD(s.header, s.index, final))
		s.pending.Write(binary.BigEndian.AppendUint32(nil, uint32(len(sealed))))
		s.pending.Write(sealed)
		s.index++
	}
	return s.pending.Read(p)
}

func openEnvelopeRecords(aead cipher.AEAD, header, body []byte) ([]byte, error) {
	var plain []byte
	final := false
	for index := uint64(0); len(body) > 0; index++ {
		if final || index >= envelopeMaxRecords || len(body) < 4 {
			return nil, errEnvelope
		}
		size := int(binary.BigEndian.Uint32(body))
		body = body[4:]
		if size < envelopeTag || size > envelopeRecord+envelopeTag || size > len(body) {
			return nil, errEnvelope
		}
		final = size == len(body)
		opened, err := aead.Open(nil, envelopeNonce(index), body[:size], envelopeAAD(header, index, final))
		if err != nil {
			return nil, errEnvelope
		}
		plain = append(plain, opened...)
		body = body[size:]
	}
	if !final {
		return nil, errEnvelope
	}
	return plain, nil
}

func openDiarizationResponse(envelope, responseKey []byte, jobID string) ([]byte, error) {
	id, err := hex.DecodeString(jobID)
	if err != nil || len(id) != 16 {
		return nil, errEnvelope
	}
	header := append([]byte("SDR1"), id...)
	if len(envelope) < envelopeResponseHeader || !bytes.Equal(envelope[:envelopeResponseHeader], header) {
		return nil, errEnvelope
	}
	aead, err := envelopeGCM(responseKey)
	if err != nil {
		return nil, err
	}
	return openEnvelopeRecords(aead, header, envelope[envelopeResponseHeader:])
}
