# CPU streaming diarization, Virginia — 2026-10-08

The deployed BF16 Nemotron-3 model executes successfully on CPU, but the v3-streaming preset did not sustain one real-time stream on the Virginia g6f.large's two x86 vCPUs. This differs substantially from the prior offline, large-window result.

## Workload and measurement

The deployed runtime provides a standalone streaming diarization C ABI. A Python ctypes harness used that ABI directly, sharing one model across independent stream handles, with no ASR model loaded in the test process. Model config gpu=-1 and CUDA_VISIBLE_DEVICES empty enforced CPU only. This does not exercise HTTP/WebSocket network overhead or a production gateway route. Test process PIDs never appeared in sampled NVIDIA compute-process lists. The existing ASR service was unchanged; gateway metadata during the concurrency run showed zero foreground streams and meeting jobs.

Each stream received120 complete560ms16kHz mono Ogg/Opus clips (67.2seconds of repeated single-speaker speech), with stream start offsets spread across560ms. Opus was decoded on receipt using libopus; pre-skip and final granule trimmed each message to8960 float samples. Independent producers maintained scheduled real-time cadence, storing any unprocessed clips in queues; processing did not slow send cadence. First10clips excluded from reported chunk-output latency.

Preset v3-streaming, not the existing meeting offline geometry: chunk13 coarse80ms frames=1.04s, right-context1=80ms, left-context0, speaker cache264, FIFO80, cache refresh40. Full attention was never invoked. Chunk-output latency measures scheduled arrival to native frame-count coverage of that chunk's end: provisional speaker probabilities, not permanently settled speaker identity. This includes algorithmic buffering and inference queueing. Push timing includes blocking native work/model scheduling; most buffering-only pushes are cheap. Advancing pushes measure calls where frame count increased. Segmentation is obtained after finishing each stream.

## Single-stream results

| Metric | Runtime defaults | OpenMP limited to2threads |
|---|---:|---:|
| Completed streams | 1/1 | 1/1 |
| Audio length | 67.2s | 67.2s |
| Wall time | 127.48s | 125.62s |
| Chunk-output p50 | 23.82s | 23.05s |
| Chunk-output p95 | 57.33s | 55.59s |
| Chunk-output p99 | 60.84s | 58.98s |
| Advancing native call p50 | 2.225s | 2.193s |
| Advancing native call p95 | 2.405s | 2.365s |
| Opus decode p95 | 1.89ms | 2.37ms |
| CPU average, relative to2vCPUs | 92.54% | 92.95% |
| Peak process RSS | 305.64MiB | 306.78MiB |
| Maximum sampled queue, chunks | 46 | 45 |

Runtime default standalone diarization backend specifies4CPUthreads. Retest set OMP_THREAD_LIMIT=2 and OMP_NUM_THREADS=2 (library links libgomp); it did not materially improve throughput. No runtime source, models, deployment config or S3 objects changed.

## Two concurrent streams

With the same two-thread OpenMP limit, both streams completed successfully. Each received 67.2 seconds of audio; total wall time was 240.75 seconds. Combined chunk-output p50/p95/p99: 70.17/163.72/172.38 seconds. Advancing native call p50/p95: 4.408/4.693 seconds, including shared-model scheduling. Opus decoding p95: 3.37 ms. CPU averaged 96.82% of two-vCPU capacity, peak process RSS was 310.94 MiB, maximum sampled queue was 67 chunks per stream. No test PID appeared in GPU compute process lists.

## Interpretation

RAM is small in these short tests; CPU is the limiting resource. Around2.2seconds per native advance cannot sustain1.04seconds of new audio indefinitely. Measured passing real-time concurrency on this worker/preset is zero. Larger bounded windows can amortize work, as the offline test demonstrated, but introduce much longer output buffering; their streaming capacity was not measured here.

These results cannot establish t4g.small capacity: it uses Graviton2 ARM cores rather than this x86 CPU/SMT topology. It also has a20%baseline pervCPU; sustained higherCPU usage consumes credits and can incur Unlimited charges. An ARM binary and actual Graviton benchmark are required. See https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/burstable-credits-baseline-concepts.html.

This is not a speaker accuracy/DER test, multi-hour memory test, or HTTP integration test.

Evidence and reusable harness: validation/optimized-rollout/coldboot-560ms/cpu-stream1.json, cpu-stream1-omp2.json, cpu-diar-stream.py. Two-stream evidence: cpu-stream2-omp2.json. All three runs completed without errors.
