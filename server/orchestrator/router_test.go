package orchestrator

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type certSource struct{ cert []byte }

func (c certSource) Certificate(context.Context, FleetConfig, SpotWorker) ([]byte, error) {
	return c.cert, nil
}
func TestRouterRetriesRefusedHandshakeAndSticksToAcceptedWorker(t *testing.T) {
	token := "012345678901234567890123456789012345"
	refused := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing backend key")
		}
		http.Error(w, "full", 429)
	}))
	defer refused.Close()
	accepted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing backend key")
		}
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		for {
			kind, b, e := c.Read(r.Context())
			if e != nil {
				return
			}
			if c.Write(r.Context(), kind, b) != nil {
				return
			}
		}
	}))
	defer accepted.Close()
	c := testConfig(t)
	cloud := &fakeCloud{workers: []SpotWorker{{InstanceID: "i-a", Region: "us-west-2", LaunchedAt: time.Now(), EndpointURL: refused.URL}, {InstanceID: "i-b", Region: "us-east-1", LaunchedAt: time.Now(), EndpointURL: accepted.URL}}}
	o, _ := NewController(c, cloud, &fakeProbe{ready: true})
	defer o.Stop()
	o.Reconcile(context.Background())
	source := certSource{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: accepted.Certificate().Raw})}
	p, e := NewHTTPProbe(c, source)
	if e != nil {
		t.Fatal(e)
	}
	// httptest TLS servers use the same test certificate. Force refusal first.
	o.mu.Lock()
	a := o.state.Workers["i-a"]
	a.LatencyMs = .1
	o.state.Workers["i-a"] = a
	o.mu.Unlock()
	server := httptest.NewServer(o.ProxyHandler(p))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	for _, msg := range []string{"one", "two"} {
		if e = conn.Write(ctx, websocket.MessageBinary, []byte(msg)); e != nil {
			t.Fatal(e)
		}
		_, b, e := conn.Read(ctx)
		if e != nil || string(b) != msg {
			t.Fatal(e, string(b))
		}
	}
}
func TestProxyRequiresKeyAndDoesNotLeakFleetStatus(t *testing.T) {
	o, _ := NewController(testConfig(t), &fakeCloud{}, &fakeProbe{})
	defer o.Stop()
	r := httptest.NewRequest("GET", "/v1/realtime", nil)
	w := httptest.NewRecorder()
	o.ProxyHandler(nil).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated proxy accepted")
	}
}

func TestBatchOnlyReplaysExplicitAdmissionRefusal(t *testing.T) {
	for _, marked := range []bool{false, true} {
		t.Run(fmt.Sprint(marked), func(t *testing.T) {
			first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if marked {
					w.Header().Set("X-SpeekToMe-Admission", "rejected")
				}
				http.Error(w, "busy", 503)
			}))
			defer first.Close()
			second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("accepted")) }))
			defer second.Close()
			c := testConfig(t)
			o, _ := NewController(c, &fakeCloud{workers: []SpotWorker{{InstanceID: "i-a", Region: "us-west-2", LaunchedAt: time.Now(), EndpointURL: first.URL}, {InstanceID: "i-b", Region: "us-east-1", LaunchedAt: time.Now(), EndpointURL: second.URL}}}, &fakeProbe{ready: true})
			defer o.Stop()
			o.Reconcile(context.Background())
			o.mu.Lock()
			a := o.state.Workers["i-a"]
			a.LatencyMs = .1
			o.state.Workers["i-a"] = a
			o.mu.Unlock()
			p, _ := NewHTTPProbe(c, certSource{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: first.Certificate().Raw})})
			req := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader("recording"))
			req.Header.Set("Authorization", "Bearer 012345678901234567890123456789012345")
			w := httptest.NewRecorder()
			o.ProxyHandler(p).ServeHTTP(w, req)
			if marked && (w.Code != 200 || w.Body.String() != "accepted") {
				t.Fatal(w.Code, w.Body.String())
			}
			if !marked && w.Code != 503 {
				t.Fatal("replayed unknown backend refusal", w.Code)
			}
		})
	}
}
