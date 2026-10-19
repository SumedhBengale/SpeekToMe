# Full L4: four consecutive 160-stream trials

On-Demand g6.xlarge i-REDACTED in us-east-1b, tested October 9, 2026 (Asia/Kolkata). Every trial completed all 160 streams, with 34,240 chunk measurements and no errors. All four p95 results meet the 560 ms target for this workload.

| Trial | p50 ms | p95 ms | p99 ms | Within 560 ms | Worst ms |
|---|---:|---:|---:|---:|---:|
| 1 | 102.18 | 165.85 | 1216.75 | 96.68% | 1873.06 |
| 2 | 103.34 | 166.98 | 1213.44 | 96.68% | 1853.93 |
| 3 | 108.37 | 179.60 | 1295.32 | 96.26% | 5754.49 |
| 4 | 108.00 | 178.36 | 1328.48 | 96.23% | 5773.61 |

| Trial | CPU average, total instance | Peak RAM MiB | Peak VRAM MiB | GPU sampled average | GPU sampled peak | Sender lateness p95 ms |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 32.03% | 1814.12 | 3267 | 25.07% | 34% | 1.49 |
| 2 | 32.31% | 1850.08 | 3273 | 25.61% | 34% | 1.50 |
| 3 | 33.81% | 1960.27 | 3335 | 26.86% | 36% | 1.48 |
| 4 | 33.70% | 1962.49 | 3337 | 26.54% | 45% | 1.48 |

## Method and configuration

Current immutable S3 bootstrap and ASR-only manifest deployed successfully from user-data.sh. Full NVIDIA L4 reports 23,034 MiB available to the driver, driver 595.91.07. ASR model and native binary hashes are retained in full-l4-hashes.txt; no native source, production instance or S3 release was changed.

Gateway max streams 160; native arena 176; HTTP threads 360; batch cap 32; queue delay 5 ms; ingress cohort 120 ms; right context 6; left context 56 frames (8 chunks); CUDA graphs enabled. These enlarged admission/state/thread limits differ from the fractional worker.

One restart of native and gateway before trial 1, no restarts or deliberate pauses between the following trials. Native PID 2773 and gateway PID 2768 remained unchanged. Each stream received the identical 214 preencoded 560 ms Opus/Ogg chunks, totaling 119.84 seconds; no fixture loop within that audio. Same audio sequence, staggered stream starts across 560 ms, and deterministic +/-20 ms cadence jitter in all four trials. No warmup measurements excluded.

The benchmark ran on the inference instance through authenticated loopback gateway. It excludes coordinator and external network latency. Its latency is chunk submission to cumulative audio_processed progress, including batching, buffering/endpointing and inference; not pure GPU compute or exact word availability. Success requires transcript text, commit acknowledgement, no errors and no pending audio marks. All trials reached 160 observed active streams and ended with zero active streams, zero gateway waiting requests and zero unreported audio.

CPU and RAM measure the whole instance, including the local Python load generator and monitoring. GPU utilization is sampled every two seconds. Harness timeouts were enlarged to allow 160 session handshakes and sufficient output drain; workload and timing measurement were unchanged.

## Interpretation

The full-L4 instance sustained the requested 160-stream workload at 166-180 ms p95, with much less run-to-run p95 variation than the previous fractional 25-stream sequence (919, 787, 1,044 ms). This strengthens the hypothesis that fractional GPU scheduling contributes to the earlier limitation, but does not isolate it: the full worker also has twice the CPU cores, more RAM and larger state/thread limits. This is not an eightfold capacity guarantee or a maximum concurrency measurement.

Tail exceptions remain: p99 1.21-1.33 seconds and rare worst chunks up to 5.77 seconds. The worst events in trials 3/4 were chunk zero, reported when cumulative processing advanced to 6.16 seconds. A separate diagnostic excluding the first 10 seconds gives p95 157-166 ms and p99 590-597 ms; the primary table includes all chunks. Identical speech across streams correlates decoder and endpointing workloads. The two-minute trials demonstrate this repeatable workload, not diverse production traffic or long-term capacity. VRAM peaked at 3,337 MiB, far below full GPU capacity.

## Cloud resources

The preceding Spot attempts failed with InsufficientInstanceCapacity in Ohio, Virginia, Oregon and Canada Central. The user authorized On-Demand in Virginia; us-east-1a/c also rejected On-Demand before us-east-1b accepted. Test instance tags exclude SpeekToMeFleet so it does not enter the coordinator pool. Results were copied locally before termination. Termination evidence is retained in termination.json; root EBS DeleteOnTermination is enabled, and final cleanup verification is stored separately.

Cleanup status at the last verification: AWS accepted termination and subsequently forced termination with OS shutdown skipped, but still reported shutting-down. Root volume vol-REDACTED remained attached with DeleteOnTermination=true; completed deletion has not yet been confirmed. Raw cleanup snapshots are retained. No benchmark outputs depend on the instance anymore.
