# Hugging Face ASR quantizations for 3 GiB VRAM and hour-long meetings

Research date: 16 October 2026. This is source/model-card research, not a new GPU benchmark.

**Accuracy-first candidate:** `handy-computer/Qwen3-ASR-1.7B-gguf`, `Qwen3-ASR-1.7B-Q5_K_M.gguf`, with application-enforced audio windows of at most 30 seconds and bounded decoder context. **Simplest bounded long-form alternative:** Whisper large-v3-turbo Q8_0, whose long-form path already uses 30-second windows. Neither a weight-file size nor a model-card context limit establishes safe peak VRAM on our worker.

## Quantization shortlist

All sizes below are exact current Hugging Face metadata converted to decimal GB/MB. WER values are the conversion publisher's reported LibriSpeech test-clean results (2,620 short utterances), not our meeting accuracy or a measured CUDA result.

| Model | Quantization | Weight size | Reported WER | Assessment |
|---|---|---:|---:|---|
| Qwen3-ASR 1.7B | Q8_0 | 2.185 GB | 1.62% | Too little comfortable headroom |
| Qwen3-ASR 1.7B | Q6_K | 1.693 GB | 1.65% | No demonstrated accuracy advantage over Q5 in this evaluation |
| Qwen3-ASR 1.7B | Q5_K_M | 1.517 GB | 1.65% | Best-supported Qwen accuracy/memory compromise |
| Qwen3-ASR 1.7B | Q4_K_M | 1.320 GB | 1.81% | More memory margin; reported accuracy regression |
| Qwen3-ASR 0.6B | Q6_K | 690 MB | 2.11% | Smaller alternative; same long-input caveat |
| Whisper large-v3 | Q5_K_M | 1.161 GB | 1.84% | Built-in windowed long-form path; larger decoder than Turbo |
| Whisper large-v3-turbo | Q8_0 | 886 MB | 2.01% | More headroom and built-in windowed long-form path |
| Parakeet TDT 1.1B | Q5_K_M | 936 MB | 1.39% | English-only specialist; not a multilingual replacement |

