import os
import io
import time
import json
import asyncio
import torch
import soundfile as sf
import numpy as np
import websockets
import pynvml
import nemo.collections.asr as nemo_asr
import torchao
from torchao.quantization.quant_api import quantize_, Int8WeightOnlyConfig

MODEL_PATH = os.environ.get("NEMOTRON_MODEL_PATH", "/opt/speektome-asr/models/nemotron-3.5-asr-streaming-0.6b.nemo")
PORT = int(os.environ.get("PORT", 8080))
CHUNK_SECS = 0.56
CHUNK_SAMPLES = int(CHUNK_SECS * 16000)

print(f"[BOOT] Initializing Nemotron Streaming ASR Worker on Port {PORT}...")

# 1. Initialize GPU and restrict memory to fit inside 3GB fractional GPU
device = torch.device("cuda:0" if torch.cuda.is_available() else "cpu")
if torch.cuda.is_available():
    torch.cuda.set_per_process_memory_fraction(0.90) # Safety limit for 3GB VRAM slice
    pynvml.nvmlInit()
    nvml_handle = pynvml.nvmlDeviceGetHandleByIndex(0)
    mem_info = pynvml.nvmlDeviceGetMemoryInfo(nvml_handle)
    print(f"[GPU] Device: {pynvml.nvmlDeviceGetName(nvml_handle)} | Total VRAM: {mem_info.total / 1024**3:.2f} GB")

# 2. Restore model and apply INT8 quantization
t0 = time.time()
print(f"[MODEL] Loading model from {MODEL_PATH}...")
model = nemo_asr.models.ASRModel.restore_from(MODEL_PATH, map_location=device)
model.eval()
model = model.half()

print("[QUANT] Applying torchao INT8 Weight-Only Quantization...")
quantize_(model, Int8WeightOnlyConfig())
torch.cuda.empty_cache()
print(f"[MODEL READY] Restored & quantized in {time.time()-t0:.2f}s!")

if torch.cuda.is_available():
    mem_info = pynvml.nvmlDeviceGetMemoryInfo(nvml_handle)
    print(f"[VRAM BASE] Dedicated INT8 Model VRAM: {mem_info.used / 1024**3:.2f} GB / {mem_info.total / 1024**3:.2f} GB")

# Warmup pass
warmup_t = torch.zeros((1, CHUNK_SAMPLES), dtype=torch.float32, device=device)
warmup_len = torch.tensor([CHUNK_SAMPLES], dtype=torch.long, device=device)
with torch.inference_mode():
    w_feat, w_feat_len = model.preprocessor(input_signal=warmup_t, length=warmup_len)
    w_enc_out, w_enc_len = model.encoder(audio_signal=w_feat, length=w_feat_len)
    _ = model.decoding.rnnt_decoder_predictions_tensor(
        encoder_output=w_enc_out,
        encoded_lengths=w_enc_len,
        return_hypotheses=False
    )
print("[WARMUP] GPU Warmup pass complete!")

async def transcribe_audio_chunk(audio_bytes):
    try:
        sig, sr = sf.read(io.BytesIO(audio_bytes), dtype="float32")
        if sig.ndim > 1:
            sig = sig.mean(axis=1)
        if len(sig) == 0:
            return ""
        
        # Resample to 16kHz if needed or pad/trim
        padded = np.zeros(max(len(sig), CHUNK_SAMPLES), dtype=np.float32)
        padded[:len(sig)] = sig
        
        audio_t = torch.tensor(padded.reshape(1, -1), dtype=torch.float32, device=device)
        len_t = torch.tensor([len(sig)], dtype=torch.long, device=device)
        
        with torch.inference_mode():
            feat, feat_len = model.preprocessor(input_signal=audio_t, length=len_t)
            enc_out, enc_len = model.encoder(audio_signal=feat, length=feat_len)
            best_hyp = model.decoding.rnnt_decoder_predictions_tensor(
                encoder_output=enc_out,
                encoded_lengths=enc_len,
                return_hypotheses=True
            )
            if best_hyp and len(best_hyp[0]) > 0:
                return best_hyp[0][0].text
    except Exception as e:
        print(f"[TRANSCRIPTION ERROR] {e}")
    return ""

async def handle_websocket_client(websocket, path):
    client_ip = websocket.remote_address[0]
    print(f"[SESSION START] Client connected from {client_ip}")
    
    audio_buffer = bytearray()
    last_text = ""
    
    try:
        async for message in websocket:
            if isinstance(message, str):
                try:
                    msg = json.loads(message)
                    m_type = msg.get("type")
                    if m_type == "start":
                        lang = msg.get("language", "en")
                        print(f"[SESSION CONFIG] Language: {lang}")
                        await websocket.send(json.dumps({"type": "session_ready", "provider": "self_hosted_nemotron_3_5"}))
                    elif m_type in ("stop", "end_of_stream"):
                        print(f"[SESSION STOP] Processing remaining buffer ({len(audio_buffer)} bytes)...")
                        if len(audio_buffer) > 0:
                            final_text = await transcribe_audio_chunk(bytes(audio_buffer))
                            if final_text:
                                await websocket.send(json.dumps({"type": "final", "text": final_text}))
                        break
                except Exception as e:
                    print(f"[CONTROL MSG ERROR] {e}")
            elif isinstance(message, bytes):
                audio_buffer.extend(message)
                # If we have accumulated roughly 560ms of audio, transcribe
                if len(audio_buffer) >= (CHUNK_SAMPLES * 2): # ~16-bit PCM equivalent
                    interim = await transcribe_audio_chunk(bytes(audio_buffer))
                    if interim and interim != last_text:
                        last_text = interim
                        await websocket.send(json.dumps({"type": "interim", "text": interim}))
                        
    except websockets.exceptions.ConnectionClosed:
        pass
    finally:
        print(f"[SESSION END] Closed connection with {client_ip}")

async def main():
    print(f"[SERVER] Starting Nemotron Streaming WebSocket Server on 0.0.0.0:{PORT}...")
    async with websockets.serve(handle_websocket_client, "0.0.0.0", PORT, max_size=32 * 1024 * 1024):
        await asyncio.Future()

if __name__ == "__main__":
    asyncio.run(main())
