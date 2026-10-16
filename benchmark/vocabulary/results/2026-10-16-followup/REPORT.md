# Vocabulary follow-up: 16 October 2026

Recommended starting configuration: inference boost 0.5 with complete-term exact server normalization, using session-relevant vocabulary. The fixed 35-term engineering list slightly improved the calibration result, but did not improve the new recordings. The data does not establish a universal optimum.

## Complete comparison

53 test streams, 1,229 reference words, 57 technical-term occurrences, and 478.161 seconds of unique audio. Fifteen configurations were evaluated. Lower word error rate (WER) is better. “Worsened” compares each clip against its identical unboosted ASR transcript; it counts an increase in word errors.

| Configuration | Word errors | WER | Target terms / 57 | Clips worsened / 53 | Clips with new unspoken terms / 53 |
|---|---:|---:|---:|---:|---:|
| Unboosted ASR; no server correction | 69 | 5.61% | 29 | 0 | 0 |
| Server only: existing phonetic/fuzzy matcher | 51 | 4.15% | 50 | 13 | 14 |
| Server only: existing matcher, 35 terms | 42 | 3.42% | 50 | 6 | 6 |
| Server only: complete-term exact normalization | 49 | 3.99% | 39 | 0 | 0 |
| Server only: exact normalization, 35 terms | 49 | 3.99% | 39 | 0 | 0 |
| Server only: exact + guarded one-edit correction | 48 | 3.91% | 40 | 0 | 0 |
| Server only: exact + four explicit aliases | 45 | 3.66% | 43 | 0 | 0 |
| Inference 0.5; no server correction | 60 | 4.88% | 34 | 0 | 0 |
| Inference 0.5 + existing matcher | 51 | 4.15% | 50 | 13 | 13 |
| Inference 0.5 + exact normalization | 42 | 3.42% | 43 | 0 | 0 |
| Inference 0.5 + exact + guarded correction | 41 | 3.34% | 44 | 0 | 0 |
| Inference 0.5 + exact + four aliases | 38 | 3.09% | 47 | 0 | 0 |
| Inference 0.25 + exact normalization | 45 | 3.66% | 41 | 0 | 0 |
| Inference 0.75 + exact normalization | 44 | 3.58% | 43 | 2 | 0 |
| Inference 0.5 with 35 terms + exact normalization | 41 | 3.34% | 44 | 0 | 0 |

The alias variant has the lowest combined WER, but all its advantage over exact-only normalization came from the original calibration recordings that supplied the aliases. It produced no further improvement on the new human or new synthetic cohorts. Guarded typo correction also helped only the calibration set. Those results do not justify treating either variant as a broadly better default.

## Calibration versus new recordings

The original calibration set contains the earlier 36 synthetic recordings and JFK recording. The new human cohort is independent of alias selection. The new synthetic cohort uses unseen sentences and a new voice; it also was fixed before viewing its ASR outputs.

| Configuration | Original 37 clips WER | New human 8 streams WER | New synthetic 8 clips WER | New human worsened | New synthetic worsened |
|---|---:|---:|---:|---:|---:|
| Unboosted ASR; no server correction | 8.72% | 2.71% | 10.00% | 0 | 0 |
| Server only: existing phonetic/fuzzy matcher | 4.36% | 3.02% | 9.23% | 2 | 3 |
| Server only: existing matcher, 35 terms | 3.44% | 2.71% | 6.92% | 0 | 2 |
| Server only: complete-term exact normalization | 5.05% | 2.71% | 6.92% | 0 | 0 |
| Server only: exact normalization, 35 terms | 5.05% | 2.71% | 6.92% | 0 | 0 |
| Server only: exact + guarded one-edit correction | 4.82% | 2.71% | 6.92% | 0 | 0 |
| Server only: exact + four explicit aliases | 4.13% | 2.71% | 6.92% | 0 | 0 |
| Inference 0.5; no server correction | 6.65% | 2.71% | 10.00% | 0 | 0 |
| Inference 0.5 + existing matcher | 4.36% | 3.02% | 9.23% | 2 | 3 |
| Inference 0.5 + exact normalization | 3.44% | 2.71% | 6.92% | 0 | 0 |
| Inference 0.5 + exact + guarded correction | 3.21% | 2.71% | 6.92% | 0 | 0 |
| Inference 0.5 + exact + four aliases | 2.52% | 2.71% | 6.92% | 0 | 0 |
| Inference 0.25 + exact normalization | 4.13% | 2.71% | 6.92% | 0 | 0 |
| Inference 0.75 + exact normalization | 3.90% | 2.87% | 6.15% | 1 | 1 |
| Inference 0.5 with 35 terms + exact normalization | 3.21% | 2.71% | 6.92% | 0 | 0 |

