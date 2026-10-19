# ASR-only fleet deployment and capacity checks — 2026-10-08

The old Virginia worker i-REDACTED and Ohio worker i-REDACTED were terminated. Their root volumes vol-REDACTED and vol-REDACTED returned InvalidVolume.NotFound afterward. The coordinator launched i-REDACTED (Virginia) and i-REDACTED (Ohio); both became ACTIVE. Cloud-init completed approximately 123 and 128 seconds after boot, respectively. Capacity acquisition time is additional.

All four configured regions share the private bucket speektome-inference-ACCOUNT_ID-us-east-1. Fleet launch pins and manual-launch aliases were updated. The current manifest downloads only the ASR model. Old immutable releases and shared S3 diarization weights remain for rollback or a future dedicated diarization server; active worker manifests do not reference them.

## Final configuration

560 ms Opus-to-PCM coalescing, RNNT right context 6 and pinned runtime left context 8 chunks, batch 32, state arena 40, max queue delay 5 ms, ingress cohort 120 ms, 80 HTTP threads, CUDA graphs enabled (GGML_CUDA_DISABLE_GRAPHS absent), CUDA_DEVICE_SCHEDULE=2. The native NeMo binary was not modified: SHA256 1ace0e6bc765e647c16d27d4706f8c98fdc509ea6b044fc514bb48ce49b4e206. Default live admission remains 20.

Both workers have only asr.gguf in their model directories; a root-level scan of /opt finds no diarization GGUFs. Authenticated diarization routes return 404; unauthenticated metadata returns 401. ASR-only readiness and streaming work. Meeting mic/system ASR routes remain, but meeting diarization is unavailable pending dedicated-server integration. Main application routing was not changed. Go race tests passed, and amd64/arm64 gateway binaries were built; the deployed g6f workers validate amd64, not a new ARM test.

## Method and limitations

Tests ran through the worker's authenticated gateway on loopback, not through the coordinator or the PC network. Each connection sent 560 ms, 16 kHz mono Opus clips at real-time cadence; starts were spread across 560 ms. Most sweeps applied deterministic ±20 ms jitter. A repeated speech fixture measures performance, not transcription or diarization accuracy. Fresh-runtime tests restarted native and gateway processes and waited for ASR readiness; they are not separate EC2 hosts.

Long runs sent 320 chunks (179.2 seconds) per stream; screening runs sent 120 (67.2 seconds). Ten initial chunks per stream are excluded from aggregate chunk percentiles, but startup spikes can continue afterward. Latency is send-to-audio_processed frontier response, including gateway decoding/coalescing, runtime buffering, batching and inference; it is not isolated GPU execution time. Rolling gateway p95 is sampled every approximately two seconds and includes initial samples, so it is a different statistic from the aggregate benchmark p95. All recorded streams completed with nonempty transcript and commit; no request errors were reported.

