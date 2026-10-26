package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"speektome-server/config"
	"speektome-server/pipeline"
	"speektome-server/vocabularydecision"
	"speektome/vocabulary"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A vocabulary proposal is decided while the user is still speaking: the
// preview first shows the ASR words, then the decision, and the end of the
// stream reuses it instead of asking Jev again.
func TestStreamDecidesVocabularyLiveAndReusesItAtTheEnd(t *testing.T) {
	a := limitApp(t)
	if _, e := a.DB.AddVocabularyTerm(1, "PostgreSQL", "postgresql"); e != nil {
		t.Fatal(e)
	}
	orig, raw, llmKey := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.CerebrasAPIKey
	t.Cleanup(func() {
		config.ConfigManager.Set(orig, raw)
		config.CerebrasAPIKey = llmKey
		pipeline.GlobalCircuitBreaker.Reset()
	})
	config.CerebrasAPIKey = "test-only"
	t.Setenv("SPEEKTOME_ACK_SECRET", "synthetic-only")
	t.Setenv("TYPESAFE_LIVE_CONTEXT_WORDS", "3")

	terms := []string{"PostgreSQL"}
	compiled, _ := vocabulary.Compile(terms)
	transcript := "We use postgrsq as our database"
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		if _, data, e := conn.Read(ctx); e != nil || !bytes.Contains(data, []byte(`"live":true`)) {
			t.Error("dictation did not ask for live proposals")
			return
		}
		var words []map[string]any
		for i, word := range strings.Fields(transcript) {
			words = append(words, map[string]any{"word": word, "start": float64(i), "end": float64(i) + 0.5})
		}
		completed, _ := json.Marshal(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": transcript, "words": words})
		conn.Write(ctx, websocket.MessageText, completed)
		update := vocabulary.Propose(ctx, transcript, compiled, vocabulary.TermsHash(terms))
		update.Type = vocabulary.UpdateType
		data, _ := json.Marshal(update)
		conn.Write(ctx, websocket.MessageText, data)
		for {
			_, data, e := conn.Read(ctx)
			if e != nil {
				return
			}
			if bytes.Contains(data, []byte("input_audio_buffer.commit")) {
				break
			}
		}
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.committed"}`))
		final, _ := json.Marshal(vocabulary.Propose(ctx, transcript, compiled, vocabulary.TermsHash(terms)))
		conn.Write(ctx, websocket.MessageText, final)
		conn.Read(ctx)
	}))
	defer worker.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	token := filepath.Join(t.TempDir(), "worker.token")
	if e := os.WriteFile(token, []byte("test-only"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)

	var jevCalls atomic.Int32
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jevCalls.Add(1)
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		answers := map[string]any{}
		for id := range body.Questions {
			answers[id] = map[string]any{"type": "choice", "choice": "use_vocabulary", "confidence": .99, "probabilities": map[string]float64{"keep_original": .01, "use_vocabulary": .99}}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers})
	}))
	defer jev.Close()
	client := vocabularydecision.Default()
	oldKey, oldURL, oldHTTP := client.Key, client.URL, client.HTTP
	client.Key, client.URL, client.HTTP = "test-only", jev.URL, jev.Client()
	t.Cleanup(func() { client.Key, client.URL, client.HTTP = oldKey, oldURL, oldHTTP })

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": body.Messages[1].Content}}}})
	}))
	defer llm.Close()

	cfg := orig
	cfg.Models.StreamingProviders = map[string]config.StreamingProviderConfig{
		"self_hosted_nemotron_3_5": {Enabled: true, Priority: 1, Type: "real", Model: "nemotron", BaseURL: "ws://unused"},
	}
	cfg.Models.EnhancementModels = []config.ModelSpec{{Provider: config.ProviderCerebras, Model: "grammar", Enabled: true, EndpointOverride: llm.URL, TimeoutMs: 2000}}
	config.ConfigManager.Set(cfg, raw)
	pipeline.GlobalCircuitBreaker.Reset()

	server := httptest.NewServer(http.HandlerFunc(a.HandleStreamTranscribe))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, e := websocket.Dial(ctx, server.URL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer user1"}}})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	start, _ := json.Marshal(StreamStartPayload{Type: "start", Language: "en", Format: "ogg_opus"})
	if e = conn.Write(ctx, websocket.MessageText, start); e != nil {
		t.Fatal(e)
	}
	read := func() StreamEventMessage {
		t.Helper()
		_, data, e := conn.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var event StreamEventMessage
		if e = json.Unmarshal(data, &event); e != nil {
			t.Fatal(e)
		}
		if event.Type == "error" {
			t.Fatalf("stream error: %s", data)
		}
		return event
	}

	var previews []string
	for {
		event := read()
		if event.Type != "interim" {
			continue
		}
		previews = append(previews, event.Text)
		if strings.Contains(event.Text, "PostgreSQL") {
			break
		}
	}
	if previews[0] != transcript || previews[len(previews)-1] != "We use PostgreSQL as our database" {
		t.Fatalf("preview must show the ASR words, then the live decision: %q", previews)
	}
	if jevCalls.Load() != 1 {
		t.Fatalf("live decision made %d Jev calls", jevCalls.Load())
	}

	stop, _ := json.Marshal(map[string]string{"type": "stop"})
	if e = conn.Write(ctx, websocket.MessageText, stop); e != nil {
		t.Fatal(e)
	}
	var final StreamEventMessage
	for final.Type != "final" {
		final = read()
	}
	if final.Text != "We use PostgreSQL as our database" || final.Vocabulary == nil || final.Vocabulary.LiveDecisions != 1 {
		t.Fatalf("final must reuse the live decision: %+v", final)
	}
	if jevCalls.Load() != 1 {
		t.Fatalf("the end of the stream asked Jev again: %d calls", jevCalls.Load())
	}
}
