package transport

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func pair(t *testing.T) (*Identity, *Session, *Session) {
	t.Helper()
	worker, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	controller, header, err := Dial(worker.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := worker.Accept(header)
	if err != nil {
		t.Fatal(err)
	}
	return worker, controller, accepted
}

func TestMessagesRoundTripInOrderBothWays(t *testing.T) {
	_, controller, worker := pair(t)
	for i, msg := range [][]byte{[]byte(`{"type":"session.update"}`), make([]byte, 17920), {}} {
		kind := 2
		if i == 0 {
			kind = 1
		}
		gotKind, got, err := worker.OpenMessage(controller.SealMessage(kind, msg))
		if err != nil || gotKind != kind || !bytes.Equal(got, msg) {
			t.Fatal(i, err)
		}
		gotKind, got, err = controller.OpenMessage(worker.SealMessage(kind, msg))
		if err != nil || gotKind != kind || !bytes.Equal(got, msg) {
			t.Fatal("reply", i, err)
		}
	}
}

func TestMessagesRejectReplayReorderTamperKindAndReflection(t *testing.T) {
	_, controller, worker := pair(t)
	first, second := controller.SealMessage(2, []byte("one")), controller.SealMessage(2, []byte("two"))
	if _, _, err := worker.OpenMessage(second); err == nil {
		t.Fatal("accepted reordered message")
	}
	if _, _, err := worker.OpenMessage(first); err != nil {
		t.Fatal(err)
	}
	if _, _, err := worker.OpenMessage(first); err == nil {
		t.Fatal("accepted replay")
	}
	third := controller.SealMessage(2, []byte("three"))
	if _, _, err := worker.OpenMessage(second); err != nil {
		t.Fatal(err)
	}
	flipped := append([]byte{}, third...)
	flipped[len(flipped)-1] ^= 1
	if _, _, err := worker.OpenMessage(flipped); err == nil {
		t.Fatal("accepted tampered message")
	}
	relabelled := append([]byte{1}, third[1:]...) // binary relabelled as text
	if _, _, err := worker.OpenMessage(relabelled); err == nil {
		t.Fatal("accepted changed kind")
	}
	// A message cannot be bounced back to its sender as if it came from the peer.
	if _, _, err := controller.OpenMessage(controller.SealMessage(2, []byte("x"))); err == nil {
		t.Fatal("accepted reflected message")
	}
	_, other, _ := pair(t)
	if _, _, err := worker.OpenMessage(other.SealMessage(2, []byte("x"))); err == nil {
		t.Fatal("accepted another session's message")
	}
}

func TestStaleAndInvalidSessionHeaders(t *testing.T) {
	old, _ := NewIdentity()
	current, _ := NewIdentity()
	_, header, _ := Dial(old.PublicKey())
	if _, err := current.Accept(header); !errors.Is(err, ErrStaleKey) {
		t.Fatal("expected stale key", err)
	}
	for _, bad := range []string{"", "v1", "v2 x y", "v1 " + current.KeyID() + " !!!", "v1 " + current.KeyID() + " AAAA"} {
		if _, err := current.Accept(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestRequestMetadataRoundTrip(t *testing.T) {
	_, controller, worker := pair(t)
	sealed := controller.SealRequest(Request{Query: "contexts=Kubernetes&language=en", ContentLength: 42})
	got, err := worker.OpenRequest(sealed)
	if err != nil || got.Query != "contexts=Kubernetes&language=en" || got.ContentLength != 42 {
		t.Fatal(got, err)
	}
	if _, err := worker.OpenRequest(sealed); err == nil {
		t.Fatal("accepted replayed request metadata")
	}
}

func seal(t *testing.T, s *Session, plain []byte, chunk int) []byte {
	t.Helper()
	var out bytes.Buffer
	w := s.BodyWriter(&out)
	for len(plain) > 0 {
		n := min(chunk, len(plain))
		if _, err := w.Write(plain[:n]); err != nil {
			t.Fatal(err)
		}
		plain = plain[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestBodiesAcrossRecordBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, record - 1, record, record + 1, 3*record + 17} {
		_, controller, worker := pair(t)
		plain := make([]byte, size)
		rand.Read(plain)
		sealed := seal(t, controller, plain, 7000)
		got, err := io.ReadAll(worker.OpenBody(bytes.NewReader(sealed)))
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatal(size, err)
		}
		reply, err := io.ReadAll(controller.OpenBody(worker.SealBody(bytes.NewReader(plain))))
		if err != nil || !bytes.Equal(reply, plain) {
			t.Fatal("reply", size, err)
		}
	}
}

func TestBodiesRejectTruncationTamperingAndTrailingData(t *testing.T) {
	plain := make([]byte, 2*record+5)
	for name, damage := range map[string]func([]byte) []byte{
		"truncated to whole records": func(b []byte) []byte { return b[:4+int(binary.BigEndian.Uint32(b))] },
		"cut mid-record":             func(b []byte) []byte { return b[:len(b)-5] },
		"tampered":                   func(b []byte) []byte { b[len(b)-3] ^= 1; return b },
		"trailing bytes":             func(b []byte) []byte { return append(b, 0) },
		"empty":                      func([]byte) []byte { return nil },
	} {
		_, controller, worker := pair(t)
		bad := damage(seal(t, controller, plain, record))
		if _, err := io.ReadAll(worker.OpenBody(bytes.NewReader(bad))); err == nil {
			t.Error("accepted", name)
		}
	}
}

func TestSpoolIsCiphertextAndReplayable(t *testing.T) {
	spool, err := NewSpool()
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("private meeting audio "), 10000)
	var sealed bytes.Buffer
	w := spool.Writer(&sealed)
	w.Write(plain)
	w.Close()
	if bytes.Contains(sealed.Bytes(), []byte("private meeting audio")) {
		t.Fatal("spool holds plaintext")
	}
	for attempt := range 2 {
		got, err := io.ReadAll(spool.Reader(bytes.NewReader(sealed.Bytes())))
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatal(attempt, err)
		}
	}
	spool.Forget()
	if _, err := io.ReadAll(spool.Reader(bytes.NewReader(sealed.Bytes()))); err == nil {
		t.Fatal("forgotten spool still readable")
	}
}
