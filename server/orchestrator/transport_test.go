package orchestrator

import (
	"bytes"
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"speektome/transport"
)

const testBearer = "Bearer 012345678901234567890123456789012345"

// sealedWorker behaves like an encrypting gateway: it refuses anything not
// sealed to its in-memory identity and records what it saw after opening.
type sealedWorker struct {
	t        *testing.T
	identity *transport.Identity
	server   *httptest.Server
	calls    atomic.Int32
}

func newSealedWorker(t *testing.T) *sealedWorker {
	t.Helper()
	identity, err := transport.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	s := &sealedWorker{t: t, identity: identity}
	s.server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

func (s *sealedWorker) serve(w http.ResponseWriter, r *http.Request) {
	s.calls.Add(1)
	if r.URL.RawQuery != "" {
		s.t.Error("query string sent outside the envelope:", r.URL.RawQuery)
	}
	session, err := s.identity.Accept(r.Header.Get(transport.SessionHeader))
	if err != nil {
		http.Error(w, "transport encryption required", 400)
		return
	}
	meta, err := session.OpenRequest(r.Header.Get(transport.RequestHeader))
	if err != nil || meta.Query != "language=en&contexts=Kubernetes" {
		s.t.Error("request metadata", meta, err)
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, sealed, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			kind, plain, err := session.OpenMessage(sealed)
			if err != nil {
				s.t.Error("controller sent an unopenable message")
				return
			}
			if conn.Write(r.Context(), websocket.MessageBinary, session.SealMessage(kind, append([]byte("heard "), plain...))) != nil {
				return
			}
		}
	}
	raw, _ := io.ReadAll(r.Body)
	if bytes.Contains(raw, []byte("recording")) {
		s.t.Error("audio crossed the wire unsealed")
	}
	plain, err := io.ReadAll(session.OpenBody(bytes.NewReader(raw)))
	if err != nil || string(plain) != "recording" || meta.ContentLength != int64(len("recording")) {
		s.t.Error("body", string(plain), err, meta.ContentLength)
	}
	w.Header().Set(transport.ResponseHeader, "v1")
	reply := session.BodyWriter(w)
	reply.Write([]byte(`{"text":"private transcript"}`))
	reply.Close()
}

func sealedController(t *testing.T, policy string, workers ...SpotWorker) (*Controller, map[string]string) {
	t.Helper()
	c := testConfig(t)
	c.TranscriptionEncryption = policy
	keys := map[string]string{}
	o, err := NewController(c, &fakeCloud{workers: workers}, &fakeProbe{ready: true, keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Stop)
	return o, keys
}

func TestRequiredEncryptionSealsBatchAndNeverUsesKeylessWorkers(t *testing.T) {
	keyed := newSealedWorker(t)
	var keylessCalls atomic.Int32
	keyless := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { keylessCalls.Add(1) }))
	defer keyless.Close()
	o, keys := sealedController(t, "required",
		SpotWorker{InstanceID: "i-keyless", Region: "us-west-2", LaunchedAt: time.Now(), EndpointURL: keyless.URL},
		SpotWorker{InstanceID: "i-keyed", Region: "us-east-1", LaunchedAt: time.Now(), EndpointURL: keyed.server.URL})
	keys["i-keyed"] = keyed.identity.PublicKey()
	o.Reconcile(context.Background())
	o.mu.Lock()
	fast := o.state.Workers["i-keyless"]
	fast.LatencyMs = .1 // would win on load and latency alone
	o.state.Workers["i-keyless"] = fast
	o.mu.Unlock()
	p, _ := NewHTTPProbe(o.cfg, certSource{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: keyed.server.Certificate().Raw})})
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions?language=en&contexts=Kubernetes", strings.NewReader("recording"))
	req.Header.Set("Authorization", testBearer)
	w := httptest.NewRecorder()
	o.ProxyHandler(p).ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != `{"text":"private transcript"}` {
		t.Fatal(w.Code, w.Body.String())
	}
	if keylessCalls.Load() != 0 || keyed.calls.Load() != 1 {
		t.Fatal("audio offered to a worker without a transport key", keylessCalls.Load(), keyed.calls.Load())
	}
}

func TestRequiredEncryptionWithOnlyKeylessWorkersRefuses(t *testing.T) {
	var calls atomic.Int32
	keyless := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer keyless.Close()
	o, _ := sealedController(t, "required", SpotWorker{InstanceID: "i-a", Region: "us-east-1", LaunchedAt: time.Now(), EndpointURL: keyless.URL})
	o.Reconcile(context.Background())
	p, _ := NewHTTPProbe(o.cfg, certSource{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: keyless.Certificate().Raw})})
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader("recording"))
	req.Header.Set("Authorization", testBearer)
	w := httptest.NewRecorder()
	o.ProxyHandler(p).ServeHTTP(w, req)
	if w.Code != 503 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
}

func TestSealedStreamRoundTripsThroughProxy(t *testing.T) {
	worker := newSealedWorker(t)
	o, keys := sealedController(t, "required", SpotWorker{InstanceID: "i-a", Region: "us-east-1", LaunchedAt: time.Now(), EndpointURL: worker.server.URL})
	keys["i-a"] = worker.identity.PublicKey()
	o.Reconcile(context.Background())
	p, _ := NewHTTPProbe(o.cfg, certSource{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: worker.server.Certificate().Raw})})
	front := httptest.NewServer(o.ProxyHandler(p))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/v1/realtime/opus?language=en&contexts=Kubernetes",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {testBearer}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, msg := range []struct {
		kind websocket.MessageType
		data string
	}{{websocket.MessageText, `{"type":"session.update"}`}, {websocket.MessageBinary, "opus page"}} {
		if err := conn.Write(ctx, msg.kind, []byte(msg.data)); err != nil {
			t.Fatal(err)
		}
		kind, reply, err := conn.Read(ctx)
		if err != nil || kind != msg.kind || string(reply) != "heard "+msg.data {
			t.Fatal(kind, string(reply), err)
		}
	}
}
