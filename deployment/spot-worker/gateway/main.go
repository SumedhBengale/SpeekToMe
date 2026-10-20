package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/opus"
)

type gateway struct {
	tokenHash        [32]byte
	upstream         *url.URL
	slots            chan struct{}
	draining         atomic.Bool
	readyFile        string
	proxy            *httputil.ReverseProxy
	stats            telemetry
	admission        sync.Mutex
	diarSlots        chan struct{}
	batchActive      bool
	meetingID        string
	meetingStages    map[string]bool
	meetingExclusive bool
	drainFile        string
	spotFile         string
}

func (g *gateway) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	hash := sha256.Sum256([]byte(strings.TrimPrefix(h, "Bearer ")))
	return subtle.ConstantTimeCompare(hash[:], g.tokenHash[:]) == 1
}
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.URL.Path == "/health" || r.URL.Path == "/ready" || r.URL.Path == "/worker/metadata" {
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(g.upstream.String() + "/health")
		ready := err == nil && resp.StatusCode == 200
		if resp != nil {
			resp.Body.Close()
		}
		if g.readyFile != "" {
			_, err = os.Stat(g.readyFile)
			ready = ready && err == nil
		}
		code := 200
		if !ready || g.isDraining() {
			code = 503
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		m := g.stats.snapshot()
		m["ready"] = ready
		m["draining"] = g.isDraining()
		m["active_streams"] = len(g.slots)
		m["max_streams"] = cap(g.slots)
		g.admission.Lock()
		m["active_meeting_jobs"] = 0
		if len(g.meetingStages) > 0 {
			m["active_meeting_jobs"] = 1
		}
		m["active_meeting_stages"] = len(g.meetingStages)
		m["meeting_exclusive"] = g.meetingExclusive
		m["configured_max_streams"] = cap(g.slots)
		if g.meetingExclusive && len(g.meetingStages) > 0 {
			m["max_streams"] = 0
		}
		g.admission.Unlock()
		m["max_diarization_requests"] = cap(g.diarSlots)
		m["vocabulary_matching_schema"] = 1
		m["instance_id"] = os.Getenv("INSTANCE_ID")
		m["region"] = os.Getenv("AWS_REGION")
		m["profile"] = os.Getenv("WORKER_PROFILE")
		m["spot_signal"] = nil
		if data, err := os.ReadFile(g.spotFile); err == nil {
			var signal any
			if json.Unmarshal(data, &signal) == nil {
				m["spot_signal"] = signal
			}
		}
		json.NewEncoder(w).Encode(m)
		return
	}
	if r.URL.Path == "/worker/drain" {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		g.draining.Store(true)
		if g.drainFile != "" {
			if err := os.WriteFile(g.drainFile, []byte("draining\n"), 0600); err != nil {
				http.Error(w, "cannot persist drain", 500)
				return
			}
		}
		w.WriteHeader(204)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		http.NotFound(w, r)
		return
	}
	if g.readyFile != "" {
		if _, err := os.Stat(g.readyFile); err != nil {
			w.Header().Set("X-SpeekToMe-Admission", "rejected")
			http.Error(w, "worker not ready", 503)
			return
		}
	}
	if g.isDraining() {
		w.Header().Set("X-SpeekToMe-Admission", "rejected")
		http.Error(w, "worker draining", 503)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/meeting/") {
		g.meeting(w, r)
		return
	}
	g.admission.Lock()
	batch := !isStreaming(r)
	full := (batch && g.batchActive) || (g.meetingExclusive && len(g.meetingStages) > 0)
	if !full {
		select {
		case g.slots <- struct{}{}:
		default:
			full = true
		}
	}
	if !full && batch {
		g.batchActive = true
	}
	g.admission.Unlock()
	if full {
		g.stats.reject()
		w.Header().Set("Retry-After", "1")
		w.Header().Set("X-SpeekToMe-Admission", "rejected")
		http.Error(w, "worker full", 429)
		return
	}
	defer func() {
		g.admission.Lock()
		<-g.slots
		if batch {
			g.batchActive = false
		}
		g.admission.Unlock()
	}()
	stat := g.stats.begin(r)
	defer g.stats.end(stat)
	diarReserved := false
	reserveDiar := func() bool {
		if diarReserved || g.diarSlots == nil {
			return true
		}
		select {
		case g.diarSlots <- struct{}{}:
			diarReserved = true
			return true
		default:
			return false
		}
	}
	if stat.diar && !reserveDiar() {
		g.stats.reject()
		w.Header().Set("X-SpeekToMe-Admission", "rejected")
		http.Error(w, "diarization full", 429)
		return
	}
	defer func() {
		if diarReserved {
			<-g.diarSlots
		}
	}()
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if websocket.IsWebSocketUpgrade(r) {
		if r.URL.Path != "/v1/realtime" && r.URL.Path != "/v1/audio/realtime" && r.URL.Path != "/v1/realtime/opus" && r.URL.Path != "/v1/audio/transcriptions/realtime" {
			http.NotFound(w, r)
			return
		}
		g.stream(w, r, stat, reserveDiar)
		return
	}
	if r.URL.Path == "/v1/vocabulary/final" {
		g.matchVocabulary(w, r)
		return
	}
	// The token is for the gateway and must never be forwarded into the model runtime.
	r.Header.Del("Authorization")
	g.proxy.ServeHTTP(w, r)
}

