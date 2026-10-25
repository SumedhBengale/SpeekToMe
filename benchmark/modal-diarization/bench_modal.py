"""Nemotron-3 diarization on Modal serverless GPUs: cold start, per-length speed, parallel jobs.

Runs the same pinned community ONNX export as deployment/diarization-server, but the fp32
chunk-step graph on ONNX Runtime's CUDA provider. Fixtures are mono Ogg/Opus files in the
`speektome-diar-bench` volume (see make_fixtures.py), decoded with ffmpeg exactly like the
CPU gateway.

    modal volume put speektome-diar-bench fixtures/ /
    modal run bench_modal.py --gpus T4,L4 --fixtures m017.opus,h1.opus,h4.opus
    modal run bench_modal.py --gpus L4 --fixtures h1.opus --parallel 8
    modal run bench_modal.py --gpus L4 --fixtures h4.opus --cpu 4 --workers 8   # parallel CPU stages
    modal run bench_modal.py::single_use --fixture h4.opus --jobs 3              # one fresh container per job
"""
import json
import time
from pathlib import Path

import modal

IMPORTED_AT = time.time()  # inside a container: when Python started, after the image was mounted

REV = 'fd1c2b082a83ea1ae82779ca30d165bd383c0eb6'
HF = f'https://huggingface.co/wannaphong/Nemotron-3-Diarization-ONNX/resolve/{REV}'
# Checksums of the int8/frontend/constants/wrapper match deployment/diarization-server/models.sha256.
SHA256 = {
    'model.onnx': None,  # pinned by REV; recorded in results for provenance
    'model.int8.onnx': '17fd04215d655d2feee413a44f96e57d4ebd96cdd9cdbdf8c26da266e08bc835',
    'preprocessor_core.onnx': '28e815a770a8cbe47c351cd36908dd129a0b36ab31461aa8f370a369419703e1',
    'constants.npz': 'fd49737a61e90e1990f4e90a3c0088f2f3ef649860ecdaa4abc76ad2428876df',
    'diarize.py': '170cb823efe5e3953a635d031511dbd984c18639fa7a4edc3e5780fd055dea1e',
}
fetch = ' && '.join(f'curl -fsSL --retry 3 {HF}/{name} -o /models/{name}' for name in SHA256)
check = ' && '.join(f"echo '{digest}  /models/{name}' | sha256sum -c -" for name, digest in SHA256.items() if digest)

image = (
    modal.Image.debian_slim(python_version='3.12')
    .apt_install('ffmpeg', 'curl')
    .pip_install('onnxruntime-gpu[cuda,cudnn]==1.30.0', 'numpy==2.5.3', 'soundfile==0.14.0')
    .run_commands('mkdir -p /models', fetch, check, 'sha256sum /models/model.onnx > /models/fp32.sha256')
    .add_local_file(Path(__file__).with_name('gpu_diarizer.py'), '/root/gpu_diarizer.py')
)
app = modal.App('speektome-diar-bench', image=image)
volume = modal.Volume.from_name('speektome-diar-bench', create_if_missing=True)


@app.cls(gpu='T4', cpu=4, memory=8192, volumes={'/fixtures': volume}, timeout=3600,
         scaledown_window=60, max_containers=10)
class Bench:
    graph: str = modal.parameter(default='model.onnx')
    workers: int = modal.parameter(default=0)  # >0: parallel decode pieces and log-mel windows

    @modal.enter()
    def load(self):
        started = time.time()
        from gpu_diarizer import GpuDiarizer
        self.diarizer = GpuDiarizer(self.graph, mel_workers=self.workers)
        self.load_s = time.time() - started
        self.boot_wall = started
        self.warm = False

    @modal.method()
    def run(self, fixture: str, submitted_at: float) -> dict:
        from gpu_diarizer import decode, describe_gpu
        received = time.time()
        warm, self.warm = self.warm, True
        timings = {}
        t = time.time()
        audio = decode(f'/fixtures/{fixture}', max(self.workers, 1))
        timings['decode_s'] = time.time() - t
        result = self.diarizer.timed_diarize(audio)
        timings.update(result.pop('timings'))
        timings['total_in_container_s'] = time.time() - received
        return dict(fixture=fixture, audio_s=len(audio)/16000, warm=warm, graph=self.graph, workers=self.workers,
                    mel_max_abs_diff=getattr(self.diarizer, 'mel_max_abs_diff', None),
                    load_s=self.load_s, queue_to_start_s=received-submitted_at,
                    container_age_s=received-self.boot_wall, gpu=describe_gpu(self.diarizer),
                    timings=timings, **result)


