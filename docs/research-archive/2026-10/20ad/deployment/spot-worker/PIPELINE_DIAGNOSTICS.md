# Virginia pipeline diagnostics, 2026-10-08

Instrumented and patched worker: i-REDACTED (us-east-1b).
Ohio and S3 release manifests are unchanged.

The authenticated /worker/metadata response now includes pipeline timings:
ogg_parse, opus_decode_packet, pcm_serialize, opus_message_to_backend,
backend_pcm_write, backend_progress_wait, client_output_write.
Counts, bytes and totals are process lifetime; percentile samples are bounded
to the latest 2048 observations per stage. These are wall times, not isolated
CUDA kernel timings. backend_progress_wait begins when PCM is submitted and
ends when audio_processed covers it; it includes buffering and endpointing.
No audio, transcript text or authorization token is recorded in these counters.

Native per-chunk encoder/RNNT decoder wall times are enabled with
/etc/systemd/system/speektome-worker-nemo.service.d/pipeline-timing.conf:
NEMO_SPEECH_TIMING=1. Watch with journalctl -u speektome-worker-nemo -f.
Encoder time includes scheduler wait, CPU/GPU work and readback.
gpu_execution_ms remains null because isolated GPU execution was not measured.

Finding: the original gateway forwarded each decoded Opus packet (~20 ms) as
one PCM WebSocket message. The pinned RecognitionStream::push enters ingress
coordination for every message. With the same 20-stream/560ms workload, replacing
the small messages with full 560ms PCM messages removed the growing delay.

Fix: accumulate Opus-decoded PCM in a bounded 17920-byte buffer (560 ms at
16kHz mono S16LE), send complete steps, and flush a partial tail BEFORE commit.
Ogg pre-skip/EOS trimming remains unchanged. No resampling or audio dropping.
Raw PCM endpoints retain their prior pass-through behavior.

Validation: go test -race ./... passes, including fragmented WebSocket/Ogg
input, two independent 590ms Ogg streams, pre-skip/granule lengths, and final
partial PCM before commit. Native model/config remained the same during the
Opus vs PCM comparison. Native timing logging was enabled for the patched run.

20 synchronized streams, 120 clips/stream, 67.2s audio, five warmup clips excluded:
- Original Opus: 20/20, p50 10689.6ms, p95 17308.7ms.
- One PCM message/clip: 20/20, p50 313.3ms, p95 453.9ms.
- Patched Opus: 20/20, p50 270.9ms, p95 311.2ms; no errors.
All delays are client-observed input-frontier proxies, not exact word attribution.
Repeated speech fixture, worker-local traffic, no meeting load; this is not a
long-duration production/network benchmark. GPU sampled average remains low
(9.2%, peak 54%), despite meeting the latency target.

Evidence: validation/optimized-rollout/coldboot-560ms/pipeline-summary.json,
virginia-pipeline-opus.json, virginia-pipeline-fixed.json, pcm-and-perf.txt,
native-perf.txt, native-timings.log. Original runtime perf: 75.44% of on-CPU
samples in libcuda, with device-to-host-copy stacks. This does not quantify GPU
kernel time or prove a driver defect.

Rollback binary: /opt/speektome-worker/bin/gateway.before-pipeline-trace.
