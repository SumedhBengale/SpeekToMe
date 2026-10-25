package handlers

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Shared with deployment/modal-diarization/test_envelope.py, so the Go sealer
// and the Python opener (and the reverse for responses) agree byte for byte.
const (
	vectorJob           = "00112233445566778899aabbccddeeff"
	vectorIssued        = 1793000000
	vectorRequestSHA256 = "411033b3b93e2649acadd112a3275705fd86ca695f1e69b99a5410ee68141cb3"
	vectorResponseKey   = "65cbd8a322004140c4ac54b4dde2f46173c377bc1a22a7698f99f04819791963"
	vectorResponse      = "5344523100112233445566778899aabbccddeeff000000431d98fc4bbc860af0b197a21efcc58edc996b9a5c872691c825c22d4f" +
		"72f46d647069344027b9b1c42aafb4d621d071820f7323907b6b3b8b450ccb7fb0e44d1ffc733b"
	vectorResult = `{"segments":[{"start":0.5,"end":1.25,"speaker":1}]}`
)

func sequence(start, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(start + i)
	}
	return b
}

func vectorKeys(t *testing.T) (*ecdh.PrivateKey, *diarizationRecipient, *ecdh.PrivateKey) {
	t.Helper()
	recipient, err := ecdh.X25519().NewPrivateKey(sequence(0, 32))
	if err != nil {
		t.Fatal(err)
	}
	ephemeral, err := ecdh.X25519().NewPrivateKey(sequence(32, 32))
	if err != nil {
		t.Fatal(err)
	}
	to, err := parseDiarizationRecipient(base64.StdEncoding.EncodeToString(recipient.PublicKey().Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return recipient, to, ephemeral
}

func vectorPlain() []byte {
	plain := make([]byte, envelopeRecord+100)
	for i := range plain {
		plain[i] = byte((i*7 + 3) % 251)
	}
	return plain
}

// openTestRequest is the receiving side, which in production is envelope.py.
func openTestRequest(t *testing.T, recipient *ecdh.PrivateKey, envelope []byte) ([]byte, string, []byte) {
	t.Helper()
	if len(envelope) < envelopeRequestHeader || string(envelope[:4]) != "SDE1" {
		t.Fatal("not a request envelope")
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(envelope[12:44])
	if err != nil {
		t.Fatal(err)
	}
	shared, err := recipient.ECDH(ephemeral)
	if err != nil {
		t.Fatal(err)
	}
	requestKey, responseKey, err := envelopeKeys(shared, envelope[12:44], recipient.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	aead, err := envelopeGCM(requestKey)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := openEnvelopeRecords(aead, envelope[:envelopeRequestHeader], envelope[envelopeRequestHeader:])
	if err != nil {
		t.Fatal(err)
	}
	return plain, hex.EncodeToString(envelope[52:68]), responseKey
}

func sealTestResponse(t *testing.T, plain, responseKey []byte, job string) []byte {
	t.Helper()
	id, _ := hex.DecodeString(job)
	header := append([]byte("SDR1"), id...)
	aead, err := envelopeGCM(responseKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed := aead.Seal(nil, envelopeNonce(0), plain, envelopeAAD(header, 0, true))
	out := append(header, byte(len(sealed)>>24), byte(len(sealed)>>16), byte(len(sealed)>>8), byte(len(sealed)))
	return append(out, sealed...)
}

func TestDiarizationEnvelopeMatchesPythonVector(t *testing.T) {
	recipient, to, ephemeral := vectorKeys(t)
	plain := vectorPlain()
	sealed, responseKey, err := sealDiarizationRequest(bytes.NewReader(plain), int64(len(plain)), to, vectorJob, time.Unix(vectorIssued, 0), ephemeral)
	if err != nil {
		t.Fatal(err)
	}
	request, err := io.ReadAll(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(request); hex.EncodeToString(sum[:]) != vectorRequestSHA256 || int64(len(request)) != sealed.Len() {
		t.Fatalf("request vector mismatch: %x len=%d declared=%d", sum, len(request), sealed.Len())
	}
	if hex.EncodeToString(responseKey) != vectorResponseKey {
		t.Fatal("response key mismatch")
	}
	opened, job, _ := openTestRequest(t, recipient, request)
	if !bytes.Equal(opened, plain) || job != vectorJob {
		t.Fatal("request round trip failed")
	}
	response, _ := hex.DecodeString(vectorResponse)
	result, err := openDiarizationResponse(response, responseKey, vectorJob)
	if err != nil || string(result) != vectorResult {
		t.Fatal(string(result), err)
	}
}

func TestDiarizationEnvelopeRejectsTamperingAndShortAudio(t *testing.T) {
	_, to, _ := vectorKeys(t)
	response, _ := hex.DecodeString(vectorResponse)
	key, _ := hex.DecodeString(vectorResponseKey)
	flipped := append([]byte{}, response...)
	flipped[len(flipped)-1] ^= 1
	for name, bad := range map[string][]byte{"flipped": flipped, "truncated": response[:len(response)-1], "trailing": append(append([]byte{}, response...), 0), "header only": response[:envelopeResponseHeader]} {
		if _, err := openDiarizationResponse(bad, key, vectorJob); err == nil {
			t.Error("accepted", name)
		}
	}
	if _, err := openDiarizationResponse(response, key, strings.Repeat("ff", 16)); err == nil {
		t.Error("accepted another job's response")
	}
	sealed, _, err := sealDiarizationRequest(bytes.NewReader([]byte("short")), 10, to, vectorJob, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(sealed); err == nil {
		t.Error("sealed fewer bytes than declared")
	}
	empty, _, _ := sealDiarizationRequest(bytes.NewReader(nil), 0, to, vectorJob, time.Now(), nil)
	if data, _ := io.ReadAll(empty); int64(len(data)) != empty.Len() || len(data) != envelopeRequestHeader+4+envelopeTag {
		t.Error("empty audio is not one final record", len(data))
	}
	if _, _, err = sealDiarizationRequest(bytes.NewReader(nil), 0, to, "not-hex", time.Now(), nil); err == nil {
		t.Error("accepted invalid job identity")
	}
}

// fakeGPU is the external endpoint: proxy-authenticated, bearer-checked, and it
// only ever receives envelopes, from which it returns sealed segments.
type fakeGPU struct {
	mu          sync.Mutex
	recipient   *ecdh.PrivateKey
	keys        map[string][]byte
	done        map[string]bool
	submissions []string
	maxLive     int
	t           *testing.T
}

func (f *fakeGPU) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Modal-Key") != "wk-test" || r.Header.Get("Modal-Secret") != "ws-test" || r.Header.Get("Authorization") != "Bearer 01234567890123456789012345678901" {
		f.t.Error("missing proxy or bearer authentication")
		w.WriteHeader(401)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/diarizations/")
	switch {
	case r.Method == "POST":
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Type") != "application/vnd.speektome.diarization-envelope" || bytes.Contains(body, []byte("fixture audio")) {
			f.t.Error("audio left the coordinator unsealed")
		}
		audio, job, responseKey := openTestRequest(f.t, f.recipient, body)
		if string(audio) != "fixture audio" || job != r.Header.Get("X-SpeekToMe-Job-ID") {
			f.t.Error("wrong audio or identity")
		}
		f.keys[job], f.done[job] = responseKey, false
		f.submissions = append(f.submissions, job)
		f.maxLive = max(f.maxLive, len(f.keys))
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"job_id":%q}`, job)
	case r.Method == "DELETE":
		delete(f.keys, id)
		w.Write([]byte(`{}`))
	case strings.HasSuffix(id, "/result"):
		job := strings.TrimSuffix(id, "/result")
		w.Write(sealTestResponse(f.t, []byte(`{"segments":[{"start":0.25,"end":2.5,"speaker":3}]}`), f.keys[job], job))
	default:
		done, ok := f.done[id]
		if _, live := f.keys[id]; !ok || !live {
			w.WriteHeader(404)
			return
		}
		// Each job completes on its second status poll.
		status := "processing"
		if done {
			status = "completed"
		}
		f.done[id] = true
		fmt.Fprintf(w, `{"status":%q,"error":""}`, status)
	}
}

func TestGPUDiarizationRunsConcurrentlyAndOpensSealedResults(t *testing.T) {
	j, d := diarizationFixture(t)
	recipient, to, _ := vectorKeys(t)
	f := &fakeGPU{recipient: recipient, keys: map[string][]byte{}, done: map[string]bool{}, t: t}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	d.url, d.recipient, d.label, d.proxyKey, d.proxySecret = server.URL, to, "GPU diarization", "wk-test", "ws-test"
	d.slots = make(chan struct{}, 2)
	var wg sync.WaitGroup
	for i, key := range []string{"first", "second", "third"} {
		addDiarizationMeeting(t, j, key, i+1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			speakers, err := d.run(context.Background(), key)
			if err != nil || len(speakers) != 1 || speakers[0].Speaker != 3 {
				t.Error(key, speakers, err)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submissions) != 3 || f.maxLive != 2 {
		t.Fatalf("expected three jobs, at most two at once: %v, %d", f.submissions, f.maxLive)
	}
	if len(f.keys) != 0 {
		t.Fatal("remote jobs not deleted after their results were read")
	}
}