@app.local_entrypoint()
def main(gpus: str = 'T4', fixtures: str = 'm017.opus', parallel: int = 1, graph: str = 'model.onnx',
         repeat: int = 1, out: str = 'results/raw.jsonl', cpu: float = 4, workers: int = 0):
    Path(out).parent.mkdir(parents=True, exist_ok=True)
    names = fixtures.split(',')
    for gpu in gpus.split(','):
        bench = Bench.with_options(gpu=gpu, cpu=cpu)(graph=graph, workers=workers)
        for name in names:
            for attempt in range(repeat):
                started = time.time()
                if parallel == 1:
                    rows = [bench.run.remote(name, started)]
                else:
                    calls = [bench.run.spawn(name, started) for _ in range(parallel)]
                    rows = [call.get() for call in calls]
                wall = time.time() - started
                for row in rows:
                    row.update(requested_gpu=gpu, cpu=cpu, parallel=parallel, attempt=attempt, batch_wall_s=wall)
                    with open(out, 'a') as sink:
                        sink.write(json.dumps(row)+'\n')
                    print(f"{gpu:6} {name:12} x{parallel} warm={row['warm']!s:5} queue={row['queue_to_start_s']:6.1f}s "
                          f"load={row['load_s']:5.1f}s decode={row['timings']['decode_s']:6.1f}s "
                          f"mel={row['timings']['log_mel_s']:5.1f}s chunks={row['timings']['chunks_s']:6.1f}s "
                          f"total={row['timings']['total_in_container_s']:6.1f}s wall={wall:6.1f}s "
                          f"segs={row['segments']} spk={row['speakers']}")


@app.cls(gpu='L4', cpu=4, memory=8192, volumes={'/fixtures': volume}, timeout=3600, single_use_containers=True,
         max_containers=10)
class SingleUse:
    """The proposed production shape: a fresh container per request, no warm-up, exits after one job."""

    @modal.enter()
    def load(self):
        self.enter_at = time.time()
        from gpu_diarizer import GpuDiarizer
        self.diarizer = GpuDiarizer('model.onnx', mel_workers=8, warmup=False)
        self.loaded_at = time.time()

    @modal.method()
    def run(self, fixture: str) -> dict:
        from gpu_diarizer import decode, describe_gpu
        started = time.time()
        audio = decode(f'/fixtures/{fixture}', 8)
        decoded = time.time()
        result = self.diarizer.timed_diarize(audio)
        timings = result.pop('timings')
        return dict(fixture=fixture, audio_s=len(audio)/16000, gpu=describe_gpu(self.diarizer),
                    imported_at=IMPORTED_AT, enter_at=self.enter_at, loaded_at=self.loaded_at,
                    started_at=started, decoded_at=decoded, finished_at=time.time(), timings=timings, **result)


@app.local_entrypoint()
def single_use(fixture: str = 'h4.opus', jobs: int = 3, out: str = 'results/single-use-L4.jsonl'):
    Path(out).parent.mkdir(parents=True, exist_ok=True)
    for job in range(jobs):
        submitted = time.time()
        row = SingleUse().run.remote(fixture)
        responded = time.time()
        row.update(job=job, submitted_at=submitted, responded_at=responded)
        with open(out, 'a') as sink:
            sink.write(json.dumps(row)+'\n')
        t = row['timings']
        print(f"job {job}: request->response {responded-submitted:5.1f}s | cold start {row['started_at']-submitted:5.1f}s "
              f"(container boot {row['imported_at']-submitted:4.1f}s, imports {row['enter_at']-row['imported_at']:4.1f}s, "
              f"model load {row['loaded_at']-row['enter_at']:4.1f}s, dispatch {row['started_at']-row['loaded_at']:4.1f}s) | "
              f"processing {row['finished_at']-row['started_at']:5.1f}s (decode {row['decoded_at']-row['started_at']:4.1f}s, "
              f"log-mel {t['log_mel_s']:4.1f}s, chunks {t['chunks_s']:5.1f}s) | return {responded-row['finished_at']:4.1f}s | "
              f"{row['segments']} segments, {row['speakers']} speakers")
