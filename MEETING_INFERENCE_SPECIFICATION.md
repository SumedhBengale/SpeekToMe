# SpeekToMe Real-Time Meeting Architecture & Unified Inference Specification

**Version**: 3.0.0 (Production Single-Instance Unified Deployment)  
**Target Components**: Golang Backend Server (`SpeekToMe-Server`), Windows Desktop Client (`SpeekToMe-Client`), and Unified NeMo-Speech Runtime

---

## 1. System Architecture Overview

```
                                  ┌────────────────────────┐
                                  │  Windows Client App    │
                                  │ (Dual-Channel Capture) │
                                  └───────────┬────────────┘
                                              │ 2 Raw Streams (Mic: 0x01 + System Loopback: 0x02)
                                              ▼
                                  ┌────────────────────────┐
                                  │   SpeekToMe Go Server  │
                                  │ (Orchestrator/Router)  │
                                  └─────┬────────────┬─────┘
                     Stream 1 (Mic ASR) │            │ Stream 2 (Remote Diarized ASR)
                                        │            │
                                        ▼            ▼
             ┌──────────────────────────────────────────────────────────┐
             │       UNIFIED INFERENCE INSTANCE (AWS EC2 L4-3Q GPU)     │
             │       IP: GPU_HOST_IP                                 │
             │                                                          │
             │  Port 80 / 443: Nginx Reverse Proxy                      │
             │  Port 8090: SpeekToMe Gateway (Opus Decoder & Router)    │
             │  Port 8080: NeMo-Speech.cpp Unified Runtime              │
             │                                                          │
             │  Hosted Models Inside Single Runtime:                    │
             │  • Nemotron-3.5 Streaming 0.6B Q8_0 (ASR)                │
             │  • Sortformer 4-Speaker F16 (Diarization)                │
             │  • State Arena (64 Concurrent Streaming Slots)           │
             └──────────────────────────┬───────────────────────────────┘
                                        │ Completed Utterances with words[] (speaker: 1..4)
                                        ▼
                                  ┌────────────────────────┐
                                  │  Word-Collapsing Turn  │
                                  │    Assembly Parser     │
                                  └───────────┬────────────┘
                                              │ Real-Time Turns & Final Dialogue
                                              ▼
                                  ┌────────────────────────┐
                                  │  Windows Desktop UI    │
                                  │  • Live Meeting Feed   │
                                  │  • DAW-Style Timeline  │
                                  │  • DeepSeek AI Summary │
                                  └────────────────────────┘
```

---

## 2. The 2-Stream Unified Meeting Data Flow

To eliminate audio leakage and guarantee clean speaker attribution:
1. **Windows Client Dual Capture**:
   * **Stream A (Microphone - Channel `0x01`)**: Captures only the local user's physical microphone via CPAL.
   * **Stream B (System Audio Loopback - Channel `0x02`)**: Captures only remote meeting attendees (Zoom, Teams, Google Meet, etc.) via WASAPI loopback.
   * Both streams are resampled to 16kHz mono and chunked into 560ms blocks (`CHUNK_560MS = 8960` samples).
2. **Go Server Dual Upstream Routing (to Single Unified Runtime)**:
   * **`Stream 1`**: `Mic Audio` $\to$ `/v1/stream?diarize=false&lang=<userLang>` $\to$ Low-latency streaming deltas for the local user ("You").
   * **`Stream 2`**: `System Audio` $\to$ `/v1/stream?diarize=true&lang=en` $\to$ Sortformer multi-speaker diarization for remote participants (`Speaker 1`, `Speaker 2`, etc.).
3. **Integrated Word-Level Attribution**:
   * Sortformer inside NeMo-Speech.cpp directly attributes each recognized word to its speaker.
   * Go server collapses consecutive words with the same speaker into clean `ClientMeetingTurn`s.

---

## 3. Unified Inference API Contract

* **Host**: `GPU_HOST_IP` (or local dev port forward `127.0.0.1:8000`)
* **Endpoint**: `ws://<host>/v1/stream?diarize=<bool>&lang=<code|auto>&stream_id=<id>`
* **Transport**: WebSocket (Binary frames containing 560ms Opus chunks)

### Egress Event Protocol:

#### Completed Utterance Event (`conversation.item.input_audio_transcription.completed`):
```json
{
  "event_id": "event_724",
  "type": "conversation.item.input_audio_transcription.completed",
  "audio_processed": 150.02,
  "transcript": "Okay right we're gonna practice with the pens...",
  "words": [
    {
      "word": "Okay",
      "start": 1.28,
      "end": 1.36,
      "speaker": 1,
      "confidence": 1.0
    },
    {
      "word": "right",
      "start": 1.92,
      "end": 2.00,
      "speaker": 1,
      "confidence": 1.0
    },
    {
      "word": "does",
      "start": 116.16,
      "end": 116.24,
      "speaker": 3,
      "confidence": 1.0
    }
  ]
}
```

#### Flush / End-of-Turn Control Message:
```json
{
  "type": "input_audio_buffer.commit"
}
```

---

## 4. Port & Service Reference

| Service | Port | Transport | Purpose |
| :--- | :---: | :---: | :--- |
| **Nginx Ingress** | `80` / `443` | HTTP / WS | Public entry point forwarding to gateway `:8090` |
| **SpeekToMe Gateway** | `8090` | HTTP / WS | Libopus decoding, delta suppression when diarize=true, buffer pool |
| **NeMo-Speech.cpp** | `8080` | HTTP / WS | Unified Nemotron-3.5 + Sortformer 4-Spk inference engine |
