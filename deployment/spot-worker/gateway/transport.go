package main

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	"speektome/transport"
)

// openTransport opens the controller's sealed session for a /v1/ request. The
// query string and body are restored in memory only; the request then proceeds
// exactly as a plaintext one would. With transport required, unsealed requests
// are refused before any audio is read.
func (g *gateway) openTransport(w http.ResponseWriter, r *http.Request) (*transport.Session, bool) {
	header := r.Header.Get(transport.SessionHeader)
	if header == "" {
		if g.transportRequired {
			http.Error(w, "transport encryption required", 400)
			return nil, false
		}
		return nil, true
	}
	if g.transport == nil {
		http.Error(w, "transport encryption unavailable", 400)
		return nil, false
	}
	session, err := g.transport.Accept(header)
	if errors.Is(err, transport.ErrStaleKey) {
		// Sealed to this worker's previous process: nothing was read, so the
		// controller may retry here once it has refreshed the key.
		w.Header().Set("X-SpeekToMe-Admission", "rejected")
		http.Error(w, "stale transport key", 503)
		return nil, false
	}
	if err != nil {
		http.Error(w, "invalid transport session", 400)
		return nil, false
	}
	meta, err := session.OpenRequest(r.Header.Get(transport.RequestHeader))
	if err != nil {
		http.Error(w, "invalid transport request", 400)
		return nil, false
	}
	r.Header.Del(transport.SessionHeader)
	r.Header.Del(transport.RequestHeader)
	r.URL.RawQuery = meta.Query
	r.RequestURI = r.URL.RequestURI()
	if !websocket.IsWebSocketUpgrade(r) {
		r.Body = struct {
			io.Reader
			io.Closer
		}{session.OpenBody(r.Body), r.Body}
		r.ContentLength = meta.ContentLength
		r.TransferEncoding = nil
		r.Header.Del("Content-Length")
		if meta.ContentLength >= 0 {
			r.Header.Set("Content-Length", strconv.FormatInt(meta.ContentLength, 10))
		}
	}
	return session, true
}

// sealedResponse seals every byte of an HTTP response body, including runtime
// results proxied from the local model server, before it leaves this process.
type sealedResponse struct {
	http.ResponseWriter
	body        *transport.BodyWriter
	wroteHeader bool
}

func newSealedResponse(w http.ResponseWriter, session *transport.Session) *sealedResponse {
	return &sealedResponse{ResponseWriter: w, body: session.BodyWriter(w)}
}

func (s *sealedResponse) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.wroteHeader = true
	s.Header().Del("Content-Length")
	s.Header().Set(transport.ResponseHeader, "v1")
	s.ResponseWriter.WriteHeader(code)
}

func (s *sealedResponse) Write(p []byte) (int, error) {
	if !s.wroteHeader {
		s.WriteHeader(200)
	}
	return s.body.Write(p)
}

func (s *sealedResponse) Flush() {
	if !s.wroteHeader {
		s.WriteHeader(200)
	}
	if s.body.Flush() == nil {
		if f, ok := s.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (s *sealedResponse) finish() {
	if !s.wroteHeader {
		s.WriteHeader(200)
	}
	_ = s.body.Close()
}

// streamCodec seals WebSocket messages to and from the controller; nil means a
// plaintext session (only when transport is not required).
type streamCodec struct{ session *transport.Session }

func (c streamCodec) read(conn *websocket.Conn) (int, []byte, error) {
	kind, data, err := conn.ReadMessage()
	if err != nil || c.session == nil {
		return kind, data, err
	}
	if kind != websocket.BinaryMessage {
		return 0, nil, transport.ErrRejected
	}
	return c.session.OpenMessage(data)
}

func (c streamCodec) write(conn *websocket.Conn, kind int, data []byte) error {
	if c.session == nil {
		return conn.WriteMessage(kind, data)
	}
	return conn.WriteMessage(websocket.BinaryMessage, c.session.SealMessage(kind, data))
}
