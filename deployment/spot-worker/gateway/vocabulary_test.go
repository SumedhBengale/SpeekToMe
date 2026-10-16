package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestVocabularyForwardedOnEveryStreamRoute(t *testing.T) {
	for _, route := range []string{"/v1/realtime", "/v1/audio/realtime", "/v1/realtime/opus", "/v1/audio/transcriptions/realtime"} {
		t.Run(route, func(t *testing.T) {
			up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := up.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer c.Close()
				for {
					_, data, err := c.ReadMessage()
					if err != nil {
						return
					}
					if c.WriteMessage(websocket.TextMessage, data) != nil {
						return
					}
				}
			}))
			defer backend.Close()
			front := httptest.NewServer(newTestGateway(backend.URL))
			defer front.Close()
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(front.URL, "http")+route+"?boosted_words=Kubernetes%2CRedis", http.Header{"Authorization": []string{"Bearer " + testToken}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			check := func(want string) {
				var e struct {
					Session struct {
						Contexts []speechContext `json:"speech_contexts"`
					} `json:"session"`
				}
				if err := c.ReadJSON(&e); err != nil {
					t.Fatal(err)
				}
				if want == "" {
					if len(e.Session.Contexts) != 0 {
						t.Fatal(e)
					}
					return
				}
				if len(e.Session.Contexts) != 1 || e.Session.Contexts[0].Phrases[0] != want {
					t.Fatalf("unexpected upstream contexts: %+v", e)
				}
			}
			check("Kubernetes")
			c.WriteJSON(map[string]any{"type": "session.update", "session": map[string]any{"sample_rate": 16000}})
			check("Kubernetes")
			c.WriteJSON(map[string]any{"type": "start", "language": "en-US"})
			check("Kubernetes")
			c.WriteJSON(map[string]any{"type": "config", "speech_contexts": []speechContext{{Phrases: []string{"PostgreSQL"}, Boost: 0.5}}})
			check("PostgreSQL")
			c.WriteJSON(map[string]any{"type": "session.update", "session": map[string]any{"speech_contexts": []speechContext{}}})
			check("")
		})
	}
}

func TestVocabularyValidationAndControlIsolation(t *testing.T) {
	for _, raw := range []string{"boosted_words=Redis&vocabulary_boost=NaN", "boosted_words=Redis&vocabulary_boost=6", "speech_contexts=bad"} {
		q, _ := url.ParseQuery(raw)
		if _, err := queryContexts(q); err == nil {
			t.Fatal(raw)
		}
	}
	contexts := []speechContext{{Phrases: []string{"Redis"}, Boost: 1.5}}
	data := []byte(`{"type":"input_audio_buffer.commit"}`)
	got, err := vocabularyControl(data, &contexts)
	if err != nil || string(got) != string(data) {
		t.Fatalf("commit changed: %s %v", got, err)
	}
	var none []speechContext
	got, err = vocabularyControl([]byte(`{"type":"session.update","session":{"language":"en"}}`), &none)
	if err != nil || strings.Contains(string(got), "Redis") {
		t.Fatal("vocabulary leaked between requests")
	}
	var event map[string]any
	if json.Unmarshal(got, &event) != nil {
		t.Fatal("invalid control")
	}
}
