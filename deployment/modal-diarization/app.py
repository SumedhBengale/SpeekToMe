"""Serverless GPU diarization: official Nemotron-3-Diarization Q8_0 GGUF on NVIDIA NeMo-Speech.cpp.

Two functions:
  api       CPU-only HTTPS front (`requires_proxy_auth`, so Modal's edge drops requests without
            the workspace proxy token before any container starts) that also checks the
            coordinator's bearer key, validates the envelope header and spawns the GPU job. It
            speaks the same job contract as deployment/diarization-server (POST, poll, result,
            DELETE), so server/handlers/meeting_diarization.go drives both. It never holds the
            diarizer private key and never sees plaintext.
  Diarizer  one fresh single-use GPU container per request: opens the envelope (envelope.py),
            decodes and diarizes entirely in memory, seals the segments to the job's response
            key and exits. Everything is baked into the image; the CLI loads the model in ~0.2 s.

    python provision.py ../../server/.env   # keys, Modal secrets, proxy token (once; see README)
    modal deploy app.py
    python provision.py ../../server/.env --url-only
    # end to end through the real Go client (server/handlers/diarization_gpu_integration_test.go):
    SPEEKTOME_REAL_GPU_ENV=$PWD/../../server/.env SPEEKTOME_REAL_AUDIO=meeting.opus \
      go -C ../../server test ./handlers -run RealGPU -v
"""
import hashlib
import hmac
import json
import os
import struct
import subprocess
import tempfile
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import modal

# NeMo-Speech.cpp nightly 0.2.0+nightly.caa3f47. v0.2.0 is ~7x slower on whole files (O(N^2)
# buffer trim, fixed by NVIDIA/NeMo-Speech.cpp#62); move to the first tagged release that
# contains caa3f47. The nightly asset is replaced over time, so the checksum fails loudly on a
# rebuild after it moves; Modal keeps the built layer, so deploys reuse it until this line changes.
RUNTIME = 'https://github.com/NVIDIA/NeMo-Speech.cpp/releases/download/nightly/nemo-speech-nightly-linux-x86_64-cuda.tar.gz'
RUNTIME_SHA256 = 'a248f862da0baa0ffcfadb5a4a4a23d4ee88bf881e8a71373ef3a46d5418b86f'
# The file and revision NeMo-Speech.cpp's own model index pins for `nemotron-3-diarization`.
MODEL = ('https://huggingface.co/nvidia/Nemotron-3-Diarization/resolve/f667ed73aee57d40cc39428eb768b4fd87a0a29e/'
         'Nemotron-3-Diarization.q8_0.gguf')
MODEL_SHA256 = '08456d9e22cd9a323c0364d98375f3746d6e68507ebb705cd46438c534c7a3a1'

ROOT = Path('/opt/nemo-speech')
CLI = ROOT/'bin'/'nemo-speech'
GGUF = Path('/models/Nemotron-3-Diarization.q8_0.gguf')
CONFIG = Path('/opt/diar.yaml')
# The model card's 30.4 s long-form geometry, in 80 ms frames. Set without `preset`: a preset
# replaces these keys, and NeMo-Speech.cpp's `v3-offline` is a different (264/188/fifo 0) geometry.
GEOMETRY = dict(chunk=340, right_context=40, fifo=40, spkcache=264, update_period=300)
# Raw 0.5 thresholds as in NVIDIA's DER protocol. The CLI's default segment postprocessing
# scored 27% DER vs 9.35% on AMI ES2004a (benchmark/modal-diarization/README.md).
SEGMENTATION = ['--onset', '0.5', '--offset', '0.5', '--pad-onset', '0', '--pad-offset', '0',
                '--min-duration-on', '0', '--min-duration-off', '0']
GPUS = ['L4', 'A10', 'L40S']  # cost first; any free type is accepted (benchmark/modal-diarization/README.md)
DECODE_PIECES = 8
MAX_ENVELOPE = 512 << 20      # ~48 h of 24 kb/s Opus
FRESHNESS = (900, 120)        # accept envelopes issued up to 15 min ago, 2 min ahead

config_yaml = 'diar:\\n  model_path: {}\\n{}'.format(GGUF, ''.join(f'  {k}: {v}\\n' for k, v in GEOMETRY.items()))
gpu_image = (
    modal.Image.debian_slim(python_version='3.12')
    .run_commands(
        'apt-get update && apt-get install -y --no-install-recommends ffmpeg curl ca-certificates'
        ' && apt-get clean && rm -rf /var/lib/apt/lists/*')
    .pip_install('numpy==2.5.3', 'cryptography==50.0.0')
    # One layer: fetch, verify, unpack, drop files the CLI never loads (headers, CMake package,
    # docs, the C-API shims), then fetch and verify the model. The diarize command links the
    # TTS/NMT/text-normalization libraries dynamically, so those stay.
    .run_commands(
        f'curl -fsSL --retry 3 {RUNTIME} -o /tmp/runtime.tgz'
        f" && echo '{RUNTIME_SHA256}  /tmp/runtime.tgz' | sha256sum -c -"
        f' && mkdir -p {ROOT} && tar -xzf /tmp/runtime.tgz -C {ROOT} --strip-components=1 && rm /tmp/runtime.tgz'
        f' && rm -rf {ROOT}/include {ROOT}/share/doc {ROOT}/lib/cmake {ROOT}/lib/libnemo_speech_*_c.so*'
        f' && mkdir -p {GGUF.parent} && curl -fsSL --retry 3 {MODEL} -o {GGUF}'
        f" && echo '{MODEL_SHA256}  {GGUF}' | sha256sum -c -"
        f" && printf '{config_yaml}' > {CONFIG}")
    .add_local_file(Path(__file__).parent.parent/'diarization-server'/'MODEL_LICENSE', '/models/LICENSE', copy=True)
    .add_local_python_source('envelope')
)
api_image = (modal.Image.debian_slim(python_version='3.12')
             .pip_install('fastapi[standard]==0.159.1', 'cryptography==50.0.0')
             .add_local_python_source('envelope'))
