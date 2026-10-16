Vocabulary pipeline validation, 16 October 2026.

Implemented and deployed: plain Nemotron 3.5 streaming ASR → final worker-side vocabulary proposals → one batched Jev evaluation → grammar enhancement. The Jev key is installed in both requested `.env` files with mode 0600. No key is included in source, logs or S3 boot artifacts.

The final immediate-stop acceptance run completed 6/6 clips with a 1,000-term dictionary. Audio was sent from this PC through an SSH tunnel to the actual application streaming handler on the main server, using an isolated SQL database and test account. The handler used the live fleet, worker matcher, real Jev API and configured grammar LLM. Production user accounts, quotas and transcripts were not used. The deployed main service uses the tested handlers; a later unit-tested guard prevents invalid legacy dictionary entries from breaking native ASR.

Each Opus frame was sent after its capture-duration interval, and stop was sent immediately after the last frame. Stop-to-final time starts immediately before sending stop and ends when the final response arrives, before fetching history. This includes this PC’s network path, ASR finalization, vocabulary evaluation, grammar enhancement and persistence. It is not a model-only timing.

Observed stop-to-final latency: **748–943 ms**, median 842 ms, n=6. Jev took **55–89 ms** across the five clips with actual changes to evaluate; the other clip skipped Jev. Every clip used the inference-worker proposal, with no Jev fallback or grammar-protection fallback.

| Clip | Vocabulary result | Stop → final |
| --- | --- | --- |
| ordinary-phrase | Kept ordinary “speak to me”; rejected SpeekToMe. | 869 ms |
| named-app | Changed “speak to me” to SpeekToMe in app-installation context. | 748 ms |
| database | Changed “postgrade SQL” to PostgreSQL. | 815 ms |
| holdout-00 | Changed redist → Redis and postgray SQL → PostgreSQL; protected already-correct Grafana. | 943 ms |
| holdout-04 | Preserved ordinary text; no proposed changes. | 790 ms |
| holdout-07 | Rejected celery → Celery, flask → Flask and team → Teams; grammar fixes remained possible around the protected words. | 873 ms |

The response and persisted history were checked for identical raw ASR, final output and audit metadata, including every occurrence’s selected option and source. There were eight actual vocabulary decisions and one already-canonical protected occurrence. Earlier text-only mixed-context probes also kept one “speak to me” occurrence while correcting a second occurrence in the same transcript.

These are six controlled synthetic speech clips, not a broad human-speech accuracy evaluation or a production p95/capacity test. Some ASR errors remain, such as “We install” for a past-tense reference. The results establish the tested vocabulary distinctions and pipeline behavior; they do not establish a general WER improvement or a sub-second SLA. The earlier run with a pre-stop pacing pause is saved separately in `acceptance-live-paced-drain.jsonl` and is excluded from these latency numbers.

The current worker matcher processed a synthetic **9,000-word transcript with 1,000 terms and 1,000 proposals in 43.8–47.9 ms**, n=6, on the us-east-1 g6f.large worker. Whole final-text RPC time, including dictionary compilation, TLS, serialization and transfer from the main server, was 69.4–82.0 ms. Native streams compile their dictionary before audio processing. This benchmark measures CPU text matching only, not an hour of audio or Jev validation of 1,000 proposals. That many proposals exceed the current Jev request budget and would use the documented fallback.

The matcher adds no GPU model. Both workers passed a real ASR-only final-transcript/commit smoke test, retain the pinned Nemotron model SHA-256 `7ece4ce06d9f5496d5a7e1dade556a5646ce25f84f828f7a8805bb1db3e27792`, and reported roughly 1.6 GiB GPU process memory afterward. This is an idle/smoke snapshot, not peak loaded VRAM.

Local verification passed: full server Go tests and vet; gateway tests with the race detector; shared matcher tests with the race detector; targeted server race tests for services, handlers, decisions, persistence and grammar. Tests cover whole-transcript matching, Unicode byte offsets, sentence/speaker boundaries, recovery prefix overlap, commit/result ordering, invalid metadata, per-occurrence choices, immutable grammar spans, invalid legacy dictionaries, timeout, 429/529, missing answers, local concurrency and oversized-batch fallback. Deliberate Jev failures were simulated locally; the live API was not intentionally rate-limited.

The source diff also restores the deployed fleet recovery and durable CPU-diarization baseline absent from this worktree. The native streaming path was exercised with real audio. Merged durable meetings and legacy/batch finalization use the worker text RPC; a live diarized meeting was not exercised in this validation. Desktop response compatibility was checked in the existing Serde decoder; no desktop decision-detail UI was added.

Both existing workers are updated and ready: `i-REDACTED` in us-east-1 and `i-REDACTED` in us-west-2. The main API and controller are running. No additional inference instances were launched. All four regions’ active boot references point to the same checksum-verified release in the shared us-east-1 artifact bucket. S3 readback verified the manifest, bootstrap, fleet configuration and all four regional user-data objects.

Worker release manifest: `deployment/fleet-v1/911f21a71027667375056b572bd27d031963bceb0e635fb9f42d60c100d235ad/fleet-manifest.json`.
Worker gateway SHA-256: `d441a8f26a5c723cd11da98c1e710f9834983d1ff1e4dba8e716c3558c43541b`.

The main remaining tradeoff is explicit: timeout, rate-limit, unavailable/malformed decisions and oversized batches accept the matcher’s choices. False replacements can survive that fallback. The conservative 48,000-byte Jev batch guard can skip validation earlier than the vendor’s token limits require. Grammar placeholders preserve chosen spans literally, including rejected originals, so grammar within those spans is intentionally restricted. Jev context alone cannot guarantee disambiguation between two plausible names.

Implementation and operating details: [VOCABULARY.md](../../../../deployment/spot-worker/VOCABULARY.md). API contract and vendor limits: [TypeSafe API](https://docs.typesafe.ai/api), [TypeSafe models](https://docs.typesafe.ai/models).

Artifacts: `acceptance-live.jsonl`, `jev-live-v2.jsonl`, `gateway-live.jsonl`, `hour-worker-benchmark.json`, `worker-binary-verification.json`, `s3-readback-verification.json`, `live-deployment-verification.json`, `regional-instance-verification.json`, `published-artifacts.json` and `main-artifacts.json`.

Final main-server binary SHA-256: `3e6d7467e802563bb048b99d7aebd3cf9ec15a878215495e10790b6542fa085d`.

The 1,000-term fixture contains 184 technical/name entries (including SpeekToMe) and 816 generated AcceptanceService entries. It tests dictionary size and protocol behavior; it is not an accuracy evaluation of 1,000 natural vocabulary items. Earlier canary/calibration files retain prior matcher iterations. `acceptance-live.jsonl` and `hour-worker-benchmark.json` contain the final matcher-v2 results.
