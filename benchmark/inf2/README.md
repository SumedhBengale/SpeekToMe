> Historical research source recovered on 2026-10-19. Deployment status, quotas,
> instance IDs and runtime observations below belong to the original experiment.
> Recovery did not launch resources or repeat the benchmarks.

# Nemotron 3.5 / Inferentia2 feasibility prototype

Current test target: **one On-Demand `inf2.xlarge` in `us-east-1`**, authorized
after the initial Ohio Spot attempt was blocked. Longer-term target remains
Ohio Spot. Input is 560-ms chunks per stream.
The existing 32–35-stream G6f benchmark is accepted without rebenchmarking it.

## Current boundary

This is a feasibility prototype, not a deployed server. `streaming_encoder.py`
wraps all 24 official FastConformer layers with explicit, batched streaming
state, language conditioning and encoder projection. The official streaming
mel processor and subsampler remain on CPU. No complete audio-history encoder
recomputation occurs.

The AWS Inf Spot quota is currently zero. An increase to **4 vCPUs** was
requested in Ohio, request `7fe91565b0d14eb78e0982e89361ce96MJ5XZIxj`, support
case `179126932500356`. All 17 enabled regions were checked and each reported
zero Inf Spot quota. Virginia On-Demand Inf quota is also zero; increase request
`533e74a1cc12416a93688c5ad36ccb9b625hjnfQ`, support case `179127027000757`,
asks for four vCPUs. No instance has been launched. Check `STATUS.md` and the
local `artifacts/` directory for evidence and current validation results.

## Fixed encoder boundary

| Tensor | Shape | Type |
|---|---|---|
| New subsampled frames | `[B,7,1024]` | FP32 reference / BF16 candidate |
| Attention keys | `[24,B,8,56,128]` | same floating type |
| Attention values | `[24,B,8,56,128]` | same floating type |
| Convolution history | `[24,B,1024,8]` | same floating type |
| Valid attention history length | `[B]` | int32 |
| Language one-hot | `[B,128]` | same floating type |
| Active lanes | `[B]` | bool |
| Projected output frames | `[B,7,640]` | same floating type |

Outputs also return the updated keys, values, convolution history and lengths.
An inactive lane leaves all its state unchanged. Reset every state tensor and
length for a new stream. Each stream may have only one encoder call outstanding.

The attention/convolution state is approximately **5.625 MiB/stream in BF16**,
or **11.25 MiB in FP32**, plus the length, CPU subsampler state and scratch.
The official front end consumes **49 mel frames for the first call, then 56**;
both yield seven encoder frames. The PCM capture tool accepts **8960 mono
samples at 16 kHz per packet** and retains analysis-window overlap separately.

## Local correctness checks

Create an isolated Python 3.12 environment. Install CPU PyTorch from its CPU
wheel index for local validation, then `requirements.txt`. The Neuron AMI must
use its supported PyTorch build instead of these CPU wheels.

```bash
uv venv --python 3.12 artifacts/venv
uv pip install --python artifacts/venv/bin/python torch --index-url https://download.pytorch.org/whl/cpu
uv pip install --python artifacts/venv/bin/python -r requirements.txt
artifacts/venv/bin/python validate_encoder.py --tiny --jit --output artifacts/tiny-jit-parity.json
artifacts/venv/bin/python validate_encoder.py --model /path/to/checkpoint --output artifacts/full-parity.json
artifacts/venv/bin/python validate_audio.py --model /path/to/checkpoint --audio /path/to/mono-16khz.wav --chunks 16
```

`validate_encoder.py` compares the explicit state against official dynamic
K/V and convolution caches across warmup, rollover, mixed active lanes,
independent language prompts, and a reset. `validate_audio.py` checks real
560-ms PCM packets, compares encoder outputs, and verifies token sequences
against official streaming `generate`. It captures projected encoder frames
for later decoder profiling. Its current scope is complete streaming chunks;
partial end-of-stream flush and commit acknowledgement are **not validated**.

