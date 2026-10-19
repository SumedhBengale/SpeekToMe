# Virginia g6f.large concurrency sweep, 2026-10-08

Worker i-REDACTED, us-east-1b, NVIDIA L4-3Q.
No native runtime source or binary changes. Native binary SHA256 remains
1ace0e6bc765e647c16d27d4706f8c98fdc509ea6b044fc514bb48ce49b4e206.

All successful trials required nonempty transcript output and final commit
acknowledgement for every stream, with no errors. Independent 560ms Ogg/Opus
clips were sent every 560ms via the worker-local authenticated gateway.
Starts were spread over a 560ms interval, rather than synchronized. Audio
was repeated from four speech clips. The first 10 clips per stream were
excluded from latency. These are input-frontier output delays, not isolated
CUDA time or recording-to-desktop latency. No meeting job was running.

| Graphs | Ingress grouping | Streams | Audio/stream | Arrival jitter | p50 ms | p95 ms |
|---|---:|---:|---:|---:|---:|---:|
| Off | 40ms | 20 | 56s | none | 740 | 1352 |
| Off | 40ms | 25 | 100.8s | none | 3196 | 6300 |
| On | 40ms | 20 | 56s | none | 650 | 933 |
| On | 120ms | 20 | 56s | none | 172 | 237 |
| Off | 120ms | 24 | 67.2s | none | 451 | 826 |
| On | 120ms | 24 | 67.2s | none | 182 | 253 |
| On | 120ms | 24 | 179.2s | +/-20ms | 190 | 491 |
| On | 120ms | 25 | 67.2s | none | 196 | 671 |
| On | 120ms | 30 | 67.2s | none | 918 | 1525 |
| On | 120ms | 40 | 67.2s | none | 15008 | 22658 |

All listed trials completed every stream. One graph-toggle attempt overlapped
startup readiness and was excluded, then rerun once ready.

Highest tested point satisfying p95 <560ms: 24, confirmed with 179.2s of audio
and seeded independent +/-20ms schedule jitter. Average late-third delay was
89ms lower than early-third delay; no accumulating output delay observed in
that run. This does not certify hour-long production traffic or ASR accuracy,
nor every possible higher concurrency. Batching effects are not monotonic.

At confirmed 24-stream jitter load:
- CPU average 51.7% across two vCPUs (includes local benchmark and monitoring).
- GPU sampled average 12.8%, sampled peak 86%.
- Peak allocated VRAM 1961 MiB; about 448 MiB free.
- Peak whole-system RAM used 1183 MiB.
- 7440 measured output-delay samples.
- The pending-audio metric includes buffered audio, not solely an internal queue.

40-stream overload: output delay grew by 15.34s from early to late thirds;
peak VRAM 2069 MiB, sampled GPU peak 100%, no runtime restart or OOM.
Low averaged GPU activity is not a reliable estimate of spare stream capacity.
nvidia-smi reports 3072 MiB total and 664 MiB reserved, approximately 2408 MiB
allocatable. No attempt was made to alter that driver/vGPU reservation.

Final Virginia-only configuration:
- CUDA graphs permitted by removing GGML_CUDA_DISABLE_GRAPHS from the actual
  service process environment using cuda-graphs.conf / UnsetEnvironment.
  Setting GGML_CUDA_DISABLE_GRAPHS=0 would still disable graphs in this runtime.
- ingress_cohort_delay_us: 120000 (previously 40000).
- max_batch_size 32, state_arena_slots 40, queue delay 5000us unchanged.
- Admission restored to 20 streams; one meeting-job policy unchanged.
- Temporary benchmark admission override removed.
- Verbose native NEMO_SPEECH_TIMING diagnostic logging removed.
- Gateway stage telemetry and 560ms Opus-to-PCM coalescing retained.
- Ohio and S3 release artifacts were not changed.
- Both fleet workers ACTIVE/ready, main controller desired=2, no extra workers.

Raw evidence and aggregate statistics:
validation/optimized-rollout/coldboot-560ms/concurrency-summary.json
validation/optimized-rollout/coldboot-560ms/sweep-*.json

Rollback: remove /etc/systemd/system/speektome-worker-nemo.service.d/cuda-graphs.conf
and restore server.before-concurrency-sweep.yaml to server.yaml, then daemon-reload
and restart the native service when idle. Native runtime changes are unnecessary.
