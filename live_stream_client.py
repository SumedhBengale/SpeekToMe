import asyncio
import json
import time
import subprocess
import websockets
import sys
import os

AUDIO_PATH = r'E:\AI\parakeet\test_audios\sample_01.opus'
WS_URL = 'ws://GPU_HOST_IP:8080/v1/realtime'
CHUNK_DURATION = 0.560

if len(sys.argv) > 1:
    AUDIO_PATH = sys.argv[1]

print('=' * 85)
print('             SPEEKTOME LIVE STREAMING ASR CLIENT (REAL-TIME DEMO)')
print('=' * 85)
print(f'Audio Source : {AUDIO_PATH}')
print(f'Server URL   : {WS_URL}')
print(f'Chunk Window : {CHUNK_DURATION * 1000:.0f} ms')
print('-' * 85)

print('Decoding audio to 16kHz 16-bit Mono PCM...')
cmd = [
    'ffmpeg', '-y', '-loglevel', 'error',
    '-i', AUDIO_PATH,
    '-f', 's16le',
    '-acodec', 'pcm_s16le',
    '-ar', '16000',
    '-ac', '1',
    '-'
]
proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
raw_pcm, _ = proc.communicate()

total_samples = len(raw_pcm) // 2
total_duration_sec = total_samples / 16000.0
print(f'Decoded Audio: {total_duration_sec:.2f} seconds ({len(raw_pcm):,} bytes)\n')

BYTES_PER_CHUNK = int(16000 * 2 * CHUNK_DURATION)

async def stream_live():
    print('Connecting to SpeekToMe Streaming WebSocket...')
    async with websockets.connect(WS_URL) as ws:
        created_evt = json.loads(await ws.recv())
        session_id = created_evt.get('event_id', 'N/A')
        print(f'Session Created (ID: {session_id})')

        session_config = {
            'type': 'session.update',
            'session': {
                'sample_rate': 16000,
                'automatic_punctuation': True,
                'language': 'auto'
            }
        }
        await ws.send(json.dumps(session_config))
        updated_evt = json.loads(await ws.recv())
        model_name = updated_evt.get('session', {}).get('model', 'nemotron-3.5')
        print(f'Session Configured (Model: {model_name})\n')

        print('=' * 85)
        print('                   LIVE TRANSCRIPTION FEED (STREAMING UPDATES)')
        print('=' * 85)
        print('\n[LIVE TRANSCRIPT] -> ', end='', flush=True)

        full_transcript = []
        recv_done = asyncio.Event()

        async def listen_deltas():
            try:
                while not recv_done.is_set():
                    try:
                        raw_msg = await asyncio.wait_for(ws.recv(), timeout=0.1)
                        evt = json.loads(raw_msg)
                        evt_type = evt.get('type', '')
                        
                        if 'delta' in evt:
                            delta_text = evt['delta']
                            full_transcript.append(delta_text)
                            print(delta_text, end='', flush=True)
                        elif evt_type == 'conversation.item.input_audio_transcription.completed':
                            final_text = evt.get('transcript', '')
                            if final_text and not ''.join(full_transcript).strip():
                                print(final_text, end='', flush=True)
                            recv_done.set()
                            break
                    except asyncio.TimeoutError:
                        continue
                    except Exception:
                        break
            except Exception:
                pass

        listen_task = asyncio.create_task(listen_deltas())

        t_stream_start = time.perf_counter()
        for idx, offset in enumerate(range(0, len(raw_pcm), BYTES_PER_CHUNK)):
            chunk = raw_pcm[offset : offset + BYTES_PER_CHUNK]
            t_chunk_start = time.perf_counter()
            
            await ws.send(chunk)
            
            elapsed = time.perf_counter() - t_chunk_start
            sleep_duration = max(0.0, CHUNK_DURATION - elapsed)
            await asyncio.sleep(sleep_duration)

        await ws.send(json.dumps({'type': 'input_audio_buffer.commit'}))

        try:
            await asyncio.wait_for(recv_done.wait(), timeout=3.0)
        except asyncio.TimeoutError:
            pass

        listen_task.cancel()
        total_time = time.perf_counter() - t_stream_start
        
        print('\n\n' + '=' * 85)
        print(f'Stream Completed in {total_time:.2f}s | Audio Duration: {total_duration_sec:.2f}s')
        print('=' * 85)

if __name__ == '__main__':
    asyncio.run(stream_live())
