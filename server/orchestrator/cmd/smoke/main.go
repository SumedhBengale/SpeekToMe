// Small end-to-end PCM16 client for the authenticated fleet proxy.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/coder/websocket"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	endpoint := flag.String("endpoint", "ws://127.0.0.1:18095/v1/audio/transcriptions/realtime", "fleet proxy WebSocket URL")
	wav := flag.String("wav", "", "16kHz mono PCM16 WAV")
	tokenFile := flag.String("token-file", "", "token file")
	flag.Parse()
	token, e := os.ReadFile(*tokenFile)
	if e != nil {
		log.Fatal(e)
	}
	data, e := os.ReadFile(*wav)
	if e != nil {
		log.Fatal(e)
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" {
		log.Fatal("WAV required")
	}
	var pcm []byte
	valid := false
	for pos := 12; pos+8 <= len(data); {
		n := int(binary.LittleEndian.Uint32(data[pos+4:]))
		start := pos + 8
		if n > len(data)-start {
			log.Fatal("truncated WAV")
		}
		switch string(data[pos : pos+4]) {
		case "fmt ":
			valid = n >= 16 && binary.LittleEndian.Uint16(data[start:]) == 1 && binary.LittleEndian.Uint16(data[start+2:]) == 1 && binary.LittleEndian.Uint32(data[start+4:]) == 16000 && binary.LittleEndian.Uint16(data[start+14:]) == 16
		case "data":
			pcm = data[start : start+n]
		}
		pos = start + n + n%2
	}
	if !valid || len(pcm) == 0 {
		log.Fatal("16kHz mono PCM16 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, *endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + strings.TrimSpace(string(token))}}})
	if e != nil {
		log.Fatal(e)
	}
	defer c.CloseNow()
	if _, _, e = c.Read(ctx); e != nil {
		log.Fatal(e)
	}
	update := []byte(`{"type":"session.update","session":{"sample_rate":16000,"language":"en-US","speaker_diarization":true,"word_timestamps":true}}`)
	if e = c.Write(ctx, websocket.MessageText, update); e != nil {
		log.Fatal(e)
	}
	_, reply, e := c.Read(ctx)
	if e != nil {
		log.Fatal(e)
	}
	var event map[string]any
	json.Unmarshal(reply, &event)
	if event["type"] == "error" {
		log.Fatal(string(reply))
	}
	done := make(chan error, 1)
	go func() {
		for {
			_, b, e := c.Read(ctx)
			if e != nil {
				done <- e
				return
			}
			var event map[string]any
			if json.Unmarshal(b, &event) != nil {
				continue
			}
			typ, _ := event["type"].(string)
			if typ == "error" {
				done <- fmt.Errorf("inference error: %s", b)
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
				speaker := false
				for _, v := range words {
					w, ok := v.(map[string]any)
					if ok {
						s, ok := w["speaker"].(float64)
						speaker = speaker || (ok && s > 0)
					}
				}
				if !speaker {
					done <- fmt.Errorf("transcript lacks speaker labels: %s", b)
					return
				}
				fmt.Println(string(b))
				done <- nil
				return
			}
		}
	}()
	for pos := 0; pos < len(pcm); pos += 17920 {
		if e = c.Write(ctx, websocket.MessageBinary, pcm[pos:min(pos+17920, len(pcm))]); e != nil {
			log.Fatal(e)
		}
		time.Sleep(560 * time.Millisecond)
	}
	if e = c.Write(ctx, websocket.MessageText, []byte(`{"type":"input_audio_buffer.commit"}`)); e != nil {
		log.Fatal(e)
	}
	if e = <-done; e != nil {
		log.Fatal(e)
	}
}