app = modal.App('speektome-diarization')
jobs = modal.Dict.from_name('speektome-diarization-jobs', create_if_missing=True)


def memfd(name, *chunks):
    fd = os.memfd_create(name)
    for chunk in chunks:
        view = memoryview(chunk).cast('B')
        while view:
            view = view[os.write(fd, view[:64 << 20]):]
    return fd


def decode(fd, pieces=DECODE_PIECES):
    """Ogg/Opus (memfd) to 16 kHz mono PCM16: concurrent ffmpeg seeks with 1 s decoder pre-roll,
    spliced at exact sample offsets (sample-identical to one sequential decode)."""
    import numpy as np
    source = f'/dev/fd/{fd}'
    seconds = float(subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0',
                                    source], check=True, capture_output=True, text=True, pass_fds=(fd,)).stdout)
    edges = [round(seconds*16000*i/pieces) for i in range(pieces+1)]

    def piece(i):
        start, count = edges[i], edges[i+1]-edges[i]
        preroll = min(start, 16000)
        limit = ['-t', f'{(preroll+count)/16000+1:.6f}'] if i < pieces-1 else []
        raw = subprocess.run(['ffmpeg', '-nostdin', '-v', 'error', '-ss', f'{(start-preroll)/16000:.6f}', *limit,
                              '-i', source, '-ar', '16000', '-ac', '1', '-f', 's16le', 'pipe:1'],
                             check=True, capture_output=True, pass_fds=(fd,)).stdout
        samples = np.frombuffer(raw, dtype=np.int16)[preroll:]
        return samples if i == pieces-1 else samples[:count]
    with ThreadPoolExecutor(pieces) as pool:
        return np.concatenate(list(pool.map(piece, range(pieces))))


def wav_header(samples):
    size = samples*2
    return (b'RIFF'+struct.pack('<I', 36+size)+b'WAVEfmt '+struct.pack('<IHHIIHH', 16, 1, 1, 16000, 32000, 2, 16)
            + b'data'+struct.pack('<I', size))


def prefetch():
    """Read the model and CUDA kernels once so the image's lazily loaded files arrive while audio decodes."""
    for path in (GGUF, *ROOT.glob('lib/libggml-cuda.so*'), *ROOT.glob('lib/libcublas*.so*')):
        if path.is_file() and not path.is_symlink():
            with path.open('rb') as source:
                while source.read(8 << 20):
                    pass


@app.cls(image=gpu_image, gpu=GPUS, cpu=4, memory=8192, timeout=1800, single_use_containers=True,
         max_containers=10, secrets=[modal.Secret.from_name('speektome-diarization-key')])
class Diarizer:
    @modal.method()
    def diarize(self, envelope: bytes) -> bytes:
        """Open one sealed recording, diarize it in memory, return the sealed segments."""
        import envelope as env
        started = time.time()
        warm = threading.Thread(target=prefetch)
        warm.start()
        keys = env.load_private_keys(os.environ['DIARIZATION_PRIVATE_KEY'],
                                     os.environ.get('DIARIZATION_PREVIOUS_PRIVATE_KEY', ''))
        try:
            audio, job_id, response_key = env.open_request(envelope, keys)
        except env.EnvelopeError:
            raise RuntimeError('envelope rejected') from None
        del envelope
        opus = memfd('audio', audio)
        del audio
        try:
            samples = decode(opus)
        except subprocess.CalledProcessError:
            raise RuntimeError('audio could not be decoded') from None
        finally:
            os.close(opus)
        wav = memfd('pcm', wav_header(len(samples)), samples)
        audio_seconds = len(samples)/16000
        del samples
        decoded = time.time()
        warm.join()
        # The CLI only accepts a *.wav path; the link holds no audio, the memfd stays in RAM.
        with tempfile.TemporaryDirectory() as links:
            link = Path(links)/'audio.wav'
            link.symlink_to(f'/proc/{os.getpid()}/fd/{wav}')
            run = subprocess.run([CLI, 'diarize', link, '--config', CONFIG, '--device', 'cuda:0', *SEGMENTATION,
                                  '--format', 'json'], capture_output=True, text=True)
        os.close(wav)
        if run.returncode != 0:
            raise RuntimeError('diarization runtime failed')
        segments = json.loads(run.stdout)['segments']
        finished = time.time()
        gpu = subprocess.run(['nvidia-smi', '--query-gpu=name', '--format=csv,noheader'], capture_output=True,
                             text=True).stdout.strip()
        result = dict(segments=segments, audio_seconds=audio_seconds, gpu=gpu,
                      timings=dict(decode_s=decoded-started, diarize_s=finished-decoded, total_s=finished-started))
        return env.seal_response(json.dumps(result, separators=(',', ':')).encode(), response_key, job_id)


