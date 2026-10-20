"""Single-slot durable CPU diarization API; raw Ogg/Opus upload, resident INT8 model."""
import asyncio
import contextlib
import gc
import hashlib
import hmac
import json
import logging
import math
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import threading
import time

import numpy as np
import onnxruntime as ort
import psutil
import soundfile as sf
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse, FileResponse
import uvicorn

ROOT = Path(os.environ.get('DIAR_ROOT', '/opt/speektome-diarization'))
DATA = Path(os.environ.get('DIAR_DATA', '/var/lib/speektome-diarization'))
TOKEN = Path(os.environ.get('DIAR_TOKEN_FILE', '/etc/speektome-diarization/token')).read_text().strip()
if len(TOKEN) < 24:
    raise RuntimeError('Authentication key missing or too short')
MAX_UPLOAD = int(os.environ.get('DIAR_MAX_UPLOAD_BYTES', str(2*1024**3)))
MEMORY_BUDGET = int(os.environ.get('DIAR_MEMORY_BUDGET', str(3*1024**3)))
RESERVE = 512*1024**2
DATA.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(ROOT/'models'))
from diarize import OnnxDiarizer, extract_speaker_dict, segments_to_rttm

logging.basicConfig(level=logging.INFO, format='%(asctime)s %(levelname)s %(message)s')
log = logging.getLogger('diarization')
ort.disable_telemetry_events()
lock = threading.RLock()
slot = threading.Lock()
active = None
ready = False
model = None
started_at = time.time()
stop = threading.Event()

def atomic_json(path, value):
    temp = path.with_suffix(path.suffix+'.tmp')
    with temp.open('w') as out:
        json.dump(value, out, separators=(',', ':'))
        out.flush()
        os.fsync(out.fileno())
    temp.replace(path)
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)

def job_dir(job_id):
    if len(job_id) != 32 or any(c not in '0123456789abcdef' for c in job_id):
        raise HTTPException(404, 'Job not found')
    return DATA/job_id

def update(job, persist=False, **fields):
    with lock:
        previous = job.get('phase')
        job.update(fields, updated_at=time.time())
        if persist:
            atomic_json(job_dir(job['job_id'])/'job.json', job)
    if 'phase' in fields and fields['phase'] != previous:
        log.info('job=%s phase=%s', job['job_id'], fields['phase'])

def public_job(job):
    result = dict(job)
    duration = job.get('audio_seconds')
    chunks = job.get('completed_windows', 0)
    result['progress_percent'] = round(min(100, chunks*27.2/duration*100), 2) if duration else 0
    if job['status'] == 'completed':
        result['progress_percent'] = 100
    result['elapsed_seconds'] = round((job.get('finished_at') or time.time())-job['created_at'], 2)
    if chunks and job.get('diarization_started_at') and duration and job['status'] == 'processing':
        elapsed = time.time()-job['diarization_started_at']
        fraction = min(1, chunks*27.2/duration)
        result['estimated_remaining_seconds'] = round(elapsed*(1/fraction-1), 1)
    return result

def snapshot():
    vm = psutil.virtual_memory()
    disk = shutil.disk_usage(DATA)
    with lock:
        job = public_job(active) if active else None
    process = psutil.Process()
    return dict(ready=ready, busy=slot.locked(), capacity=1, active_job=job,
                runtime={'model': 'Nemotron-3-Diarization ONNX INT8', 'backend': 'CPUExecutionProvider',
                         'onnxruntime': ort.__version__, 'threads': 2, 'mode': 'bounded-offline'},
                system={'cpu_percent': psutil.cpu_percent(), 'cpu_count': psutil.cpu_count(),
                        'ram_total_bytes': vm.total, 'ram_available_bytes': vm.available,
                        'ram_percent': vm.percent, 'process_rss_bytes': process.memory_info().rss,
                        'disk_free_bytes': disk.free, 'disk_total_bytes': disk.total,
                        'load_average': os.getloadavg(), 'uptime_seconds': round(time.time()-started_at, 1)},
                limits={'max_upload_bytes': MAX_UPLOAD, 'memory_budget_bytes': MEMORY_BUDGET,
                        'disk_reserve_bytes': RESERVE}, queue_length=0)

