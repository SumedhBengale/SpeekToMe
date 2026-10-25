package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"speektome/transport"
)

func sealedGateway(t *testing.T, upstream string) *gateway {
	t.Helper()
	g := newTestGateway(upstream)
	identity, err := transport.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g.transport, g.transportRequired = identity, true
	return g
}

func TestRequiredTransportRefusesPlaintextBeforeTheRuntime(t *testing.T) {
	called := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer backend.Close()
	server := httptest.NewServer(sealedGateway(t, backend.URL))
	defer server.Close()
	req, _ := http.NewRequest("POST", server.URL+"/v1/audio/transcriptions?language=en", strings.NewReader("plaintext audio"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 || called {
		t.Fatalf("plaintext request: HTTP %d, reached runtime=%v", res.StatusCode, called)
	}
	stale, _ := transport.NewIdentity()
	_, header, _ := transport.Dial(stale.PublicKey())
	req, _ = http.NewRequest("POST", server.URL+"/v1/audio/transcriptions", strings.NewReader("x"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set(transport.SessionHeader, header)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 || res.Header.Get("X-SpeekToMe-Admission") != "rejected" || called {
		t.Fatal("stale key must be a retryable refusal", res.StatusCode)
	}
}

func TestSealedBatchRequestReachesRuntimeAsPlaintextAndReturnsSealed(t *testing.T) {
	audio := bytes.Repeat([]byte("meeting audio "), 20000) // spans several records
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, audio) || r.URL.Query().Get("language") != "en" || r.ContentLength != int64(len(audio)) {
			t.Errorf("runtime saw wrong request: %d bytes, query %q, length %d", len(body), r.URL.RawQuery, r.ContentLength)
		}
		if r.Header.Get(transport.SessionHeader) != "" || r.Header.Get(transport.RequestHeader) != "" {
			t.Error("transport headers forwarded to the runtime")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"private transcript"}`))
	}))
	defer backend.Close()
	g := sealedGateway(t, backend.URL)
	server := httptest.NewServer(g)
	defer server.Close()
	session, header, err := transport.Dial(g.transport.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", server.URL+"/v1/audio/transcriptions", session.SealBody(bytes.NewReader(audio)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set(transport.SessionHeader, header)
	req.Header.Set(transport.RequestHeader, session.SealRequest(transport.Request{Query: "language=en", ContentLength: int64(len(audio))}))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.Header.Get(transport.ResponseHeader) != "v1" || bytes.Contains(raw, []byte("private transcript")) {
		t.Fatal("response left the gateway unsealed")
	}
	plain, err := io.ReadAll(session.OpenBody(bytes.NewReader(raw)))
	if err != nil || string(plain) != `{"text":"private transcript"}` {
		t.Fatal(string(plain), err)
	}
}

func TestSealedStreamOpensMessagesAndSealsReplies(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("contexts") != "Kubernetes" {
			t.Errorf("runtime query %q", r.URL.RawQuery)
		}
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		for {
			kind, b, e := c.ReadMessage()
			if e != nil {
				return
			}
			if kind == websocket.BinaryMessage {
				b = append([]byte("heard "), b...)
			}
			if c.WriteMessage(kind, b) != nil {
				return
			}
		}
	}))
	defer backend.Close()
	g := sealedGateway(t, backend.URL)
	server := httptest.NewServer(g)
	defer server.Close()
	session, header, _ := transport.Dial(g.transport.PublicKey())
	headers := http.Header{"Authorization": {"Bearer " + testToken}, transport.SessionHeader: {header},
		transport.RequestHeader: {session.SealRequest(transport.Request{Query: "contexts=Kubernetes", ContentLength: -1})}}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/audio/transcriptions/realtime", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, msg := range []struct {
		kind int
		data []byte
	}{{websocket.TextMessage, []byte(`{"type":"session.update","session":{}}`)}, {websocket.BinaryMessage, []byte("pcm chunk")}} {
		if err = c.WriteMessage(websocket.BinaryMessage, session.SealMessage(msg.kind, msg.data)); err != nil {
			t.Fatal(err)
		}
		wire, raw, err := c.ReadMessage()
		if err != nil || wire != websocket.BinaryMessage || bytes.Contains(raw, msg.data) {
			t.Fatal("reply not sealed", err)
		}
		kind, plain, err := session.OpenMessage(raw)
		if err != nil || kind != msg.kind {
			t.Fatal(err)
		}
		if msg.kind == websocket.TextMessage {
			var event map[string]any
			if json.Unmarshal(plain, &event) != nil || event["type"] != "session.update" {
				t.Fatal(string(plain))
			}
		} else if string(plain) != "heard pcm chunk" {
			t.Fatal(string(plain))
		}
	}
	// A plaintext frame inside a sealed session closes the stream instead of reaching the runtime.
	if err = c.WriteMessage(websocket.BinaryMessage, []byte("plaintext")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.ReadMessage(); err == nil {
		t.Fatal("plaintext frame accepted inside sealed session")
	}
}
