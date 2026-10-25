package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	result, err := (&gateway{upstream: upstream}).meetingStreamingASR(context.Background(), bytes.NewReader(pcm), "en", []speechContext{{Phrases: []string{"Redis"}, Boost: 1.5}})
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

func TestPCMSectionsAndWAVHeader(t *testing.T) {
	pcm := make([]byte, 10*pcmBytesPerSecond)
	if got := len(pcmSection(pcm, 2, 4.5)); got != int(2.5*pcmBytesPerSecond) {
		t.Fatal(got)
	}
	if got := len(pcmSection(pcm, 9, 40)); got != pcmBytesPerSecond {
		t.Fatal(got)
	}
	header := wavHeader(len(pcm))
	if string(header[:4]) != "RIFF" || string(header[36:40]) != "data" || len(header) != 44 {
		t.Fatal(header)
	}
}

func TestDecodeMeetingAudioStaysInMemory(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	encode := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-ac", "1", "-ar", "16000", "-c:a", "libopus", "-f", "ogg", "pipe:1")
	ogg, err := encode.Output()
	if err != nil {
		t.Skip("ffmpeg without libopus")
	}
	t.Setenv("TMPDIR", t.TempDir())
	pcm, err := decodeMeetingAudio(context.Background(), bytes.NewReader(ogg))
	if err != nil {
		t.Fatal(err)
	}
	if seconds := float64(len(pcm)) / pcmBytesPerSecond; seconds < 2.9 || seconds > 3.1 {
		t.Fatal(seconds)
	}
	if entries, _ := os.ReadDir(os.Getenv("TMPDIR")); len(entries) != 0 {
		t.Fatal("decoder wrote temporary files")
	}
	if _, err = decodeMeetingAudio(context.Background(), bytes.NewReader([]byte("not audio"))); err == nil {
		t.Fatal("decoded invalid audio")
	}
}

func TestParseMeetingRanges(t *testing.T) {
	if got, err := parseMeetingRanges("", 90); err != nil || len(got) != 1 || got[0] != (meetingRange{0, 90}) {
		t.Fatal("no ranges means the whole recording", got, err)
	}
	got, err := parseMeetingRanges("12.40-75.10,80-90.5", 90)
	if err != nil || len(got) != 2 || got[0] != (meetingRange{12.4, 75.1}) || got[1] != (meetingRange{80, 90}) {
		t.Fatal("a range may end just past the recording and is clipped", got, err)
	}
	for _, invalid := range []string{"10", "5-5", "9-3", "-1-4", "1-4,3-6", "20-30,1-4", "1-95", "a-b", "NaN-4", "1-Inf"} {
		if _, err := parseMeetingRanges(invalid, 90); err == nil {
			t.Error("accepted", invalid)
		}
	}
	many := strings.TrimSuffix(strings.Repeat("1-2,", maxMeetingRanges+1), ",")
	if _, err := parseMeetingRanges(many, 90); err == nil {
		t.Error("accepted more than the coordinator ever sends")
	}
}

// Only gap audio, plus its context, reaches the model, and only words inside
// the requested range come back.
func TestTranscribeRangesSendsOnlyTheGapAudio(t *testing.T) {
	var mu sync.Mutex
	var sessions []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var settings map[string]interface{}
		conn.ReadJSON(&settings)
		received := 0
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.BinaryMessage {
				received += len(data)
				continue
			}
			break
		}
		mu.Lock()
		sessions = append(sessions, received)
		mu.Unlock()
		// Relative to the section: 1 s in lies in the leading context, 6 s in
		// lies inside the gap.
		conn.WriteJSON(map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "transcript": "context gap", "words": []meetingWord{{Word: "context", Start: 1, End: 1.4}, {Word: "gap", Start: 6, End: 6.4}}})
		conn.WriteJSON(map[string]string{"type": "input_audio_buffer.committed"})
	}))
	defer server.Close()
	upstream, _ := url.Parse(server.URL)
	duration := 120.0
	pcm := make([]byte, int(duration*pcmBytesPerSecond))
	result, err := (&gateway{upstream: upstream}).transcribeRanges(context.Background(), pcm, duration, []meetingRange{{40, 50}}, "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	// One window: the 10-second gap plus 4.48 s of context on each side.
	gapStart, gapEnd, pad := 40.0, 50.0, 4.48 // variables: the same runtime float arithmetic as the gateway
	want := len(pcmSection(pcm, gapStart-pad, gapEnd+pad))
	if len(sessions) != 1 || sessions[0] != want {
		t.Fatalf("sent %v bytes, want one session of %d", sessions, want)
	}
	if len(result.Words) != 1 || result.Words[0].Word != "gap" || result.Words[0].Start < 41.5 || result.Words[0].Start > 41.53 {
		t.Fatalf("%+v", result.Words)
	}
}
