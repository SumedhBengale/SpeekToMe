# Feasibility status — 2026-10-06

**Deployment blocked by AWS account quotas. No Inf2 instance is running.**

User authorized Ohio Spot first, then Virginia On-Demand for initial testing.
Use exactly one `inf2.xlarge` and the `SpeekToMe` key; do not increase size or
switch market automatically. The Virginia public key matches the local
`/home/ssben/.ssh/YOUR_SSH_KEY.pub`.

| Target | Current quota | Requested | Request ID | Support case | Last status |
|---|---:|---:|---|---|---|
| Virginia On-Demand Inf | 0 vCPUs | 4 vCPUs | `533e74a1cc12416a93688c5ad36ccb9b625hjnfQ` | `179127027000757` | CASE_OPENED |
| Ohio Inf Spot | 0 vCPUs | 4 vCPUs | `7fe91565b0d14eb78e0982e89361ce96MJ5XZIxj` | `179126932500356` | CASE_OPENED |

Every one of the 17 enabled AWS regions reported zero Inf Spot quota.
Initial Ohio RunInstances attempts reported insufficient capacity across all
three AZs and automatic placement. RequestSpotInstances then reported
MaxSpotInstanceCountExceeded; direct Service Quotas reads confirmed zero.
Quota approval and available Spot capacity are separate requirements.

Created only an unused Ohio SSH security group, `sg-REDACTED`, limited
to workstation IPv4 on port 22. No new instance or EBS volume was created.
No changes were made to existing AWS application instances.

## Verified locally

- Official checkpoint downloaded and pinned to
  `ea30d66debe3740a08b573244286791d423d6b3e`.
- Explicit-state, batched 24-layer encoder traced on CPU, retaining persistent
  attention and convolution caches.
- Fourteen sequential test iterations, 23 active stream calls across two
  independent lanes: cache warmup/rollover, independent prompts, inactive lanes,
  and one stream reset passed against official dynamic caches.
- Maximum FP32 output difference: **0.0001580**; maximum cache difference:
  **0.0006490**. Independent B=1 oracle and B=2 adapter use different GEMM
  reduction orders; cache tolerance is documented in the validator.
- Real JFK speech: **16 packets × 560 ms**, 8960 mono samples per packet,
  49 mel frames on the first call then 56, seven new encoder frames per call.
- Maximum real-audio projected encoder difference: **0.00001425**.
- **33 nonblank tokens matched official streaming `generate` exactly**:
  “And so my fellow Americans ask not what your country can do for you. Ask”
- Audio front end retained at most **1376 PCM samples** after processing;
  historical encoder context stayed in caches.
- Found an official eager-mask issue; the independent official SDPA path works.
  Details and the adapter's boolean-mask behavior are in README.md.

Raw results: `artifacts/full-jit-parity.json`, `artifacts/audio-parity.json`,
captured decoder inputs in `artifacts/audio-parity.pt`, and quota/API evidence
in `artifacts/`. These generated files are intentionally Git-ignored.

## Not yet verified

Neuron compilation/operator support, accelerator numerical parity, device
cache residency, use of both cores, native C/libnrt integration, target CPU
decoder capacity, long-session/end-of-stream behavior, real-time concurrency,
tail output delay, and economics remain unmeasured. CPU correctness and
TorchScript tracing are not evidence of Neuron compatibility or capacity.

## Resume

1. Read the current quota directly; a support case closing does not by itself
   prove that the applied quota changed.
2. Once Virginia's applied quota reaches four, run:
   `python3 launch_instance.py --region us-east-1 --market on-demand`.
3. Re-read instance state and public IP, SSH using the local SpeekToMe key, and
   inventory/activate the AMI's supported Neuron PyTorch environment.
4. Copy this prototype and pinned checkpoint, run parity checks on that stack,
   then compile the first batch with no graph partitioning.
5. Complete the accelerator/decoder/arrival-driven gates in README.md before
   deciding whether to build the production server or investigate quantization.

The existing 32–35-stream G6f baseline is accepted. It has not been rebenchmarked.