Sources: [Qwen 1.7B quantizations](https://huggingface.co/handy-computer/Qwen3-ASR-1.7B-gguf/blob/main/README.md), [Qwen 0.6B quantization evaluation](https://github.com/handy-computer/transcribe.cpp/blob/main/docs/models/qwen3-asr-0.6b.md), [Whisper large-v3](https://huggingface.co/handy-computer/whisper-large-v3-gguf), [Whisper Turbo](https://huggingface.co/handy-computer/whisper-large-v3-turbo-gguf), [Parakeet 1.1B](https://huggingface.co/handy-computer/parakeet-tdt-1.1b-gguf).

The Qwen quantization evaluation used an Apple M4. Q5, Q6 and Q8 results are reported as within bootstrap uncertainty; Q4 loses about 0.2 percentage points relative to the reference. These results support choosing Q5 over Q6/Q8 for this memory budget. They do not establish that Qwen wins on accents, noise, simultaneous speakers, technical vocabulary or hour-long recordings. Small cross-model WER differences should not be treated as definitive rankings.

Parakeet's low English WER is relevant if the product is explicitly English-only. Its Conformer runtime still needs a length/memory audit; a transducer decoder without an LLM KV cache does not make unrestricted encoder attention safe. It is not the recommended general multilingual path here.

## The long-audio issue is real in the inspected Qwen runtime

I inspected transcribe.cpp at commit `e013b9f4135a37b60e19188cf6cb746ecc36b19b`, not just model-card claims about long input support.

- [`encoder.cpp`](https://github.com/handy-computer/transcribe.cpp/blob/e013b9f4135a37b60e19188cf6cb746ecc36b19b/src/arch/qwen3_asr/encoder.cpp): the mask builder uses full bidirectional attention over valid audio rows. The graph allocates a `T_enc × T_enc` mask, including when Flash Attention is selected. The comments explicitly distinguish this from vLLM's windowed path. This runtime is not automatically doing bounded long-form ASR for Qwen.
- [`decoder.cpp`](https://github.com/handy-computer/transcribe.cpp/blob/e013b9f4135a37b60e19188cf6cb746ecc36b19b/src/arch/qwen3_asr/decoder.cpp): causal LM prefill also allocates a square prompt mask, and decoding keeps audio/text KV state.
- [`model.cpp`](https://github.com/handy-computer/transcribe.cpp/blob/e013b9f4135a37b60e19188cf6cb746ecc36b19b/src/arch/qwen3_asr/model.cpp): F16 KV is initially allocated for 2,048 tokens and grows to fit longer requests, rounded upward, subject to the context ceiling. Clearing a cache between requests does not by itself shrink its allocation.

Consequently, a documented 65,536-token capacity or roughly 87-minute input ceiling is a validity limit, not proof of a 3 GiB memory fit. [Runtime input contract](https://github.com/handy-computer/transcribe.cpp/blob/main/docs/input-limits.md).

For an indicative calculation, a 28-layer decoder with eight KV heads and head dimension 128 needs `2 × 28 × 8 × 128 × 2 = 114,688 bytes` of F16 KV per token. A 2,048-token cache is 224 MiB. At the nominal 12.5 audio tokens/second, one hour contributes about 45,000 tokens: roughly 4.8 GiB of KV payload before transcript tokens, weights or workspaces. Rounding to 65,536 tokens allocates 7 GiB. An F16 45,000-square attention mask is another roughly 3.8 GiB. Actual padding/token counts vary by runtime, so these are illustrative lower-level calculations, not measured peak residency.

Quantizing weights to Q4 or Q5 does not remove those activation/cache allocations. Flash Attention does not remove the explicitly allocated masks in this implementation either.

## How Qwen can still process an hour safely by design

Use the hour-long recording as a sequence of bounded jobs:

1. Keep the source audio on disk/CPU and decode only the next window before GPU inference.
2. Enforce a hard maximum of 30 seconds per submitted window, **including overlap**. Prefer pause boundaries; use a small overlap when a boundary is unavoidable. Validate transcript stitching rather than assuming it is lossless.
3. Cap the decoder context, initially 2,048 tokens; bound generation and explicitly handle truncation. Do not append the whole preceding meeting transcript to each prompt.
4. Reset decoder state and discard previous embeddings/results from GPU memory after each window. Keep the loaded weights resident. Bound retained graph/workspace allocations as well as the input length.
5. Initially process one GPU window at a time. Queue mic and system tracks rather than loading a second model or allowing independent unbounded jobs. Establish safe batching separately.
6. Keep diarization/alignment separate from this GPU budget. Qwen in this runtime does not supply timestamps natively; an additional forced aligner must be budgeted if precise word alignment is required. [Qwen runtime capabilities](https://github.com/handy-computer/transcribe.cpp/blob/main/docs/models/qwen3-asr.md).

Under those constraints, peak inference memory should depend on the largest permitted window and concurrency, rather than total meeting duration. That is an architectural expectation requiring verification of allocator/cache retention over a complete meeting, not a guarantee against every OOM.

Whisper has a 30-second receptive field, and its supported long-form algorithms process consecutive or overlapping windows. This makes it a more straightforward fit for the long-meeting requirement. [Upstream long-form description](https://huggingface.co/openai/whisper-large-v3-turbo#chunked-long-form). Its bounded duration does not prove a particular batch size fits 3 GiB either.

## Exact proposed Qwen artifact

- Repo: `handy-computer/Qwen3-ASR-1.7B-gguf`.
- Revision: `0e0a596e3959c3f3f2ab7d443a5b6f0f4a4bba6f`.
- File: `Qwen3-ASR-1.7B-Q5_K_M.gguf`.
- Bytes: `1517290464`.
- Hugging Face advertised SHA256: `034c557fe92ff8fcd9a9c041cbdaad347be0a86a58d3a348f63cf3f0180879d0`.
- [Pinned download](https://huggingface.co/handy-computer/Qwen3-ASR-1.7B-gguf/resolve/0e0a596e3959c3f3f2ab7d443a5b6f0f4a4bba6f/Qwen3-ASR-1.7B-Q5_K_M.gguf).

This artifact targets transcribe.cpp's tensor contract; it is not a drop-in file for the current NeMo-Speech.cpp server or every other GGUF ASR engine. Other runtimes may split audio and language weights into separate files, which must both be counted.

Exact repository revisions, weight sizes and advertised hashes for all inspected candidates are saved in `hf-quantization-snapshot.json`. Old mirrors rounded GiB as GB or carried older values; the table uses current API byte counts.

## Recommendation and remaining evidence

Start with Qwen 1.7B Q5_K_M as the accuracy-first experimental candidate, paired with a hard window/context limit. Compare it with Whisper Turbo Q8 for throughput and meeting usability; include full Whisper large-v3 Q5 if raw multilingual accuracy is more important than decoding cost. Do not submit an hour in one Qwen call on the 3 GiB worker.

There is no published evidence in the reviewed sources establishing peak CUDA VRAM and sustained hour-long behavior on our exact 3,072 MiB L4-3Q. Before calling any candidate an easy production fit, require a full-hour run with a memory plateau, no omitted/truncated segments, and independently scored output. A provisional operating target is at most 2,560 MiB device usage, leaving 512 MiB headroom; this is our proposed acceptance target, not a measured footprint. Test separate/two-track input, realistic noise and multiple meetings in sequence to expose retained allocations. Keep WER, vocabulary false substitutions, peak memory and completed audio-hours per GPU-hour separate.

Research performed: read Hugging Face cards and current file metadata, inspected pinned runtime source, and calculated memory scaling. No weight files were downloaded, no new GPU inference was run, and production was not changed.
