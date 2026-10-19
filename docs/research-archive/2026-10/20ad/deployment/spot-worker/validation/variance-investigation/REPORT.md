# Fractional L4 latency variance investigation

Tests conducted 2026-10-08 UTC / 2026-10-09 India time on Virginia worker **i-REDACTED**, an EC2 g6f.large. This is an investigation of the deployed pinned runtime, not a change to NeMo's implementation. No diarization workload ran during these tests.

## Findings supported by controlled measurements

There is no single reliable 25-stream hardware ceiling. Identical 26-stream short-clip tests on the same machine returned approximately **929/948 ms p95 when cold and 308 ms when warm**, with CUDA graphs enabled and a batch cap of 32. Workload history and cold initialization therefore materially affect results. The earlier 25-to-26 comparison was insufficient to establish a hard capacity boundary.

The workload also matters. With different offsets through a longer speech recording, a warm 26-stream batch-32 run returned **482 ms p95**, rather than the approximately 308 ms measured with repeated short clips. Changing speech changes the conditional RNNT decoder's token work and batch compatibility. This experiment changes both speech content and cross-stream alignment; it does not isolate their individual contributions or measure accuracy.

Smaller batches improve startup behavior and reduce memory in these runs, but did **not** demonstrate a steady-state throughput gain at 26 streams. The cold varied-speech run with batch 8 reached approximately 485 ms p95 after 15 seconds; batch 32 reached approximately 473 ms after 30 seconds. Their all-chunk cold p95 values were 1198 and 1536 ms respectively. Keep the full cold-start results visible; do not present the post-startup percentile as an unconditional capacity guarantee.

CUDA graphs should remain enabled for this deployed workload. The tested cold 26-stream graphs-off/batch-32 run returned **1373 ms p95**, versus 929/948 ms with graphs on. After 30 seconds, graphs-off p95 remained approximately 888 ms. This is one graphs-off test, not an exhaustive experiment across configurations.

## Why the historical figures are not directly comparable

The original `/mnt/sumedh/AI/run_interference_benchmark.py` has three relevant differences, verified in its source:

- Its reported `server_p95_ms` is **the median of sampled rolling p95 metrics**, rather than p95 across all raw chunk observations (`med_srv_p95 = np.median(srv_p95_vals)`). These are different statistics; a brief bad interval can disappear from the median of window summaries.
- It sends only **35 chunks / 19.6 seconds per stream**, whereas the varied-speech runs here last 134.4–168 seconds per stream. It counts a stream successful if it has any recorded sends; the current harness requires text, commit and all marks consumed.
- Its client chunk mapping divides `audio_processed` by **0.57** even though its configured cadence/chunk duration is **0.560**. It also considers only transcript delta events. The current harness matches all pending exact cumulative chunk endpoints to progress responses instead.

The preceding ASR-only sweep excluded ten initial chunks; this investigation excludes none from the primary statistic. Warm/cold process history, window summaries versus raw percentiles, audio content, chunk matching and test duration all have to match before interpreting two figures as a performance regression. Historical 553 ms server p95 and 3791 ms client p95 are not a single interchangeable latency measurement.

## Mechanism: where the time goes

Gateway telemetry in the diagnostic run reported approximately **2.14 ms average / 3.24 ms p95** for a full Opus message decoded/coalesced and sent upstream. Backend PCM writes averaged 0.11 ms and client output writes 0.09 ms. The dominant hundreds-of-milliseconds delay is downstream of the gateway. The gateway sends one 17,920-byte PCM16 message per 560 ms of mono 16 kHz audio, rather than one native request per small Opus packet.

The deployed source explains why GPU utilization is insufficient to assess headroom:

