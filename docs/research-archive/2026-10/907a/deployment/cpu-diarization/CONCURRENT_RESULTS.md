# Two simultaneous six-minute CPU diarization jobs

Tested 2026-10-08 on `i-REDACTED`, t4g.medium, us-east-1a: two Graviton2 CPU cores, 4 GiB RAM. ONNX INT8, ONNX Runtime 1.23.2, CPUExecutionProvider, published bounded-window geometry.

Input: two distinct six-minute excerpts from AMI ES2004a (0–360 s and 360–720 s), encoded separately as mono Opus with 16 kHz input, 24 kbps VBR and `voip` application. They are independent jobs using different audio excerpts, not two different original AMI recordings. Each is decoded/resampled explicitly to 16 kHz mono before inference.

Both jobs were launched together as separate processes, each loading its own model with two intra-op threads. No model sharing, batching, thread tuning, or runtime source changes were introduced. A fresh single-job baseline on the first Opus excerpt was run immediately beforehand.

| Measurement | Single job baseline | Two concurrent jobs |
|---|---:|---:|
| Completion time | 24.81 s | Job 1: 52.63 s; job 2: 52.73 s |
| Entire run wall time | 24.81 s | 52.94 s |
| Peak process RSS | 397.70 MiB | 814.14 MiB combined |
| Average process-tree CPU | 183.39% | 194.46% |
| Share of two-core CPU capacity | 91.7% | 97.2% |
| Outcome | Successful | 2/2 successful |
| Inference windows | 14 | 14 per job |

Every job produced finite frame probabilities with shape [1,36001,8] and four speaker identities. First concurrent job produced 126 segments; second produced 207. No memory/time guard triggered; all processes exited normally. This is a capacity/latency test, not a scored accuracy evaluation of these Opus excerpts.

Times include Opus decoding, file reading, model loading, feature extraction, inference, output writing and shutdown; exclude fixture creation and uploading. Concurrent job times were measured from each launch to observed process exit (100 ms polling). The combined run includes its small launcher/monitor overhead. CPU is cumulative observed process-tree CPU-seconds divided by run wall-seconds; 200% corresponds to both cores. RSS is the sum of active process RSS sampled every 100 ms, and can count shared library pages more than once; it is not unique physical memory/PSS.

Inference alone took 50.21 and 50.23 seconds for the concurrent jobs. Most full windows took about 3.6–4.0 seconds, versus ~1.7 seconds for the single job.

## Interpretation

Two short meetings fit comfortably in this instance's RAM. Each finishes about 2.1x later when run concurrently with the tested thread settings. The CPU is already nearly saturated by one job, so this run did not demonstrate a throughput gain from concurrent jobs. One active job with a queue remains the recommended default. Longer-file concurrency is not established by this six-minute test.

## Reproduce

After SSH to the test instance:

```bash
cd /opt/speektome-diarization
BENCH_AUDIO_SECONDS=720 venv/bin/python -u benchmark.py pair-6min \
  venv/bin/python -u pair-test.py | tee -a results/benchmark.log
```

Raw measurements and speaker outputs are in `results/pair-6min*` and `results/single-6min-opus*`. Input Opus files and frame probabilities remain on the instance. Benchmark processes have finished; the instance remains running.
