"""Official Nemotron-3-Diarization GGUF on NVIDIA NeMo-Speech.cpp, single-use L4 containers.

Four variants, all one fresh container per request:
  NemoCLI           one-shot `nemo-speech diarize` per job with raw 0.5 thresholds (production path)
  NemoServe         resident `nemo-speech serve`, started in @enter (timing baseline for snapshots)
  NemoServeMemSnap  + enable_memory_snapshot (CPU state only; the server starts after restore)
  NemoServeGpuSnap  + experimental GPU snapshot (server and CUDA state captured with the model loaded)

The HTTP server always applies NeMo's default segment postprocessing, which scores much worse on
AMI than raw thresholds (see README), so server variants are for cold-start timing only.
GPU snapshots need a deployed app:

    modal deploy bench_nemo.py
    modal run bench_nemo.py::drive --variant NemoCLI --jobs 3
    modal run bench_nemo.py::drive --variant NemoServeGpuSnap --prime 1 --jobs 3
"""
import json
import time
from pathlib import Path

import modal

IMPORTED_AT = time.time()  # inside a container: when Python started

BUILD = 'nightly'  # 0.2.0+nightly.caa3f47; v0.2.0 predates the O(N^2) whole-file fix (NVIDIA/NeMo-Speech.cpp#62)
TARBALL = f'https://github.com/NVIDIA/NeMo-Speech.cpp/releases/download/{BUILD}/nemo-speech-{BUILD}-linux-x86_64-cuda.tar.gz'
TARBALL_SHA256 = 'a248f862da0baa0ffcfadb5a4a4a23d4ee88bf881e8a71373ef3a46d5418b86f'
ROOT = f'/opt/nemo-speech-{BUILD}-linux-x86_64-cuda'
CLI = f'{ROOT}/bin/nemo-speech'
RAW = ['--preset', 'v3-offline', '--onset', '0.5', '--offset', '0.5', '--pad-onset', '0', '--pad-offset', '0',
       '--min-duration-on', '0', '--min-duration-off', '0']
PORT = 18080
# The revision and checksum NeMo-Speech.cpp's model index pins for `nemotron-3-diarization`.
GGUF = ('https://huggingface.co/nvidia/Nemotron-3-Diarization/resolve/f667ed73aee57d40cc39428eb768b4fd87a0a29e/'
        'Nemotron-3-Diarization.q8_0.gguf')
GGUF_SHA256 = '08456d9e22cd9a323c0364d98375f3746d6e68507ebb705cd46438c534c7a3a1'

image = (
    modal.Image.debian_slim(python_version='3.12')
    .apt_install('ffmpeg', 'curl', 'ca-certificates')
    .pip_install('numpy==2.5.3')
    .run_commands(f'curl -fsSL --retry 3 {TARBALL} -o /tmp/ns.tgz',
                  f"echo '{TARBALL_SHA256}  /tmp/ns.tgz' | sha256sum -c -",
                  'tar -xzf /tmp/ns.tgz -C /opt && rm /tmp/ns.tgz',
                  # The CLI needs libcuda even for `pull`, so fetch the same pinned file it would.
                  f'mkdir -p /models && curl -fsSL --retry 3 {GGUF} -o /models/diar.gguf',
                  f"echo '{GGUF_SHA256}  /models/diar.gguf' | sha256sum -c -")
    .env({'NEMO_SPEECH_MODEL_DIR': '/models'})
)
app = modal.App('speektome-nemo-bench', image=image)
volume = modal.Volume.from_name('speektome-diar-bench')
COMMON = dict(gpu='L4', cpu=4, memory=8192, volumes={'/fixtures': volume}, timeout=1800,
              single_use_containers=True, max_containers=4)


def decode_wav(path, out, parts=8):
    """16 kHz mono PCM16 WAV via `parts` concurrent ffmpeg seeks with 1 s pre-roll, spliced at exact samples."""
    import subprocess
    import wave
    from concurrent.futures import ThreadPoolExecutor
    import numpy as np
    seconds = float(subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0',
                                    path], check=True, capture_output=True, text=True).stdout)
    edges = [round(seconds*16000*i/parts) for i in range(parts+1)]

    def piece(i):
        start, count = edges[i], edges[i+1]-edges[i]
        preroll = min(start, 16000)
        limit = ['-t', f'{(preroll+count)/16000+1:.6f}'] if i < parts-1 else []
        raw = subprocess.run(['ffmpeg', '-nostdin', '-v', 'error', '-ss', f'{(start-preroll)/16000:.6f}', *limit,
                              '-i', path, '-ar', '16000', '-ac', '1', '-f', 's16le', 'pipe:1'],
                             check=True, capture_output=True).stdout
        samples = np.frombuffer(raw, dtype=np.int16)[preroll:]
        return samples if i == parts-1 else samples[:count]
    with ThreadPoolExecutor(parts) as pool:
        audio = np.concatenate(list(pool.map(piece, range(parts))))
    with wave.open(out, 'wb') as sink:
        sink.setnchannels(1)
        sink.setsampwidth(2)
        sink.setframerate(16000)
        sink.writeframes(audio.tobytes())
    return len(audio)/16000


