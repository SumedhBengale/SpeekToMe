The production path is plain Nemotron 3.5 ASR, a final text matcher on the inference worker, one batched TypeSafe Jev evaluation on the main server, then grammar enhancement.

Native fleet WebSockets send `session.vocabulary_matching.terms` once, before audio. The snapshot supports up to 1,000 terms. The gateway consumes this configuration and sends empty `speech_contexts` to Nemotron; vocabulary and user prompts do not bias ASR decoding. Intermediate previews remain raw ASR. Automatic utterance endings accumulate final timestamped words but do not run the matcher. An explicit final commit flushes the remaining PCM, finishes ASR, and triggers matching after the native commit acknowledgement. The pinned runtime emits its last completed transcript before that acknowledgement.

The worker returns a `vocabulary.final` JSON event containing:

- `schema_version`, `matcher_version`, `raw_transcript`, and `vocabulary_transcript`;
- SHA-256 hashes of the immutable raw transcript and vocabulary snapshot;
- `choices`, each with an occurrence ID, UTF-8 `start_byte`/`end_byte`, exact original text, proposed replacement, similarity score and match type;
- worker matching time and an explicit matcher error, if matching could not complete.

Offsets refer to the original transcript, including its original punctuation and whitespace. Edits cannot overlap. Matching preserves sentence and speaker-line boundaries. The final pass checks the entire transcript; fuzzy matching is no longer restricted to the last twelve words. It uses the existing indexed phonetic/SymSpell and Jaro-Winkler approach, extracted into the standard-library-only `shared/vocabulary` module. The score is string similarity, not an ASR probability. Already canonical occurrences are recorded for grammar protection but do not need a Jev decision.

Each native worker keeps only the committed transcript, compiled dictionary and checkpoint metadata for its connection. On recovery, the main server supplies the confirmed prefix, confirmed audio position, replay offset and the same dictionary. Timestamped overlap is discarded before appending fresh words. The main server validates the returned transcript/hash, vocabulary hash, matcher version, edit locations, canonical membership and reconstructed enhanced text before using the result. Worker connections are released before Jev or the grammar LLM runs.

Legacy/batch providers and completed merged meeting transcripts use the authenticated `/v1/vocabulary/final` worker RPC. That RPC can use another available worker. The durable meeting path currently evaluates the merged final transcript after ASR and CPU diarization are available; it does not retain a GPU lease while waiting for CPU diarization. The same-worker, immediate post-ASR behavior applies to native fleet WebSockets. Existing deployed fleet recovery and durable CPU-diarization code was restored into this checkout before applying the vocabulary change.

Jev receives only proposed changes and transcript context, never the user's whole dictionary. Each occurrence gets an independent two-option Choice question. Short transcripts include shared full context; long transcripts use surrounding excerpts. Questions show the original and candidate readings explicitly. All actual changes are evaluated in one POST to `https://api.typesafe.ai/v1/systemone`. The default model is pinned to `jev-1.13.0`.

| Main-server setting | Default | Behavior |
| --- | --- | --- |
| `TYPESAFE_API_KEY` | Required for validation | Stored only in the local server and deployed main-server `.env` files |
| `TYPESAFE_MODEL` | `jev-1.13.0` | Override deliberately after evaluating another model |
| `TYPESAFE_TIMEOUT_MS` | `400` | Entire HTTP evaluation, including network time; allowed range 25–2,000 ms |
| `TYPESAFE_MAX_CONCURRENT` | `16` | No waiting for a slot; excess requests fall back |
| `TYPESAFE_RPS` | `0` | Optional local request limit; 0 leaves enforcement to the API/cooldown |
| `TYPESAFE_TPS` | `0` | Optional conservative local guard using request bytes as a token upper bound |

There are no Jev retries. Timeout, network errors, 429/529, other HTTP errors, missing/invalid answers, local capacity limits and oversized batches select the corresponding matcher choices and continue. A 429/529 cooldown skips future evaluations instead of sleeping the current request. Missing answers can fall back individually while valid answers are retained. Account limits are organization-specific; set the optional local guards from the TypeSafe console. The current batch budget is a conservative 48,000 UTF-8 JSON bytes, with no long-context mode enabled. This is intentionally below the vendor's token limits and can cause large batches to fall back earlier than the API would require.

The grammar LLM receives the resolved sentence with immutable placeholders for recorded spans. It receives no alternative vocabulary choices or full dictionary. Missing, duplicated or reordered placeholders reject the grammar result and restore the resolved transcript. This also protects already canonical terms and rejected originals. The deliberate tradeoff is that grammar inside those protected spans is immutable. The owned streaming route always runs grammar enhancement; legacy batch requests retain their existing enhancement switch.

Final streaming responses include `vocabulary` audit metadata. Usage history persists the untouched ASR in `input_transcript`, final output, worker proposals, every occurrence's selected option/source/confidence, Jev latency/model and fallback reasons. Meeting records persist the equivalent audit. The existing desktop Serde decoder accepts the additional response field; displaying individual decisions in the desktop UI is not part of this change.

Bounds are 256 bytes per term, 128 KiB for the dictionary, and 2 MiB for final transcript text. Existing invalid dictionary snapshots are kept out of native worker configuration so they cannot prevent ASR; the final audit reports the vocabulary error. A final matching pass has a one-second CPU deadline. Native final-result compatibility waiting is bounded at 1.5 seconds. A missing worker result can use the final-text RPC and then the identical matcher on the main server as a last fallback.

Fallback is a latency policy, not contextual validation: false replacements can survive whenever Jev is skipped. Jev itself can also make wrong choices, particularly when two names fit the same context. The matcher supplies one candidate per occurrence, so a correct lower-ranked alternative is not reconsidered by Jev. A long meeting with enough proposals to exceed the batch budget needs fallback under the current one-request rule.

The four active launch regions are `us-east-1`, `us-east-2`, `us-west-2` and `ca-central-1`. They share the artifact bucket `speektome-inference-ACCOUNT_ID-us-east-1`. `publish-vocabulary.py` publishes content-addressed amd64/arm64 gateways, the readiness helper, manifest, bootstrap, regional user-data snapshots and fleet configuration, and verifies S3 checksums. Update the live controller configuration to the generated fleet file after worker validation. Historical immutable artifacts remain available for rollback. Both current workers were updated in place, and every active regional boot reference now pins the same manifest. The readiness helper explicitly tests ASR without GPU diarization.

Main-server rollback backups are `/home/ec2-user/speektome/speektome-server.before-jev-20261016` and `/etc/speektome-fleet/fleet.before-jev-20261016.json`. Worker backups are `/opt/speektome-worker/bin/gateway.before-jev-20261016`. Restart the main service after rotating the Jev key.

Protocol and limits: [TypeSafe API](https://docs.typesafe.ai/api), [TypeSafe models and context limits](https://docs.typesafe.ai/models). Current validation evidence is in [the test report](../../benchmark/vocabulary/results/2026-10-16-jev/REPORT.md).