@app.function(image=api_image, cpu=0.25, memory=1024, timeout=300,
              secrets=[modal.Secret.from_name('speektome-diarization-api')])
@modal.concurrent(max_inputs=32)
@modal.asgi_app(requires_proxy_auth=True)
def api():
    import envelope as env
    from fastapi import FastAPI, Request, Response
    from fastapi.responses import JSONResponse

    token_hash = bytes.fromhex(os.environ['DIARIZATION_TOKEN_SHA256'])
    accepted_keys = {bytes.fromhex(k) for k in os.environ['DIARIZATION_KEY_IDS'].split(',') if k}
    web = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)

    def error(code, message, **headers):
        return JSONResponse({'detail': message}, status_code=code, headers=headers)

    @web.middleware('http')
    async def bearer(request: Request, call_next):
        # Modal's edge has already required the proxy token; this is the coordinator's own key.
        supplied = request.headers.get('authorization', '')
        given = hashlib.sha256(supplied.removeprefix('Bearer ').encode()).digest()
        if not supplied.startswith('Bearer ') or not hmac.compare_digest(given, token_hash):
            return error(401, 'unauthorized', **{'WWW-Authenticate': 'Bearer'})
        return await call_next(request)

    def valid(job_id):
        return len(job_id) == 32 and all(c in '0123456789abcdef' for c in job_id)

    async def outcome(job_id):
        entry = await jobs.get.aio(job_id) if valid(job_id) else None
        if entry is None:
            return None, None
        call = modal.FunctionCall.from_id(entry['call'])
        try:
            return 'completed', await call.get.aio(timeout=0)
        except (TimeoutError, modal.exception.TimeoutError):
            return 'processing', None
        except modal.exception.OutputExpiredError:
            return 'failed', 'result expired'
        except Exception as exc:
            message = str(exc)
            return 'failed', message if message in ('envelope rejected', 'audio could not be decoded') else 'diarization failed'

    @web.post('/v1/diarizations')
    async def submit(request: Request):
        job_id = request.headers.get('x-speektome-job-id', '')
        if not valid(job_id):
            return error(400, 'X-SpeekToMe-Job-ID must be 32 lowercase hex characters')
        if int(request.headers.get('content-length') or 0) > MAX_ENVELOPE:
            return error(413, 'envelope too large')
        if await jobs.get.aio(job_id) is not None:  # same identity: never rerun, never re-read the body
            return JSONResponse(dict(job_id=job_id, status_url=f'/v1/diarizations/{job_id}',
                                     result_url=f'/v1/diarizations/{job_id}/result'), status_code=202)
        body = await request.body()
        if len(body) > MAX_ENVELOPE:
            return error(413, 'envelope too large')
        try:
            header = env.parse_request_header(body)
        except env.EnvelopeError:
            return error(400, 'not a diarization envelope')
        now = time.time()
        if header['job_id'] != job_id or header['key_id'] not in accepted_keys:
            return error(400, 'envelope does not match this endpoint or job')
        if not now-FRESHNESS[0] <= header['issued_at'] <= now+FRESHNESS[1]:
            return error(400, 'envelope expired')
        call = await Diarizer().diarize.spawn.aio(body)
        await jobs.put.aio(job_id, dict(call=call.object_id, created=now))
        return JSONResponse(dict(job_id=job_id, status_url=f'/v1/diarizations/{job_id}',
                                 result_url=f'/v1/diarizations/{job_id}/result'), status_code=202)

    @web.get('/v1/diarizations/{job_id}')
    async def status(job_id: str):
        state, detail = await outcome(job_id)
        if state is None:
            return error(404, 'job not found')
        return {'job_id': job_id, 'status': state, 'error': detail if state == 'failed' else ''}

    @web.get('/v1/diarizations/{job_id}/result')
    async def result(job_id: str):
        state, detail = await outcome(job_id)
        if state is None:
            return error(404, 'job not found')
        if state == 'processing':
            return error(409, 'still processing')
        if state == 'failed':
            return error(422, detail)
        return Response(detail, media_type='application/vnd.speektome.diarization-envelope')

    @web.delete('/v1/diarizations/{job_id}')
    async def acknowledge(job_id: str):
        entry = await jobs.get.aio(job_id) if valid(job_id) else None
        if entry is None:
            return error(404, 'job not found')
        await jobs.pop.aio(job_id)
        return {}

    @web.get('/healthz')
    async def health():
        return {'ok': True}

    return web
