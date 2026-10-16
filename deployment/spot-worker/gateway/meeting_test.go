package main

import (
	"context"
	"crypto/sha256"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestMeetingAdmissionSeparatesLiveCapacity(t *testing.T) {
	g := &gateway{slots: make(chan struct{}, 20)}
	for range 20 {
		g.slots <- struct{}{}
	}
	for _, stage := range []string{"mic", "system", "diar"} {
		if !g.admitMeeting("job", stage) {
			t.Fatal(stage)
		}
	}
	if g.admitMeeting("other", "mic") || g.admitMeeting("job", "mic") {
		t.Fatal("accepted a second job or duplicate stage")
	}
	g.releaseMeeting("mic")
	g.releaseMeeting("system")
	if g.admitMeeting("other", "mic") {
		t.Fatal("diarization still active")
	}
	g.releaseMeeting("diar")
	if !g.admitMeeting("other", "mic") {
		t.Fatal("meeting slot leaked")
	}
	if len(g.slots) != 20 {
		t.Fatal("meeting consumed live capacity")
	}
}

func TestRecordedASRUsesCacheAwareStreaming(t *testing.T) {
	pcm := make([]byte, 17920*3)
	file := filepath.Join(t.TempDir(), "audio.pcm")
	if err := os.WriteFile(file, pcm, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions/realtime" {
			t.Errorf("unexpected backend %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var settings map[string]interface{}
		if err := conn.ReadJSON(&settings); err != nil {
			t.Error(err)
			return
		}
		session := settings["session"].(map[string]interface{})
		if contexts, ok := session["speech_contexts"].([]interface{}); !ok || len(contexts) != 1 {
			t.Errorf("missing recorded vocabulary: %v", session)
		}
		if session["speaker_diarization"] != false || session["word_timestamps"] != true {
			t.Errorf("settings: %v", settings)
		}
		bytes := 0
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				t.Error(err)
				return
			}
			if kind == websocket.BinaryMessage {
				bytes += len(data)
				if len(data) > 17920 {
					t.Error("unbounded frame")
				}
				continue
			}
			if bytes != len(pcm) {
				t.Errorf("audio bytes %d", bytes)
			}
			break
		}
		conn.WriteJSON(map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "transcript": "hello", "words": []meetingWord{{Word: "hello", Start: 0.1, End: 0.5}}})
		conn.WriteJSON(map[string]string{"type": "input_audio_buffer.committed"})
	}))
	defer server.Close()
	upstream, _ := url.Parse(server.URL)
	result, err := (&gateway{upstream: upstream}).meetingStreamingASR(context.Background(), file, "en", []speechContext{{Phrases: []string{"Redis"}, Boost: 1.5}})
	if err != nil || len(result.Words) != 1 || result.Words[0].Word != "hello" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestExclusiveMeetingPreventsLiveAdmission(t *testing.T) {
	token := "01234567890123456789012345678901"
	g := &gateway{slots: make(chan struct{}, 20), meetingExclusive: true, tokenHash: sha256.Sum256([]byte(token))}
	g.slots <- struct{}{}
	if g.admitMeeting("job", "mic") {
		t.Fatal("meeting admitted onto busy worker")
	}
	<-g.slots
	for _, stage := range []string{"mic", "system", "diar"} {
		if !g.admitMeeting("job", stage) {
			t.Fatal(stage)
		}
	}
	request := httptest.NewRequest("GET", "/v1/realtime", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, request)
	if w.Code != 429 || len(g.slots) != 0 {
		t.Fatal("live admission raced meeting", w.Code)
	}
	for _, stage := range []string{"mic", "system", "diar"} {
		g.releaseMeeting(stage)
	}
	if !g.admitMeeting("next", "mic") {
		t.Fatal("exclusive slot leaked")
	}
}
