# Three identical 25-stream trials — 2026-10-09 India time

Virginia worker i-REDACTED. Gateway admission was changed to **30**. Native runtime and gateway were restarted once, then three consecutive 25-stream tests ran without further restart, artificial warmup or deliberate cooldown. Native PID **46889** and gateway PID **46885** remained unchanged across the sequence. The runner was started immediately after systemctl restart returned; first audio began approximately 12.5 seconds after the logged restart timestamp, after server readiness and all 25 session.updated handshakes. This negotiation delay is separate from chunk response latency.

Every stream in every trial receives the **same first 214 preencoded 560 ms Opus clips**, in the same order: **119.84 seconds of mono 16 kHz audio**. No within-stream looping is used. Ogg end granules and pre-skip were checked for all 214 clips: each encodes exactly 560 ms. Starts are evenly staggered over one 560 ms interval and ±20 ms jitter is deterministically seeded per stream/chunk, identical across trials. Audio encoding was completed before the restart, so ffmpeg did not delay the first test or run during measurement.

All three use the same model, batch cap 32, arena 40, 5 ms stage queue, 120 ms ingress cohort, RNNT right context 6, and CUDA graphs enabled. No native runtime code or model artifact was changed. Tests run through the authenticated worker gateway over loopback, isolating the worker from coordinator/network RTT.

Primary percentiles include **all 5,350 chunks per trial**, including stream startup and final flush; none are excluded. Completion requires text, commit acknowledgment, no errors and no outstanding audio marks. Latency is Opus-send to cumulative audio_processed response, not isolated GPU compute or exact word-availability latency.

## Results

| Trial | Engine state | Completed | p50 ms | p95 ms | p99 ms | Chunks <=560 ms |
|---|---|---:|---:|---:|---:|---:|
| 1 | Freshly restarted | 25/25 | 276 | 919 | 1347 | 70.90% |
| 2 | Same running process | 25/25 | 206 | 787 | 1313 | 89.20% |
| 3 | Same running process | 25/25 | 257 | 1044 | 1689 | 73.85% |

## Resources

CPU is total machine CPU from counter deltas; RAM is MemTotal-MemAvailable. GPU averages/peaks use approximately two-second nvidia-smi samples, not assigned-share occupancy. CPU includes the benchmark sender and gateway.

| Trial | CPU avg % | RAM peak MiB | VRAM peak MiB | GPU sample avg % | GPU sample peak % | Sender late p95 ms |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 52.1 | 1191 | 1799 | 14.8 | 100 | 1.08 |
| 2 | 50.8 | 1183 | 1803 | 31.6 | 100 | 1.12 |
| 3 | 54.3 | 1212 | 1809 | 27.0 | 100 | 1.26 |

## Post-startup slices

These slices do not replace the full-run results above.

| Trial | p95 after 30s | p95 after 60s | p95 after 90s |
|---|---:|---:|---:|
| 1 | 850 ms | 708 ms | 809 ms |
| 2 | 769 ms | 816 ms | 918 ms |
| 3 | 994 ms | 1058 ms | 1197 ms |

## Interpretation

**None of these three trials met p95 <=560 ms.** Trial 2 improved, but trial 3 regressed with the same processes, input bytes, duration and cadence. This directly shows that cold initialization alone is insufficient to explain the variance and that a warm process does not reliably make 25 streams pass this workload. The earlier 26-stream passing point remains a result for that particular workload/run, not a general capacity guarantee.

Input duration is controlled. Sender p95 lateness was only 1.08–1.26 ms. Observed peak active streams was exactly 25 throughout the loaded portion; admission was 30. Final authenticated metadata showed ready=true, active_streams=0, max=30 and reported pending audio=0. All three completed without request errors. VRAM peaks increased only 10 MiB across the sequence, so these data do not demonstrate VRAM exhaustion or an unbounded leak. Cache working-set/scheduling effects and host-side vGPU variation remain possible; this set does not isolate their contributions.

Identical audio across streams also correlates decoder work and endpointing. This is a repeatability test under that controlled workload, not independent production conversations or an accuracy comparison. No diarization was run.

The Virginia gateway is left at **30** as explicitly requested. Ohio and S3 bootstrap/release artifacts were not modified. Native batching remains 32; stream admission and physical neural batch cap are different limits.

## Reproduction and evidence

`benchmark.py` reproduces the harness; `analyze.py` verifies common settings, completion, clip durations and generates `summary.json`/`fixture.json`. Raw reports and chunk traces are stored beside this report. The preencoded cache remains at /tmp/variance-real-opus.json on the worker and local PC.

Cache-file SHA256: `8b7b2e7d6dab37e06fd3fed66bc2fa59792cb0f32443c7c1505d55e00ffcb4c6`

Used 214-clip sequence SHA256: `628924f45a0c0ec3640d724d24b2c76ea9f74edb8394791726664fe5cc4ad47e`

Audio epochs (India time):
- Trial 1: 2026-10-09T00:26:58.456696+05:30
- Trial 2: 2026-10-09T00:29:01.273703+05:30
- Trial 3: 2026-10-09T00:31:03.253747+05:30
