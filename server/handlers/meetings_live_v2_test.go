package handlers

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"speektome-server/db"
)

// liveChunk is a 560 ms Ogg page header, enough for the fleet's duration check.
func liveChunk(channel byte, seq uint32) []byte {
	page := make([]byte, 28)
	copy(page, "OggS")
	binary.LittleEndian.PutUint64(page[6:14], 26880)
	page[26] = 1
	frame := []byte{channel, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(frame[1:5], seq)
	return append(frame, page...)
}

// liveFixture runs the live relay against a fake fleet worker that finalizes
// one word, 0.1-0.3 s into each chunk it receives.
func liveFixture(t *testing.T) (*httptest.Server, *db.Database) {
	t.Helper()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		for chunk := 0; ; {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if kind == websocket.MessageBinary {
				start := float64(chunk)*0.56 + 0.1
				conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"w%d","words":[{"word":"w%d","start":%.2f,"end":%.2f}]}`, chunk, chunk, start, start+0.2)))
				chunk++
				continue
			}
			if strings.Contains(string(data), "input_audio_buffer.commit") {
				conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.committed"}`))
				return
			}
		}
	}))
	t.Cleanup(worker.Close)
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("01234567890123456789012345678901"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err = database.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(1,'owner@example.com','Owner','pro',1)`); err != nil {
		t.Fatal(err)
	}
	database.CreateSession("live-session", 1, 0)
	if _, err = database.GetDB().Exec(`INSERT INTO account_leases(user_id,operation,owner,expires) VALUES(1,'meeting recording','desktop',?)`, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	app := &App{DB: database}
	server := httptest.NewServer(http.HandlerFunc(app.HandleMeetingStream))
	t.Cleanup(server.Close)
	return server, database
}

func dialLive(t *testing.T, ctx context.Context, server *httptest.Server, resume string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer live-session"}}})
	if err != nil {
		t.Fatal(err)
	}
	start := `{"type":"start","protocol":2,"job_id":"m1","language":"en","sample_rate":16000,"channels":1,"resume":` + resume + `}`
	if err = conn.Write(ctx, websocket.MessageText, []byte(start)); err != nil {
		t.Fatal(err)
	}
	return conn
}

// readEvent returns the next event of one type, skipping preview turns.
func readEvent(t *testing.T, ctx context.Context, conn *websocket.Conn, kind string) []byte {
	t.Helper()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", kind, err)
		}
		var event struct {
			Type string `json:"type"`
		}
		json.Unmarshal(data, &event)
		if event.Type == kind {
			return data
		}
	}
}

func TestLiveMeetingSignsWordsOnTheRecordingTimeline(t *testing.T) {
	server, _ := liveFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A reconnect resuming the microphone at chunk 10 (5.6 s), with the desktop
	// already holding words up to 5.6 s.
	conn := dialLive(t, ctx, server, `{"mic":{"seq":10,"covered_to":5.6}}`)
	defer conn.CloseNow()
	readEvent(t, ctx, conn, "session_ready")
	if err := conn.Write(ctx, websocket.MessageBinary, liveChunk(0x01, 10)); err != nil {
		t.Fatal(err)
	}
	line := readEvent(t, ctx, conn, "words")
	got := parseLiveTranscript(line, 1, "m1")["mic"]
	if len(got.Words) != 1 || got.Words[0].Word != "w0" || got.Words[0].Start < 5.69 || got.Words[0].Start > 5.71 {
		t.Fatalf("words not on the recording timeline: %+v", got)
	}
	if len(got.Covered) != 1 || got.Covered[0].Start < 5.6-1e-9 || got.Covered[0].Start > 5.6+1e-9 {
		t.Fatal(got.Covered)
	}
	// A skipped chunk would misplace every later word, so the relay refuses it.
	if err := conn.Write(ctx, websocket.MessageBinary, liveChunk(0x01, 12)); err != nil {
		t.Fatal(err)
	}
	if event := readEvent(t, ctx, conn, "error"); !strings.Contains(string(event), "out of order") {
		t.Fatal(string(event))
	}
}

func TestLiveMeetingReconnectReplacesTheStaleSession(t *testing.T) {
	server, _ := liveFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stale := dialLive(t, ctx, server, `{}`)
	defer stale.CloseNow()
	readEvent(t, ctx, stale, "session_ready")
	// The desktop's network dropped without closing the old socket; it
	// reconnects for the same meeting.
	fresh := dialLive(t, ctx, server, `{"mic":{"seq":3,"covered_to":1.68}}`)
	defer fresh.CloseNow()
	readEvent(t, ctx, fresh, "session_ready")
	if _, _, err := stale.Read(ctx); err == nil {
		// Drain anything already in flight; the stale socket must then close.
		for {
			if _, _, err = stale.Read(ctx); err != nil {
				break
			}
		}
	}
	if err := fresh.Write(ctx, websocket.MessageBinary, liveChunk(0x01, 3)); err != nil {
		t.Fatal(err)
	}
	readEvent(t, ctx, fresh, "words")
}