1. **Backend graph execution is serialized.** `Session::run_impl` holds the shared backend compute mutex through graph cache handling, allocation, submission, output readback and synchronization. Multiple active streams do not imply independently executing GPU graphs. Microbatching improves efficiency by combining exact-compatible work into wider submissions.
2. **RNNT decode has conditional stages.** Predictor batches split by recurrent state bank; joint batches split by remaining frames and optional bias. A batch limit of 32 does not mean every stage runs a batch of 32. Independent speech causes streams to diverge in decode work.
3. **Graph shapes are cached in bounded working sets.** The encoder cache cap is `max(16,min(32,max_batch_size))`; the shared decoder session cap is 64. Its input names/dimensions distinguish predictor bank, joint time shape and actual batch width. Cache misses rebuild graphs, and scheduler pool generation changes invalidate old placements. A diagnostic 25-stream run logged 887 graph cache-entry allocations in the captured journal, mostly decoder entries, with 106–159 builds per full ten-second interval during the run. The cache remained bounded; continuing entries at capacity represent turnover, not proof of an unbounded memory leak.
4. **Synchronization/readback costs CPU time.** The CPU sample profile has at least 48.7% of listed self samples in libcuda, plus kernel NVIDIA driver activity; prominent stacks pass through `cuMemcpyDtoHAsync_v2`/`cudaMemcpyAsync` from encoder output readback. Encoder and decoder outputs are packed into ordinary host vectors. NVIDIA documents that pageable-host-memory “Async” transfers may synchronize. This profile identifies host driver/readback work, but cannot separate actual GPU execution from virtual-GPU scheduling delay.

A useful distinction is **serialized pipeline occupancy versus sampled GPU utilization**. Restricting the diagnostic journal to the benchmark epoch, timed sessions sum to 2.59 s frontend, 36.70 s encoder and 18.82 s decoder: **58.11 seconds over 68.88 seconds elapsed**. These calls share the backend mutex, so their measured portions alone occupied it for approximately **84%** of that diagnostic interval, before uncounted cache construction and logging overhead. This is an instrumented run, not a production utilization estimate, but it demonstrates why a low GPU counter can coexist with little headroom on the serialized execution path.

Native timing logs require care: the `compute` field measures **asynchronous graph submission**; the following `out` field includes GPU completion/synchronization. Graph construction and waiting to acquire the shared mutex occur outside the displayed session total. Neither this timer nor `backend_progress_wait` alone is isolated CUDA kernel duration.

Supported logging revealed individual allocation spikes (one decoder example: alloc 29.78 ms versus submit 0.15 ms). Instrumentation itself can perturb scheduling, so passing/failing capacity results above were collected **without** native timing/memory logging. The uninstrumented baseline reproduced the slowdown.

External 30-second CUDA API samples found 11 captures/4784 launches on the small-batch runtime and 49 captures/5662 launches on a wide-batch runtime. They were not synchronized to identical phase/concurrency and are supporting observations, not an isolated causal comparison. The initial diagnostic profile recorded 152 captures, instantiations and updates with 940 launches over a partial run.

## What has been ruled out, and what remains uncertain

- **The coordinator is not needed to reproduce the slowdown:** these tests use the worker gateway over loopback. This does not independently verify coordinator latency or network performance.
- **Late benchmark sending is not the primary cause:** p95 sender lateness was approximately 1–2 ms on the relevant runs, versus hundreds of milliseconds of response delay. Deterministic ±20 ms input jitter is applied before this lateness measurement.
- **No VRAM exhaustion occurred:** the tested profiles peaked around 1.5–1.8 GiB in nvidia-smi, and every recorded stream completed with a transcript and commit acknowledgment. Memory was not fully consumed and there were no request/OOM failures in these runs.
- **CPU steal is small in the reported samples:** typically below 0.2%; measured total CPU averages around 50–61% on most runs do not prove whole-machine CPU saturation. The serialized critical path can still constrain throughput while another vCPU is partly idle.
- **The earlier OS-update interference was real but separate:** the preceding deployment report documents APT/MOTD work causing a contaminated 22-stream run and a clean repeat afterward. Automatic servicing was already disabled during this investigation.
- **There is no demonstrated unbounded leak:** current evidence shows bounded cache/allocation retention and shape turnover. These short runs cannot exclude a leak over many hours.
- **AWS host scheduling/other tenants remain unmeasured:** the guest cannot expose the physical GPU's other VMs or configure its host time-slicing scheduler. `nvidia-smi vgpu --query` returned no supported devices; power/throttling counters were unavailable. One live snapshot showed P0 and 2040 MHz SM clock. This is not enough to rule out all host-side variance.