def cleanup_old():
    for path in DATA.iterdir():
        if not path.is_dir():
            continue
        try:
            job = json.loads((path/'job.json').read_text())
            if job['status'] in ('completed', 'failed') and time.time()-job.get('finished_at', job['created_at']) > 7*86400:
                shutil.rmtree(path)
        except (OSError, ValueError, KeyError):
            pass

def run_job(job):
    global active
    path = job_dir(job['job_id'])
    begin = time.monotonic()
    original_chunk = model.run_chunk
    original_mel = model.log_mel
    try:
        update(job, persist=True, status='processing', phase='probing', completed_windows=0)
        probe = subprocess.run(['ffprobe', '-v', 'error', '-show_entries',
                                'format=duration:stream=codec_name,channels', '-of', 'json', str(path/'input.opus')],
                               capture_output=True, timeout=60, check=True)
        info = json.loads(probe.stdout)
        streams = info.get('streams', [])
        if len(streams) != 1 or streams[0].get('codec_name') != 'opus' or streams[0].get('channels') != 1:
            raise ValueError('Input must contain one mono Opus audio stream')
        duration = float(info['format']['duration'])
        if not math.isfinite(duration) or duration <= 0:
            raise ValueError('Audio duration is invalid')
        estimate = 512*1024**2 + duration/60*8*1024**2
        if estimate > MEMORY_BUDGET:
            raise ValueError('Recording exceeds this worker memory budget; use a larger worker')
        if shutil.disk_usage(DATA).free < duration*16000*2 + RESERVE:
            raise ValueError('Insufficient disk space for decoded audio')
        update(job, phase='decoding', audio_seconds=duration, decode_progress_percent=0)
        cmd = ['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error', '-y', '-i', str(path/'input.opus'),
               '-ar', '16000', '-ac', '1', '-progress', 'pipe:1', str(path/'decoded.wav')]
        decode_start = time.monotonic()
        with (path/'decode-error.txt').open('w') as err:
            process = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=err, text=True)
            for line in process.stdout:
                key, _, value = line.strip().partition('=')
                if key == 'out_time_us':
                    with contextlib.suppress(ValueError):
                        update(job, decode_progress_percent=round(min(100, int(value)/1e6/duration*100), 1))
            if process.wait() != 0:
                raise ValueError('Opus decoding failed')
        audio, sr = sf.read(path/'decoded.wav', dtype='float32')
        (path/'decoded.wav').unlink()
        duration = len(audio)/sr
        update(job, audio_seconds=duration, decode_seconds=time.monotonic()-decode_start,
               total_windows=math.ceil(math.ceil((len(audio)//160+1)/model.subsampling_factor)/model.chunk_length))
        def features(waveform):
            update(job, phase='extracting_features')
            return original_mel(waveform)
        def chunk(*args):
            if job.get('phase') != 'diarizing':
                update(job, phase='diarizing', diarization_started_at=time.time())
            output = original_chunk(*args)
            count = job.get('completed_windows', 0)+1
            update(job, completed_windows=count, processed_audio_seconds=min(duration, count*27.2))
            if count == 1 or count == job['total_windows'] or count % max(1, job['total_windows']//10) == 0:
                log.info('job=%s windows=%s/%s progress=%.1f%%', job['job_id'], count,
                         job['total_windows'], min(100, count*27.2/duration*100))
            return output
        model.log_mel = features
        model.run_chunk = chunk
        probs = model.diarize(audio, sample_rate=sr)
        update(job, phase='writing_results')
        segments = extract_speaker_dict(probs)
        result = {'job_id': job['job_id'], 'audio_seconds': duration, 'segments': segments,
                  'speakers': sorted({s['Speaker'] for s in segments}),
                  'model': 'Nemotron-3-Diarization ONNX INT8', 'mode': 'bounded-offline'}
        atomic_json(path/'result.json', result)
        rttm = segments_to_rttm(segments, job['job_id'])
        with (path/'result.rttm').open('w') as out:
            out.write(rttm)
            out.flush()
            os.fsync(out.fileno())
        update(job, persist=True, status='completed', phase='completed', finished_at=time.time(),
               processing_seconds=time.monotonic()-begin, segment_count=len(segments),
               speaker_count=len(result['speakers']), result_url=f'/v1/diarizations/{job["job_id"]}/result')
        log.info('job=%s completed seconds=%.2f', job['job_id'], time.monotonic()-begin)
    except Exception as exc:
        # Avoid sending decoder stderr, audio content, or stack traces to logs.
        message = str(exc) if isinstance(exc, ValueError) else type(exc).__name__
        update(job, persist=True, status='failed', phase='failed', error=message, finished_at=time.time())
        log.warning('job=%s failed; private details omitted', job['job_id'])
    finally:
        model.run_chunk = original_chunk
        model.log_mel = original_mel
        for name in ('input.opus', 'decoded.wav', 'decode-error.txt'):
            (path/name).unlink(missing_ok=True)
        # Release whole-file arrays; keep the model resident.
        audio = probs = None
        gc.collect()
        with lock:
            active = None
        slot.release()

def initialize():
    global ready, model, active
    try:
        cleanup_old()
        model = OnnxDiarizer(onnx_dir=ROOT/'models', threads=2, model_filename='model.int8.onnx')
        recover = []
        for path in DATA.iterdir():
            if not path.is_dir():
                continue
            try:
                job = json.loads((path/'job.json').read_text())
                if job['status'] in ('uploading', 'uploaded', 'processing'):
                    if job['status'] == 'uploading' or not (path/'input.opus').exists() or job.get('recovery_attempts', 0) >= 1:
                        update(job, persist=True, status='failed', phase='failed',
                               error='Upload interrupted or repeated runtime crash; resubmit from coordinator', finished_at=time.time())
                        for name in ('input.opus', 'input.part', 'decoded.wav', 'decode-error.txt'):
                            (path/name).unlink(missing_ok=True)
                    else:
                        recover.append(job)
            except (OSError, ValueError, KeyError):
                continue
        if recover:
            job = sorted(recover, key=lambda j: j['created_at'])[0]
            slot.acquire()
            active = job
            update(job, persist=True, recovery_attempts=job.get('recovery_attempts', 0)+1)
            ready = True
            log.info('runtime ready; recovering job=%s', job['job_id'])
            run_job(job)
        else:
            ready = True
            log.info('runtime ready CPU INT8 threads=2')
    except Exception:
        log.error('Runtime initialization failed; private details omitted')
        os._exit(1)

@contextlib.asynccontextmanager
async def lifespan(app):
    threading.Thread(target=initialize, daemon=True).start()
    yield
    stop.set()

app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)

@app.middleware('http')
async def authenticate(request, call_next):
    supplied = request.headers.get('authorization', '')
    if not hmac.compare_digest(supplied.encode(), ('Bearer '+TOKEN).encode()):
        return JSONResponse({'error': 'unauthorized'}, status_code=401,
                            headers={'WWW-Authenticate': 'Bearer'})
    return await call_next(request)

@app.get('/healthz')
async def health():
    return JSONResponse({'ready': ready, 'busy': slot.locked()}, status_code=200 if ready else 503)

@app.get('/v1/metadata')
async def metadata():
    return snapshot()

@app.post('/v1/diarizations')
async def submit(request: Request):
    global active
    if not ready:
        return JSONResponse({'error': 'runtime_not_ready'}, status_code=503, headers={'Retry-After': '5'})
    # Coordinator persists this identity before sending audio. Repeating an
    # upload after a lost 202 response reattaches to the durable job.
    requested_id = request.headers.get('x-speektome-job-id')
    if requested_id is not None:
        job_dir(requested_id)  # Validate before touching paths.
        with lock:
            if active and active['job_id'] == requested_id:
                return JSONResponse({'job_id': requested_id}, status_code=202)
        if (DATA/requested_id/'job.json').exists():
            return JSONResponse({'job_id': requested_id}, status_code=202)
    if not slot.acquire(blocking=False):
        return JSONResponse({'error': 'worker_busy', 'active_job': snapshot()['active_job'], 'queue_on_coordinator': True},
                            status_code=429, headers={'Retry-After': '15'})
    job_id = requested_id or secrets.token_hex(16)
    path = DATA/job_id
    handed_off = False
    job = dict(job_id=job_id, status='uploading', phase='uploading', created_at=time.time(),
               uploaded_bytes=0, completed_windows=0, recovery_attempts=0)
    try:
        cleanup_old()
        content_length = request.headers.get('content-length')
        size = int(content_length) if content_length else None
        if size is not None and (size <= 0 or size > MAX_UPLOAD):
            raise HTTPException(413, 'Upload size exceeds limit or is empty')
        if size and shutil.disk_usage(DATA).free < size + RESERVE:
            raise HTTPException(507, 'Insufficient disk space')
        path.mkdir(mode=0o700)
        with lock:
            active = job
        update(job, persist=True, expected_upload_bytes=size)
        # Async network chunks are appended in the thread pool; request body is never accumulated in RAM.
        out = (path/'input.part').open('wb')
        written = 0
        last_disk_check = 0
        try:
            async for data in request.stream():
                written += len(data)
                if written > MAX_UPLOAD:
                    raise HTTPException(413, 'Upload size exceeds limit')
                if written-last_disk_check > 8*1024**2:
                    last_disk_check = written
                    if shutil.disk_usage(DATA).free < RESERVE:
                        raise HTTPException(507, 'Insufficient disk space')
                await asyncio.to_thread(out.write, data)
                update(job, uploaded_bytes=written,
                       upload_progress_percent=round(written/size*100, 1) if size else None)
            if written == 0 or (size is not None and written != size):
                raise HTTPException(400, 'Incomplete or empty upload')
            await asyncio.to_thread(out.flush)
            await asyncio.to_thread(os.fsync, out.fileno())
        finally:
            out.close()
        (path/'input.part').replace(path/'input.opus')
        update(job, persist=True, status='uploaded', phase='uploaded', uploaded_bytes=written)
        threading.Thread(target=run_job, args=(job,), daemon=True).start()
        handed_off = True
        return JSONResponse({'job_id': job_id, 'status_url': f'/v1/diarizations/{job_id}',
                             'result_url': f'/v1/diarizations/{job_id}/result'}, status_code=202)
    except BaseException:
        if path.exists():
            update(job, persist=True, status='failed', phase='failed', error='Upload failed or interrupted', finished_at=time.time())
            for name in ('input.part', 'input.opus'):
                (path/name).unlink(missing_ok=True)
        raise
    finally:
        if not handed_off:
            with lock:
                active = None
            slot.release()

@app.get('/v1/diarizations/{job_id}')
async def status(job_id: str):
    path = job_dir(job_id)
    with lock:
        if active and active['job_id'] == job_id:
            return public_job(active)
    try:
        return public_job(json.loads((path/'job.json').read_text()))
    except FileNotFoundError:
        raise HTTPException(404, 'Job not found')

@app.get('/v1/diarizations/{job_id}/result')
async def result(job_id: str, format: str = 'json'):
    job = await status(job_id)
    if job['status'] != 'completed':
        return JSONResponse({'status': job['status'], 'error': job.get('error')},
                            status_code=422 if job['status'] == 'failed' else 409,
                            headers={'Retry-After': '15'})
    if format not in ('json', 'rttm'):
        raise HTTPException(400, 'format must be json or rttm')
    return FileResponse(job_dir(job_id)/f'result.{format}',
                        media_type='application/json' if format == 'json' else 'text/plain')

@app.delete('/v1/diarizations/{job_id}')
async def acknowledge(job_id: str):
    with lock:
        if active and active['job_id'] == job_id:
            raise HTTPException(409, 'Job still active')
        path = job_dir(job_id)
        if not path.exists():
            raise HTTPException(404, 'Job not found')
        shutil.rmtree(path)
    return {'deleted': job_id}

if __name__ == '__main__':
    uvicorn.run(app, host=os.environ.get('DIAR_LISTEN', '0.0.0.0'),
                port=int(os.environ.get('DIAR_PORT', '8081')), access_log=False,
                timeout_keep_alive=10, limit_concurrency=32)
