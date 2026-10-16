package orchestrator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type CertificateSource interface {
	Certificate(context.Context, FleetConfig, SpotWorker) ([]byte, error)
}
type HTTPProbe struct {
	cfg     FleetConfig
	source  CertificateSource
	token   string
	mu      sync.Mutex
	clients map[string]*http.Client
}

func NewHTTPProbe(c FleetConfig, source CertificateSource) (*HTTPProbe, error) {
	b, e := os.ReadFile(c.TokenFile)
	if e != nil {
		return nil, e
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 32 {
		return nil, errors.New("worker token must be at least 32 characters")
	}
	return &HTTPProbe{cfg: c, source: source, token: token, clients: map[string]*http.Client{}}, nil
}
func (p *HTTPProbe) Client(ctx context.Context, w SpotWorker) (*http.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := w.InstanceID + "/" + w.EndpointURL
	if client := p.clients[key]; client != nil {
		return client, nil
	}
	cert, e := p.source.Certificate(ctx, p.cfg, w)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		return nil, errors.New("invalid worker certificate")
	}
	// Explicitly trust only this worker's S3-enrolled certificate. Never skip TLS
	// verification; IP SAN must match the EC2-discovered IPv6 address.
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, MaxIdleConnsPerHost: 8, ResponseHeaderTimeout: 5 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p.clients[key] = client
	return client, nil
}
func (p *HTTPProbe) Metadata(ctx context.Context, w SpotWorker) (WorkerMetadata, float64, error) {
	var m WorkerMetadata
	client, e := p.Client(ctx, w)
	if e != nil {
		return m, 0, e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", w.EndpointURL+"/worker/metadata", nil)
	if e != nil {
		return m, 0, e
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	start := time.Now()
	resp, e := client.Do(req)
	rtt := float64(time.Since(start).Microseconds()) / 1000
	if e != nil {
		return m, rtt, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		return m, rtt, errors.New("metadata rejected")
	}
	e = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&m)
	return m, rtt, e
}
func (p *HTTPProbe) Drain(ctx context.Context, w SpotWorker) error {
	client, e := p.Client(ctx, w)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", w.EndpointURL+"/worker/drain", nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, e := client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		return errors.New("drain rejected")
	}
	return nil
}