def start_server():
    import subprocess
    import urllib.request
    # Without a preset the server uses the model's low-latency streaming geometry (~5x slower on long files).
    Path('/tmp/diar.yaml').write_text('diar:\n  model_path: /models/diar.gguf\n  preset: v3-offline\n')
    process = subprocess.Popen([CLI, 'serve', '--config', '/tmp/diar.yaml', '--device', 'cuda:0', '--port', str(PORT)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(400):
        try:
            urllib.request.urlopen(f'http://127.0.0.1:{PORT}/ready', timeout=1)
            return process
        except OSError:
            time.sleep(0.05)
    raise RuntimeError('nemo-speech serve did not become ready')


def post_wav(path):
    import urllib.request
    boundary = 'speektome-bench-boundary'
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="a.wav"\r\n'
            'Content-Type: audio/wav\r\n\r\n').encode()+Path(path).read_bytes()+f'\r\n--{boundary}--\r\n'.encode()
    request = urllib.request.Request(f'http://127.0.0.1:{PORT}/v1/audio/diarizations', data=body, method='POST',
                                     headers={'Content-Type': f'multipart/form-data; boundary={boundary}'})
    with urllib.request.urlopen(request, timeout=1700) as response:
        return json.loads(response.read())['segments']


def gpu_name():
    import subprocess
    return subprocess.run(['nvidia-smi', '--query-gpu=name', '--format=csv,noheader'], capture_output=True,
                          text=True).stdout.strip()


def job(self, fixture, diarize):
    started = time.time()
    audio_s = decode_wav(f'/fixtures/{fixture}', '/tmp/job.wav')
    decoded = time.time()
    segments = diarize('/tmp/job.wav')
    finished = time.time()
    return dict(fixture=fixture, audio_s=audio_s, gpu=gpu_name(), imported_at=IMPORTED_AT,
                enter_at=self.enter_at, loaded_at=self.loaded_at, snap_restored=getattr(self, 'restored', None),
                started_at=started, decoded_at=decoded, finished_at=finished,
                segments=len(segments), speakers=len({s['speaker'] for s in segments}), segment_list=segments)


@app.cls(**COMMON)
class NemoCLI:
    @modal.enter()
    def load(self):
        self.enter_at = self.loaded_at = time.time()  # the CLI loads the model inside each job

    @modal.method()
    def run(self, fixture: str) -> dict:
        import subprocess

        def diarize(wav):
            subprocess.run([CLI, 'diarize', wav, '--model', '/models/diar.gguf', '--device', 'cuda:0', *RAW,
                            '--format', 'json', '--output', '/tmp/job.json', '--force'], check=True,
                           capture_output=True)
            return json.loads(Path('/tmp/job.json').read_text())['segments']
        return job(self, fixture, diarize)


@app.cls(**COMMON)
class NemoServe:
    @modal.enter()
    def load(self):
        self.enter_at = time.time()
        self.server = start_server()
        self.loaded_at = time.time()

    @modal.method()
    def run(self, fixture: str) -> dict:
        return job(self, fixture, post_wav)


@app.cls(**COMMON, enable_memory_snapshot=True)
class NemoServeMemSnap:
    @modal.enter(snap=True)
    def prepare(self):
        # No GPU exists while a CPU memory snapshot is taken, so only CPU-side state can be captured.
        self.model_bytes = Path('/models/diar.gguf').read_bytes()
        self.prepared_at = time.time()

    @modal.enter(snap=False)
    def load(self):
        # A restored container's snapshot phase ran long ago; the creating container ran it just now.
        self.restored = time.time()-self.prepared_at > 2
        self.enter_at = time.time()
        self.server = start_server()
        self.loaded_at = time.time()

    @modal.method()
    def run(self, fixture: str) -> dict:
        return job(self, fixture, post_wav)


@app.cls(**COMMON, enable_memory_snapshot=True, experimental_options={'enable_gpu_snapshot': True})
class NemoServeGpuSnap:
    @modal.enter(snap=True)
    def load(self):
        # Captured in the snapshot: server process, CUDA context and model weights in GPU memory.
        self.server = start_server()
        self.prepared_at = time.time()

    @modal.enter(snap=False)
    def after_restore(self):
        import urllib.request
        self.restored = time.time()-self.prepared_at > 2
        self.enter_at = time.time()
        urllib.request.urlopen(f'http://127.0.0.1:{PORT}/ready', timeout=30)
        self.loaded_at = time.time()

    @modal.method()
    def run(self, fixture: str) -> dict:
        return job(self, fixture, post_wav)


@app.local_entrypoint()
def drive(variant: str = 'NemoCLI', fixture: str = 'h4.opus', jobs: int = 3, prime: int = 0,
          out: str = 'results/nemo-L4.jsonl'):
    """Call the deployed class one job at a time, so every job is a fresh cold container."""
    Path(out).parent.mkdir(parents=True, exist_ok=True)
    cls = modal.Cls.from_name('speektome-nemo-bench', variant)
    for index in range(prime+jobs):
        submitted = time.time()
        row = cls().run.remote(fixture)
        responded = time.time()
        row.update(variant=variant, job=index-prime, primer=index < prime, submitted_at=submitted,
                   responded_at=responded)
        with open(out, 'a') as sink:
            sink.write(json.dumps(row)+'\n')
        # Restored containers carry the snapshot's clock for IMPORTED_AT; measure from the restore hook instead.
        boot = (row['enter_at'] if row.get('snap_restored') else row['imported_at'])-submitted
        print(f"{variant:16} {'prime' if index < prime else 'job '+str(index-prime):6} "
              f"request->response {responded-submitted:5.1f}s | cold start {row['started_at']-submitted:5.1f}s "
              f"(boot {boot:4.1f}s, enter {row['loaded_at']-row['enter_at']:4.1f}s"
              f"{', restored' if row.get('snap_restored') else ''}) | processing {row['finished_at']-row['started_at']:5.1f}s "
              f"(decode {row['decoded_at']-row['started_at']:4.1f}s, diarize {row['finished_at']-row['decoded_at']:5.1f}s) | "
              f"return {responded-row['finished_at']:4.1f}s | {row['segments']} segs, {row['speakers']} spk")
