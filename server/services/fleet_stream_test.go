package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"speektome-server/db"
	"speektome/vocabulary"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A synthetic Ogg page is sufficient for the journal duration parser; the fake
// backend deliberately exercises protocol/recovery, not acoustic inference.
func journalFrame() []byte {
	b := make([]byte, 28)
	copy(b, "OggS")
	binary.LittleEndian.PutUint64(b[6:14], 26880)
	b[26] = 1
	b[27] = 0
	return b
}
func TestFleetStreamReplaysPendingAudioAndSuppressesCommittedOverlap(t *testing.T) {
	var sessions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer conn.CloseNow()
		session := sessions.Add(1)
		ctx := r.Context()
		_, _, e = conn.Read(ctx)
		if e != nil {
			return
		}
		count := 0
		for {
			kind, data, e := conn.Read(ctx)
			if e != nil {
				return
			}
			if kind == websocket.MessageBinary {
				count++
				if session > 1 && count == 1 {
					conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.delta","delta":"first replayed preview"}`))
				}
				if session == 1 {
					if count == 1 {
						conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"first","words":[{"word":"first","start":0,"end":0.4}]}`))
					} else {
						conn.CloseNow()
						return
					}
				}
			} else {
				var control map[string]any
				json.Unmarshal(data, &control)
				if control["type"] == "input_audio_buffer.commit" {
					conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"first second","words":[{"word":"first","start":0,"end":0.4},{"word":"second","start":0.6,"end":1.0}]}`))
					conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.committed"}`))
					return
				}
			}
		}
	}))
	defer server.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", server.URL)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, e := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", []string{"en"}, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close(websocket.StatusNormalClosure, "")
	if e = session.SendAudio(ctx, journalFrame()); e != nil {
		t.Fatal(e)
	}
	select {
	case text := <-session.TextChan:
		if text != "first" {
			t.Fatal(text)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e = session.SendAudio(ctx, journalFrame()); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sessions.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sessions.Load() != 2 {
		t.Fatal("did not reconnect")
	}
	if e = session.SendControl(ctx, map[string]interface{}{"type": "stop"}); e != nil {
		t.Fatal(e)
	}
	if e = session.AwaitFinal(ctx); e != nil {
		t.Fatal(e)
	}
	final := ""
	for text := range session.TextChan {
		if text != "first second" {
			t.Fatalf("replayed partial escaped: %q", text)
		}
		final = text
	}
	if final != "first second" {
		t.Fatal(final)
	}
}

func TestFleetStreamPublishesPreviewsBeforeCommitAndReplacesWithFinal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		for _, event := range []string{
			`{"type":"conversation.item.input_audio_transcription.delta","delta":"Okay","audio_processed":0.56}`,
			`{"type":"conversation.item.input_audio_transcription.delta","delta":" one two","audio_processed":0.56}`,
		} {
			if conn.Write(ctx, websocket.MessageText, []byte(event)) != nil {
				return
			}
		}
		// No commit can arrive until the caller has received both previews.
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"Okay one three","words":[{"word":"Okay","start":0,"end":0.1},{"word":"one","start":0.1,"end":0.2},{"word":"three","start":0.2,"end":0.4}]}`))
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.delta","delta":" audio testing"}`))
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"audio tested","words":[{"word":"audio","start":0.6,"end":0.8},{"word":"tested","start":0.8,"end":1.0}]}`))
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.committed"}`))
	}))
	defer server.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", server.URL)
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", []string{"en"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(websocket.StatusNormalClosure, "")
	read := func(want string) {
		t.Helper()
		select {
		case got := <-session.TextChan:
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err = session.SendAudio(ctx, journalFrame()); err != nil {
		t.Fatal(err)
	}
	read("Okay")
	read("Okay one two")
	if err = session.SendAudio(ctx, journalFrame()); err != nil {
		t.Fatal(err)
	}
	read("Okay one three")
	read("Okay one three audio testing")
	if err = session.SendControl(ctx, map[string]interface{}{"type": "stop"}); err != nil {
		t.Fatal(err)
	}
	if err = session.AwaitFinal(ctx); err != nil {
		t.Fatal(err)
	}
	final := ""
	for text := range session.TextChan {
		final = text
	}
	if final != "Okay one three audio tested" {
		t.Fatal(final)
	}
}

func TestFleetWaitsForStructuredVocabularyFinalAfterCommit(t *testing.T) {
	terms := []db.VocabularyTerm{{Canonical: "PostgreSQL"}}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		_, data, e := conn.Read(ctx)
		if e != nil {
			return
		}
		if bytes.Contains(data, []byte("speech_contexts")) || !bytes.Contains(data, []byte("vocabulary_matching")) {
			t.Error("decoder bias or missing final matcher config")
		}
		for {
			_, data, e = conn.Read(ctx)
			if e != nil {
				return
			}
			if bytes.Contains(data, []byte("input_audio_buffer.commit")) {
				break
			}
		}
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"postgrsq","words":[{"word":"postgrsq","start":0,"end":1}]}`))
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.committed"}`))
		v, _ := vocabulary.Compile([]string{"PostgreSQL"})
		result := vocabulary.Propose(ctx, "postgrsq", v, vocabulary.TermsHash([]string{"PostgreSQL"}))
		data, _ = json.Marshal(result)
		conn.Write(ctx, websocket.MessageText, data)
	}))
	defer worker.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("token"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, e := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", nil, "", terms)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(websocket.StatusNormalClosure, "")
	if e = s.SendControl(ctx, map[string]interface{}{"type": "stop"}); e != nil {
		t.Fatal(e)
	}
	if e = s.AwaitFinal(ctx); e != nil {
		t.Fatal(e)
	}
	if r := s.VocabularyResult(); r == nil || r.Raw != "postgrsq" || r.Enhanced != "PostgreSQL" {
		t.Fatalf("structured result lost: %+v", r)
	}
}

