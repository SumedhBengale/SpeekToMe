package orchestrator

import (
	"errors"
	"io"
	"net/http"

	"speektome/transport"
)

// transportFor starts a sealed session to w. It returns ok=false when audio may
// not be sent to w at all: the policy requires encryption and w advertises no
// key. A nil session with ok=true is the "preferred"-mode plaintext fallback.
func (o *Controller) transportFor(w SpotWorker) (*transport.Session, string, bool) {
	if w.Metadata.TransportKey == "" {
		return nil, "", o.cfg.TranscriptionEncryption != "required"
	}
	session, header, err := transport.Dial(w.Metadata.TransportKey)
	if err != nil {
		return nil, "", false
	}
	return session, header, true
}

// sealHeaders moves the query string (which carries vocabulary terms) and the
// plaintext length into the sealed request metadata.
func sealHeaders(h http.Header, session *transport.Session, header, query string, length int64) {
	h.Set(transport.SessionHeader, header)
	h.Set(transport.RequestHeader, session.SealRequest(transport.Request{Query: query, ContentLength: length}))
}

// requestURI is the worker URL path: without its query once the query is sealed.
func requestURI(r *http.Request, session *transport.Session) string {
	if session == nil {
		return r.URL.RequestURI()
	}
	return r.URL.EscapedPath()
}

var errUnsealedResponse = errors.New("worker returned an unsealed response")

// responseBody opens a sealed worker response. A sealed session must get a
// sealed reply; anything else is treated as a failed request.
func responseBody(session *transport.Session, resp *http.Response) (io.Reader, error) {
	if session == nil {
		return resp.Body, nil
	}
	if resp.Header.Get(transport.ResponseHeader) != "v1" {
		return nil, errUnsealedResponse
	}
	return session.OpenBody(resp.Body), nil
}
