package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpusFragmentedInputCoalescesAndFlushesBeforeCommit(t *testing.T) {
	type frame struct {
		kind int
		data []byte
	}
	frames := make(chan []frame, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var got []frame
		for {
			kind, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			got = append(got, frame{kind, data})
			if kind == websocket.TextMessage {
				var e map[string]any
				json.Unmarshal(data, &e)
				if e["type"] == "input_audio_buffer.commit" {
					frames <- got
					return
				}
			}
		}
	}))
	defer backend.Close()
	server := httptest.NewServer(newTestGateway(backend.URL))
	defer server.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime/opus", http.Header{"Authorization": {"Bearer " + testToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ogg, err := os.ReadFile("testdata/590ms.ogg")
	if err != nil {
		t.Fatal(err)
	}
	// Two independent Ogg streams with their own pre-skip/end granules, split
	// across arbitrary WS boundaries. This must preserve exactly 1.18s of PCM.
	for repeat := 0; repeat < 2; repeat++ {
		for at := 0; at < len(ogg); {
			end := min(at+137, len(ogg))
			if err = c.WriteMessage(websocket.BinaryMessage, ogg[at:end]); err != nil {
				t.Fatal(err)
			}
			at = end
		}
	}
	if err = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"input_audio_buffer.commit"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-frames:
		if len(got) != 4 {
			t.Fatal("unexpected frames", len(got))
		}
		for i, n := range []int{17920, 17920, 1920} {
			if got[i].kind != websocket.BinaryMessage || len(got[i].data) != n {
				t.Fatalf("frame %d: kind=%d size=%d", i, got[i].kind, len(got[i].data))
			}
		}
		if got[3].kind != websocket.TextMessage {
			t.Fatal("tail not sent before commit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no commit")
	}
}