func TestWorkerVocabularyBoundsPreserveASRForLegacyInvalidEntries(t *testing.T) {
	for _, terms := range [][]string{make([]string, 1001), {strings.Repeat("x", 257)}, {"line one\nline two"}} {
		if validWorkerVocabulary(terms) {
			t.Fatal("invalid legacy dictionary forwarded to worker")
		}
	}
	if !validWorkerVocabulary([]string{"PostgreSQL", "SpeekToMe"}) {
		t.Fatal("valid dictionary rejected")
	}
}

func TestFleetForwardsOneExplicitSpeakingLanguage(t *testing.T) {
	for _, tc := range []struct{ active, want string }{{"en", "en-US"}, {"hi", "hi-IN"}, {"auto", "auto"}} {
		t.Run(tc.active, func(t *testing.T) {
			received := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_, data, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				var control struct {
					Session struct {
						Language string `json:"language"`
					} `json:"session"`
				}
				if err = json.Unmarshal(data, &control); err != nil {
					return
				}
				received <- control.Session.Language
				for {
					if _, _, err := conn.Read(r.Context()); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			t.Setenv("SPEEKTOME_FLEET_URL", server.URL)
			token := filepath.Join(t.TempDir(), "token")
			os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600)
			t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", []string{tc.active}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(websocket.StatusNormalClosure, "")
			select {
			case got := <-received:
				if got != tc.want {
					t.Errorf("model language=%q, want %q", got, tc.want)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestMeetingSessionConfirmsContiguousWordsOnRecordingTimeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		audio := 0
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if kind == websocket.MessageBinary {
				audio++
				if audio == 1 {
					conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"hello","words":[{"word":"hello","start":0.1,"end":0.4}]}`))
					conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"","words":[],"audio_processed":0.5}`))
				}
				continue
			}
			var control map[string]any
			json.Unmarshal(data, &control)
			if control["type"] == "input_audio_buffer.commit" {
				conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"world","words":[{"word":"world","start":0.7,"end":0.9}]}`))
				conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.committed"}`))
				return
			}
		}
	}))
	defer server.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", server.URL)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Resuming at chunk 10 puts the stream's audio at 5.6 s of the recording.
	session, err := CreateMeetingStreamingSession(ctx, "self_hosted_nemotron_3_5", []string{"en"}, nil, 5.6)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(websocket.StatusNormalClosure, "")
	var batches []WordBatch
	done := make(chan struct{})
	go func() {
		defer close(done)
		for batch := range session.Words {
			batches = append(batches, batch)
		}
	}()
	go func() {
		for range session.TextChan {
		}
	}()
	for range 2 {
		if err = session.SendAudio(ctx, journalFrame()); err != nil {
			t.Fatal(err)
		}
	}
	if err = session.SendControl(ctx, map[string]interface{}{"type": "stop"}); err != nil {
		t.Fatal(err)
	}
	if err = session.AwaitFinal(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	near := func(a, b float64) bool { return a-b < 1e-6 && b-a < 1e-6 }
	want := []WordBatch{
		{From: 5.6, To: 6.0, Words: []TimedWord{{"hello", 5.7, 6.0}}},
		{From: 6.0, To: 6.1}, // silence
		{From: 6.1, To: 6.5, Words: []TimedWord{{"world", 6.3, 6.5}}},
		{From: 6.5, To: 6.72}, // trailing silence, confirmed by the commit
	}
	if len(batches) != len(want) {
		t.Fatalf("%+v", batches)
	}
	for i, b := range batches {
		w := want[i]
		if !near(b.From, w.From) || !near(b.To, w.To) || len(b.Words) != len(w.Words) {
			t.Fatalf("batch %d: %+v, want %+v", i, b, w)
		}
		for k, word := range b.Words {
			if word.Word != w.Words[k].Word || !near(word.Start, w.Words[k].Start) || !near(word.End, w.Words[k].End) {
				t.Fatalf("batch %d word %d: %+v", i, k, word)
			}
		}
	}
}