The best-supported explanation is the interaction between cold graph preparation, conditional decoder batch/shape working sets, and a serialized host/GPU synchronization path. These mechanisms explain important measured differences, but their exact shares of total latency have not been isolated with an on-GPU activity trace. A statement that the GPU has “88% spare capacity” from a 12% sampled utilization figure is unjustified.

## Ingress delay, warmup, and proposed next steps

The current **ingress cohort delay is 120 ms**: it aligns incoming streaming messages before frontend/encoder processing, releasing early when its participant target is reached. It is separate from the **5 ms neural-stage queue delay**. Increasing either value is not guaranteed to improve throughput. The runtime is already microbatching; enabling “microbatching” again would not change the architecture.

Boot readiness currently proves the engine can answer a smoke request. It does not prove its multi-stream shape working set is prepared. A future rollout should evaluate a representative multi-stream warmup before admitting production traffic, and retain cold-start measurements in acceptance checks. Warming only one shape is insufficient. Even a warm model has stream startup/lookahead and end-flush latency; prewarming cannot remove all stream-intrinsic delay.

The GGUF is block Q8_0. NVIDIA's pinned documentation recommends a CUDA-only tensor-planar Q8 artifact; this runtime can repack some block encoder weights in place on first use. A planar artifact may reduce preparation and improve supported kernels, but was **not** converted or benchmarked here. It must be a separate model-parity and throughput experiment, not an assumed improvement.

`CUDA_DEVICE_SCHEDULE=2` is present in the production environment but is not read by the pinned NeMo/ggml source and is not a documented CUDA environment switch. Earlier descriptions of it as a confirmed scheduling optimization were too strong. Do not rely on this variable to change CUDA scheduling policy.

## Test method and evidence

- All requests authenticated; streaming ASR only. No application or native runtime code was changed. Gateway admission was temporarily raised from 20 to 40; native state arena stayed 40 and HTTP workers stayed 80. All tested stream counts were admitted, so the 26–30 latency boundary was not an admission or state-slot rejection.
- Explicitly await `session.updated`, then use one global audio epoch for all senders, evenly staggered across a 560 ms interval. Each sender stays on a fixed cadence with seeded ±20 ms jitter, so model lag cannot silently slow the sender's intended cadence.
- Independent Ogg/Opus 560 ms clips decode to mono 16 kHz PCM. The longer fixture is `sample_24_holmes_ch06_240s.wav`, preencoded into 400 560 ms clips; clients start at offsets `client_id*17` through this sequence.
- Capture every sent chunk, match it to the cumulative `audio_processed` frontier, and require nonempty text, final commit acknowledgment, no errors and no outstanding marks for completion. All results below include startup; post-startup statistics are separately labelled.
- **Latency is input-frontier response delay, not exact word-availability or capture-to-user delay.** In the pinned runner `audio_processed` reflects the received audio frontier; right-context/lookahead can leave some received audio buffered. Final nonempty transcript checks establish completion, not word-level timing or WER.
- Resource sampling every ~2 seconds records CPU counters, RAM, GPU utilization/memory, scheduler statistics, PSI, and authenticated gateway metadata. GPU averages are averages of those utilization samples, not SM occupancy or assigned-time-share utilization.
- The diagnostics run and external API probes are tagged separately. Longer runs are 134.4–168 seconds of audio per stream, not multi-hour endurance tests or multiple EC2 hosts.

