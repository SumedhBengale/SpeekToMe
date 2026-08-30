import asyncio
import json
import sounddevice as sd
import websockets
import sys
import io
import time
import soundfile as sf
import numpy as np

WS_URL = "ws://GPU_HOST_IP:8080/v1/realtime/opus"
SAMPLE_RATE = 16000
CHUNK_DURATION = 0.560  # 560ms chunks
BLOCK_SIZE = int(SAMPLE_RATE * CHUNK_DURATION)  # 8,960 samples

if len(sys.argv) > 1 and sys.argv[1].startswith("ws"):
    WS_URL = sys.argv[1]

print("=" * 85)
print("     SPEEKTOME LIVE MICROPHONE STREAMING (OPUS COMPRESSED / 85% LESS BANDWIDTH)")
print("=" * 85)
print(f"Server Endpoint : {WS_URL}")
print(f"Sample Rate     : {SAMPLE_RATE} Hz")
print(f"Chunk Window    : {CHUNK_DURATION * 1000:.0f} ms ({BLOCK_SIZE} samples / chunk)")
print("-" * 85)

audio_q = asyncio.Queue()
loop = None

def audio_callback(indata, frames, time_info, status):
    if status:
        print(f"[Audio Status Warning]: {status}", file=sys.stderr)
    if loop and not loop.is_closed():
        # Copy float data
        float_chunk = indata[:, 0].copy()
        loop.call_soon_threadsafe(audio_q.put_nowait, float_chunk)

async def run_mic_stream():
    global loop
    loop = asyncio.get_running_loop()
    
    print("Connecting to SpeekToMe Opus Gateway...")
    try:
        async with websockets.connect(WS_URL, open_timeout=5.0) as ws:
            created_evt = json.loads(await ws.recv())
            session_id = created_evt.get("event_id", "N/A")
            print(f"Session Created (ID: {session_id})")

            session_config = {
                "type": "session.update",
                "session": {
                    "sample_rate": SAMPLE_RATE,
                    "automatic_punctuation": True,
                    "language": "auto"
                }
            }
            await ws.send(json.dumps(session_config))
            updated_evt = json.loads(await ws.recv())
            model_name = updated_evt.get("session", {}).get("model", "nemotron-3.5")
            print(f"Session Configured (Model: {model_name})")

            print("\n" + "=" * 85)
            print("   MICROPHONE LIVE (OPUS)! Speak into your mic (Press Ctrl+C to stop)...")
            print("=" * 85)
            print("\n[LIVE TRANSCRIPTION] -> ", end="", flush=True)

            stop_event = asyncio.Event()

            async def receiver():
                try:
                    while not stop_event.is_set():
                        try:
                            raw = await asyncio.wait_for(ws.recv(), timeout=0.1)
                            evt = json.loads(raw)
                            if "delta" in evt:
                                print(evt["delta"], end="", flush=True)
                        except asyncio.TimeoutError:
                            continue
                        except websockets.exceptions.ConnectionClosed:
                            break
                except Exception:
                    pass

            async def sender():
                try:
                    while not stop_event.is_set():
                        try:
                            float_data = await asyncio.wait_for(audio_q.get(), timeout=0.2)
                            # Compress 560ms frame to Opus in-memory
                            with io.BytesIO() as buf:
                                sf.write(buf, float_data, samplerate=SAMPLE_RATE, format="OGG", subtype="OPUS")
                                opus_bytes = buf.getvalue()
                            await ws.send(opus_bytes)
                        except asyncio.TimeoutError:
                            continue
                        except websockets.exceptions.ConnectionClosed:
                            break
                except Exception:
                    pass

            with sd.InputStream(samplerate=SAMPLE_RATE, channels=1, dtype="float32",
                                blocksize=BLOCK_SIZE, callback=audio_callback):
                recv_task = asyncio.create_task(receiver())
                send_task = asyncio.create_task(sender())

                try:
                    while not stop_event.is_set():
                        await asyncio.sleep(0.1)
                except (KeyboardInterrupt, asyncio.CancelledError):
                    print("\n\nStopping microphone stream...")
                finally:
                    stop_event.set()
                    try:
                        await ws.send(json.dumps({"type": "input_audio_buffer.commit"}))
                        final_raw = await asyncio.wait_for(ws.recv(), timeout=2.0)
                        final_evt = json.loads(final_raw)
                        final_transcript = final_evt.get("transcript", "")
                        if final_transcript:
                            print(f"\n[FINAL TRANSCRIPT] -> {final_transcript}")
                    except Exception:
                        pass

                    recv_task.cancel()
                    send_task.cancel()

    except Exception as e:
        print(f"\n[Connection Error]: {e}", file=sys.stderr)

if __name__ == "__main__":
    try:
        asyncio.run(run_mic_stream())
    except KeyboardInterrupt:
        print("\nExited.")
