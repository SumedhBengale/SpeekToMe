// Package transport is the application-layer encryption between the fleet
// controller and inference workers, inside the pinned-TLS connection.
//
// Each worker gateway creates an X25519 identity at startup and keeps it only in
// memory; its public key reaches the controller through the authenticated,
// certificate-pinned /worker/metadata endpoint. For every WebSocket session or
// HTTP request the controller derives fresh keys from an ephemeral key pair, so
// TLS terminators and proxies on the worker (nginx) only ever carry ciphertext,
// and plaintext exists solely in the gateway's memory while it is processed.
//
//	session header  X-SpeekToMe-Transport: v1 <key id hex> <ephemeral public key, base64>
//	keys            HKDF-SHA256(X25519, salt = ephemeral | worker public key,
//	                info = "speektome-transport/v1/" + "controller" | "worker")
//	nonce           purpose byte | 0 0 0 | u64 sequence (per direction and purpose)
//	AAD             "SDT1" | direction | purpose | u64 sequence | message kind / final flag
//
// WebSocket messages are strictly ordered, so a replayed, dropped, reordered or
// cross-session message fails authentication. Bodies are 64 KiB records whose
// last record carries the final flag, so truncation is detected too.
package transport

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

const (
	// SessionHeader carries the ephemeral key that opens a session.
	SessionHeader = "X-SpeekToMe-Transport"
	// RequestHeader carries sealed request metadata (query string, plaintext length).
	RequestHeader = "X-SpeekToMe-Transport-Request"
	// ResponseHeader marks a response whose body is sealed.
	ResponseHeader = "X-SpeekToMe-Transport-Response"
	// Schema is advertised by workers in /worker/metadata.
	Schema = 1

	version     = "v1"
	record      = 64 << 10
	tag         = 16
	maxMessage  = 8 << 20
	purposeMsg  = 1
	purposeBody = 2
	purposeMeta = 3
)

// ErrRejected is returned for any message that fails to authenticate; callers
// must not reveal more than that.
var ErrRejected = errors.New("transport message rejected")

// Identity is a worker's in-memory key pair. It is never written to disk; a new
// one is created whenever the gateway process starts.
type Identity struct {
	key *ecdh.PrivateKey
	id  string
}

func NewIdentity() (*Identity, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{key: key, id: keyID(key.PublicKey().Bytes())}, nil
}

// PublicKey is the base64 X25519 public key advertised in worker metadata.
func (i *Identity) PublicKey() string {
	return base64.StdEncoding.EncodeToString(i.key.PublicKey().Bytes())
}

// KeyID identifies the advertised key, so a stale controller view is detected.
func (i *Identity) KeyID() string { return i.id }

func keyID(public []byte) string {
	sum := sha256.Sum256(append([]byte("speektome-transport-key/v1"), public...))
	return hex.EncodeToString(sum[:8])
}

// Request is the sealed metadata of an HTTP request: everything user-specific
// that would otherwise travel outside the body (query strings carry vocabulary).
type Request struct {
	Query         string `json:"query"`
	ContentLength int64  `json:"content_length"`
}

// Session holds the directional keys of one WebSocket session or HTTP exchange.
// Message sealing and opening are each safe for one writer and one reader.
type Session struct {
	send, recv       cipher.AEAD
	sendDir, recvDir byte
	sendMu, recvMu   sync.Mutex
	sendSeq, recvSeq [4]uint64 // indexed by purpose
}

func direction(controller bool) byte {
	if controller {
		return 'C'
	}
	return 'W'
}

func newSession(shared, ephemeral, worker []byte, controller bool) (*Session, error) {
	salt := append(append([]byte{}, ephemeral...), worker...)
	derive := func(label string) (cipher.AEAD, error) {
		key, err := hkdf.Key(sha256.New, shared, salt, "speektome-transport/v1/"+label, 32)
		if err != nil {
			return nil, err
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	}
	fromController, err := derive("controller")
	if err != nil {
		return nil, err
	}
	fromWorker, err := derive("worker")
	if err != nil {
		return nil, err
	}
	s := &Session{send: fromWorker, recv: fromController, sendDir: 'W', recvDir: 'C'}
	if controller {
		s.send, s.recv, s.sendDir, s.recvDir = fromController, fromWorker, 'C', 'W'
	}
	return s, nil
}

// Dial starts a session to a worker advertising publicKey and returns the value
// of SessionHeader to send with the request.
func Dial(publicKey string) (*Session, string, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return nil, "", err
	}
	worker, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, "", err
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	shared, err := ephemeral.ECDH(worker)
	if err != nil {
		return nil, "", err
	}
	s, err := newSession(shared, ephemeral.PublicKey().Bytes(), raw, true)
	if err != nil {
		return nil, "", err
	}
	header := strings.Join([]string{version, keyID(raw), base64.StdEncoding.EncodeToString(ephemeral.PublicKey().Bytes())}, " ")
	return s, header, nil
}

