package orchestrator

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The real worker gateway binary (deployment/spot-worker/gateway) behind TLS,
// driven by the real controller proxy: proves both ends agree on the transport
// envelope, and that the model runtime receives plaintext only on loopback.
//
//	go build -o /tmp/gateway ../deployment/spot-worker/gateway
//	SPEEKTOME_GATEWAY_BINARY=/tmp/gateway go test ./orchestrator -run RealGateway -v
func TestRealGatewayEncryptedBatchAndStream(t *testing.T) {
	binary := os.Getenv("SPEEKTOME_GATEWAY_BINARY")
	if binary == "" {
		t.Skip("requires a built worker gateway")
	}
	var sawBatch, sawStream string
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.Write([]byte("ok"))
		case strings.EqualFold(r.Header.Get("Upgrade"), "websocket"):
			sawStream = r.URL.RawQuery
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			for {
				kind, data, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				if kind == websocket.MessageBinary {
					data = []byte(fmt.Sprintf(`{"type":"pcm","bytes":%d}`, len(data)))
				}
				if conn.Write(r.Context(), websocket.MessageText, data) != nil {
					return
				}
			}
		default:
			body, _ := io.ReadAll(r.Body)
			sawBatch = fmt.Sprintf("%s?%s %s", r.URL.Path, r.URL.RawQuery, body)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"text":"private transcript"}`))
		}
	}))
	defer runtime.Close()

	dir := t.TempDir()
	token := "012345678901234567890123456789012345"
	os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600)
	os.WriteFile(filepath.Join(dir, "ready"), nil, 0600)
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	address := listener.Addr().String()
	listener.Close()
	gateway := exec.Command(binary, "-listen", address, "-upstream", runtime.URL, "-token-file", filepath.Join(dir, "token"),
		"-ready-file", filepath.Join(dir, "ready"), "-max-streams", "4")
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Process.Kill()
	gatewayURL, _ := url.Parse("http://" + address)
	tls := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(gatewayURL)) // stands in for nginx
	defer tls.Close()

	// The key arrives the same way production gets it: authenticated metadata.
	var meta WorkerMetadata
	for deadline := time.Now().Add(10 * time.Second); meta.TransportKey == ""; {
		req, _ := http.NewRequest("GET", tls.URL+"/worker/metadata", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if resp, err := tls.Client().Do(req); err == nil {
			json.NewDecoder(resp.Body).Decode(&meta)
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway never advertised a transport key")
		}
		time.Sleep(50 * time.Millisecond)
	}
	req, _ := http.NewRequest("POST", tls.URL+"/v1/audio/transcriptions", strings.NewReader("plaintext audio"))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := tls.Client().Do(req)
	if err != nil || resp.StatusCode != 400 || sawBatch != "" {
		t.Fatal("gateway accepted an unsealed request", err)
	}
	resp.Body.Close()

	o, keys := sealedController(t, "required", SpotWorker{InstanceID: "i-real", Region: "us-east-1", LaunchedAt: time.Now(), EndpointURL: tls.URL})
	keys["i-real"] = meta.TransportKey
	o.Reconcile(context.Background())
	p, _ := NewHTTPProbe(o.cfg, certSource{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tls.Certificate().Raw})})
	front := httptest.NewServer(o.ProxyHandler(p))
	defer front.Close()

	batch, _ := http.NewRequest("POST", front.URL+"/v1/audio/transcriptions?language=en", strings.NewReader("recorded meeting audio"))
	batch.Header.Set("Authorization", testBearer)
	batch.Header.Set("Content-Type", "audio/wav")
	resp, err = http.DefaultClient.Do(batch)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != `{"text":"private transcript"}` || sawBatch != "/v1/audio/transcriptions?language=en recorded meeting audio" {
		t.Fatalf("batch: HTTP %d %s; runtime saw %q", resp.StatusCode, body, sawBatch)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/v1/audio/transcriptions/realtime?language=en",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {testBearer}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"session.update","session":{}}`))
	if _, reply, err := conn.Read(ctx); err != nil || !strings.Contains(string(reply), "session.update") {
		t.Fatal("stream control", string(reply), err)
	}
	conn.Write(ctx, websocket.MessageBinary, make([]byte, 17920))
	if _, reply, err := conn.Read(ctx); err != nil || string(reply) != `{"type":"pcm","bytes":17920}` {
		t.Fatal("stream audio", string(reply), err)
	}
	if sawStream != "language=en" {
		t.Fatal("runtime stream query", sawStream)
	}
	t.Log("real gateway: unsealed request refused; sealed batch and stream reached the runtime as plaintext only on loopback")
}