Checkpoint pinned to `ea30d66debe3740a08b573244286791d423d6b3e` in
`nvidia/nemotron-3.5-asr-streaming-0.6b`. C reference inspected at
`5cc20265c3723de0e5a6a5244d01e051aca2e344`. C code-switch recovery is an extra
heuristic; it is not enabled in this official-reference parity prototype.

## Launch when quota permits

```bash
python3 launch_instance.py --region us-east-1 --market on-demand
# Original Spot target, when its separate quota is approved:
python3 launch_instance.py --region us-east-2 --market spot
```

The launcher checks quota, avoids duplicate tagged instances/pending requests,
reuses the dedicated SSH security group, and tries default Ohio subnets plus
AWS's automatic AZ selection. There is no automatic market or instance-size
fallback. It always uses `inf2.xlarge`. The AMI is the latest AWS-owned Neuron
PyTorch 2.9 Ubuntu 24.04 image in the selected region: currently
`ami-REDACTED` in Virginia and `ami-REDACTED` in Ohio.
SSH uses the existing `SpeekToMe` key. Public IPv4 is explicitly requested
because the Virginia default subnet does not auto-assign it.

In Spot mode the launcher uses a persistent request, stop-on-interruption and a 120-GiB encrypted
gp3 root disk. Stop preserves the disk; AWS may restart it when capacity
returns. There is no guaranteed Spot reservation duration. The last observed
Spot quote was $0.0758/hour on 2026-10-06; storage and public IPv4 are extra.
Do not confuse an unfulfilled request with a running instance.

## Observed reference-runtime issue

With Transformers 5.18.0, the full official encoder's eager path builds an
additive float attention mask, while this attention implementation interprets
its mask as boolean. On the first real-audio call this makes all attention
scores negative infinity and produces NaNs. The unmodified official SDPA path
uses the expected boolean mask and works. `validate_audio.py` uses that SDPA
path as the independent waveform/token oracle. The accelerator adapter uses
eager attention with its own explicit boolean mask, avoiding the faulty mask
construction. This behavior must be rechecked against the Neuron AMI's stack.

## Accelerator gate

After SSH access, first inventory the AMI's installed Neuron packages and
activate its supported PyTorch environment. Install only the extra requirements.
Run CPU parity there before compiling. Start with one batch, then sweep
`1,8,16,32,64,128` sequentially as memory/compiler behavior permits:

```bash
python compile_encoder.py --batch 8 --model /path/to/checkpoint
```

No graph partitioner is configured: unsupported encoder operations fail
compilation. Compiler intermediates, the compiled module, graph and raw batch
timings are retained. The first diagnostic graph has explicit host cache I/O;
**device residency remains unimplemented and unverified**. A poor result must
be separated into compute and transfer costs before rejecting Inf2.

Remaining gates, in order:

1. Verify numerical and transcript parity of the actual Neuron BF16 graph over
   long sessions, silence, independent resets, language prompts and final flush.
2. Inspect compiler placement and confirm the encoder stays entirely on Neuron.
3. Implement/prove resident cache slots, safe slot reuse, and state locality per
   NeuronCore. Compare one core with replicas on both cores.
4. Profile real captured RNN-T workloads on the four-vCPU target, using the C
   runtime or a suitable batched implementation; local CPU timing is not target
   capacity evidence.
5. Run an arrival-driven harness with phased 560-ms streams, actual batch
   occupancy, transcript and commit verification, output delays, queue depth,
   oldest pending audio age and sustained backlog trend. Include host CPU,
   Neuron utilization, HBM and transfer measurements.
6. Compare sustainable capacity per current dollar, reserve operating headroom,
   and decide whether native C/libnrt integration and quantization are justified.

The diagnostic `chunks/sec * 0.560` number is a theoretical encoder ceiling.
It is never a production stream-capacity result.