On the new 793-word cohorts, server-only exact correction and inference 0.5 + exact correction tied at 27 errors (3.40% WER). The added benefit of 0.5 over server-only exact correction was confined to the original set. Its use as a technical-dictation default is provisional; broader human technical speech would be needed to establish that benefit independently.

## Confirmed failure modes

- Existing matcher on a human recording: “going just the same” became “going just the SAML.”
- Existing matcher on a second human recording: “deck of the frigate” became “deck of the Fargate.”
- Existing matcher worsened 13 of 53 clips. Reducing its vocabulary to 35 terms cut this to 6; partial-word/fuzzy corrections remained.
- Inference 0.5 + the unchanged existing matcher still worsened 13 clips. Lowering inference boost alone does not repair server matching.
- Inference 0.75 + exact correction worsened 2 clips: one human clip and one new synthetic technical sentence. In the latter, “Redis” became “RES.”
- Inference 0.5 + exact correction, scoped 0.5 + exact correction, and server-only exact correction introduced no additional word errors in these 53 clips. This is an observed result, not a guarantee.

## Meaning of the server policies

The existing server matcher has similarity thresholds, not a boost parameter. The number 0.5 controls inference boosting. Server-only conditions send no vocabulary to ASR and apply their selected matcher on the main server afterward.

- **Existing:** the repository production VocabCache.ProcessText, including phonetic matching, fuzzy matching, and compound-word stem expansion.
- **Exact:** complete canonical terms and complete CamelCase/separator/symbol expansions. For example, “fast API” can become FastAPI and “py torch” can become PyTorch, but “torch” alone cannot become PyTorch. It preserves surrounding punctuation and whitespace and does not cross punctuation boundaries. Conflicting forms remain unchanged.
- **Guarded:** exact plus a unique one-edit correction for a whole single-token canonical term at least eight characters long, with input at least seven characters long. No phonetic or stem matching.
- **Aliases:** exact plus four explicitly supplied mappings: graphana -> Grafana, postresql -> PostgreSQL, postgress -> PostgreSQL, pitorch -> PyTorch. They were derived from the earlier calibration run and frozen before new recordings were tested. They are benchmark examples, not automatically learned or enabled user aliases.

## Workload, evidence and limitations

