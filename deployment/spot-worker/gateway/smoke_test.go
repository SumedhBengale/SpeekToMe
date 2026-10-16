package main

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSmokeConfiguredDiarizationAndCommit(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		diarization, speaker, ackFirst, wantError bool
	}{
		{"asr-only", false, false, false, false},
		{"asr-ack-first", false, false, true, false},
		{"diarized", true, true, false, false},
		{"missing-speaker", true, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wav := make([]byte, 46)
			copy(wav, "RIFF")
			binary.LittleEndian.PutUint32(wav[4:], 38)
			copy(wav[8:], "WAVEfmt ")
			binary.LittleEndian.PutUint32(wav[16:], 16)
			binary.LittleEndian.PutUint16(wav[20:], 1)
			binary.LittleEndian.PutUint16(wav[22:], 1)
			binary.LittleEndian.PutUint32(wav[24:], 16000)
			binary.LittleEndian.PutUint16(wav[34:], 16)
			copy(wav[36:], "data")
			binary.LittleEndian.PutUint32(wav[40:], 2)
			path := filepath.Join(t.TempDir(), "smoke.wav")
			if err := os.WriteFile(path, wav, 0600); err != nil {
				t.Fatal(err)
			}
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer c.Close()
				c.WriteJSON(map[string]any{"type": "session.created"})
				var update map[string]any
				if err := c.ReadJSON(&update); err != nil {
					t.Error(err)
					return
				}
				if update["session"].(map[string]any)["speaker_diarization"] != tc.diarization {
					t.Error("wrong diarization request")
				}
				c.WriteJSON(map[string]any{"type": "session.updated"})
				if _, _, err := c.ReadMessage(); err != nil {
					t.Error(err)
					return
				}
				if err := c.ReadJSON(&update); err != nil {
					t.Error(err)
					return
				}
				if update["type"] != "input_audio_buffer.commit" {
					t.Error("missing commit")
				}
				final := map[string]any{"type": "transcription.completed", "transcript": "A real transcript."}
				if tc.speaker {
					final["words"] = []map[string]any{{"word": "A", "speaker": 1}}
				}
				ack := map[string]any{"type": "input_audio_buffer.committed"}
				if tc.ackFirst {
					c.WriteJSON(ack)
					c.WriteJSON(final)
				} else {
					c.WriteJSON(final)
					c.WriteJSON(ack)
				}
			}))
			defer server.Close()
			err := smokeTest(server.URL, path, tc.diarization)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tc.wantError)
			}
		})
	}
}