// ErrStaleKey means the controller sealed to a previous identity of this worker
// (the gateway restarted); the request was not processed and can be retried.
var ErrStaleKey = errors.New("transport key is not this worker's current key")

// Accept opens the session described by a SessionHeader value.
func (i *Identity) Accept(header string) (*Session, error) {
	parts := strings.Fields(header)
	if len(parts) != 3 || parts[0] != version {
		return nil, ErrRejected
	}
	if parts[1] != i.id {
		return nil, ErrStaleKey
	}
	raw, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrRejected
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, ErrRejected
	}
	shared, err := i.key.ECDH(ephemeral)
	if err != nil {
		return nil, ErrRejected
	}
	return newSession(shared, raw, i.key.PublicKey().Bytes(), false)
}

func nonce(purpose byte, seq uint64) []byte {
	n := make([]byte, 12)
	n[0] = purpose
	binary.BigEndian.PutUint64(n[4:], seq)
	return n
}

func aad(dir, purpose byte, seq uint64, extra byte) []byte {
	a := []byte{'S', 'D', 'T', '1', dir, purpose}
	a = binary.BigEndian.AppendUint64(a, seq)
	return append(a, extra)
}

func (s *Session) seal(purpose, extra byte, plain []byte) []byte {
	seq := s.sendSeq[purpose]
	s.sendSeq[purpose]++
	return s.send.Seal(nil, nonce(purpose, seq), plain, aad(s.sendDir, purpose, seq, extra))
}

func (s *Session) open(purpose, extra byte, sealed []byte) ([]byte, error) {
	seq := s.recvSeq[purpose]
	plain, err := s.recv.Open(nil, nonce(purpose, seq), sealed, aad(s.recvDir, purpose, seq, extra))
	if err != nil {
		return nil, ErrRejected
	}
	s.recvSeq[purpose]++
	return plain, nil
}

// SealMessage seals one WebSocket message of the given kind (text or binary);
// the result is sent as a binary message.
func (s *Session) SealMessage(kind int, plain []byte) []byte {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return append([]byte{byte(kind)}, s.seal(purposeMsg, byte(kind), plain)...)
}

// OpenMessage reverses SealMessage, returning the original message kind.
func (s *Session) OpenMessage(sealed []byte) (int, []byte, error) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if len(sealed) < 1+tag || len(sealed) > maxMessage+1+tag {
		return 0, nil, ErrRejected
	}
	plain, err := s.open(purposeMsg, sealed[0], sealed[1:])
	return int(sealed[0]), plain, err
}

// SealRequest seals request metadata for RequestHeader.
func (s *Session) SealRequest(r Request) string {
	data, _ := json.Marshal(r)
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return base64.StdEncoding.EncodeToString(s.seal(purposeMeta, 0, data))
}

// OpenRequest reverses SealRequest.
func (s *Session) OpenRequest(header string) (Request, error) {
	var r Request
	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return r, ErrRejected
	}
	s.recvMu.Lock()
	plain, err := s.open(purposeMeta, 0, raw)
	s.recvMu.Unlock()
	if err != nil || json.Unmarshal(plain, &r) != nil || r.ContentLength < -1 {
		return Request{}, ErrRejected
	}
	return r, nil
}

// SealBody streams src as sealed records. It does not buffer more than one record.
func (s *Session) SealBody(src io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		w := s.BodyWriter(pw)
		_, err := io.Copy(w, src)
		if err == nil {
			err = w.Close()
		}
		pw.CloseWithError(err)
	}()
	return pr
}

// BodyWriter seals everything written to it; Close emits the final record and
// must be called exactly once, after which nothing more may be written.
func (s *Session) BodyWriter(dst io.Writer) *BodyWriter {
	return &BodyWriter{s: s, dst: dst, buf: make([]byte, 0, record)}
}

type BodyWriter struct {
	s      *Session
	dst    io.Writer
	buf    []byte
	closed bool
}