func TestFleetDeliversValidLiveVocabularyProposals(t *testing.T) {
	terms := []db.VocabularyTerm{{Canonical: "PostgreSQL"}}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		_, data, e := conn.Read(ctx)
		if e != nil {
			return
		}
		if !bytes.Contains(data, []byte(`"live":true`)) {
			t.Error("dictation must ask for live vocabulary proposals")
		}
		v, _ := vocabulary.Compile([]string{"PostgreSQL"})
		update := func(raw string) []byte {
			result := vocabulary.Propose(ctx, raw, v, vocabulary.TermsHash([]string{"PostgreSQL"}))
			result.Type = vocabulary.UpdateType
			data, _ := json.Marshal(result)
			return data
		}
		conn.Write(ctx, websocket.MessageText, update("not what was confirmed postgrsq"))
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"We use postgrsq","words":[{"word":"We","start":0,"end":0.2},{"word":"use","start":0.2,"end":0.4},{"word":"postgrsq","start":0.4,"end":1}]}`))
		conn.Write(ctx, websocket.MessageText, update("We use postgrsq"))
		conn.Read(ctx)
	}))
	defer worker.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("token"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, e := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", nil, "", terms)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(websocket.StatusNormalClosure, "")
	select {
	case r := <-s.Proposals:
		if r.Raw != "We use postgrsq" || len(r.Choices) != 1 || r.Choices[0].Replacement != "PostgreSQL" {
			t.Fatalf("wrong live proposal: %+v", r)
		}
	case <-ctx.Done():
		t.Fatal("live proposal not delivered")
	}
	select {
	case r := <-s.Proposals:
		t.Fatalf("proposal for a different transcript accepted: %+v", r)
	default:
	}
}
