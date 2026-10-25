package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const (
	testLiveKey      = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	testLiveKeyOther = "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
)

func liveLines(t *testing.T, user int64, batches ...liveBatch) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, b := range batches {
		event := signLiveBatch(user, b)
		if event == nil {
			t.Fatal("no signing key")
		}
		out.Write(event)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func TestLiveTranscriptVerifiesSignatureMeetingAndUser(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	good := liveBatch{JobID: "meeting-1", Channel: "mic", Session: "a", From: 0, To: 2, Words: []liveWord{{"hello", 0.2, 0.6}}}
	data := liveLines(t, 7, good)
	if got := parseLiveTranscript(data, 7, "meeting-1")["mic"]; len(got.Words) != 1 || len(got.Covered) != 1 {
		t.Fatalf("valid batch rejected: %+v", got)
	}
	tampered := bytes.Replace(data, []byte(`"hello"`), []byte(`"hijack"`), 1)
	stretched := bytes.Replace(data, []byte(`"to":2`), []byte(`"to":2000`), 1)
	for name, check := range map[string]func() *liveChannel{
		"tampered words":    func() *liveChannel { return parseLiveTranscript(tampered, 7, "meeting-1")["mic"] },
		"tampered coverage": func() *liveChannel { return parseLiveTranscript(stretched, 7, "meeting-1")["mic"] },
		"another user":      func() *liveChannel { return parseLiveTranscript(data, 8, "meeting-1")["mic"] },
		"another meeting":   func() *liveChannel { return parseLiveTranscript(data, 7, "meeting-2")["mic"] },
	} {
		if got := check(); len(got.Words) != 0 || len(got.Covered) != 0 {
			t.Errorf("%s accepted: %+v", name, got)
		}
	}
	// After rotation, batches signed with the previous key still verify; a key
	// that was never configured does not.
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKeyOther)
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY_PREVIOUS", testLiveKey)
	if got := parseLiveTranscript(data, 7, "meeting-1")["mic"]; len(got.Words) != 1 {
		t.Fatal("previous key not accepted")
	}
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY_PREVIOUS", "")
	if got := parseLiveTranscript(data, 7, "meeting-1")["mic"]; len(got.Words) != 0 {
		t.Fatal("retired key accepted")
	}
}

func TestLiveTranscriptSkipsTornLinesAndKeepsFirstOwnerOfOverlap(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	first := liveBatch{JobID: "m", Channel: "system", Session: "a", From: 0, To: 10, Words: []liveWord{{"before", 9.2, 9.6}}}
	// A reconnect resent audio from 8 s for decoder context; its words there are
	// already held and must not be duplicated.
	second := liveBatch{JobID: "m", Channel: "system", Session: "b", From: 8, To: 15, Words: []liveWord{{"duplicate", 9.3, 9.5}, {"after", 12, 12.4}}}
	data := liveLines(t, 1, first, second)
	data = append(data, []byte(`{"type":"words","batch":{"v":1,"kid"`)...) // torn by a crash
	got := parseLiveTranscript(data, 1, "m")["system"]
	var words []string
	for _, w := range got.Words {
		words = append(words, w.Word)
	}
	if strings.Join(words, " ") != "before after" {
		t.Fatal(words)
	}
	if len(got.Covered) != 1 || got.Covered[0] != (timeSpan{0, 15}) {
		t.Fatal(got.Covered)
	}
}

func TestTranscriptGapsJoinNearbyAndDropTinyGaps(t *testing.T) {
	covered := []timeSpan{{0, 5}, {5.2, 10}, {15, 20}, {20.5, 22}}
	got := transcriptGaps(covered, 25)
	// 5-5.2 is too short to matter; the others are more than a second apart, so
	// they stay separate; 22-25 is the missing tail.
	want := []timeSpan{{10, 15}, {20, 20.5}, {22, 25}}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatal(got)
		}
	}
	if gaps := transcriptGaps(nil, 30); len(gaps) != 1 || gaps[0] != (timeSpan{0, 30}) {
		t.Fatal("a meeting with no live transcript is one whole gap", gaps)
	}
	// Two gaps with half a second of coverage between them go as one range.
	if gaps := transcriptGaps([]timeSpan{{0, 4}, {4.5, 5}, {5.6, 30}}, 30); len(gaps) != 1 || gaps[0] != (timeSpan{4, 5.6}) {
		t.Fatal("gaps under a second apart should join", gaps)
	}
}

func TestSignedLiveEventKeepsBatchBytesVerbatim(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	event := signLiveBatch(3, liveBatch{JobID: "m", Channel: "mic", From: 0, To: 1})
	var parsed signedLiveEvent
	if json.Unmarshal(event, &parsed) != nil || parsed.Type != "words" {
		t.Fatal(string(event))
	}
	sig, _ := base64.StdEncoding.DecodeString(parsed.Sig)
	keys := liveKeys()
	if !bytes.Equal(sig, liveMAC(keys[0].secret, 3, parsed.Batch)) {
		t.Fatal("signature does not cover the batch bytes the desktop stores")
	}
	if !strings.Contains(string(parsed.Batch), `"words":[]`) {
		t.Fatal("silence batch must carry an empty word list", string(parsed.Batch))
	}
}