func (g *gateway) stream(w http.ResponseWriter, r *http.Request, stat *streamStats, reserveDiar func() bool) {
	contexts, err := queryContexts(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	upstream := *g.upstream
	upstream.Scheme = "ws"
	upstream.Path = "/v1/audio/transcriptions/realtime"
	upstream.RawQuery = r.URL.RawQuery
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	backend, _, err := dialer.DialContext(r.Context(), upstream.String(), nil)
	if err != nil {
		http.Error(w, "inference unavailable", 502)
		return
	}
	defer backend.Close()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	front, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer front.Close()
	if len(contexts) > 0 {
		if err := backend.WriteJSON(map[string]any{"type": "session.update", "session": map[string]any{"speech_contexts": contexts}}); err != nil {
			return
		}
	}
	front.SetReadLimit(8 << 20)
	backend.SetReadLimit(8 << 20)
	post := &postVocabulary{}
	var frontWriter sync.Mutex
	writeFront := func(kind int, data []byte) error {
		frontWriter.Lock()
		defer frontWriter.Unlock()
		return front.WriteMessage(kind, data)
	}
	done := make(chan struct{}, 1)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			kind, data, err := backend.ReadMessage()
			if err != nil {
				front.Close()
				return
			}
			if kind == websocket.TextMessage {
				g.stats.progress(stat, data, time.Now())
			}
			writeStart := time.Now()
			writeErr := writeFront(kind, data)
			g.stats.stage("client_output_write", time.Since(writeStart), len(data))
			if writeErr != nil {
				return
			}
			if kind == websocket.TextMessage {
				if result := post.observe(data); result != nil {
					encoded, _ := json.Marshal(result)
					_ = writeFront(websocket.TextMessage, encoded)
					front.Close()
					return
				}
			}
		}
	}()
	decoder, err := opus.NewDecoderWithOutput(16000, 1)
	if err != nil {
		return
	}
	ogg := oggPackets{}
	pcmQueue := pcmChunkQueue{}
	writePCM := func(buf []byte) error {
		g.stats.audio(stat, len(buf), time.Now())
		start := time.Now()
		err := backend.WriteMessage(websocket.BinaryMessage, buf)
		g.stats.stage("backend_pcm_write", time.Since(start), len(buf))
		return err
	}
	for {
		kind, data, err := front.ReadMessage()
		if err != nil {
			break
		}
		if kind == websocket.BinaryMessage {
			post.audio()
		}
		if kind == websocket.BinaryMessage && r.URL.Path == "/v1/realtime/opus" {
			chunkStart := time.Now()
			parseStart := time.Now()
			packets, err := ogg.feed(data)
			g.stats.stage("ogg_parse", time.Since(parseStart), len(data))
			if err != nil {
				front.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1003, "invalid Ogg Opus"), time.Now().Add(time.Second))
				break
			}
			for _, framed := range packets {
				packet := framed.data
				if strings.HasPrefix(string(packet), "OpusHead") {
					if len(packet) < 19 || packet[9] != 1 {
						err = fmt.Errorf("mono Opus required")
						break
					}
					decoder, err = opus.NewDecoderWithOutput(16000, 1)
					if err != nil {
						break
					}
					ogg.preSkip = int(binary.LittleEndian.Uint16(packet[10:12]))
					ogg.skip = ogg.preSkip / 3
					ogg.decoded = 0
					continue
				}
				if strings.HasPrefix(string(packet), "OpusTags") {
					continue
				}
				pcm := make([]int16, 1920)
				decodeStart := time.Now()
				n, e := decoder.DecodeToInt16(packet, pcm)
				g.stats.stage("opus_decode_packet", time.Since(decodeStart), len(packet))
				if e != nil {
					err = e
					break
				}
				skip := min(ogg.skip, n)
				ogg.skip -= skip
				pcm = pcm[skip:n]
				if framed.eos {
					if framed.granule < uint64(ogg.preSkip) {
						err = fmt.Errorf("invalid Opus duration")
						break
					}
					target := int((framed.granule - uint64(ogg.preSkip)) / 3)
					pcm = pcm[:min(len(pcm), max(0, target-ogg.decoded))]
				}
				ogg.decoded += len(pcm)
				serializeStart := time.Now()
				buf := make([]byte, len(pcm)*2)
				for i, s := range pcm {
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
				}
				g.stats.stage("pcm_serialize", time.Since(serializeStart), len(buf))
				if len(buf) > 0 {
					if e = pcmQueue.push(buf, writePCM); e != nil {
						err = e
						break
					}
				}
			}
			g.stats.stage("opus_message_to_backend", time.Since(chunkStart), len(data))
			if err != nil {
				log.Print("opus stream rejected; private details omitted")
				front.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1003, "unsupported Opus stream"), time.Now().Add(time.Second))
				break
			}
			continue
		}
		if kind == websocket.TextMessage {
			data, err = vocabularyControl(data, &contexts)
			if err == nil {
				data, err = post.control(data)
			}
			if err != nil {
				encoded, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"message": err.Error()}})
				_ = writeFront(websocket.TextMessage, encoded)
				break
			}
			var event struct {
				Type    string `json:"type"`
				Session struct {
					Diar bool `json:"speaker_diarization"`
				} `json:"session"`
			}
			if json.Unmarshal(data, &event) == nil && event.Type == "session.update" && event.Session.Diar && !reserveDiar() {
				g.stats.reject()
				front.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1013, "diarization full"), time.Now().Add(time.Second))
				break
			}
			g.stats.control(stat, data)
		}
		if kind == websocket.BinaryMessage {
			g.stats.audio(stat, len(data), time.Now())
		}
		if kind == websocket.TextMessage && r.URL.Path == "/v1/realtime/opus" {
			var event map[string]any
			if json.Unmarshal(data, &event) == nil && event["type"] == "input_audio_buffer.commit" {
				if err = pcmQueue.flush(writePCM); err != nil {
					break
				}
			}
			if json.Unmarshal(data, &event) == nil && event["type"] == "session.update" {
				if s, ok := event["session"].(map[string]any); ok {
					s["sample_rate"] = 16000
					delete(s, "input_audio_format")
					data, _ = json.Marshal(event)
				}
			}
		}
		if err = backend.WriteMessage(kind, data); err != nil {
			break
		}
	}
	backend.Close()
	front.Close()
	<-done
}

