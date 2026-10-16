package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"net/url"
	"os"
	"strings"
	"time"
)

// Smoke-test cache-aware ASR, optionally requiring diarization for configured workers.
func smokeTest(upstream, wav string, diarization bool) error {
	data, err := os.ReadFile(wav)
	if err != nil {
		return err
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return fmt.Errorf("PCM WAV required")
	}
	var pcm []byte
	rate := uint32(0)
	for pos := 12; pos+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[pos+4:]))
		start := pos + 8
		if size > len(data)-start {
			return fmt.Errorf("truncated WAV")
		}
		switch string(data[pos : pos+4]) {
		case "fmt ":
			if size < 16 || binary.LittleEndian.Uint16(data[start:]) != 1 || binary.LittleEndian.Uint16(data[start+2:]) != 1 || binary.LittleEndian.Uint16(data[start+14:]) != 16 {
				return fmt.Errorf("mono PCM16 required")
			}
			rate = binary.LittleEndian.Uint32(data[start+4:])
		case "data":
			pcm = data[start : start+size]
		}
		pos = start + size + (size % 2)
	}
	if rate != 16000 || len(pcm) == 0 {
		return fmt.Errorf("16kHz PCM required")
	}
	u, err := url.Parse(upstream)
	if err != nil {
		return err
	}
	u.Scheme = "ws"
	u.Path = "/v1/audio/transcriptions/realtime"
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(90 * time.Second))
	_, _, err = c.ReadMessage()
	if err != nil {
		return err
	}
	err = c.WriteJSON(map[string]any{"type": "session.update", "session": map[string]any{"sample_rate": 16000, "language": "en-US", "speaker_diarization": diarization, "word_timestamps": true}})
	if err != nil {
		return err
	}
	_, msg, err := c.ReadMessage()
	if err != nil {
		return err
	}
	var update map[string]any
	json.Unmarshal(msg, &update)
	if update["type"] == "error" {
		return fmt.Errorf("smoke session rejected: %s", msg)
	}
	done := make(chan error, 1)
	go func() {
		committed := false
		completed := ""
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			var event map[string]any
			if json.Unmarshal(msg, &event) != nil {
				continue
			}
			typ, _ := event["type"].(string)
			if typ == "input_audio_buffer.committed" {
				committed = true
			}
			if typ == "error" {
				done <- fmt.Errorf("stream error: %s", msg)
				return
			}
			if strings.HasSuffix(typ, ".completed") {
				text, _ := event["transcript"].(string)
				if text == "" {
					text, _ = event["text"].(string)
				}
				if strings.TrimSpace(text) == "" {
					continue
				}
				words, _ := event["words"].([]any)
				withSpeaker := false
				for _, w := range words {
					if word, ok := w.(map[string]any); ok {
						if speaker, ok := word["speaker"].(float64); ok && speaker > 0 {
							withSpeaker = true
						}
					}
				}
				if diarization && !withSpeaker {
					done <- fmt.Errorf("completed transcript has no speaker labels")
					return
				}
				completed = string(msg)
			}
			if committed && completed != "" {
				fmt.Println(completed)
				done <- nil
				return
			}
		}
	}()
	const chunk = 17920 // 560ms, 16000Hz mono PCM16
	for pos := 0; pos < len(pcm); pos += chunk {
		if err = c.WriteMessage(websocket.BinaryMessage, pcm[pos:min(pos+chunk, len(pcm))]); err != nil {
			return err
		}
		time.Sleep(560 * time.Millisecond)
	}
	if err = c.WriteJSON(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-time.After(60 * time.Second):
		return fmt.Errorf("stream smoke test timeout")
	}
}
