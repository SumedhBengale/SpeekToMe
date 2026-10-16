package orchestrator

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// ProxyHandler balances only new sessions/requests. It does not claim that an
// accepted stateful WebSocket can be replayed without the application's journal.
func (o *Controller) ProxyHandler(p *HTTPProbe) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !o.authenticated(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			o.proxyStream(p, w, r)
			return
		}
		o.proxyBatch(p, w, r)
	})
}
func (o *Controller) proxyStream(p *HTTPProbe, w http.ResponseWriter, r *http.Request) {
	for _, worker := range o.Candidates() {
		if !o.reserve(worker.InstanceID) {
			continue
		}
		client, e := p.Client(r.Context(), worker)
		if e != nil {
			o.release(worker.InstanceID)
			continue
		}
		target := "wss" + strings.TrimPrefix(worker.EndpointURL, "https") + r.URL.RequestURI()
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		wsClient := *client
		wsClient.Timeout = 0
		backend, resp, e := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: &wsClient, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + p.token}}})
		cancel()
		if e != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			o.release(worker.InstanceID)
			continue
		}
		front, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // localhost, bearer-authenticated service; not TLS verification
		if e != nil {
			backend.CloseNow()
			o.release(worker.InstanceID)
			return
		}
		front.SetReadLimit(8 << 20)
		backend.SetReadLimit(8 << 20)
		ctx, cancel = context.WithCancel(r.Context())
		done := make(chan struct{}, 2)
		copyMessages := func(dst, src *websocket.Conn) {
			defer func() { done <- struct{}{} }()
			for {
				kind, b, e := src.Read(ctx)
				if e != nil {
					return
				}
				if dst.Write(ctx, kind, b) != nil {
					return
				}
			}
		}
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					o.mu.RLock()
					state := o.state.Workers[worker.InstanceID]
					o.mu.RUnlock()
					if state.Rebalance || state.Interruption || state.State != StateActive {
						backend.CloseNow()
						return
					}
				}
			}
		}()
		go copyMessages(front, backend)
		go copyMessages(backend, front)
		<-done
		cancel()
		front.CloseNow()
		backend.CloseNow()
		<-done
		o.release(worker.InstanceID)
		return
	}
	w.Header().Set("Retry-After", "1")
	http.Error(w, "no inference worker has available capacity", 503)
}
func (o *Controller) proxyBatch(p *HTTPProbe, w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/meeting/") {
		o.proxyMeeting(p, w, r)
		return
	}
	// Replay body only for explicit pre-admission refusals. Use a disk spool so
	// recordings don't multiply memory usage on the main server.
	f, e := os.CreateTemp("", "speektome-inference-body-*")
	if e != nil {
		http.Error(w, "spool unavailable", 503)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = io.Copy(f, http.MaxBytesReader(w, r.Body, 100<<20)); e != nil {
		http.Error(w, "invalid or oversized body", 413)
		return
	}
	for _, worker := range o.Candidates() {
		if !o.reserve(worker.InstanceID) {
			continue
		}
		client, e := p.Client(r.Context(), worker)
		if e != nil {
			o.release(worker.InstanceID)
			continue
		}
		if _, e := f.Seek(0, 0); e != nil {
			o.release(worker.InstanceID)
			http.Error(w, "spool read failed", 500)
			return
		}
		req, e := http.NewRequestWithContext(r.Context(), r.Method, worker.EndpointURL+r.URL.RequestURI(), io.NopCloser(f))
		if e != nil {
			o.release(worker.InstanceID)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
		// Batch jobs may last much longer than the health/handshake timeout.
		batchClient := *client
		batchClient.Timeout = 0
		transport := client.Transport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 0
		defer transport.CloseIdleConnections()
		batchClient.Transport = transport
		resp, e := batchClient.Do(req)
		if e != nil {
			o.release(worker.InstanceID)
			http.Error(w, "inference request failed; outcome may be unknown", 502)
			return
		}
		if (resp.StatusCode == 429 || resp.StatusCode == 503) && resp.Header.Get("X-SpeekToMe-Admission") == "rejected" {
			resp.Body.Close()
			o.release(worker.InstanceID)
			continue
		}
		for _, name := range []string{"Content-Type", "Retry-After"} {
			if v := resp.Header.Get(name); v != "" {
				w.Header().Set(name, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		resp.Body.Close()
		o.release(worker.InstanceID)
		return
	}
	w.Header().Set("Retry-After", "1")
	http.Error(w, "no inference worker has available capacity", 503)
}
