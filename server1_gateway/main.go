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
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/coder/websocket"
)

const (
	GatewayPort     = 8002
	UpstreamASRWS   = "ws://127.0.0.1:8000/v1/realtime"
	CoreDiarURL     = "http://127.0.0.1:8000/v1/audio/diarizations"
	SampleRate      = 16000
	Channels        = 1
	MaxFrameSize    = 5760 // 120ms max frame @ 16kHz
)

var (
	activeConnections int64
	diarSem           = make(chan struct{}, 4) // Max 4 concurrent Sortformer GPU passes to protect 3GB VRAM
	httpClient        = &http.Client{
		Timeout: 600 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     120 * time.Second,
		},
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

	// 1. OGG Opus Container (starts with OggS)
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

	// 4. Channel-prefixed Opus frame (1-byte channel prefix e.g. 0x00, 0x01, 0x02)
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

func pcmToWav(pcm []byte) []byte {
	pcmLen := uint32(len(pcm))
	wavHeader := make([]byte, 44)
	copy(wavHeader[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wavHeader[4:8], 36+pcmLen)
	copy(wavHeader[8:12], "WAVE")
	copy(wavHeader[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wavHeader[16:20], 16)
	binary.LittleEndian.PutUint16(wavHeader[20:22], 1)
	binary.LittleEndian.PutUint16(wavHeader[22:24], 1)
	binary.LittleEndian.PutUint32(wavHeader[24:28], 16000)
	binary.LittleEndian.PutUint32(wavHeader[28:32], 32000)
	binary.LittleEndian.PutUint16(wavHeader[32:34], 2)
	binary.LittleEndian.PutUint16(wavHeader[34:36], 16)
	copy(wavHeader[36:40], "data")
	binary.LittleEndian.PutUint32(wavHeader[40:44], pcmLen)

	var fullWav bytes.Buffer
	fullWav.Grow(44 + len(pcm))
	fullWav.Write(wavHeader)
	fullWav.Write(pcm)
	return fullWav.Bytes()
}

type DiarSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
}

func queryCoreDiarization(wavBytes []byte) ([]DiarSegment, error) {
	diarSem <- struct{}{}
	defer func() { <-diarSem }()

	var bodyBuf bytes.Buffer
	writer := multipart.NewWriter(&bodyBuf)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, err
	}
	_, _ = part.Write(wavBytes)
	_ = writer.Close()

	req, err := http.NewRequest("POST", CoreDiarURL, &bodyBuf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	t0 := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core diarization error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("core diarization status %d: %s", resp.StatusCode, string(respData))
	}

	var rawResult struct {
		Segments []struct {
			Start   float64     `json:"start"`
			End     float64     `json:"end"`
			Speaker interface{} `json:"speaker"`
		} `json:"segments"`
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(respData, &rawResult); err != nil {
		return nil, fmt.Errorf("failed to parse core diarization JSON: %w", err)
	}

	segments := make([]DiarSegment, 0, len(rawResult.Segments))
	for _, s := range rawResult.Segments {
		spkID := 1
		switch v := s.Speaker.(type) {
		case float64:
			spkID = int(v)
		case string:
			if strings.Contains(v, "_") {
				parts := strings.Split(v, "_")
				if id, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
					spkID = id + 1
				}
			} else if id, err := strconv.Atoi(v); err == nil {
				spkID = id
			}
		}

		segments = append(segments, DiarSegment{
			Start:   float64(int(s.Start*1000+0.5)) / 1000.0,
			End:     float64(int(s.End*1000+0.5)) / 1000.0,
			Speaker: spkID,
		})
	}

	log.Printf("[SORTFORMER v2.1 CORE HTTP] Diarized in %.3fs -> %d segments", time.Since(t0).Seconds(), len(segments))
	return segments, nil
}

// ----------------------------------------------------------------------
// WEBSOCKET HANDLERS (STREAMING ONLY)
// ----------------------------------------------------------------------

func handleRealtimeWS(w http.ResponseWriter, r *http.Request) {
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[WS ERROR] Accept failed: %v", err)
		return
	}
	defer clientConn.Close(websocket.StatusNormalClosure, "done")

	decoder, err := NewNativeOpusDecoder()
	if err != nil {
		log.Printf("[DECODER ERROR] %v", err)
		return
	}
	defer decoder.Close()

	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upstreamConn, _, err := websocket.Dial(ctx, UpstreamASRWS, nil)
	if err != nil {
		log.Printf("[GATEWAY ERROR] Upstream connection failed: %v", err)
		errResp := map[string]interface{}{
			"type":  "error",
			"error": "Upstream ASR engine unavailable: " + err.Error(),
		}
		errBytes, _ := json.Marshal(errResp)
		_ = clientConn.Write(ctx, websocket.MessageText, errBytes)
		return
	}
	defer upstreamConn.Close(websocket.StatusNormalClosure, "done")

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

func handleWSDiarize(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[WS ERROR] Accept failed: %v", err)
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "done")

	decoder, err := NewNativeOpusDecoder()
	if err != nil {
		log.Printf("[DECODER ERROR] %v", err)
		return
	}
	defer decoder.Close()

	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	ctx := r.Context()
	var pcmAccumulator bytes.Buffer

	handshake := map[string]interface{}{
		"type":     "session.created",
		"event_id": "event_0",
		"session": map[string]interface{}{
			"model":              "diar_streaming_sortformer_4spk-v2.1.q8_0",
			"sample_rate":        16000,
			"input_audio_format": "opus",
			"chunk_size":         560,
		},
	}
	handshakeBytes, _ := json.Marshal(handshake)
	_ = c.Write(ctx, websocket.MessageText, handshakeBytes)

	for {
		msgType, data, err := c.Read(ctx)
		if err != nil {
			break
		}

		if msgType == websocket.MessageBinary {
			pcm := decoder.DecodeChunk(data)
			if len(pcm) > 0 {
				pcmAccumulator.Write(pcm)
			}
		} else if msgType == websocket.MessageText {
			var event map[string]interface{}
			if err := json.Unmarshal(data, &event); err == nil {
				mtype, _ := event["type"].(string)
				if mtype == "session.update" {
					resp := map[string]interface{}{
						"type":     "session.updated",
						"event_id": "event_1",
						"session": map[string]interface{}{
							"model":       "diarization",
							"sample_rate": 16000,
							"format":      "opus",
							"chunk_size":  560,
						},
					}
					respBytes, _ := json.Marshal(resp)
					_ = c.Write(ctx, websocket.MessageText, respBytes)
				} else if mtype == "input_audio_buffer.commit" || mtype == "end_of_stream" || mtype == "stop" || mtype == "end_of_meeting" {
					if pcmAccumulator.Len() > 0 {
						wavBytes := pcmToWav(pcmAccumulator.Bytes())
						segments, err := queryCoreDiarization(wavBytes)
						if err == nil {
							for _, s := range segments {
								segEvent := map[string]interface{}{
									"type":    "diarization.segment",
									"start":   s.Start,
									"end":     s.End,
									"speaker": fmt.Sprintf("speaker_%d", s.Speaker),
								}
								segBytes, _ := json.Marshal(segEvent)
								_ = c.Write(ctx, websocket.MessageText, segBytes)
							}
							_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"diarization.completed"}`))
						} else {
							log.Printf("[WS DIARIZATION ERROR] %v", err)
							_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":"Diarization failed"}`))
						}
					}
				}
			}
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"status":         "healthy",
		"backend":        "speektome-universal-cgo-gateway",
		"architecture":   "pure-streaming-opus",
		"format":         "opus",
		"chunk_size":     560,
		"asr_model":      "nemotron-3.5-asr-streaming-0.6b.planar.q8_0",
		"diar_model":     "diar_streaming_sortformer_4spk-v2.1.q8_0",
		"active_streams": atomic.LoadInt64(&activeConnections),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/realtime", handleRealtimeWS)
	mux.HandleFunc("/v1/realtime/opus", handleRealtimeWS)
	mux.HandleFunc("/v1/audio/diarize", handleWSDiarize)
	mux.HandleFunc("/health", handleHealth)

	addr := fmt.Sprintf("127.0.0.1:%d", GatewayPort)
	log.Printf("[SPEEKTOME SERVER 1 UNIVERSAL GATEWAY] Pure Streaming Gateway starting on %s...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Gateway fatal error: %v", err)
	}
}