// Ogg pages and packets may be split across arbitrary WebSocket message boundaries.
// Raw Opus packets are deliberately not guessed: this endpoint accepts Ogg Opus.
type oggPackets struct {
	buf, partial           []byte
	skip, preSkip, decoded int
}

type oggPacket struct {
	data    []byte
	granule uint64
	eos     bool
}

func (o *oggPackets) feed(data []byte) ([]oggPacket, error) {
	o.buf = append(o.buf, data...)
	var packets []oggPacket
	if len(o.buf) > 8<<20 {
		return nil, fmt.Errorf("Ogg buffer too large")
	}
	for len(o.buf) >= 27 {
		if string(o.buf[:4]) != "OggS" || o.buf[4] != 0 {
			return nil, fmt.Errorf("invalid Ogg page")
		}
		n := int(o.buf[26])
		if len(o.buf) < 27+n {
			break
		}
		size := 0
		for _, v := range o.buf[27 : 27+n] {
			size += int(v)
		}
		if len(o.buf) < 27+n+size {
			break
		}
		offset := 27 + n
		for i, v := range o.buf[27 : 27+n] {
			o.partial = append(o.partial, o.buf[offset:offset+int(v)]...)
			offset += int(v)
			if len(o.partial) > 1<<20 {
				return nil, fmt.Errorf("Ogg packet too large")
			}
			if v < 255 {
				packets = append(packets, oggPacket{data: append([]byte(nil), o.partial...), granule: binary.LittleEndian.Uint64(o.buf[6:14]), eos: o.buf[5]&4 != 0 && i == n-1})
				o.partial = o.partial[:0]
			}
		}
		o.buf = o.buf[offset:]
	}
	return packets, nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8090", "gateway listen address")
	upstream := flag.String("upstream", "http://127.0.0.1:8080", "NeMo HTTP endpoint")
	diarLimit := flag.Int("max-diarization", 1, "concurrent diarization requests; provisional, benchmark before raising")
	drainFile := flag.String("drain-file", "", "persistent drain marker")
	spotFile := flag.String("spot-file", "", "spot watcher signal JSON")
	exclusive := flag.Bool("meeting-exclusive", false, "require idle worker for meetings and reject live work until meeting stages finish")
	limit := flag.Int("max-streams", 20, "concurrent inference request limit; benchmark before raising")
	tokenFile := flag.String("token-file", "/etc/speektome/worker.token", "bearer token file")
	ready := flag.String("ready-file", "/run/speektome/ready", "GPU smoke-test readiness marker")
	smoke := flag.String("smoke-wav", "", "Run local ASR+diarization streaming smoke test and exit")
	smokeDiarization := flag.Bool("smoke-diarization", true, "Require diarization in the streaming smoke test; disable for ASR-only workers")
	flag.Parse()
	if *smoke != "" {
		if err := smokeTest(*upstream, *smoke, *smokeDiarization); err != nil {
			log.Fatal("Worker operation failed; private details omitted")
		}
		return
	}
	if *limit < 1 || *diarLimit < 1 {
		log.Fatal("max-streams must be positive")
	}
	token, err := os.ReadFile(*tokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		log.Fatal("valid worker token file required")
	}
	u, err := url.Parse(*upstream)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		log.Fatal("invalid upstream")
	}
	g := &gateway{tokenHash: sha256.Sum256([]byte(strings.TrimSpace(string(token)))), upstream: u, slots: make(chan struct{}, *limit), readyFile: *ready, proxy: httputil.NewSingleHostReverseProxy(u), diarSlots: make(chan struct{}, *diarLimit), drainFile: *drainFile, spotFile: *spotFile, meetingExclusive: *exclusive}
	g.proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { http.Error(w, "inference unavailable", 502) }
	server := http.Server{Addr: *listen, Handler: g, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	log.Print("Authenticated worker gateway listening")
	log.Fatal(server.ListenAndServe())
}

func (g *gateway) isDraining() bool {
	if g.draining.Load() {
		return true
	}
	if g.drainFile != "" {
		_, err := os.Stat(g.drainFile)
		return err == nil
	}
	return false
}

// HTTP panic/error values can contain private upstream responses.
type privateLogWriter struct{}

func (privateLogWriter) Write(value []byte) (int, error) {
	log.Print("Worker HTTP operation failed; private details omitted")
	return len(value), nil
}
