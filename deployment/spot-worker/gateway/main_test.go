package main

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testToken = "test-token-012345678901234567890123456789"

func newTestGateway(upstream string) *gateway {
	u, _ := url.Parse(upstream)
	return &gateway{tokenHash: sha256.Sum256([]byte(testToken)), upstream: u, slots: make(chan struct{}, 1), proxy: httputil.NewSingleHostReverseProxy(u)}
}
func TestAuthBeforeAnyUpstreamRequest(t *testing.T) {
	called := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.Write([]byte("ok")) }))
	defer backend.Close()
	server := httptest.NewServer(newTestGateway(backend.URL))
	defer server.Close()
	for _, path := range []string{"/health", "/v1/audio/transcriptions", "/v1/realtime", "/worker/drain"} {
		for _, token := range []string{"", "Bearer wrong"} {
			req, _ := http.NewRequest("GET", server.URL+path, nil)
			req.Header.Set("Authorization", token)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 401 {
				t.Fatalf("%s: %d", path, res.StatusCode)
			}
		}
	}
	if called {
		t.Fatal("unauthenticated request reached backend")
	}
	req, _ := http.NewRequest("POST", server.URL+"/v1/audio/transcriptions", strings.NewReader("audio"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	backend.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("worker token leaked upstream")
		}
		w.Write([]byte("transcript"))
	})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if string(b) != "transcript" {
		t.Fatal(string(b))
	}
}
func TestWebSocketAdmissionReleaseAndDrain(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if c.WriteMessage(kind, b) != nil {
				return
			}
		}
	}))
	defer backend.Close()
	g := newTestGateway(backend.URL)
	server := httptest.NewServer(g)
	defer server.Close()
	target := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/realtime"
	headers := http.Header{"Authorization": []string{"Bearer " + testToken}}
	c, _, err := websocket.DefaultDialer.Dial(target, headers)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{1, 2, 3, 4}
	if err = c.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	_, got, err := c.ReadMessage()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal(err, got)
	}
	_, res, err := websocket.DefaultDialer.Dial(target, headers)
	if err == nil || res.StatusCode != 429 {
		t.Fatal("capacity guard failed")
	}
	res.Body.Close()
	_, res, err = websocket.DefaultDialer.Dial(target, nil)
	if err == nil || res.StatusCode != 401 {
		t.Fatal("WS auth failed")
	}
	res.Body.Close()
	c.Close()
	deadline := time.Now().Add(time.Second)
	for len(g.slots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(g.slots) != 0 {
		t.Fatal("slot leak")
	}
	req, _ := http.NewRequest("POST", server.URL+"/worker/drain", nil)
	req.Header = headers
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	_, res, err = websocket.DefaultDialer.Dial(target, headers)
	if err == nil || res.StatusCode != 503 {
		t.Fatal("draining worker accepted stream")
	}
	res.Body.Close()
}
func TestOggPacketAcrossPagesAndMessages(t *testing.T) {
	page := func(laces []byte, data []byte) []byte {
		h := make([]byte, 27)
		copy(h, "OggS")
		h[26] = byte(len(laces))
		return append(append(h, laces...), data...)
	}
	input := append(page([]byte{255}, bytes.Repeat([]byte{7}, 255)), page([]byte{2, 3}, []byte{7, 7, 1, 2, 3})...)
	p := oggPackets{}
	var all []oggPacket
	for _, b := range input {
		packets, err := p.feed([]byte{b})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, packets...)
	}
	if len(all) != 2 || len(all[0].data) != 257 || !bytes.Equal(all[1].data, []byte{1, 2, 3}) {
		t.Fatal("packet assembly failed")
	}
}

func TestOggEndGranuleAndReadyGuard(t *testing.T) {
	page := make([]byte, 29)
	copy(page, "OggS")
	page[5] = 4
	page[6] = 123
	page[26] = 1
	page[27] = 1
	page[28] = 42
	parser := oggPackets{}
	packets, err := parser.feed(page)
	if err != nil || len(packets) != 1 || !packets[0].eos || packets[0].granule != 123 {
		t.Fatal("EOS duration lost", err, packets)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unready worker reached upstream") }))
	defer backend.Close()
	g := newTestGateway(backend.URL)
	g.readyFile = t.TempDir() + "/missing"
	server := httptest.NewServer(g)
	defer server.Close()
	req, _ := http.NewRequest("POST", server.URL+"/v1/audio/transcriptions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal("unready worker accepted inference", res.StatusCode)
	}
}
