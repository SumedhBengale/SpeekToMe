# Offline ASR assessment: 16 October 2026

Recommendation: evaluate final-only dictation using offline ASR. Qwen3-ASR 1.7B with a quantized text decoder and higher-precision audio encoder is a plausible candidate on the 3 GiB worker, but neither safe peak memory nor improved capacity/accuracy has been demonstrated there. Compare Qwen 0.6B and the existing engine in file/offline mode before choosing.

## Verified hardware and artifacts

- Read-only SSH query of the running us-east-1 worker returned `NVIDIA L4-3Q, 3072 MiB, 1651 MiB` (name, total memory, current used memory). Current usage includes the existing workload/runtime; it is not the available budget for a replacement model. No services were changed.
- AWS describes g6f.large as a 1/8 L4 partition with 3 GB GPU memory: [AWS G6 specifications](https://aws.amazon.com/ec2/instance-types/g6/). This is also a fractional compute allocation, not a full L4 with a smaller memory limit.
- Existing local file `/mnt/sumedh/AI/models/speech/qwen3-asr-1.7b-q8_0.gguf` is 2,506,723,200 bytes (2.51 GB / 2.33 GiB), with GGUF architecture `qwen3asr`. It is a different model architecture from the current NeMo ASR engine.
- The local file stores 2,349,243,536 tensor elements, including the audio component and separately stored embedding/output matrices. This is serialized storage, not a claim about unique trainable parameter count. The marketing name alone is insufficient for memory estimation.
- Hugging Face file metadata for `cstr/qwen3-asr-1.7b-GGUF` lists Q4_K at 1,490,915,200 bytes and Q5_K at 1,744,867,200 bytes. [Publisher model card](https://huggingface.co/cstr/qwen3-asr-1.7b-GGUF/blob/main/README.md).
- The publisher documents a repaired Q4_K export retaining the audio encoder at Q8 after earlier low-precision encoder exports produced empty/repeated transcripts. That is a project-specific reported failure, not a universal property of all Q4 models. Pin the actual artifact hash and runtime during testing.
- `ggml-org/Qwen3-ASR-1.7B-GGUF` uses separate language and audio/projector files: Q8 language file 2,165,034,944 bytes plus Q8 projector 355,709,344 bytes. Both must be counted. [File listing](https://huggingface.co/ggml-org/Qwen3-ASR-1.7B-GGUF/tree/main).

All file sizes are weights/download sizes, not measured GPU residency. Runtime tensor conversion, CUDA workspace, encoder activations, KV cache, context allocation and concurrent jobs can change peak memory. A small GGUF from one project is not necessarily compatible with another project's loader.

## Why the architecture becomes simpler

With final-only output, vocabulary retrieval consumes an immutable transcript. There is no need to retract candidate matches as ASR partials change, reschedule decisions after token revisions, or map preview edits back to a revised transcript. Jev can process bounded contextual sections after ASR. The optional enhancement LLM then sees resolved text, and the original transcript plus decisions remain available for audit.

Proposed first version:

`record/upload audio -> offline ASR -> CPU vocabulary candidates -> Jev when candidates exist -> optional LLM cleanup -> final output + change history`

Upload can occur while the user speaks even if ASR runs only after stopping. Removing preview does not require waiting until recording ends to start uploading. Longer recordings still need bounded audio processing; use pauses and controlled overlap rather than arbitrary cuts through words. If stop-to-result delay proves excessive, background transcription of completed utterances is a later option, with no live text preview required.

## Accuracy and capacity are separate questions

[Qwen's official model card](https://huggingface.co/Qwen/Qwen3-ASR-1.7B) reports better results for its offline mode than its streaming mode: on its inference-mode comparison, 1.7B average WER is 2.69 offline versus 3.33 streaming. That is evidence for testing offline recognition, not a paired comparison against our Nemotron build or a quantization validation.

Qwen uses an audio encoder followed by autoregressive text decoding. Offline scheduling can reduce frequent small updates and make batching easier, but a larger autoregressive decoder can cost more than a transducer. Fitting a model says little about requests per hour. [Qwen technical report](https://arxiv.org/html/2601.21337v2).

The earlier local files named `qwen3_concurrency_*_results.json` do not establish 1.7B capacity on this worker. Their script selects `/mnt/sumedh/AI/models/speech/qwen3_asr_0.6b_hf`, uses vLLM with 85% GPU allocation, and repeats identical audio across streams. The results record about 11,032 MiB device memory usage. The loop times generation rather than full end-to-end ingestion, and does not finish streaming states in the concurrency sweep. They must not be transferred to this 3,072 MiB slice.

The restored gateway currently allows at most one ordinary HTTP batch request at a time through `batchActive` (`deployment/spot-worker/gateway/main.go:141`). Raising that admission limit is not a substitute for adding safe queuing/batching and measuring actual memory. A Qwen backend would need a compatible resident runtime and integration; it is not a model-file-only swap in NeMo-Speech.cpp.

## Recommended comparison

Use identical audio and vocabulary for:

1. Current streaming Nemotron baseline.
2. Existing engine's offline/file path, with verified runtime support.
3. Qwen3-ASR 0.6B with a supported quantization.
4. Qwen3-ASR 1.7B mixed precision, initially Q4 text / Q8 audio; Q5 text if memory permits.

Run short dictation, longer dictation, human technical speech, ordinary phrases, noise and accents. Score raw ASR first, then vocabulary precision/recall and final pipeline output separately. Measure peak memory through the longest permitted input, completed requests per hour, audio-seconds processed per GPU-second, sustained mixed-request queuing, and p50/p95/p99 stop-to-result delay. Distinguish quantization regressions from recognizer errors and Jev decisions.

If ASR starts only after recording stops, total delay includes upload tail, queueing, complete ASR execution, Jev and optional LLM. For illustration only, a recognizer running at 10x realtime needs about 3 seconds for a 30-second recording before subsequent steps. This is arithmetic, not a measured speed for our proposed deployment.

This assessment made read-only hardware/source checks. No new inference test, model download, API charge or deployment change was performed.
