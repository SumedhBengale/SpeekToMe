# Nemotron-3 diarization on Modal serverless GPUs (2026-10-25)

Can the main server send each meeting to its own on-demand GPU, get diarization back in seconds, and pay nothing while idle? Measured here, before any production change.

**Short answer: yes.** A 4-hour meeting takes **20.5 s on an L4** (vs 965 s on today's CPU worker), costs about **$0.014 per job**, and jobs from different users run side by side instead of in the single-slot FIFO. Results match the CPU worker within the model's own int8/fp32 tolerance.

## What was run

- Same pinned model as production: `wannaphong/Nemotron-3-Diarization-ONNX` @ `fd1c2b0`, but the **fp32** chunk graph (`model.onnx`) on ONNX Runtime 1.30.0 `CUDAExecutionProvider`. The int8 graph is a CPU optimisation; its dynamic-quantised ops don't run on CUDA.
- The wrapper's offline loop and speaker cache are unchanged (`gpu_diarizer.py` only swaps providers and adds timing).
- Fixtures: AMI test-split Mix-Headset meetings (CC BY 4.0), encoded like the desktop client (mono Opus, 24 kb/s). `m017` = ES2004a (17.5 min); `h1`/`h4` = distinct meetings concatenated to 1 h and 4 h (`make_fixtures.py`).
- Container: 4 physical cores (8 vCPU), 8 GiB RAM, preemptible, any region.
- Each fixture was run twice per GPU. The first call is cold; the reported time is the warm in-container time (decode → result).

## Where the time goes

The model is sequential: each 30.4 s chunk depends on the speaker cache from the previous chunk. So one meeting can't be split across GPUs, and a GPU only speeds up each chunk. Once the GPU took over the chunk step, the CPU stages were left as the bottleneck:

| 4-hour meeting, L40S | decode | log-mel | chunks (GPU + numpy cache) | total |
|---|---|---|---|---|
| straight port | 25.0 s | 14.6 s | 4.6 s | 44.3 s |
| `--workers 8` | 3.8 s | 2.5 s | 4.6 s | **11.0 s** |

`--workers 8` makes two changes:
- **Decode:** the Opus file is decoded as 8 concurrent ffmpeg seeks with 1 s of decoder pre-roll, spliced at exact sample offsets. The output is sample-identical to a single decode.
- **Log-mel:** the independent 60 s log-mel windows run on 8 single-thread CPU sessions. The output is bit-identical to the wrapper's serial path.

## Results (optimised, warm, `--workers 8`)

| GPU | 17.5 min | 1 h | 4 h | cold start (single) | $/job, 4 h* | jobs per $30, 4 h |
|---|---|---|---|---|---|---|
| T4 | 3.5 s | 11.6 s | 45.4 s | 63–67 s (×2) | $0.017 | ~1,780 |
| **L4** | 1.9 s | 5.4 s | **20.5 s** | 7–9 s | **$0.014** | **~2,170** |
| A10 | 1.6 s | 4.8 s | 18.5 s | 35 s (×1) | $0.017 | ~1,810 |
| L40S | 1.0 s | 2.9 s | 11.0 s | 5–6 s | $0.022 | ~1,340 |
| H100 | 1.2 s | 3.3 s | 12.0 s | 7 s | $0.044 | ~680 |
| *today: t4g.medium CPU* | *~75 s†* | *~240 s†* | *965 s* | *always on* | *$24.53/month flat* | |

\* Billed = ~3 s container boot + 2.5–4 s model load + processing + a 20 s `scaledown_window` idle tail, at Modal base rates (modal.com/pricing, read 2026-10-25). The fixed ~25 s overhead dominates short meetings, so a job costs about the same at any length: **$0.007–0.014 on an L4**. Run `python costs.py results/*.jsonl` to recompute.
† Scaled from the measured 6-minute (25 s) and 4-hour (965 s) production runs in `deployment/diarization-server/VERIFICATION.md`.

The unoptimised port (`results/sweep-*.jsonl`) took 44–93 s for the 4-hour file. The A100 (72 s) was slower than the L40S because its host CPU is slower. Beyond about an L4, the GPU no longer limits this workload.

**Concurrency.** 8 four-hour jobs were submitted at once on L4. All 8 ran in parallel at 20.7–24.6 s each, so jobs don't slow each other down. That burst needed 8 fresh GPUs, though, and each waited **60–66 s** before starting, versus 7–9 s for a single cold L4. Modal's Starter plan allows 10 GPUs at a time; extra inputs wait in Modal's own queue rather than failing.

**Accuracy.** On ES2004a, frame-level DER (10 ms, no collar, overlap scored, best speaker mapping; `parity.py`):

- GPU fp32 vs CPU fp32: 0.00–0.15% on every GPU.
- GPU fp32 vs production CPU int8: 0.90–0.93%. CPU int8 vs CPU fp32 is itself 0.93%, so this gap is the int8 quantisation the model README already documents (int8 and fp32 are within 0.03 points DER against AMI ground truth), not a GPU effect.
- On the 4-hour file (over 8 real speakers, so past the model's capacity), segment counts differ by about 1% between GPUs. These are the cache tie-flips the model README describes.

## Single-use containers (proposed production shape)

`modal run bench_modal.py::single_use --fixture h4.opus --jobs 3`: one fresh L4 container per request (`single_use_containers=True`, no warm-up pass, `--workers 8`). Jobs ran one after another, so every one was a cold start. Times run from submission on the client to the response arriving back (NTP-synced clocks).

| 4-hour meeting | job 1 | job 2 | job 3 |
|---|---|---|---|
| **Request → response** | **31.2 s** | **28.4 s** | **35.9 s** |
| Cold start (submit → audio processing begins) | 7.9 s | 6.8 s | 14.3 s |
| · container scheduling + boot | 4.1 s | 4.7 s | 11.9 s |
| · model load (`@modal.enter`) | 3.7 s | 2.0 s | 2.2 s |
| **Processing** (decode + diarization) | **22.1 s** | **21.2 s** | **21.2 s** |
| · decode / log-mel / chunks | 6.4 / 3.8 / 11.7 s | 6.0 / 3.8 / 11.1 s | 6.0 / 3.7 / 11.1 s |
| Result return (6,567 segments) | 1.2 s | 0.4 s | 0.4 s |

All three produced identical output. The first chunks pay one-time CUDA setup because there's no warm-up pass, which adds under 1 s compared with the warm runs. Variation comes almost entirely from Modal scheduling the container (4–12 s).

## NVIDIA NeMo-Speech.cpp (official GGUF), single-use L4

`bench_nemo.py`: the official `nvidia/Nemotron-3-Diarization` Q8_0 GGUF (revision `f667ed7`, as pinned by the runtime's model index) on NeMo-Speech.cpp nightly `caa3f47`. v0.2.0 is 7× slower on whole files because of an O(N²) buffer trim, fixed in NVIDIA/NeMo-Speech.cpp#62. The CLI is run with `--preset v3-offline` and raw 0.5 thresholds.

- **Accuracy on ES2004a vs AMI forced-alignment ground truth (collar 0):** NeMo-Speech.cpp 9.64% DER, ONNX fp32 9.43%, production ONNX int8 9.37%. NeMo-Speech.cpp's *default* segment post-processing scores 27.06%, so production must pass the raw-threshold flags. The HTTP server can't set them.
- **4-hour meeting, CLI, 3 jobs:** request → response 39.8 / 42.1 / 50.8 s. Cold start 7.4–18.3 s; model load is ~0.2 s inside the CLI. Processing 32–34 s (decode 6 s + diarize 26–28 s). That's slower than ONNX on L4 (21 s), because its mel frontend is single-threaded on the CPU.
- **Snapshots (resident `nemo-speech serve`, timing only):** no measurable gain. Server start is 0.3–0.5 s with or without snapshots, so there's nothing left for a snapshot to save. Cold start is dominated by Modal finding an L4.
- **L4 capacity:** during this run, waits for a free L4 were 5–303 s (ten jobs over 100 s).
- **Billing:** for the 05:00 UTC hour, the per-app billing report shows 142.1 L4-seconds against 140.6 s of measured container runtime. Including the GPU waits would make it 178.9 s. So billing starts when the container starts, not while waiting for a GPU.

## Recommendation

**L4, 4 cores, `--workers 8`, `scaledown_window` around 20 s, `min_containers=0`, `max_containers=10`.**

- Each 4-hour meeting comes back in about 30 s end to end when a warm container is free, or 30–90 s from cold.
- Each job costs about 1 cent, so the $30 monthly credit covers roughly 2,000–3,500 meetings.
- The CPU box ($24.53/month) can be retired, or kept as a fallback.
- L40S halves the latency for about 1.6× the price per job. It's worth it if the 4-hour wait matters more than cost.
- Avoid T4: its cold starts were the slowest observed (63–67 s), and it's the slowest warm GPU.

## Open questions before production

1. **Privacy and data processing.** Meeting audio would leave your AWS VPC for Modal over TLS. Modal briefly stores large function inputs in its own blob storage. This needs a decision: a Modal DPA, an updated privacy policy, and possibly per-job encryption with a key passed separately.
2. **Burst cold starts.** About 60 s when many GPUs start at once. Options: accept it (it's still far below today's queue), set `buffer_containers=1` at peak hours, or shrink the image. Keeping one L4 warm all the time costs about $25/day, which is too much for this volume.
3. **Fallback.** Keep the CPU worker for when Modal is down or the monthly budget is exhausted?

## Proposed integration (not built)

The Go loop in `server/handlers/meeting_diarization.go` already speaks the gateway's HTTP contract: POST audio, poll status, GET result, DELETE. Keep that contract:

- **Modal side:** a `@modal.asgi_app` exposing `/v1/diarizations` with the same routes and the same bearer key (stored as a Modal Secret, with proxy auth on top).
  - POST does `diarize.spawn(audio)` and returns the `FunctionCall` id as `job_id`.
  - Status and result routes read `FunctionCall.from_id(id).get(timeout=0)`.
  - The GPU function is `@app.cls(gpu="L4", cpu=4, memory=8192, scaledown_window=20, max_containers=10)`, loading the model in `@modal.enter()`.
- **Go side:** point `SPEEKTOME_DIARIZATION_URL` at the endpoint, then replace the single-running unique index and the one-at-a-time `tick` with up to N running rows. The existing durability, retry and acknowledgement logic stays.

## Reproduce

```bash
python make_fixtures.py /path/to/ami fixtures/          # AMI Mix-Headset WAVs → Opus fixtures
modal volume put speektome-diar-bench fixtures/ /
modal run bench_modal.py --gpus L4,L40S --fixtures m017.opus,h1.opus,h4.opus --repeat 2 --workers 8 --out results/opt-w8.jsonl
modal run bench_modal.py --gpus L4 --fixtures h4.opus --parallel 8 --workers 8 --out results/concurrency-L4.jsonl
python costs.py results/opt-w8.jsonl
```

Raw rows, fixtures and logs are gitignored. They are regenerated by the commands above.