| Test | Streams | Audio/stream, s | p50 ms | p95 ms | p99 ms | Chunks ≤560 ms | CPU avg % | RAM peak MiB | VRAM peak MiB | GPU avg/peak % |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| asr-only20-final | 20 | 67.2 | 187 | 302 | 550 | 99.18% | 77.0 | 1412 | 1809 | 11.1/100 |
| asr-only20-ohio | 20 | 179.2 | 180 | 248 | 551 | 99.06% | 46.4 | 1009 | 1679 | 5.6/56 |
| asr-only20 | 20 | 179.2 | 165 | 229 | 241 | 99.97% | 42.3 | 917 | 1641 | 10.9/100 |
| asr-only22-clean-os | 22 | 179.2 | 179 | 264 | 669 | 98.14% | 46.3 | 1091 | 1713 | 6.7/100 |
| asr-only22-fresh | 22 | 179.2 | 695 | 1746 | 2317 | 30.09% | 99.7 | 1578 | 1811 | 21.8/100 |
| asr-only23-fresh | 23 | 179.2 | 186 | 563 | 773 | 94.89% | 47.1 | 1072 | 1759 | 16.8/100 |
| asr-only24-fresh | 24 | 179.2 | 191 | 679 | 863 | 91.42% | 48.4 | 1117 | 1801 | 18.5/100 |
| asr-only25-after26 | 25 | 67.2 | 188 | 501 | 713 | 96.40% | 49.6 | 1166 | 1825 | 25.7/100 |
| asr-only25-before26 | 25 | 67.2 | 179 | 244 | 307 | 99.75% | 48.8 | 1110 | 1787 | 12.7/100 |
| asr-only25-fresh | 25 | 179.2 | 198 | 725 | 895 | 89.02% | 49.6 | 1109 | 1787 | 16.1/100 |
| asr-only25-screen | 25 | 67.2 | 183 | 253 | 357 | 99.53% | 50.0 | 959 | 1677 | 10.8/100 |
| asr-only26-controlled | 26 | 67.2 | 635 | 945 | 1040 | 36.47% | 52.6 | 1167 | 1825 | 34.6/100 |
| asr-only26-screen | 26 | 67.2 | 512 | 914 | 1035 | 56.36% | 52.6 | 1190 | 1857 | 10.1/100 |
| asr-only28-screen | 28 | 67.2 | 627 | 902 | 974 | 37.79% | 51.5 | 1167 | 1845 | 28.1/100 |
| asr-only32-screen | 32 | 67.2 | 677 | 960 | 1044 | 28.38% | 53.4 | 1126 | 1835 | 21.2/100 |

## Interpretation

20 streams passed in both regions. A fresh-runtime 22-stream repeat after OS maintenance was disabled passed at p95 264 ms, making 22 the highest fresh-runtime passing count tested in this release. Its p99 was 669 ms and startup rolling p95 exceeded 560 ms for approximately 14 seconds; this is not a per-chunk deadline guarantee or a long-duration production ceiling. Fresh 23 streams narrowly failed (563 ms), 24 failed (679 ms), and 25 failed (725 ms), chiefly during initial bursts. Warmed 25 streams can pass at approximately 244–253 ms p95. Keep admission at 20 until longer representative workloads validate an increase.

The controlled same-process 25 → 26 → 25 sequence returned p95 244 → 945 → 501 ms. All active streams and pending audio returned to zero between phases. Native RSS was approximately 457 MiB after repeated 25-stream work, then 512 MiB after 26 and 513 MiB after returning to 25. VRAM was 1787 → 1825 → 1825 MiB. This demonstrates workload-history variation and bounded retained allocations in this sequence, but does not prove an unbounded leak or identify the specific runtime mechanism behind the 25/26 cliff.

The first fresh 22-stream run (p95 1746 ms, CPU 99.7%) was contaminated by unattended OS upgrades. apt-daily-upgrade began at 17:25:33 UTC, before the 22-stream runtime restart at 17:26:00; its log lists kernel and package upgrades. An SSH MOTD apt-check also consumed a full CPU core. The clean repeat after stopping these jobs returned p95 264 ms and CPU 46.3%. Treat the contaminated run as interference evidence, not an ASR capacity point.

Automatic APT download/upgrade and update-notifier timers are now disabled on both workers and in bootstrap. Login-triggered update checks are disabled too. Running package maintenance was stopped cleanly before the clean repeat. Apply OS/security updates through image maintenance; these ephemeral serving workers no longer patch automatically during inference.

The benchmark admission override was removed; both workers are restored to a limit of 20. No diarization-server routing changes are included in this release.

## Published pins

- manifest: `deployment/fleet-v1/9d79175b7565ffa24ef5f3d566af9b1e33fbd6934e718290205d721aa7e34905/fleet-manifest.json`; SHA256 `9d79175b7565ffa24ef5f3d566af9b1e33fbd6934e718290205d721aa7e34905`
- bootstrap: `deployment/fleet-v1/3280c246f40020ab6e19edb2aab71b8409ec69d9ad198fbd2d01f663788d7ab7/bootstrap.sh`; SHA256 `3280c246f40020ab6e19edb2aab71b8409ec69d9ad198fbd2d01f663788d7ab7`
