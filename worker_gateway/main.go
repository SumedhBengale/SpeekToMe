package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	GatewayPort = 8002
	CoreASRURL  = "http://127.0.0.1:8000/v1/audio/transcriptions"
	CoreDiarURL = "http://127.0.0.1:8000/v1/audio/diarizations"
)

var (
	httpClient = &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
		},
	}
)

type DiarSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
}

type DiarResponse struct {
	Segments []DiarSegment `json:"segments"`
}

func convertTo16kWav(input []byte) ([]byte, error) {
	if len(input) > 44 && string(input[:4]) == "RIFF" && string(input[8:12]) == "WAVE" {
		sampleRate := int(input[24]) | int(input[25])<<8 | int(input[26])<<16 | int(input[27])<<24
		channels := int(input[22]) | int(input[23])<<8
		bitsPerSample := int(input[34]) | int(input[35])<<8
		if sampleRate == 16000 && channels == 1 && bitsPerSample == 16 {
			return input, nil
		}
	}

	cmd := exec.Command("ffmpeg", "-y", "-i", "pipe:0", "-ar", "16000", "-ac", "1", "-f", "s16le", "pipe:1")
	cmd.Stdin = bytes.NewReader(input)
	var pcmBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &pcmBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w (stderr: %s)", err, errBuf.String())
	}

	pcm := pcmBuf.Bytes()
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

	return fullWav.Bytes(), nil
}

func queryCoreDiarization(wavBytes []byte) ([]DiarSegment, error) {
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
		return nil, fmt.Errorf("failed to parse core diarization JSON output: %w", err)
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
// HTTP HANDLERS
// ----------------------------------------------------------------------

func handleHTTPTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	err := r.ParseMultipartForm(50 << 20)
	if err != nil {
		http.Error(w, "Failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}
	defer file.Close()

	rawBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read audio data", http.StatusInternalServerError)
		return
	}

	wavBytes, err := convertTo16kWav(rawBytes)
	if err != nil {
		http.Error(w, "Audio transcoding error: "+err.Error(), http.StatusBadRequest)
		return
	}

	var bodyBuf bytes.Buffer
	writer := multipart.NewWriter(&bodyBuf)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		http.Error(w, "Internal multipart error", http.StatusInternalServerError)
		return
	}
	_, _ = part.Write(wavBytes)
	_ = writer.Close()

	req, err := http.NewRequestWithContext(r.Context(), "POST", CoreASRURL, &bodyBuf)
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "ASR Engine unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func handleHTTPDiarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	err := r.ParseMultipartForm(50 << 20)
	if err != nil {
		http.Error(w, "Failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}
	defer file.Close()

	rawBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read audio data", http.StatusInternalServerError)
		return
	}

	wavBytes, err := convertTo16kWav(rawBytes)
	if err != nil {
		http.Error(w, "Audio transcoding error: "+err.Error(), http.StatusBadRequest)
		return
	}

	segments, err := queryCoreDiarization(wavBytes)
	if err != nil {
		log.Printf("[DIARIZATION ERROR] %v", err)
		http.Error(w, "Diarization failure: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(DiarResponse{Segments: segments})
}

// ----------------------------------------------------------------------
// WEBSOCKET HANDLERS
// ----------------------------------------------------------------------

func handleWSRealtime(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[WS ERROR] Accept failed: %v", err)
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "done")

	ctx := r.Context()
	var rawBuffer bytes.Buffer

	handshake := map[string]interface{}{
		"type":     "session.created",
		"event_id": "event_0",
		"session": map[string]interface{}{
			"model":               "diar_streaming_sortformer_4spk-v2.1.q8_0",
			"sample_rate":         16000,
			"input_audio_format":  "opus",
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
			rawBuffer.Write(data)
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
						},
					}
					respBytes, _ := json.Marshal(resp)
					_ = c.Write(ctx, websocket.MessageText, respBytes)
				} else if mtype == "input_audio_buffer.commit" || mtype == "end_of_stream" || mtype == "stop" || mtype == "end_of_meeting" {
					if rawBuffer.Len() > 0 {
						wavBytes, err := convertTo16kWav(rawBuffer.Bytes())
						if err == nil {
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
							}
						}
					}
				}
			}
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok","backend":"speektome-worker-gateway-golang"}`))
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/transcriptions", handleHTTPTranscribe)
	mux.HandleFunc("/v1/audio/diarizations", handleHTTPDiarize)
	mux.HandleFunc("/v1/realtime/opus", handleWSRealtime)
	mux.HandleFunc("/v1/realtime", handleWSRealtime)
	mux.HandleFunc("/health", handleHealth)

	addr := fmt.Sprintf("127.0.0.1:%d", GatewayPort)
	log.Printf("[SPEEKTOME WORKER GATEWAY] Ultra-High-Concurrency Go Gateway starting on %s...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Gateway fatal error: %v", err)
	}
}
