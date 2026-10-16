# Qwen3-ASR 1.7B Q8 offline worker experiment

This is an isolated offline runtime benchmark, not a deployed gateway backend.
Results are in `results/2026-10-16/REPORT.md`. Every timed result includes the
complete CPU mel frontend, CUDA encoder, prefill and autoregressive decode.
Model loading, input-file loading, network, gateway, vocabulary matching and LLM
processing are excluded from the throughput denominator.

## Pinned inputs

- Runtime: `handy-computer/transcribe.cpp` at
  `e013b9f4135a37b60e19188cf6cb746ecc36b19b`.
- Model: `handy-computer/Qwen3-ASR-1.7B-gguf`, revision
  `0e0a596e3959c3f3f2ab7d443a5b6f0f4a4bba6f`,
  `Qwen3-ASR-1.7B-Q8_0.gguf`, 2,185,030,624 bytes.
- Model SHA256:
  `9a0d81792dfea2d5f278b8a63deb3ea6e02139ce42c2301f32ea19c4f77526b7`.
- Worker: `i-REDACTED`, us-east-1, NVIDIA L4-3Q (3,072 MiB),
  2 vCPUs / one SMT physical core, AMD EPYC 7R13, Ubuntu 22.04.5.
- Compiler/runtime: CUDA 13.2.86, cuBLAS 13.4.1.3, Ubuntu GCC 11,
  CUDA target SM 8.9. Driver 595.91.07. GGML native CPU tuning disabled;
  compatible x86 CPU backend retained.

The unmodified runtime cannot load the model on this worker because it allocates an extra
714 MiB packed gate/up projection. `transcribe-low-memory.patch` adds opt-in
changes for this experiment:

1. `TRANSCRIBE_QWEN_LOW_MEMORY=1`: use the original separate gate/up projections
   and allocate KV lazily. Quantization and model tensors are unchanged.
2. `TRANSCRIBE_QWEN_CPU_EMBED=1`: place the tied embedding/output matrix
   (315.30 MiB) on the CPU, keeping the remaining weights on CUDA. Disable the
   scheduler's optional offload of CPU weights back to GPU.
3. `TRANSCRIBE_QWEN_RELEASE_ENCODER=1`: release encoder graph buffers before
   allocating decoder state; free the previous request's KV before the next
   encoder pass. This trades some allocator work for a lower memory peak.
4. `TRANSCRIBE_VERIFY_FLASH=1`: log the actual scheduler placement of encoder
   and decoder Flash Attention nodes. `GGML_CUDA_ALLOC_TRACE=1` optionally
   records CUDA allocation requests and the free-memory values preceding them.

All changes are experimental and gated by environment variables. The patch
is saved here rather than changing the shared NeMo checkout or fleet launch
configuration. CUDA allocation failures inside upstream kernels sometimes
abort the process; each case runs in a separate process with core dumps disabled.

## Reproduction

Clone the pinned runtime, apply the patch, and build a CUDA shared library:

```sh
git clone https://github.com/handy-computer/transcribe.cpp
cd transcribe.cpp
git checkout e013b9f4135a37b60e19188cf6cb746ecc36b19b
git apply /absolute/path/to/benchmark/qwen-q8/transcribe-low-memory.patch
cmake -S . -B build -DTRANSCRIBE_CUDA=ON -DTRANSCRIBE_BUILD_SHARED=ON \
  -DTRANSCRIBE_BUILD_TESTS=OFF -DTRANSCRIBE_USE_SYSTEM_BLAS=OFF \
  -DGGML_NATIVE=OFF -DCMAKE_CUDA_ARCHITECTURES=89 -DCMAKE_BUILD_TYPE=Release
cmake --build build --target transcribe-cli -j 8
cmake --install build --prefix /absolute/path/to/runtime
```

The actual build used an Ubuntu 22.04 container on the desktop with the host
CUDA 13.2 toolkit mounted read-only, so its glibc requirements match the worker.
For linking executables inside that compiler-only container, the CUDA library
directory and a `libcuda.so.1` symlink to the toolkit's driver stub were on
`LD_LIBRARY_PATH`. On the worker, use its real NVIDIA driver, not the stub.

Compile `offline_bench.cpp` against `include/transcribe.h` and `libtranscribe`:

```sh
g++ -O3 -std=c++17 -I/path/to/transcribe.cpp/include offline_bench.cpp \
  -L/path/to/runtime/lib -ltranscribe -o /path/to/runtime/bin/offline-bench
```

Place the model, runtime, CUDA libraries and `run_case.py` in one test directory.
The wrapper's CUDA-library path matches the extracted CUDA 13.2 Debian package.
Prepare audio using `prepare.py`; its TSV paths target
`/home/ubuntu/qwen-q8-test/audio`, which should be changed for another host.
Only little-endian 16 kHz mono float32 PCM is submitted to the runtime.

```sh
TRANSCRIBE_QWEN_CPU_EMBED=1 TRANSCRIBE_QWEN_RELEASE_ENCODER=1 \
CUDA_MODULE_LOADING=LAZY python3 run_case.py example \
  --unpacked --manifest audio/5s.tsv --batch 3 --context 512 --iterations 8
```

Each case keeps one model/session resident. A full warmup is logged and
excluded. Batch items use different speech windows; eight distinct windows
cycle deterministically. Each measured row records status and transcript.
Success requires status OK and nonempty text. Batch wall time is counted once,
not once per item. Percentiles use nearest rank and describe batch service
latency, not gateway latency or queued-job completion latency. The GPU sampler
runs every 100 ms; it can miss short allocation peaks. CUDA's free-memory
accounting is also recorded in allocation-trace cases and is the relevant
constraint when allocations fail.

The one-hour endurance workload repeats eight human-speech fixtures. It tests
completed audio volume, bounded memory and retained state. It does not establish
meeting WER, diarization, boundary stitching or multilingual accuracy. The small
separate accuracy sanity check uses eight complete synthetic Indian-English
clips with references and no vocabulary prompt.

## Restoration repair

Restarting Nemotron exposed an existing readiness mismatch: the installed
gateway smoke test always required speaker diarization, while this worker's
server configuration loads only ASR. The gateway now has
`-smoke-diarization=false`; the default remains `true` for diarized workers.
The test worker's `check-ready.sh` uses the ASR-only setting. The smoke test
requires both a nonempty completed transcript and the commit acknowledgment.
Tests cover both event orders and the speaker-label requirement.

The original gateway and readiness script were backed up on the worker as
`gateway.before-qwen-readiness-20261016` and
`check-ready.before-qwen-20261016.sh`. This fix was applied only to the test
worker; fleet launch artifacts were not republished. Both original inference
workers were confirmed ACTIVE and ready after the fleet controller resumed.