- Original recordings: 37 clips, 173.2 seconds, 436 words, 44 technical-term occurrences. Existing unboosted and boost-0.5 ASR transcripts were reused byte-for-byte for paired server-policy comparisons. New 0.25, 0.75 and 35-term inference conditions were run live.
- New human recordings: [OpenSLR LibriSpeech test-clean](https://huggingface.co/datasets/openslr/librispeech_asr), CC BY 4.0; fixed row offsets 0, 200, 400 and 600, eight consecutive utterances each. Four speakers: 6930, 260, 3575 and 8463. Dataset revision and all source IDs are recorded in human-source.json.
- The 32 human utterances were grouped four at a time into eight streams, with 250-ms synthetic silences between utterances. Streams range from 11.8 to 64.1 seconds; total 248.545 seconds and 663 reference words. They are stitched audiobook speech, not uninterrupted conversations or meetings. References come from the dataset, never from the tested model.
- New synthetic recordings: eight unseen texts spoken by en-IN-NeerjaNeural, four technical and four ordinary-word controls; 130 words and 13 target-term occurrences. Text and voice differ from the original calibration set. Scripted TTS references are useful for controlled comparisons but do not establish human pronunciation accuracy for technical names.
- Main matrix: 265 successful ASR rows (five conditions per clip), consisting of 191 new gateway requests and 74 reused original transcripts. Each new request required nonempty transcript text and an input_audio_buffer.committed acknowledgment.
- Main ASR input uses mono 16-kHz PCM16 in 560-ms chunks sent 40 ms apart. The real-time confirmation file separately records selected 560-ms-cadence trials.
- All five ASR variants for a given cohort used the same worker and the same audio SHA256: original/new-human trials on us-east-1 worker i-REDACTED; new synthetic trials on us-west-2 worker i-REDACTED. Workers use the same pinned engine and patched gateway.
- Full vocabulary: 184 phrases. Narrow vocabulary: a fixed 35-term engineering list, selected from the existing technical context, never selected from each new reference or ASR output. Inference-scoped mode narrows ASR hints; server normalization still sees the full vocabulary. Explicit server-narrow modes narrow the server list instead.
- All server-policy transformations ran in an isolated compiled harness on the main ARM server. No user database, application enhancement, or authenticated desktop session is involved. This evaluates final-text correction, not interim rendering or the complete deployed application route.
- Overall WER is case-insensitive and punctuation-normalized. Target recovery requires the full normalized canonical term. Newly introduced unspoken-term detection is a lower bound: it ignores capitalization-only changes and misses partial corruptions that are not a complete vocabulary term.
- No confidence intervals or statistical significance claims are made. Small one-word differences between conservative variants are not strong evidence of superiority. New human recordings contain ordinary literary speech, not human technical dictation; performance on accents, noise, spontaneous speech and real meetings remains unestablished.

## Code and deployment state

- New opt-in policies: server/vocab/conservative.go, with regression tests covering ordinary words, complete term spans, punctuation/whitespace, Unicode terms, alias scoping, and ambiguous typo candidates.
- server/cmd/vocab-eval accepts legacy, exact and guarded modes and explicit alias maps. Existing production handlers continue calling the original VocabCache.
- Harness installed separately as /home/ec2-user/speektome/vocab-eval-policies on the main server; SHA256 2129de299ba797ae37771182d47ac7a188dde1b4fab373bf1468359c8809e5cb.
- Server go test ./... passed. Targeted race tests and go vet passed after the final policy implementation.
- Main application defaults and binary were not changed during this follow-up. The earlier gateway forwarding deployment remains in place. Main server and both inference workers remain running.

## Reproduction

prepare_human.py downloads the fixed public dataset sample and creates explicitly stitched streams. prepare_holdout.py creates the fixed new synthetic texts. followup.py inference resumes original/new-human trials; run_holdout.py writes separate new-synthetic trials. After combining their rows into inference.jsonl, followup.py score evaluates the server policies on the main server. followup_realtime.py runs the separate real-time confirmation. write_followup_report.py regenerates this report.

Saved inputs and results: corpus.json, human-source.json, vocabulary.json, engineering-vocabulary.json, aliases.json, inference.jsonl, inference-heldout.jsonl, scored.jsonl, summary.json, combined-summary.json, realtime-confirmation.jsonl and the generated audio. Credentials remain outside the repository.

## Real-time confirmation

| Clip | Inference boost | Complete-term exact output | Existing matcher output |
|---|---:|---|---|
| human-0600-0 | none | it will take us, but we're going just the same | it will take us, but we're going just the SAML |
| human-0600-0 | 0.5 | it will take us, but we're going just the same | it will take us, but we're going just the SAML |
| human-0600-4 | none | baggage was immediately carried to the deck of the frigate. I rushed aboard | baggage was immediately carried to the deck of the Fargate. I rushed aboard |
| human-0600-4 | 0.5 | baggage was immediately carried to the deck of the frigate. I rushed aboard | baggage was immediately carried to the deck of the Fargate. I rushed aboard |
| holdout-00 | none | When the checkout service slows down, Grafana displays the Redis latency next to the postgray SQL connection count | When the checkout service slows down, Grafana displays the Redis latency next to the PostgreSQL connection count |
| holdout-00 | 0.5 | When the checkout service slows down, Grafana displays the Redis latency next to the postgray SQL connection count | When the checkout service slows down, Grafana displays the Redis latency next to the PostgreSQL connection count |

These trials reuse the same audio with a 560-ms producer cadence. They are confirmations, excluded from the main accuracy denominators.