func (b *BodyWriter) Write(p []byte) (int, error) {
	if b.closed {
		return 0, errors.New("transport body already closed")
	}
	n := len(p)
	for len(p) > 0 {
		// Hold back a full record: it can only be emitted once more data proves it isn't final.
		if len(b.buf) == record {
			if err := b.emit(false); err != nil {
				return n - len(p), err
			}
		}
		take := min(record-len(b.buf), len(p))
		b.buf = append(b.buf, p[:take]...)
		p = p[take:]
	}
	return n, nil
}

// Flush emits buffered data early (for streamed responses); a later empty final
// record still terminates the body.
func (b *BodyWriter) Flush() error {
	if len(b.buf) == 0 || b.closed {
		return nil
	}
	return b.emit(false)
}

func (b *BodyWriter) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	err := b.emit(true)
	clear(b.buf[:cap(b.buf)])
	return err
}

func (b *BodyWriter) emit(final bool) error {
	var flag byte
	if final {
		flag = 1
	}
	b.s.sendMu.Lock()
	sealed := b.s.seal(purposeBody, flag, b.buf)
	b.s.sendMu.Unlock()
	clear(b.buf)
	b.buf = b.buf[:0]
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(sealed)))
	if _, err := b.dst.Write(frame); err != nil {
		return err
	}
	_, err := b.dst.Write(sealed)
	return err
}

// OpenBody returns a reader of the plaintext of a sealed body. Reading past the
// end without a final record, or any tampered record, fails with ErrRejected.
func (s *Session) OpenBody(src io.Reader) io.Reader {
	return &bodyReader{s: s, src: bufio.NewReaderSize(src, 4+record+tag)}
}

type bodyReader struct {
	s       *Session
	src     *bufio.Reader
	plain   []byte
	done    bool
	err     error
	scratch []byte
}

func (b *bodyReader) Read(p []byte) (int, error) {
	for len(b.plain) == 0 {
		if b.err != nil {
			return 0, b.err
		}
		if b.done {
			// Anything after the final record is an attack or a framing bug.
			if _, err := b.src.ReadByte(); err != io.EOF {
				b.err = ErrRejected
				return 0, b.err
			}
			return 0, io.EOF
		}
		b.err = b.next()
	}
	n := copy(p, b.plain)
	clear(b.plain[:n])
	b.plain = b.plain[n:]
	return n, nil
}

func (b *bodyReader) next() error {
	var size [4]byte
	if _, err := io.ReadFull(b.src, size[:]); err != nil {
		return ErrRejected
	}
	n := int(binary.BigEndian.Uint32(size[:]))
	if n < tag || n > record+tag {
		return ErrRejected
	}
	if cap(b.scratch) < n {
		b.scratch = make([]byte, n)
	}
	sealed := b.scratch[:n]
	if _, err := io.ReadFull(b.src, sealed); err != nil {
		return ErrRejected
	}
	b.s.recvMu.Lock()
	defer b.s.recvMu.Unlock()
	// The sender marks only the last record final; try the common case first.
	seq := b.s.recvSeq[purposeBody]
	for _, flag := range []byte{0, 1} {
		plain, err := b.s.recv.Open(nil, nonce(purposeBody, seq), sealed, aad(b.s.recvDir, purposeBody, seq, flag))
		if err == nil {
			b.s.recvSeq[purposeBody]++
			b.plain, b.done = plain, flag == 1
			return nil
		}
	}
	return ErrRejected
}

// Spool keeps a request body encrypted at rest under a key held only in memory,
// so a body spooled for retries never touches disk in plaintext. Each Writer or
// Reader starts a fresh sequence, so the spool can be replayed several times.
type Spool struct{ key []byte }

func NewSpool() (*Spool, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &Spool{key: key}, nil
}

func (s *Spool) session() *Session {
	session, _ := newSession(s.key, nil, nil, true) // fixed-size inputs cannot fail
	session.recv, session.recvDir = session.send, session.sendDir
	return session
}

// Writer seals a body into dst; Close must be called to finish it.
func (s *Spool) Writer(dst io.Writer) *BodyWriter { return s.session().BodyWriter(dst) }

// Reader opens a body written by Writer, from its start.
func (s *Spool) Reader(src io.Reader) io.Reader { return s.session().OpenBody(src) }

// Forget drops the key, making the spool unreadable.
func (s *Spool) Forget() { clear(s.key) }