Raw reports: `sweep-*.json`; chunk timestamps: `trace-*.json`; statistical windows/resources: `summary.json`; reproduction harness: `variance_bench.py`; analyzer: `analyze.py`; profiler evidence: `cpu-diag25a.txt`, `native-diag25a.log.gz`, `api-*.txt`.

## Primary references

- [Pinned NVIDIA batching guide](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/6a3ca369370782acee0dd155e34394ba60b920c4/docs/development/asr-batching.md)
- [Pinned session cache/mutex/async timing implementation](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/6a3ca369370782acee0dd155e34394ba60b920c4/src/runtime/ggml/session.cpp)
- [Pinned decoder batch compatibility and cache capacity](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/6a3ca369370782acee0dd155e34394ba60b920c4/src/asr/model.cpp)
- [CUDA API synchronization semantics](https://docs.nvidia.com/cuda/cuda-runtime-api/api-sync-behavior.html)
- [NVIDIA vGPU scheduling](https://docs.nvidia.com/vgpu/latest/grid-vgpu-user-guide/changing-vgpu-scheduling-policy.html)
- [Guest monitoring limits](https://docs.nvidia.com/vgpu/latest/grid-vgpu-user-guide/performance-monitoring-gpu.html)

## Capacity conclusion and restored state

**26 is the highest tested warm varied-speech count that passed the full-run p95 target in this investigation.** Batch-32 warm tests returned 482 ms at 26, 564 ms at 27, and 581 ms at 28 streams. A warm batch-8 28-stream repeat returned 604 ms, and its 30-stream run returned 1100 ms (815 ms after the first 30 seconds). The 30-stream run completed and its drift was negative; it failed the latency target, not a demonstrated sustained-throughput or growing-backlog limit. The boundary is close enough that it should be treated as workload/host dependent, not a universal 26-stream specification. None of these data establish a multi-hour SLA or a reliable fleet-wide increase.

Production remains at **20 admitted streams**, batch 32, arena 40, graphs enabled, ingress 120 ms and stage queue 5 ms. After restoring the original configuration and removing all four CUDA perf probes, a fresh native 20-stream varied-speech run completed all 20 streams: **p50 192 ms, p95 438 ms, p99 1395 ms**, 95.81% of chunks within 560 ms. After 15 seconds its p95 was 276 ms. Startup/end-flush tails remain; this is not a per-chunk deadline guarantee. Peak RAM was 1033 MiB, peak VRAM 1679 MiB, CPU average 52.2%, sampled GPU-utilization average 24.9%.

The temporary gateway admission/diagnostic/graph environment overrides and CUDA probes were removed. The worker YAML matches its exact saved pre-test configuration. The coordinator health grace was restored from 3600 to **60 seconds**. Its authenticated status confirms desired=2, no pending launch, no last error, and both existing Virginia/Ohio workers ACTIVE/ready with 0 active streams and max=20. No S3 release/config artifact was changed, and no EC2 instance was created or terminated by this investigation.

Verified final hashes:
- Native binary: `1ace0e6bc765e647c16d27d4706f8c98fdc509ea6b044fc514bb48ce49b4e206`
- ASR GGUF: `7ece4ce06d9f5496d5a7e1dade556a5646ce25f84f828f7a8805bb1db3e27792`

## All-run latency results

Primary percentiles include every chunk and final flush. The after-30-second column is a separate slice and must not replace the primary result. GPU graphs are enabled except `off32a`; `diag25a` enables native diagnostic logging. `real26prep8` is a short cold preparation run. “Warm” describes a reused native process, not removal of each stream's startup.

| Test | Streams | Batch cap | Audio/stream, s | p50 ms | p95 ms | p99 ms | Chunks <=560 ms | p95 after 30s |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| clean32a | 26 | 32 | 67.2 | 488 | 929 | 1286 | 58.30% | 774 |
| clean32b | 26 | 32 | 67.2 | 555 | 948 | 1268 | 50.90% | 854 |
| clean8a | 26 | 8 | 67.2 | 194 | 939 | 1385 | 90.71% | 304 |
| diag25a | 25 | 32 | 67.2 | 633 | 959 | 1346 | 37.77% | 929 |
| off32a | 26 | 32 | 67.2 | 677 | 1373 | 1592 | 32.31% | 888 |
| production20restore | 20 | 32 | 134.4 | 192 | 438 | 1395 | 95.81% | 285 |
| real26b32 | 26 | 32 | 168.0 | 261 | 1536 | 2410 | 85.41% | 473 |
| real26b8 | 26 | 8 | 168.0 | 256 | 1198 | 2363 | 90.38% | 488 |
| real26prep8 | 26 | 8 | 67.2 | 279 | 1269 | 2024 | 85.06% | 525 |
| real26warmb32 | 26 | 32 | 134.4 | 240 | 482 | 1340 | 96.75% | 455 |
| real27warmb32 | 27 | 32 | 134.4 | 279 | 564 | 1391 | 94.89% | 528 |
| real28warmb32 | 28 | 32 | 134.4 | 299 | 581 | 1388 | 94.46% | 552 |
| real28warmb8 | 28 | 8 | 134.4 | 309 | 604 | 1315 | 93.57% | 587 |
| real30b8 | 30 | 8 | 134.4 | 437 | 1100 | 1753 | 68.75% | 815 |
| warm32b | 26 | 32 | 67.2 | 188 | 308 | 905 | 97.92% | 264 |
| warm8a | 26 | 8 | 67.2 | 185 | 281 | 955 | 98.01% | 254 |

## Sampled resources

CPU is total machine utilization from /proc/stat deltas over the sampled interval; RAM is MemTotal-MemAvailable. GPU is the mean of nvidia-smi samples approximately two seconds apart. These figures include test-client and gateway CPU on the same host.

| Test | CPU avg % | RAM peak MiB | VRAM peak MiB | GPU sample avg % | Sender late p95 ms |
|---|---:|---:|---:|---:|---:|
| clean32a | 52.2 | 1183 | 1803 | 14.2 | 1.2 |
| clean32b | 53.8 | 1172 | 1803 | 16.9 | 1.3 |
| clean8a | 51.2 | 1044 | 1547 | 34.1 | 1.1 |
| diag25a | 54.6 | 1202 | 1801 | 15.6 | 1.6 |
| off32a | 52.1 | 981 | 1651 | 28.6 | 1.1 |
| production20restore | 52.2 | 1033 | 1679 | 24.9 | 0.8 |
| real26b32 | 59.2 | 1164 | 1765 | 8.7 | 1.3 |
| real26b8 | 59.3 | 1085 | 1563 | 23.5 | 1.3 |
| real26prep8 | 58.6 | 1038 | 1551 | 13.9 | 1.3 |
| real26warmb32 | 59.1 | 1153 | 1765 | 11.5 | 1.3 |
| real27warmb32 | 60.0 | 1164 | 1765 | 18.5 | 1.3 |
| real28warmb32 | 60.2 | 1158 | 1765 | 19.2 | 1.4 |
| real28warmb8 | 60.5 | 1059 | 1553 | 14.0 | 1.4 |
| real30b8 | 61.2 | 1096 | 1563 | 12.4 | 1.7 |
| warm32b | 49.8 | 1191 | 1801 | 29.3 | 1.2 |
| warm8a | 49.9 | 1039 | 1557 | 8.7 | 1.0 |

![Latency timeline](/home/ssben/.codex/worktrees/20ad/SpeekToMe/deployment/spot-worker/validation/variance-investigation/latency-variance.png)

## Subsequent controlled repeat — 2026-10-09 India time

Three identical 25-stream, 119.84-second trials after one restart returned p95 **919, 787 and 1044 ms**. Cold initialization alone does not explain variance; a warmed process did not reliably pass the 560 ms target on this controlled identical-audio workload. See ../identical-25-three-trials/REPORT.md. This qualifies the earlier passing 26-stream point; it is not a universal capacity specification.
