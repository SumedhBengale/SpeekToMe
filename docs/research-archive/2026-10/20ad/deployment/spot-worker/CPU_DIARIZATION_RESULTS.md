# CPU-only diarization validation — 2026-10-08

Virginia g6f.large i-REDACTED successfully ran the deployed 190 MiB Nemotron-3 diarization BF16 GGUF on CPU only. ASR service and endpoints were unchanged. No foreground ASR workload was deliberately generated during this test.

Input: complete 180-second repeated-speech file, decoded from the preceding meeting Ogg/Opus fixture into 16 kHz mono WAV. Existing bounded-window geometry was copied into a standalone diar YAML: chunk340, spkcache264, fifo40, right_context40, left_context1, update_period300. No full-attention flag or preset override.

Command:
```
CUDA_VISIBLE_DEVICES= /opt/speektome-worker/runtime/bin/nemo-speech diarize \
 /tmp/cpu-diar-180.wav --config /tmp/cpu-diar.yaml \
 -m /opt/speektome-worker/models/diar.gguf --device cpu \
 --format json --output /tmp/cpu-diar-result-repeat.json --force
```

First run: detected completed within35.50s with5-second process polling, exit0. Diarization PID31030 never appeared in sampled NVIDIA compute process lists; only the already running native ASR service appeared. CUDA initialization reports no visible CUDA device, expected with the explicit visibility restriction.

Repeat measured with /usr/bin/time -v: wall31.90s, user61.94s, system0.37s, CPU195% (approximately97.5% of two vCPUs), maxRSS293628KiB (286.75MiB), swaps0, exit0. Full-file output contains one speaker segment 0.071–180.010s. The earlier GPU mixed-workload run took approximately7.24–7.34s for the same three-minute fixture, but this is not an isolated matched CPU/GPU comparison.

RTF31.90/180=0.177, about5.64x faster than real time. Linear extrapolation gives roughly10.6minutes for onehour, not a measured hour-long result. Repeated single-speaker audio does not establish multi-speaker DER or timing parity with GPU.

This establishes CPU backend feasibility and low process memory use on this CPU. It does not justify co-locating CPU diarization with live ASR: the job used nearly both vCPUs. A separate queued CPU worker is a candidate for further tests. CPU architecture portability, production meeting accuracy, longer recordings, and concurrency remain untested.

Evidence: validation/optimized-rollout/coldboot-560ms/cpu-diar-benchmark.json and cpu-diar-repeat.txt.
