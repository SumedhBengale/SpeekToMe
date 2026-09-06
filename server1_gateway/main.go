package main

/*
#cgo CFLAGS: -I/usr/include/opus
#cgo LDFLAGS: -lopus
#include <opus/opus.h>
#include <stdlib.h>

static OpusDecoder* create_opus_decoder(int sample_rate, int channels) {
    int error;
    OpusDecoder* dec = opus_decoder_create(sample_rate, channels, &error);
    if (error != OPUS_OK) return NULL;
    return dec;
}

static int decode_opus_frame(OpusDecoder* dec, const unsigned char* data, int len, short* out, int max_frame_size) {
    return opus_decode(dec, data, len, out, max_frame_size, 0);
}

static void free_opus_decoder(OpusDecoder* dec) {
    if (dec) {
        opus_decoder_destroy(dec);
    }
}
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/coder/websocket"
)

const (
	GatewayPort     = 8002
	UpstreamASRWS   = "ws://127.0.0.1:8000/v1/realtime"
	UpstreamDiarWS  = "ws://127.0.0.1:8000/v1/audio/diarize"
	BackendHealth   = "http://127.0.0.1:8000/health"
	SampleRate      = 16000
	Channels        = 1
	MaxFrameSize    = 5760 // 120ms max frame @ 16kHz
)

var (
	activeConnections int64
	httpClient        = &http.Client{
		Timeout: 5 * time.Second,
	}
)

type NativeOpusDecoder struct {
	handle *C.OpusDecoder
	outBuf []int16
}

func NewNativeOpusDecoder() (*NativeOpusDecoder, error) {
	handle := C.create_opus_decoder(C.int(SampleRate), C.int(Channels))
	if handle == nil {
		return nil, fmt.Errorf("failed to create libopus decoder")
	}
	return &NativeOpusDecoder{
		handle: handle,
		outBuf: make([]int16, MaxFrameSize),
	}, nil
}

func (d *NativeOpusDecoder) Close() {
	if d.handle != nil {
		C.free_opus_decoder(d.handle)
		d.handle = nil
	}
}

func extractOggOpusPackets(oggBytes []byte) [][]byte {
	var packets [][]byte
	pos := 0
	for pos < len(oggBytes)-27 {
		if string(oggBytes[pos:pos+4]) != "OggS" {
			pos++
			continue
		}
		numSegments := int(oggBytes[pos+26])
		if pos+27+numSegments > len(oggBytes) {
			break
		}
		segTable := oggBytes[pos+27 : pos+27+numSegments]
		dataOffset := pos + 27 + numSegments

		packetBuf := []byte{}
		for _, segLen := range segTable {
			l := int(segLen)
			if dataOffset+l <= len(oggBytes) {
				packetBuf = append(packetBuf, oggBytes[dataOffset:dataOffset+l]...)
				dataOffset += l
			}
			if l < 255 {
				if len(packetBuf) > 0 {
					if !bytes.HasPrefix(packetBuf, []byte("OpusHead")) && !bytes.HasPrefix(packetBuf, []byte("OpusTags")) {
						packets = append(packets, packetBuf)
					}
					packetBuf = []byte{}
				}
			}
		}
		pos = dataOffset
	}
	return packets
}

func (d *NativeOpusDecoder) DecodeChunk(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	// 1. OGG Opus Container
	if bytes.HasPrefix(data, []byte("OggS")) {
		packets := extractOggOpusPackets(data)
		if len(packets) > 0 {
			var pcmBuf bytes.Buffer
			for _, p := range packets {
				if len(p) > 0 {
					samples := int(C.decode_opus_frame(
						d.handle,
						(*C.uchar)(unsafe.Pointer(&p[0])),
						C.int(len(p)),
						(*C.short)(unsafe.Pointer(&d.outBuf[0])),
						C.int(MaxFrameSize),
					))
					if samples > 0 {
						for i := 0; i < samples; i++ {
							var sBytes [2]byte
							binary.LittleEndian.PutUint16(sBytes[:], uint16(d.outBuf[i]))
							pcmBuf.Write(sBytes[:])
						}
					}
				}
			}
			if pcmBuf.Len() > 0 {
				return pcmBuf.Bytes()
			}
		}
	}

	// 2. Length-prefixed multi-frame: [uint16 big-endian len][packet]...
	totalLen := len(data)
	if totalLen >= 3 {
		testOffset := 0
		isPrefixed := false
		for testOffset < totalLen {
			if testOffset+2 > totalLen {
				break
			}
			frameLen := int(binary.BigEndian.Uint16(data[testOffset : testOffset+2]))
			if frameLen <= 0 || testOffset+2+frameLen > totalLen {
				break
			}
			testOffset += 2 + frameLen
		}
		if testOffset == totalLen && totalLen > 0 {
			isPrefixed = true
		}

		if isPrefixed {
			var pcmBuf bytes.Buffer
			offset := 0
			for offset < totalLen {
				frameLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
				offset += 2
				frameData := data[offset : offset+frameLen]
				offset += frameLen

				if len(frameData) > 0 {
					samples := int(C.decode_opus_frame(
						d.handle,
						(*C.uchar)(unsafe.Pointer(&frameData[0])),
						C.int(len(frameData)),
						(*C.short)(unsafe.Pointer(&d.outBuf[0])),
						C.int(MaxFrameSize),
					))
					if samples > 0 {
						for i := 0; i < samples; i++ {
							var sBytes [2]byte
							binary.LittleEndian.PutUint16(sBytes[:], uint16(d.outBuf[i]))
							pcmBuf.Write(sBytes[:])
						}
					}
				}
			}
			if pcmBuf.Len() > 0 {
				return pcmBuf.Bytes()
			}
		}
	}

	// 3. Raw single Opus frame
	samples := int(C.decode_opus_frame(
		d.handle,
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
		(*C.short)(unsafe.Pointer(&d.outBuf[0])),
		C.int(MaxFrameSize),
	))
	if samples > 0 {
		var pcmBuf bytes.Buffer
		for i := 0; i < samples; i++ {
			var sBytes [2]byte
			binary.LittleEndian.PutUint16(sBytes[:], uint16(d.outBuf[i]))
			pcmBuf.Write(sBytes[:])
		}
		return pcmBuf.Bytes()
	}

	// 4. Channel-prefixed Opus frame
	if len(data) > 1 {
		samples := int(C.decode_opus_frame(
			d.handle,
			(*C.uchar)(unsafe.Pointer(&data[1])),
			C.int(len(data)-1),
			(*C.short)(unsafe.Pointer(&d.outBuf[0])),
			C.int(MaxFrameSize),
		))
		if samples > 0 {
			var pcmBuf bytes.Buffer
			for i := 0; i < samples; i++ {
				var sBytes [2]byte
				binary.LittleEndian.PutUint16(sBytes[:], uint16(d.outBuf[i]))
				pcmBuf.Write(sBytes[:])
			}
			return pcmBuf.Bytes()
		}
	}

	// 5. Raw PCM16 passthrough
	return data
}

// ----------------------------------------------------------------------
// WEBSOCKET FORWARDER HELPER
// ----------------------------------------------------------------------
func forwardWebSocketStream(w http.ResponseWriter, r *http.Request, upstreamURL string, name string) {
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[%s WS ERROR] Accept failed: %v", name, err)
		return
	}
	defer clientConn.Close(websocket.StatusNormalClosure, "done")

	decoder, err := NewNativeOpusDecoder()
	if err != nil {
		log.Printf("[%s DECODER ERROR] %v", name, err)
		return
	}
	defer decoder.Close()

	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upstreamConn, _, err := websocket.Dial(ctx, upstreamURL, nil)
	if err != nil {
		log.Printf("[%s GATEWAY ERROR] Upstream %s dial failed: %v", name, upstreamURL, err)
		errResp := map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Upstream %s engine unavailable: %v", name, err),
		}
		errBytes, _ := json.Marshal(errResp)
		_ = clientConn.Write(ctx, websocket.MessageText, errBytes)
		return
	}
	defer upstreamConn.Close(websocket.StatusNormalClosure, "done")

	// Upstream -> Client goroutine
	go func() {
		defer cancel()
		for {
			msgType, data, err := upstreamConn.Read(ctx)
			if err != nil {
				return
			}
			if err := clientConn.Write(ctx, msgType, data); err != nil {
				return
			}
		}
	}()

	// Client -> Upstream decode & forward loop
	for {
		msgType, data, err := clientConn.Read(ctx)
		if err != nil {
			break
		}

		if msgType == websocket.MessageBinary {
			pcm := decoder.DecodeChunk(data)
			if len(pcm) > 0 {
				if err := upstreamConn.Write(ctx, websocket.MessageBinary, pcm); err != nil {
					break
				}
			}
		} else if msgType == websocket.MessageText {
			if err := upstreamConn.Write(ctx, websocket.MessageText, data); err != nil {
				break
			}
		}
	}
}

// ----------------------------------------------------------------------
// HANDLERS
// ----------------------------------------------------------------------

func handleRealtimeASR(w http.ResponseWriter, r *http.Request) {
	forwardWebSocketStream(w, r, UpstreamASRWS, "ASR")
}

func handleStreamingDiarize(w http.ResponseWriter, r *http.Request) {
	forwardWebSocketStream(w, r, UpstreamDiarWS, "DIARIZATION")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var backendStatus map[string]interface{}

	resp, err := httpClient.Get(BackendHealth)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(body, &backendStatus)
	}

	gatewayHealth := map[string]interface{}{
		"status":         "healthy",
		"gateway":        "speektome-universal-cgo-gateway",
		"active_streams": atomic.LoadInt64(&activeConnections),
		"backend":        backendStatus,
	}

	_ = json.NewEncoder(w).Encode(gatewayHealth)
}

func main() {
	mux := http.NewServeMux()

	// ASR streaming routes
	mux.HandleFunc("/v1/realtime", handleRealtimeASR)
	mux.HandleFunc("/v1/realtime/opus", handleRealtimeASR)

	// Diarization streaming routes
	mux.HandleFunc("/v1/audio/diarize", handleStreamingDiarize)
	mux.HandleFunc("/v1/audio/diarizations", handleStreamingDiarize)
	mux.HandleFunc("/v1/realtime/diarize", handleStreamingDiarize)

	// Health check
	mux.HandleFunc("/health", handleHealth)

	addr := fmt.Sprintf("127.0.0.1:%d", GatewayPort)
	log.Printf("[SPEEKTOME SERVER 1 GATEWAY] Starting unified gateway on %s...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Gateway fatal error: %v", err)
	}
}
