# Vocabulary research and implementation feasibility

Research date: 16 October 2026. Recommendation: improve our existing RNNT decoder with conservative, cached phrase biasing, using TurboBias as the reference design. Keep complete-term normalization on the server. Acoustic word spotting is a useful alternative to investigate if decoder biasing cannot meet our false-positive target, but it requires another scoring path with our current model.

There is no single universal state-of-the-art winner across languages, models, vocabulary sizes and latency budgets. The important distinction here is best fit for an existing streaming RNNT model, all 1,000 user terms available every time, no additional LLM vocabulary prompt, and low compute overhead.

## Research findings

### GPU phrase biasing: the best fit for this engine

[TurboBias / GPU-PB, ASRU 2025](https://arxiv.org/html/2508.07014v1) represents phrases with an Aho-Corasick graph and adds graph scores during decoding, without retraining. It supports greedy decoding and preserves the unmodified blank/nonblank decision. The paper reports roughly 2–5% throughput overhead in its GPU implementation, including experiments with large lists up to 20,000 phrases. These are research results on its hardware and runtime, not a performance promise for our C++ implementation.

Particularly relevant: the authors identify corruption of non-vocabulary words in greedy decoding and recommend an off-graph/unknown score near the first graph-transition score. This reduces the advantage of entering a vocabulary phrase while retaining help farther along an established match. That is a promising inexpensive experiment for our false-positive problem.

[TurboBias 2.0, August 2026](https://arxiv.org/html/2608.21343v1) adds case-insensitive graphs accepting alternative BPE segmentations, independent per-stream graphs in a batch, caching and streaming beam search. It evaluates streaming RNNT models, including Nemotron. Its global per-stream vocabulary setting includes distractors; the local setting assumes knowledge of terms in the current segment and is less applicable to us.

Setup costs matter: in its 128-stream throughput experiment, caching/pre-registration greatly changes performance. The paper reports about 9–17% overhead with CPU-memory caching, and better results with decoder pre-registration. Its beam results use beam size 32. We should adopt the graph and caching ideas first; that beam configuration is not our initial low-compute choice.

[Current NeMo documentation](https://docs.nvidia.com/nemo/speech/nightly/asr/asr_customization/word_boosting.html) exposes case-insensitive BPE mode, per-phrase alpha, per-stream biasing and memory/disk/decoder caching. These belong to the Python/PyTorch NeMo stack. They are references for a C++ port, not settings that can simply be passed to our installed binary. Per-phrase weighting would let us assign less help to confusable terms without discarding any of the user's terms.

### Acoustic word spotting: stronger evidence, a less direct fit

[Streaming CTC-based Word Spotting, May 2026](https://arxiv.org/html/2605.18222v1) searches a vocabulary graph against CTC frame probabilities, retains partial matches across audio chunks, and compares detected spans with the original recognition. Its experiments use a hybrid CTC/RNNT model and lists of 687 and 1,107 phrases. On STOP1 RNNT, it reports 12.09% WER versus 12.21% for GPU-PB, with keyword F-score 88.67 versus 84.88; keyword precision decreases from 95.91% to 91.51%. Better recall does not mean fewer false positives.

At 560-ms chunks, its table reports total mean processing 61.9 ms versus 41.6 ms for CTC inference alone. The quoted 3.6% extra-processing figure is relative to audio duration, not a 3.6% increase in inference compute. We must preserve that denominator when assessing cost.

[FluidAudio's implementation](https://github.com/FluidInference/FluidAudio/blob/main/Documentation/ASR/CustomVocabulary.md) is a concrete open-source example: lexical candidate discovery, CTC scoring of candidate and original, timing alignment, and short-word/stopword guards. A hybrid model can reuse its encoder; a pure transducer needs a separate CTC encoder. Its documentation also tightens lexical gating for large distractor lists. Its scores, memory figures and Apple-device throughput should not be transferred directly to our CUDA/GGUF runtime.

This is distinct from text-only fuzzy replacement: acoustic probabilities provide evidence for both alternatives. It still needs calibration, and its output scores are not guaranteed calibrated probabilities.

## What users and implementers report

These are anecdotes and project-specific reports, not independent comparative benchmarks.

- [Reddit: 30 Days Testing Parakeet v3 vs Whisper](https://www.reddit.com/r/LocalLLaMA/comments/1nf10ye/30_days_testing_parakeet_v3_vs_whisper/): a later commenter claiming to develop Dictato describes FluidAudio CTC rescoring and persistent French false positives, including “si tu” becoming “SwiftUI.” They report tightening similarity and adding confidence/acoustic-advantage gates, but still keeping vocabulary boosting experimental. The comment promotes their own application; its benchmark claims are unverified here. The useful takeaway is the reported failure pattern, not their numerical claims or decoder terminology.
- [Reddit: Best Local TTS/STT Models, October 2025](https://www.reddit.com/r/LocalLLaMA/comments/1ohqev8/best_local_ttsstt_models_october_2025/): a Handy user reports custom vocabulary replacing correctly spoken generic words when using Parakeet. This does not isolate whether the cause is the model, application matcher or its settings, so it cannot establish a flaw in TurboBias.
- [FluidAudio issue #702](https://github.com/FluidInference/FluidAudio/issues/702) supplies a more actionable report: short terms such as CRAN and Snyk displace ordinary words despite threshold sweeps. Opt-in controls reduced reported over-fires, but the author later documents residual errors where the nearly unboosted CTC score already favors the wrong replacement. Acoustic verification helps; it does not guarantee correctness.
- [FluidAudio issue #841](https://github.com/FluidInference/FluidAudio/issues/841) documents a Nemotron RNNT implementation with word-start anchoring, case/segmentation-independent piece matching, avoidance of bare word-boundary and single-letter start candidates, and tracing of boosted token flips. It also reports harmful competition from garbled near-duplicate hints.
- [FluidAudio PR #866, merged 19 August 2026](https://github.com/FluidInference/FluidAudio/pull/866) upstreams that RNNT design, precomputing fresh-start candidates and using a trie for continuations. This is strong feasibility evidence for the technique on this model family; it is Swift/CoreML code rather than a compatible drop-in C++ patch. Its default boost scale is not interchangeable with ours.

I did not find substantial firsthand Reddit deployment reports specifically for TurboBias 2.0. The primary paper and maintained implementation are stronger evidence for that method; the Reddit discussions illuminate practical vocabulary failure modes.

## How our actual implementation differs

The audit used the S3 deployment source archive, not just current upstream documentation:

- Bucket: `speektome-inference-ACCOUNT_ID-us-east-1`.
- Source key: `build/nemo-source-v0.2.0.tar.gz`.
- Verified source archive SHA256: `0428c94c5a55c03e71880c3f58e28ba23a581f4420e7a76c524d1158d757e848`.
- Manifest source commit: `6a3ca369370782acee0dd155e34394ba60b920c4`.
- Archived RNNT decoder SHA256: `10f85c437537511c8ecafd06648d897756a4b3a74683838a11d2f7422fc24cac`.
- Archived graph implementation SHA256: `f9a1f226fa9f02c42b4ec4357ab6609cbe2094c3e0141881621234f55137874d`.
- Those two source files are byte-identical to the inspected local external engine checkout, although its overall Git HEAD differs.
- Local configured model SHA256 matches the S3 manifest: `7ece4ce06d9f5496d5a7e1dade556a5646ce25f84f828f7a8805bb1db3e27792`. Metadata: `asr.head_type=rnnt`, `asr.rnnt.vocab_size=13088`, 657 tensors, no tensor names containing `ctc`. The model is not exposing a usable CTC head in this runtime.

| Component | Current behavior | Proposed direction |
|---|---|---|
| Server matcher | Text-only phonetic/fuzzy replacement, including partial stems | Complete-term normalization; preserve uncertain originals |
| RNNT graph | Already Aho-Corasick with depth-dependent scoring and partial-match backoff | Retain the method; add conservative entry scoring |
| Phrase tokenization | One SentencePiece encoding per phrase | Case-insensitive alternative segmentation graph |
| Phrase weights | Highest request boost becomes one alpha for every phrase | Correct independent phrase weights, with reduced boost for risky entries |
| Graph setup | Rebuilt in `set_request_options` per decoder/request | Immutable bounded cache keyed by model/tokenizer, vocabulary version and scoring config |
| Runtime graph lookup | CPU hash tables and fresh allocations in `step_boost` | Precomputed root candidates and allocation-free traversal; bounded lazy state caching |
| GPU selection | Separate unboosted and boosted joint-network calls | Compute joint logits once and derive both selections from them |
| Blank handling | Unboosted logits control blank/nonblank | Preserve this behavior |
| Gateway limits | 512 phrases / 32,768 text bytes | Explicit support for at least 1,000 complete entries with bounded bytes |

Source anchors in the archived engine: `src/asr/decoders/rnnt_greedy_decoder.cpp:58–104,247–321`, `src/asr/decoders/context_biasing.cpp:100–120`, and `src/asr/model.cpp:298–412,1911–1939`. Application anchors: `server/vocab/vocab.go:333`, `server/pipeline/pipeline.go:569`, `deployment/spot-worker/gateway/vocabulary.go:32`.

The second joint call repeats decoder joint computation; it does not repeat the audio encoder. Boosting also bypasses the current fused predictor/joint fast path. Consequently, “small graph” alone does not establish small total overhead. The optimization must return both token decisions from one GPU graph, rather than downloading all logits and introducing CPU/GPU synchronization.

Another configuration trap: in this decoder, a nonempty phrase list with every request boost set to zero falls back to alpha 1.0. Disabling a request currently requires an empty context list; setting the global decoder alpha to zero also disables biasing. A future independent-weight implementation should make zero-weight semantics explicit and test them.

Server corrections currently enter a protected-vocabulary list which the enhancement prompt says not to change. If we later implement sparse before/after candidate selection, tentative fuzzy candidates must not receive that unconditional protection.

## Local feasibility probe: 1,000 phrases

Added `benchmark/vocabulary/graph_cost_probe.cpp`, compiled against the verified archived graph source with `g++ -O3 -std=c++17`. Results are in `graph-cost.jsonl`. Host: AMD Ryzen 5 5600, one process. Synthetic five-token phrases; two deliberately different prefix shapes. Builds: 31 iterations. Step timings: median of five repeated batches of at least 14,000 calls. Cache equality checked for every visited state.

| 1,000-phrase graph shape | Median graph build | Median uncached score lookup | Eager cache pair payload |
|---|---:|---:|---:|
| 32 shared initial tokens | 0.386 ms | 1.14 µs | 1.06 MB |
| 1,000 distinct initial tokens | 0.431 ms | 38.22 µs | 40.04 MB |

This supports implementing graph bookkeeping without a large neural model. It also shows why eagerly caching every state is a poor general solution: copying root candidates into thousands of states can consume substantial memory. Prefer shared root storage, bounded lazy caches and per-stream match state over copying all scores per stream. The cached lookup experiment consumes only the already-built result's base and size, so it is not a full scoring benchmark.

This probe excludes phrase tokenization, dense bias construction, GPU transfer, joint inference, concurrency, network latency and audio recognition. It uses synthetic token IDs, not a representative user dictionary. It does not establish accuracy or total latency at 1,000 phrases.

Reproduce after extracting the verified S3 source into a directory:

```sh
g++ -O3 -std=c++17 -I "$SOURCE_DIR/src/asr/decoders" \
  benchmark/vocabulary/graph_cost_probe.cpp \
  "$SOURCE_DIR/src/asr/decoders/context_biasing.cpp" -o /tmp/graph_cost_probe
/tmp/graph_cost_probe
```

## Implementation recommendation and validation boundary

1. Keep all 1,000 user terms in the bias graph. Use structured session configuration, rather than a long comma-delimited URL. Raise and test gateway limits; include punctuation, Unicode, duplicates, updates and explicitly disabled vocabulary.
2. First port conservative entry scoring, boundary handling and diagnostics behind a feature flag. Sweep unknown scores near the first-token context score; independently measure how this affects rare-term recall. Do not adopt another runtime's numeric boost defaults.
3. Cache the immutable graph by vocabulary version/content plus tokenizer/model and scoring settings. Keep active match state separate for each stream and across chunks. Invalidate correctly on vocabulary edits; bound cache size across users.
4. Fuse unbiased and biased selection over the same joint logits, preserving blank decisions and GPU batching. This requires a C++ engine build and deployment artifact, not only gateway changes. Record original token, selected token, unboosted score gap, applied bonus and graph state in opt-in diagnostics. Scores are diagnostic margins, not calibrated confidence.
5. Add case-insensitive/alternative-segmentation graphs and genuine per-phrase weights after the basic path is measured. Shared prefixes require consistent graph weights/backoff; simply changing the existing scores vector is not sufficient to reproduce per-phrase alpha correctly.
6. Retain exact complete-term server normalization. If uncertainty needs the existing enhancement model, send only structured candidate spans with keep-original as an option; do not send the complete vocabulary.

The first acceptance experiment should compare no bias, current boost 0.5, conservative-entry 0.5, and the fused equivalent, all with the identical full 1,000-term list and identical audio. Include independent human technical speech, ordinary speech containing no target terms, near-homophones, short acronyms, noise, accents and phrases crossing chunk boundaries. Measure keyword precision/recall, false vocabulary substitutions, overall and non-keyword WER, graph setup/cache churn, CPU/RAM/VRAM, and p50/p95/p99 streaming delay at sustained concurrency. Separate startup from steady state. Require nonempty completed transcripts and commit acknowledgments.

The earlier 53-stream benchmark used 184 terms, not 1,000. Boost 0.5 plus exact normalization remains a candidate baseline, not a validated 1,000-term optimum. No method here can guarantee zero false positives, especially for true homophones such as a brand “Snyk” versus ordinary “sync.” More acoustic evidence cannot reliably resolve acoustically identical alternatives without additional context.

This research added only the isolated cost probe and report. It did not change the production matcher, decoder binary, gateway limits, model, or fleet settings.
