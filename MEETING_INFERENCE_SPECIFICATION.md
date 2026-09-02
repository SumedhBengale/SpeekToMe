# SpeekToMe Real-Time Meeting Architecture & Dual-Server Inference Specification

**Version**: 2.0.0 (Production Dual-Server Deployment)  
**Target Components**: Golang Backend Server (`SpeekToMe-Server`) & Windows Desktop Client (`SpeekToMe-Client`)

---

## 1. System Architecture Overview

```
                                  ┌────────────────────────┐
                                  │  Windows Client App    │
                                  │ (Dual-Channel Capture) │
                                  └───────────┬────────────┘
                                              │ 2 Raw Streams (Mic + System Loopback)
                                              ▼
                                  ┌────────────────────────┐
                                  │   SpeekToMe Go Server  │
                                  │ (Orchestrator/Router)  │
                                  └─────┬────────────┬─────┘
                     Stream 1 & 2 (ASR) │            │ Stream 3 & 4 (Diarization)
                                        │            │
             ┌──────────────────────────┘            └──────────────────────────┐
             ▼                                                                  ▼
┌──────────────────────────────────────┐              ┌──────────────────────────────────────┐
│  SERVER 1: Real-Time Streaming ASR   │              │  SERVER 2: Diarization & Batch ASR   │
│  IP: GPU_HOST_IP                  │              │  IP: BATCH_ASR_HOST_IP                  │
│  Instance: AWS g6f.large (3GB VRAM)  │              │  Instance: AWS g6f.large (3GB VRAM)  │
│  Port 8000: NeMo-Speech.cpp Engine   │              │  Port 8080: Nginx Reverse Proxy      │
│  Port 8002: Opus Streaming Gateway   │              │  Port 8000: NeMo-Speech.cpp Engine   │
│                                      │              │  Port 8002: Opus Streaming Gateway   │
│  Hosted Model:                       │              │  Hosted Models:                      │
│  • Nemotron-3.5 Streaming 0.6B RNNT  │              │  • Sortformer 4-Spk Streaming (Q8_0) │
│    (Planar Q8_0 GGUF)                │              │  • Parakeet TDT 0.6B Batch ASR(Q8_0) │
└──────────────────────────────────────┘              └──────────────────────────────────────┘
```

---

## 2. The 4-Stream Meeting Data Flow

To eliminate audio leakage and guarantee clean speaker attribution:
1. **Windows Client Dual Capture**:
   * **Stream A (Microphone)**: Captures only the local user's physical microphone.
   * **Stream B (System Audio Loopback)**: Captures only remote meeting attendees (Zoom, Teams, Google Meet, etc.) via WASAPI loopback.
2. **Go Server Fan-Out (4 Concurrent Inference Streams)**:
   * **`Stream 1`**: `Mic Audio` $\to$ **Server 1** (`/v1/realtime/opus`) $\to$ Real-time ASR for local user.
   * **`Stream 2`**: `System Audio` $\to$ **Server 1** (`/v1/realtime/opus`) $\to$ Real-time ASR for remote speakers.
   * **`Stream 3`**: `Mic Audio` $\to$ **Server 2** (`/v1/realtime/opus`) $\to$ Diarization / speaking interval tracking for local user.
   * **`Stream 4`**: `System Audio` $\to$ **Server 2** (`/v1/realtime/opus`) $\to$ Multi-speaker diarization for remote participants (`speaker_1`, `speaker_2`, etc.).
3. **Timestamp Assembly**:
   * Every audio packet has an exact timestamp offset $T$ from meeting start.
   * Transcripts and speaker segments are matched using time intervals.

---

## 3. Server 1: Real-Time Streaming ASR API Contract

* **Host**: `GPU_HOST_IP`
* **Model**: `nvidia/nemotron-3.5-asr-streaming-0.6b` (Planar Q8_0 GGUF)
* **Idle VRAM**: `2,148 MiB / 3,072 MiB`

### Route 1.1: Real-Time Opus Streaming WebSocket (Recommended)
* **URL**: `ws://GPU_HOST_IP:8002/v1/realtime/opus`
* **Transport**: WebSocket (Binary frames)
* **Audio Format**: Opus-compressed audio frames (16,000 Hz mono).
* **Chunk Cadence**: **560ms** per packet (~8,960 PCM samples / ~2.2 KB Opus payload).

#### WebSocket Message Protocol:

1. **Initial Handshake (Received on Connect)**:
```json
{
  "type": "session.created",
  "event_id": "event_0",
  "session": {
    "model": "nemotron-3.5-asr-streaming-0.6b.planar.q8_0",
    "sample_rate": 16000,
    "input_audio_format": "opus"
  }
}
```

2. **Live Word Deltas (Received per Chunk)**:
```json
{
  "type": "conversation.item.input_audio_transcription.delta",
  "event_id": "event_5",
  "audio_processed": 1.680,
  "delta": " And so my fellow"
}
```
* `audio_processed` *(float)*: Exact cumulative audio timestamp in seconds (zero-drift master clock).
* `delta` *(string)*: Newly recognized word token(s).

3. **Stream Completion (Received on WebSocket Close)**:
```json
{
  "type": "conversation.item.input_audio_transcription.completed",
  "event_id": "event_22",
  "audio_processed": 11.025,
  "transcript": "And so my fellow Americans ask not what your country can do for you. Ask what you can do for your country."
}
```

### Route 1.2: Raw PCM16 Streaming WebSocket
* **URL**: `ws://GPU_HOST_IP:8000/v1/realtime`
* **Payload**: Raw `pcm_s16le` binary bytes (16kHz, 16-bit signed, mono, 8,960 bytes per 560ms).
* **Output Protocol**: Identical JSON event schema as Route 1.1.

