# Virginia mixed-workload benchmark, 2026-10-08

Both workloads completed, but neither met an overall live p95 below 560 ms. The 20-stream trial recovered after the meeting-start burst; the 25-stream trial remained above target throughout the sampled full-load interval.

## Environment and workload

- Replacement Virginia g6f.large worker i-REDACTED, us-east-1a. The preceding worker was terminated with AWS reporting user-initiated shutdown. These are not matched before/after results on the old physical worker.
- Restored tested gateway with Opus decoding coalesced into 560 ms PCM sends, pipeline telemetry, CUDA graphs enabled, ingress cohort 120,000 us, queue delay 5,000 us, maximum batch size 32, state arena 40. Native runtime binary unchanged.
- Each live stream sent 560 ms, 16 kHz mono Opus clips at real-time cadence for 320 chunks (179.2 seconds), with arrivals staggered across a 560 ms interval and no injected jitter.
- After 15 seconds, submitted one meeting ID with three concurrent whole-file requests: microphone ASR, system ASR, and diarization. Each used a complete 180-second Ogg/Opus file.
- Diarization used the deployed bounded-window full-file HTTP path, mode=streaming; no full attention. Existing geometry unchanged: chunk340, spkcache264, fifo40, right-context40, left-context1, update-period300. No preset override was introduced.
- Meeting ASR uses the existing windowed streaming backend internally, paced to available headroom. It speeds up after live connections finish, so completion time includes a brief final interval without all live streams present.
- Repeated smoke-test speech, not a diverse conversational recording or an accuracy/DER evaluation. The one-speaker diarization output validates completion, not speaker-label accuracy.
- Live latency measures chunk send to output audio_processed frontier, including buffering, scheduling, endpointing and inference; it is not isolated GPU execution. First 10 chunks per stream excluded from overall percentiles. Rolling gateway p95 is over the latest 256 samples, sampled about every 2.05 seconds; overlapping windows are not independent.
- Harness ran on the worker through authenticated local gateway, not through the coordinator or local PC network. CPU/RAM include harness and monitoring overhead.

## Results

| Metric | 20 live + meeting | 25 live + meeting |
|---|---:|---:|
| Successful live streams | 20/20 | 25/25 |
| Measured chunk delays | 6,200 | 7,750 |
| Live p50 | 183 ms | 982 ms |
| Live p95 | 1,888 ms | 4,271 ms |
| Rolling p95 observations above 560 ms | 18/87 | 86/86 |
| Peak rolling p95 | 5,102 ms | 5,789 ms |
| Above target after first 30 seconds | 7/72 | 71/71 |
| Above target after first 60 seconds | 0/57 | 57/57 |
| Maximum rolling p95 after 60 seconds | 466 ms | 2,689 ms |
| Meeting microphone ASR | 178.19 s | 180.88 s |
| Meeting system ASR | 177.87 s | 181.17 s |
| Meeting diarization | 7.34 s | 7.24 s |
| CPU average / peak | 47.8% / 77.1% | 91.2% / 100% |
| Sampled GPU average / peak | 14.1% / 100% | 26.1% / 100% |
| Peak VRAM used | 1,931 MiB | 2,033 MiB |
| RAM average / peak | 1,034 / 1,071 MiB | 1,330 / 1,533 MiB |

Resource aggregates cover samples with all requested live connections active. No live or meeting request errors; both meeting transcriptions returned 404 words each, diarization returned one segment each. GPU sampling can miss short execution bursts and does not directly measure fractional-GPU entitlement utilization.

At 20 streams, observed above-target episodes were startup (0–6.15 s), meeting-related burst (16.42–36.98 s), and a smaller excursion (51.37–55.48 s), relative to the first full-load sample. Aggregate audio not yet reported processed rose to about 101 seconds across 20 streams during the burst, then cleared. Rolling windows retain delayed responses after diarization completes, so measuring only while the diarization stage is active understates its impact.

At 25 streams, all sampled rolling windows were above target, including before the meeting started. CPU saturation is a likely contributor. Without a matched 25-stream no-meeting baseline on this replacement, the entire regression cannot be attributed solely to meeting work.

## Operational conclusion

20 live streams plus a meeting is functional but does not satisfy a strict 560 ms tail-latency promise. Prefer a worker without a meeting for new live streams. For strict p95, meeting work needs greater isolation or reduced foreground admission; these tests do not establish a safe mixed-workload stream limit. 25 live streams plus a meeting is not a passing operating point.

The temporary 25-stream admission override was removed after testing. Gateway is ready, idle, and restored to max_streams=20. Only Virginia was modified; no S3 or Ohio changes for this benchmark.

Raw reports and derived summaries: validation/optimized-rollout/coldboot-560ms/sweep-mixed20.json and sweep-mixed25.json, and their -summary.json files. Harness: mixed.py in the same directory.
