"""Compare GPU fp32 output with the production CPU int8 graph on the same fixture.

    python parity.py cpu MODELS_DIR fixtures/m017.opus results/cpu-int8-m017.json   # local CPU reference
    python parity.py compare results/cpu-int8-m017.json results/sweep-T4.jsonl m017.opus

Agreement is frame-level (10 ms, overlap scored, no collar) DER of one output against the other
after the best one-to-one speaker mapping -- the README's DER(int8, fp32) check.
"""
import json
import subprocess
import sys
import time
from pathlib import Path

import numpy as np

FRAME = 0.01


def to_frames(segments, frames):
    speakers = sorted({s['Speaker'] for s in segments})
    grid = np.zeros((frames, max(len(speakers), 1)), dtype=bool)
    for s in segments:
        grid[int(round(s['Start']/FRAME)):int(round(s['End']/FRAME)), speakers.index(s['Speaker'])] = True
    return grid


def der(reference, hypothesis):
    from scipy.optimize import linear_sum_assignment
    frames = int(max(s['End'] for s in reference+hypothesis)/FRAME)+1
    ref, hyp = to_frames(reference, frames), to_frames(hypothesis, frames)
    overlap = ref.T.astype(np.int64) @ hyp.astype(np.int64)
    rows, cols = linear_sum_assignment(-overlap)
    mapped = np.zeros_like(ref)
    for r, c in zip(rows, cols):
        mapped[:, r] = hyp[:, c]
    ref_n, hyp_n = ref.sum(1), hyp.sum(1)
    correct = (ref & mapped).sum(1)
    miss = np.maximum(ref_n-hyp_n, 0).sum()
    false_alarm = np.maximum(hyp_n-ref_n, 0).sum()
    confusion = (np.minimum(ref_n, hyp_n)-correct).sum()
    total = ref_n.sum()
    return dict(der=(miss+false_alarm+confusion)/total, miss=miss/total, fa=false_alarm/total, conf=confusion/total)


def cpu(models, fixture, out, graph='model.int8.onnx'):
    sys.path.insert(0, models)
    from diarize import OnnxDiarizer, extract_speaker_dict
    raw = subprocess.run(['ffmpeg', '-nostdin', '-v', 'error', '-i', fixture, '-ar', '16000', '-ac', '1', '-f', 'f32le',
                          'pipe:1'], check=True, capture_output=True).stdout
    audio = np.frombuffer(raw, dtype=np.float32)
    started = time.time()
    probs = OnnxDiarizer(onnx_dir=Path(models), threads=2, model_filename=graph).diarize(audio, 16000)
    Path(out).write_text(json.dumps(dict(seconds=time.time()-started, segment_list=extract_speaker_dict(probs))))


def compare(reference, rows, fixture):
    ref = json.loads(Path(reference).read_text())['segment_list']
    for line in Path(rows).read_text().splitlines():
        row = json.loads(line)
        if row['fixture'] == fixture:
            scores = der(ref, row['segment_list'])
            print(row['requested_gpu'], fixture, ' '.join(f'{k}={v*100:.3f}%' for k, v in scores.items()))
            return


if __name__ == '__main__':
    {'cpu': cpu, 'compare': compare}[sys.argv[1]](*sys.argv[2:])