### Route 1.3: Health Check
* **URL**: `GET http://GPU_HOST_IP:8000/health`
* **Response**: `{"status": "ok", "version": "0.1.0"}`

---

## 4. Server 2: Diarization & Fast Batch ASR API Contract

* **Host**: `BATCH_ASR_HOST_IP`
* **Nginx Gateway Port**: `8080` (Direct C++: `8000`, Direct Opus: `8002`)
* **Models**:
  * Diarization: `nvidia/diar_streaming_sortformer_4spk-v2` (Q8_0 GGUF)
  * Batch ASR: `nvidia/parakeet-tdt-0.6b-v3` (Q8_0 GGUF)
* **Idle VRAM**: `1,406 MiB / 3,072 MiB` (45.7% used, 1.66 GB headroom)

### Route 2.1: Real-Time Streaming Diarization WebSocket
* **URL**: `ws://BATCH_ASR_HOST_IP:8080/v1/realtime/opus` *(or direct port `8002`)*
* **Transport**: WebSocket (Binary frames)
* **Input Audio**: The **identical 560ms Opus audio packets** sent to Server 1.

#### Output JSON Event Protocol:

1. **Speaker Turn Segment Event**:
```json
{
  "type": "diarization.segment",
  "start": 6.491,
  "end": 7.359,
  "speaker": "speaker_1",
  "confidence": 0.984
}
```

2. **Concurrent Overlapping Speech Event** (Multi-speaker cross-talk):
```json
{
  "type": "diarization.segment",
  "start": 6.850,
  "end": 8.120,
  "speaker": "speaker_2",
  "confidence": 0.920
}
```
* `start` *(float)*: Start offset in seconds from session start.
* `end` *(float)*: End offset in seconds from session start.
* `speaker` *(string)*: Unique speaker identifier (`speaker_1`, `speaker_2`, `speaker_3`, `speaker_4`).

### Route 2.2: Fast Batch ASR (Parakeet TDT 50x Real-Time)
* **URL**: `POST http://BATCH_ASR_HOST_IP:8080/v1/audio/transcriptions`
* **Headers**: `Content-Type: multipart/form-data`
* **Form Fields**:
  * `file`: Audio file binary (`.wav` format, 16kHz PCM16).
  * `model`: `"parakeet-tdt"`
  * `response_format`: `"json"` *(or `"verbose_json"`)*
  * `diarize`: `"true"` *(Optional)*

#### Response Payload:
```json
{
  "text": "Concord returned to its place amidst the tents. You will be frank with me? I always am.",
  "segments": [
    {
      "id": 0,
      "start": 0.331,
      "end": 6.479,
      "text": "Concord returned to its place amidst the tents.",
      "speaker": "speaker_1"
    },
    {
      "id": 1,
      "start": 7.051,
      "end": 25.439,
      "text": "You will be frank with me? I always am. This has indeed been a harassing day.",
      "speaker": "speaker_2"
    }
  ]
}
```

### Route 2.3: Health Check
* **URL**: `GET http://BATCH_ASR_HOST_IP:8080/health`
* **Response**: `{"status": "ok", "version": "0.1.0"}`

---

## 5. Drift-Free Timestamp Synchronization & Client Alignment Logic

### A. Monotonic Session Clock
* Both Server 1 (ASR) and Server 2 (Diarization) are fed the exact same 560ms packets starting at $T = 0.000\text{s}$.
* Every 560ms chunk sent increments the audio offset by $+0.560\text{s}$.
* Server 1's `audio_processed` field aligns $1:1$ with Server 2's `start` and `end` timestamps.

### B. Word-to-Speaker Attachment Rule (Client / Server)
When a phrase $[w_{start}, w_{end}]$ is emitted by ASR, match it to the speaker segment $[d_{start}, d_{end}]$ with maximum overlap:
$$\text{Overlap Duration} = \max\left(0, \min(w_{end}, d_{end}) - \max(w_{start}, d_{start})\right)$$

### C. Client Timeline UI Data Structure
```typescript
interface MeetingTurn {
  id: string;
  channel: "mic" | "system";        // "mic" = local user, "system" = remote attendees
  speakerId: string;                // e.g. "speaker_local", "speaker_1", "speaker_2"
  speakerName: string;              // User editable display name
  startTime: number;                // e.g. 6.491 (seconds)
  endTime: number;                  // e.g. 13.120 (seconds)
  text: string;                     // "And so my fellow Americans..."
  isLocked: boolean;                // true if user manually re-assigned speaker
}
```

---

## 6. Port & Firewall Quick Reference

| Server | Role | Port | Transport | Purpose |
| :--- | :--- | :---: | :---: | :--- |
| **Server 1** (`GPU_HOST_IP`) | Streaming ASR Engine | `8002` | WebSocket | Real-time Opus ASR Stream |
| **Server 1** (`GPU_HOST_IP`) | Streaming ASR Engine | `8000` | HTTP / WS | PCM Real-time & Health |
| **Server 2** (`BATCH_ASR_HOST_IP`) | Gateway / Nginx | `8080` | HTTP / WS | Reverse proxy for Diarization & Batch ASR |
| **Server 2** (`BATCH_ASR_HOST_IP`) | Diarization Gateway | `8002` | WebSocket | Direct Opus Diarization |
| **Server 2** (`BATCH_ASR_HOST_IP`) | NeMo-Speech Engine | `8000` | HTTP / WS | Core C++ Engine |
