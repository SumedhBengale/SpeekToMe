# Nemotron-3 CPU diarization benchmark, 2026-10-08

## Recommendation

Use the community ONNX INT8 export in its published bounded-window offline mode on this t4g.medium. It processed the six-minute fixture in 24.22 seconds including process startup, model loading, output writing, and shutdown. Peak process RSS was 399.73 MiB. This is a single-file benchmark, not a validated concurrent-service capacity claim or a general accuracy ranking.

## Workload and environment

- Dedicated `i-REDACTED`, us-east-1a, t4g.medium, Graviton2/Neoverse-N1, two ARM vCPUs and 4 GiB RAM; Unlimited credits. No GPU, ASR workload, inference gateway, or fleet registration.
- First 360 seconds of [AMI ES2004a Mix-Headset](https://groups.inf.ed.ac.uk/ami/AMICorpusMirror/amicorpus/ES2004a/audio/), resampled to 16 kHz mono PCM WAV by ffmpeg. Four reference speakers. Timings exclude downloading/uploading the audio and Opus decoding; input is already decoded PCM.
- Each inference process ran alone. Native source commit `b809bbb467fb2da2be0042fff5ef1f503828c9cf`; unmodified committed source, its pinned ggml submodule, Release CPU build with `GGML_NATIVE=ON`, CUDA/TTS/microphone/HTTP/gRPC disabled. The existing source tree's untracked files were not included.
- Final ONNX runs use ONNX Runtime **1.23.2**, explicitly CPUExecutionProvider and two intra-op threads. Environment limits OMP/OpenBLAS to two threads. The native runtime's own thread selection is unchanged.
- Resources sampled every 100 ms by `benchmark.py`. CPU percent is process CPU-seconds/wall-seconds; 200% corresponds to the two cores. Peak RSS is sampled, not an exact allocator high-water mark. RAM numbers exclude the OS and filesystem cache.

## Final results

| Variant / configuration | Total wall time | Peak RSS | Process CPU | Fixture DER |
|---|---:|---:|---:|---:|
| Existing BF16 GGUF, `v3-offline` | >647.35 s; stopped without output | 303.97 MiB | 195.3% | unavailable |
| Official Q8 GGUF, `v3-offline`, default segmentation | 55.47 s | 216.24 MiB | 188.6% | 32.93% |
| Q8 GGUF, matched offline window and 0.5 segmentation thresholds | 56.88 s | 216.22 MiB | 188.7% | 12.70% |
| ONNX FP32, published bounded windows | 44.73 s | 750.72 MiB | 181.6% | 12.89% |
| **ONNX INT8, published bounded windows** | **24.22 s** | **399.73 MiB** | **189.2%** | **12.69%** |
| Q8 GGUF, native full attention, 0.5 thresholds | 72.13 s | 345.32 MiB | 189.2% | 34.72% |
| ONNX INT8, experimental one-forward full attention | 37.21 s | 1,835.33 MiB | 188.3% | 31.37% |

INT8 bounded inference alone was 23.33 s; loading/reading 0.62 s. FP32 bounded inference alone was 41.24 s; loading/reading 3.22 s. INT8 full attention inference alone was 36.25 s; loading/reading 0.58 s. These are observed single-run latencies, not p95 estimates.

Full attention increased INT8 total latency by 54% and peak RSS by 4.59x. Both full-attention implementations detected only three speaker identities on this four-speaker excerpt and primarily increased missed speech. Bounded processing performed better on this fixture. This does not prove bounded processing always has lower error.

The BF16 trial was terminated after 647.35 seconds with no completed diarization output. `perf` sampled 97.60% of cycles inside `ggml_vec_dot_bf16`; it was computing, not waiting for audio. This diagnoses the tested native BF16 path on this CPU, not all possible PyTorch/BF16 implementations.

## Window geometry and accuracy scoring

Published ONNX offline geometry: center chunk 340 encoder frames (27.2 s), right context 40 frames (3.2 s), speaker cache 264 frames (21.12 s), FIFO 40, update period 300. Attention spans the cached context plus the audio window, not just 30.4 seconds. Native matched-window run used these sizes and left context zero; preprocessing/runtime implementations still differ, so it is not a bit-exact parity test.

Native `v3-offline` preset: chunk 264, cache 264, FIFO zero, update period 188, left/right context one frame each. Its baseline also uses different default segmentation settings. The improvement in the matched-window run combines geometry and postprocessing changes; this experiment does not isolate either change.

Matched Q8 runs set onset/offset to 0.5, padding and minimum on/off duration to zero, matching ONNX's threshold-only output more closely.

Reference: [forced-alignment AMI ES2004a RTTM](https://github.com/nttcslab-sp/diar-forced-alignment/blob/9527b7c64846fb38316a610f32e9d3466bd6d8b7/AMI/test/ES2004a.rttm). Local `score-rttm.py` computes DER on a 10 ms grid, zero collar, overlap included, clipped to [0,360) with optimal global speaker mapping. Denominator is 146.65 reference speaker-seconds, including overlapping speakers separately. This is a focused excerpt check, not the published 16-meeting AMI score or an independent verification of the conversion author's whole-dataset claims.

## Full-attention support

Native `--offline` is explicitly full attention, distinct from `--preset v3-offline`. The GGUF metadata sets a 5,000-frame position limit, roughly 400 seconds; this six-minute fixture fits, multi-hour meetings do not.

The [community ONNX export](https://huggingface.co/wannaphong/Nemotron-3-Diarization-ONNX) only advertises bounded offline processing. Its exported graph has dynamic chunk/context axes. The experimental test adapter sets the entire recording as one chunk, zero right context and an initially empty cache, yielding a single full-attention forward. It changes neither model weights nor runtime source. This is not a publisher-supported mode and has no independently established full-attention parity. The default bounded wrapper remains the recommended path.

All runs had a 3,200 MiB process-RSS safety cutoff and 20-minute timeout. Neither full-attention run hit these cutoffs. BF16 was stopped earlier after profiling.

## Runtime shutdown issue

Initial ONNX Runtime 1.30.0 runs completed inference but lingered at process exit. Inspection showed its Microsoft telemetry database and uploader thread; even disabling telemetry events did not eliminate this shutdown wait in the tested environment. Version 1.23.2 completed all final tests and exited normally. The graphs use ONNX IR 10, compatible with this pinned runtime.

Initial `onnx-*-bounded` and `onnx-int8-full` artifacts retain those diagnostics; their process-wall timings include or were interrupted during shutdown and must not be substituted for final `*-clean` timings. Model output was already written. No `os._exit` workaround or runtime source edits were used.

## Models and provenance

- [NVIDIA Q8 GGUF](https://huggingface.co/nvidia/Nemotron-3-Diarization), revision `f667ed73aee57d40cc39428eb768b4fd87a0a29e`.
- Existing BF16 GGUF copied from `/opt/speektome-worker/models/diar.gguf` on the Virginia inference worker. This is a converted GGUF, not a test of NVIDIA's original `.nemo`/PyTorch execution path.
- [Community ONNX FP32/INT8](https://huggingface.co/wannaphong/Nemotron-3-Diarization-ONNX), repository revision observed during download `fd1c2b082a83ea1ae82779ca30d165bd383c0eb6`; independent conversion, not endorsed by NVIDIA. Complete frontend, NumPy speaker cache and original `diarize.py` retained.
- Downloaded files have SHA256s in `results/sha256sums.txt`; final package versions in `results/packages-final.txt`. Audio and large models remain on the instance, not in this checkout.

## Run and inspect

SSH/live progress:

```bash
ssh -i ~/.ssh/YOUR_SSH_KEY \
  -o 'ProxyCommand=ssh -i ~/.ssh/YOUR_SSH_KEY -W %h:%p ec2-user@BASTION_HOST_IP' \
  ubuntu@DIARIZATION_PRIVATE_IP \
  'tail -n 40 -F /opt/speektome-diarization/results/benchmark.log'
```

Recommended CPU inference after connecting:

```bash
cd /opt/speektome-diarization
venv/bin/python models/diarize.py models/meeting-360.wav \
  --onnx-dir models --model model.int8.onnx --threads 2 \
  --out results/manual-meeting.rttm
```

Reproduce monitored bounded test:

```bash
cd /opt/speektome-diarization
venv/bin/python -u benchmark.py repeat-int8 \
  venv/bin/python -u onnx-test.py repeat-int8 model.int8.onnx bounded \
  | tee -a results/benchmark.log
```

The instance remains running with models and runtimes available. There are no active test processes. The production ASR workers and fleet configuration were not changed. Persistent API/job service integration and multi-hour/concurrent testing remain separate work.
