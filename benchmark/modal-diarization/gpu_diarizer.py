"""OnnxDiarizer with a selectable ONNX Runtime provider and per-phase timing.

The pinned wrapper hard-codes CPUExecutionProvider, so sessions are rebuilt here; the
offline chunk loop and speaker cache are the wrapper's own, unchanged.
"""
import hashlib
import json
import os
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import numpy as np
import onnxruntime as ort

MODELS = Path(os.environ.get('DIAR_MODELS', '/models'))
sys.path.insert(0, str(MODELS))
from diarize import HOP_LENGTH, LOG_MEL_WINDOW_FRAMES, N_FFT, PREEMPHASIS  # noqa: E402
from diarize import OnnxDiarizer, extract_speaker_dict  # noqa: E402

CUDA = ('CUDAExecutionProvider', {'cudnn_conv_algo_search': 'HEURISTIC', 'arena_extend_strategy': 'kSameAsRequested'})


class GpuDiarizer(OnnxDiarizer):
    def __init__(self, graph='model.onnx', onnx_dir=MODELS, provider='cuda', threads=None, mel_workers=0,
                 warmup=True):
        if provider == 'cuda':
            ort.preload_dlls()
        ort.disable_telemetry_events()
        options = ort.SessionOptions()
        if threads:
            options.intra_op_num_threads = threads
        providers = [CUDA, 'CPUExecutionProvider'] if provider == 'cuda' else ['CPUExecutionProvider']
        self.preprocessor_core = ort.InferenceSession(str(onnx_dir/'preprocessor_core.onnx'), options, providers=providers)
        self.model = ort.InferenceSession(str(onnx_dir/graph), options, providers=providers)
        self.constants = dict(np.load(onnx_dir/'constants.npz'))
        self.subsampling_factor = int(self.constants['subsampling_factor'])
        self.chunk_length = int(self.constants['chunk_length'])
        self.chunk_right_context = int(self.constants['chunk_right_context'])
        self.fifo_length = int(self.constants['fifo_length'])
        self.speaker_cache_update_period = int(self.constants['speaker_cache_update_period'])
        self.graph = graph
        # mel_workers > 0 computes the independent log-mel windows on single-threaded CPU
        # sessions in parallel instead of one window at a time on `preprocessor_core`.
        self.mel_workers = mel_workers
        if mel_workers:
            single = ort.SessionOptions()
            single.intra_op_num_threads = 1
            self.mel_sessions = [ort.InferenceSession(str(onnx_dir/'preprocessor_core.onnx'), single,
                                                      providers=['CPUExecutionProvider']) for _ in range(mel_workers)]
            self.mel_pool = ThreadPoolExecutor(mel_workers)
        if warmup:
            self.warmup()

    def warmup(self):
        """Run one short recording so CUDA kernels and arenas exist before the first job."""
        rng = np.random.default_rng(0)
        self.diarize(rng.standard_normal(16000*35).astype(np.float32)*0.01)
        if self.mel_workers:
            probe = rng.standard_normal(16000*150+77).astype(np.float32)*0.1
            serial = OnnxDiarizer.log_mel(self, probe)
            self.mel_max_abs_diff = float(np.abs(self.log_mel(probe)-serial).max())

    def log_mel(self, waveform):
        """Same windows, preemphasis and padding as the wrapper's `log_mel`, run concurrently."""
        if not self.mel_workers:
            return OnnxDiarizer.log_mel(self, waveform)
        waveform = np.asarray(waveform, dtype=np.float32)
        num_samples = waveform.shape[0]
        num_frames_total = 1+num_samples//HOP_LENGTH
        mel = np.empty((1, num_frames_total, 128), dtype=np.float32)

        def window(index):
            start_frame = index*LOG_MEL_WINDOW_FRAMES
            end_frame = min(start_frame+LOG_MEL_WINDOW_FRAMES, num_frames_total)
            raw_start = start_frame*HOP_LENGTH-N_FFT//2
            raw_end = (end_frame-1)*HOP_LENGTH-N_FFT//2+N_FFT
            lookback = raw_start-1
            real_start = max(0, lookback)
            real_end = max(real_start, min(num_samples, raw_end))
            real_segment = waveform[real_start:real_end]
            if lookback >= 0:
                preemph_real = real_segment[1:]-PREEMPHASIS*real_segment[:-1]
                content_start = raw_start
            else:
                preemph_real = real_segment.copy()
                preemph_real[1:] -= PREEMPHASIS*real_segment[:-1]
                content_start = real_start
            left_pad, right_pad = content_start-raw_start, raw_end-real_end
            preemphasized = np.pad(preemph_real, (left_pad, right_pad)) if (left_pad or right_pad) else preemph_real
            session = self.mel_sessions[index % self.mel_workers]
            (mel_chunk,) = session.run(None, {'preemphasized': preemphasized[None, :]})
            mel[:, start_frame:end_frame] = mel_chunk[:, :end_frame-start_frame]

        windows = -(-num_frames_total//LOG_MEL_WINDOW_FRAMES)
        # Each worker owns the sessions at its index modulo mel_workers; windows sharing a
        # session run one after another because map hands them out in that order.
        list(self.mel_pool.map(lambda w: [window(i) for i in range(w, windows, self.mel_workers)],
                               range(self.mel_workers)))
        if num_frames_total:
            mel[:, -1, :] = 0.0
        return mel

    def timed_diarize(self, audio):
        timings = {'chunk_calls': 0, 'chunk_run_s': 0.0}
        original_mel, original_chunk = self.log_mel, self.run_chunk

        def log_mel(waveform):
            t = time.time()
            mel = original_mel(waveform)
            timings['log_mel_s'] = time.time()-t
            timings['chunks_started'] = time.time()
            return mel

        def run_chunk(*args):
            t = time.time()
            out = original_chunk(*args)
            timings['chunk_run_s'] += time.time()-t
            timings['chunk_calls'] += 1
            return out

        self.log_mel, self.run_chunk = log_mel, run_chunk
        try:
            t = time.time()
            probs = self.diarize(audio, sample_rate=16000)
            timings['chunks_s'] = time.time()-timings.pop('chunks_started')
            timings['diarize_s'] = time.time()-t
        finally:
            del self.log_mel, self.run_chunk
        segments = extract_speaker_dict(probs)
        active = probs[0] > 0.5
        return dict(timings=timings, segments=len(segments), speakers=len({s['Speaker'] for s in segments}),
                    segments_digest=digest_segments(segments), active_frames=active.sum(axis=0).tolist(),
                    segment_list=segments)


def describe_gpu(diarizer):
    import subprocess
    try:
        name = subprocess.run(['nvidia-smi', '--query-gpu=name,memory.total', '--format=csv,noheader'],
                              capture_output=True, text=True, timeout=10).stdout.strip()
    except (OSError, subprocess.TimeoutExpired):
        name = 'none'
    return dict(name=name, providers=diarizer.model.get_providers(), ort=ort.__version__)


def digest_segments(segments):
    return hashlib.sha256(json.dumps(segments, sort_keys=True).encode()).hexdigest()[:16]


def decode(path, parts=1):
    """ffmpeg to 16 kHz mono float32, optionally as `parts` concurrent seeks spliced at exact sample offsets."""
    def run(*args):
        return subprocess.run(['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error', *args, '-i', str(path),
                               '-ar', '16000', '-ac', '1', '-f', 'f32le', 'pipe:1'], check=True, capture_output=True).stdout
    if parts == 1:
        return np.frombuffer(run(), dtype=np.float32)
    seconds = float(subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0',
                                    str(path)], check=True, capture_output=True, text=True).stdout)
    edges = [round(seconds*16000*i/parts) for i in range(parts+1)]

    def piece(i):
        # Start a second early so the Opus decoder state has converged by the splice point.
        start, count = edges[i], edges[i+1]-edges[i]
        preroll = min(start, 16000)
        limit = ('-t', f'{(preroll+count)/16000+1:.6f}') if i < parts-1 else ()
        raw = np.frombuffer(run('-ss', f'{(start-preroll)/16000:.6f}', *limit), dtype=np.float32)[preroll:]
        return raw if i == parts-1 else raw[:count]
    with ThreadPoolExecutor(parts) as pool:
        return np.concatenate(list(pool.map(piece, range(parts))))
