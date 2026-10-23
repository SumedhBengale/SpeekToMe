package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLiveTurnsSplitOnPauseAndKeepRevisionsInOpenTurn(t *testing.T) {
	turns := &liveTurns{channel: "mic", speakerID: "speaker_local", speakerName: "Asha"}
	start := time.Unix(100, 0)
	out := turns.update("hello there", start, 1)
	if len(out) != 1 || out[0].ID != "live_mic_0" || out[0].Text != "hello there" || out[0].IsFinal {
		t.Fatalf("first update = %+v", out)
	}
	// A partial revision within the pause window rewrites the open turn.
	out = turns.update("hello their friend", start.Add(500*time.Millisecond), 1.5)
	if len(out) != 1 || out[0].ID != "live_mic_0" || out[0].Text != "hello their friend" {
		t.Fatalf("revision = %+v", out)
	}
	// After a pause the previous turn is closed and a new one starts.
	out = turns.update("hello their friend how are you", start.Add(3*time.Second), 4)
	if len(out) != 2 || !out[0].IsFinal || out[0].Text != "hello their friend" || out[1].ID != "live_mic_1" || out[1].Text != "how are you" {
		t.Fatalf("pause split = %+v", out)
	}
	if final := turns.finish(5); final == nil || !final.IsFinal || final.Text != "how are you" {
		t.Fatalf("finish = %+v", final)
	}
}

func TestLiveTurnsSplitLongMonologues(t *testing.T) {
	turns := &liveTurns{channel: "system", speakerID: "live_system", speakerName: "Meeting audio"}
	text := ""
	now := time.Unix(0, 0)
	var emitted []ClientMeetingTurn
	for i := 0; i < liveTurnMaxWords+5; i++ {
		text += "word "
		now = now.Add(100 * time.Millisecond)
		emitted = append(emitted, turns.update(text, now, float64(i))...)
	}
	last := emitted[len(emitted)-1]
	if last.ID != "live_system_1" {
		t.Fatalf("long monologue was not split: %+v", last)
	}
}

func TestMeetingStreamRequiresHeaderToken(t *testing.T) {
	app := &App{}
	for _, target := range []string{"/v1/meetings/live", "/v1/meetings/live?token=secret"} {
		recorder := httptest.NewRecorder()
		app.HandleMeetingStream(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", target, recorder.Code)
		}
	}
}
