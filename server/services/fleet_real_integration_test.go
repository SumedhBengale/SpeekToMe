package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Opt-in test uses two real GPU gateways through SSH tunnels. The bridge drops
// the first connection, then routes the same application session to worker two.
func TestRealFleetStreamRecovery(t *testing.T) {
	targets := strings.Split(os.Getenv("SPEEKTOME_REAL_REPLAY_WORKERS"), ",")
	if len(targets) != 2 {
		t.Skip("requires two real worker tunnel URLs")
	}
	tokenPath := os.Getenv("SPEEKTOME_REAL_TOKEN_FILE")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	var sessions atomic.Int32
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		number := sessions.Add(1)
		index := 0
		if number > 1 {
			index = 1
		}
		back, _, e := websocket.Dial(r.Context(), strings.Replace(targets[index], "http", "ws", 1)+r.URL.RequestURI(), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + strings.TrimSpace(string(token))}}})
		if e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		defer back.CloseNow()
		front, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer front.CloseNow()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		done := make(chan struct{}, 2)
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				kind, data, e := back.Read(ctx)
				if e != nil {
					return
				}
				if front.Write(ctx, kind, data) != nil {
					return
				}
			}
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			chunks := 0
			for {
				kind, data, e := front.Read(ctx)
				if e != nil {
					return
				}
				if back.Write(ctx, kind, data) != nil {
					return
				}
				if kind == websocket.MessageBinary {
					chunks++
					if number == 1 && chunks == 10 {
						return
					}
				}
			}
		}()
		<-done
		cancel()
		front.CloseNow()
		back.CloseNow()
		<-done
	}))
	defer bridge.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", bridge.URL)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", tokenPath)
	files, err := filepath.Glob(filepath.Join(os.Getenv("SPEEKTOME_REAL_REPLAY_AUDIO"), "*.ogg"))
	if err != nil || len(files) < 12 {
		t.Fatal("requires independently encoded Opus chunks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stream, err := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", []string{"en"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close(websocket.StatusNormalClosure, "")
	for _, path := range files {
		audio, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = stream.SendAudio(ctx, audio); e != nil {
			t.Fatal(e)
		}
		time.Sleep(560 * time.Millisecond)
	}
	if err = stream.SendControl(ctx, map[string]interface{}{"type": "stop"}); err != nil {
		t.Fatal(err)
	}
	if err = stream.AwaitFinal(ctx); err != nil {
		t.Fatal(err)
	}
	final := ""
	for text := range stream.TextChan {
		final = text
	}
	if sessions.Load() < 2 || strings.TrimSpace(final) == "" {
		t.Fatalf("sessions=%d transcript=%q", sessions.Load(), final)
	}
	t.Logf("one application session recovered across both real workers; backend sessions=%d transcript=%q", sessions.Load(), final)
}

// Opt-in acoustic check: preview must arrive while the caller is still sending,
// not just when input_audio_buffer.commit flushes the decoder.
func TestRealFleetInterimBeforeCommit(t *testing.T) {
	fleet := os.Getenv("SPEEKTOME_TEST_INTERIM_FLEET_URL")
	if fleet == "" {
		t.Skip("requires real fleet")
	}
	t.Setenv("SPEEKTOME_FLEET_URL", fleet)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", os.Getenv("SPEEKTOME_TEST_INTERIM_TOKEN_FILE"))
	clips, err := filepath.Glob(filepath.Join(os.Getenv("SPEEKTOME_TEST_INTERIM_OPUS_DIR"), "*.ogg"))
	if err != nil || len(clips) == 0 {
		t.Fatal("requires speech clips")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	session, err := createFleetStreamingSession(ctx, "self_hosted_nemotron_3_5", []string{"en"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(websocket.StatusNormalClosure, "")
	preview := ""
	for i := 0; i < 20; i++ {
		clip, e := os.ReadFile(clips[i%len(clips)])
		if e != nil {
			t.Fatal(e)
		}
		if e = session.SendAudio(ctx, clip); e != nil {
			t.Fatal(e)
		}
		select {
		case text := <-session.TextChan:
			if strings.TrimSpace(text) != "" {
				preview = text
			}
		case <-time.After(560 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if preview == "" {
		t.Fatal("no text before commit")
	}
	t.Logf("received text before commit (%d characters)", len(preview))
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
	if strings.TrimSpace(final) == "" {
		t.Fatal("no final transcript")
	}
}
