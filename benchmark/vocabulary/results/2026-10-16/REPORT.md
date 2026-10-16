# Vocabulary accuracy test: 16 October 2026

Both inference boosting and the main-server matcher can replace genuine words with vocabulary terms. Boost 0.5 was the most conservative tested inference setting; it did not introduce additional unspoken terms or worsen word error counts in the primary paired set. This is a small controlled screening result, not a production accuracy guarantee.

## Paired accuracy results

Each condition uses the same 37 recordings, 436 reference words, and 44 target-term occurrences. All vocabulary-enabled conditions receive the same 184 phrases. AI enhancement is excluded. Lower word error rate (WER) is better.

| Mode | Word errors / 436 | WER | Target terms recovered / 44 | Clips worse than baseline / 37 | Clips with newly introduced unspoken vocabulary / 37 |
|---|---:|---:|---:|---:|---:|
| ASR without vocabulary | 38 | 8.72% | 23 | 0 | 0 |
| Inference only, boost 0.5 | 29 | 6.65% | 28 | 0 | 0 |
| Inference only, boost 1.5 (existing application setting) | 24 | 5.50% | 33 | 2 | 2 |
| Inference only, boost 3.0 (stress setting) | 50 | 11.47% | 38 | 14 | 17 |
| Unboosted ASR + server matcher | 19 | 4.36% | 40 | 8 | 9 |

The unspoken-vocabulary detector counts case-insensitive whole terms absent from both the reference and the corresponding unboosted transcript. It is a lower bound: partial corruptions such as `gRPCANA` do not count as the complete term `gRPC`. Capitalization-only changes are excluded. “Clips worse” means more word errors, so an equally wrong but more distorted spelling may also escape that metric. Target recovery requires the complete normalized canonical term, ignoring capitalization and punctuation; split spellings such as “fast API” do not equal “FastAPI”.

## Evidence of unwanted substitutions

| Spoken reference | Mode | Actual output excerpt |
|---|---|---|
| Please read this document | Inference 1.5 | Plaid read this document |
| monitor Redis using Grafana | Inference 1.5 | monitor Redis using gRPCANA |
| Our Python service uses | Inference 3.0 | Our PyTorch SAML uvises |
| She opened the blue cupboard and put the clean plates | Inference 3.0 | Sentry opened the glue cupboard and pnpm the clean pnpm pnpm pnpm Shelf |
| And so my fellow Americans | Inference 3.0, human recording | And SAML Americans |
| near the old docks | Server matcher | near the old Docker |
| carrying the torch | Server matcher | carrying the PyTorch |
| before I opened Redis | Server matcher | before I OpenID Redis |

The existing boost of 1.5 improved total WER but damaged both “Please read this” controls. The confuser subset is 12 clips / 132 words: unboosted and boost 0.5 each had 2 word errors; boost 1.5 had 5; boost 3.0 had 14; the server matcher had 6. The main matcher has the best overall target recovery, but the overall WER hides those ordinary-word substitutions.

## Workload and isolation

- 36 synthetic recordings: 18 scripted sentences, each spoken by en-US-AriaNeural and en-GB-RyanNeural through Edge TTS. Categories include technical targets, similar-sounding ordinary words, neutral speech, and mixed technical/ordinary sentences.
- One local archival JFK human recording with its known spoken reference.
- Total unique audio: 173.2 seconds. WAV PCM16, mono, 16 kHz. The same audio bytes are reused across all four primary ASR conditions. Per-request SHA256 is saved.
- 148 primary inference calls, 9 additional route checks, 4 recorded mic/system checks, and 18 real-time repeat calls, plus 2 cross-worker confirmation calls all completed with nonempty transcripts and commit acknowledgments (recorded calls additionally returned timestamped words).
- Primary inference calls originate on the local device through an authenticated SSH tunnel to the us-east-1 worker gateway, bypassing main-server vocabulary correction and enhancement. Primary PCM chunks are 560 ms, sent 40 ms apart; transcript collection continues until the explicit commit acknowledgment.
- Selected baseline/0.5/1.5 comparisons were repeated twice with a 560 ms input cadence. Both repeats retained the baseline/lower-boost “Please read this” and reproduced “Plaid read this” at 1.5; both reproduced `gRPCANA` at 1.5.
- Second-worker confirmation: the us-west-2 worker returned “Please read this document” without vocabulary and “Plaid read this document” at boost 1.5 on the identical audio.
- Server comparison: the repository production `vocab.ProcessText` implementation was compiled into an isolated harness and executed on the main ARM server, using the exact unboosted transcripts and same vocabulary. No user database/session, application enhancement, or second ASR run is involved. This measures the final-text matching step, not the complete authenticated desktop request path or interim text behavior.
- These short, mostly synthetic clips do not establish accuracy for spontaneous speech, accents beyond the two synthetic voices, noise, meetings, or long-session behavior. No confidence interval or broad production guarantee is claimed.

## Gateway changes and verification

The production gateway source was recovered from the local prior deployment archive because this checkout had removed that deployment directory. The restored module and changes are in `deployment/spot-worker/gateway`.

- All four WebSocket aliases translate URL `boosted_words` and optional `vocabulary_boost` into native `session.update.speech_contexts`. The native JSON query form `speech_contexts` is also supported.
- Existing native session contexts retain their requested boost. Later configuration frames without vocabulary retain the active vocabulary instead of clearing it. An explicit empty array clears it. Legacy `start` frames are normalized to `session.update`.
- Recorded meeting `/v1/meeting/mic` and `/v1/meeting/system` propagate query vocabulary into every model stream created for the recording.
- Vocabulary is scoped to each stream/request. Validation bounds phrase count, size and boost. The decoder is unchanged. Existing application/default boost 1.5 remains unchanged.
- Live checks covered `/v1/realtime`, `/v1/audio/realtime`, `/v1/audio/transcriptions/realtime`, `/v1/realtime/opus`, and both recorded meeting stages.
- Gateway `go test -race ./...` and `go vet ./...` passed; production vocabulary package tests passed. Both amd64 and arm64 gateway builds completed.

## Deployment and rollback

- Main server `i-REDACTED` was started; `speektome` and `speektome-fleet` are active.
- Test worker: `i-REDACTED`, us-east-1, g6f.large. Second worker: `i-REDACTED`, us-west-2, g6f.large.
- Pinned engine: NeMo-Speech.cpp v0.2.0, manifest source commit `6a3ca369370782acee0dd155e34394ba60b920c4`, Q8 RNNT model with SentencePiece vocabulary. Runtime/config unchanged.
- Engine executable SHA256: `1ace0e6bc765e647c16d27d4706f8c98fdc509ea6b044fc514bb48ce49b4e206`.
- New amd64 gateway executable SHA256: `b74cef922df7c145784591ef413d14827f434e519e13967d7c25e09de15e900d`.
- Both running workers now use the patched amd64 gateway, with its executable checksum verified.
- The first us-west-2 artifact fetch stalled on the IPv4 S3 endpoint and was retried successfully using the dual-stack endpoint already used by the bootstrap.
- Immutable replacement gateway archives and a new manifest were uploaded to the existing inference S3 bucket. Existing blobs were preserved.
- Future worker launches in all four configured regions now pin manifest `deployment/fleet-v1/79c53ab927ab93d0f00b00b18747aa41a9300f267a5a5e9356100fdcdab2d042/fleet-manifest.json`.
- Fleet desired size remains 2, existing hourly fleet cap remains $0.40, and instance type/regions are unchanged.
- Worker rollback executable: `/opt/speektome-worker/bin/gateway.before-vocabulary`. Controller config rollback: `/etc/speektome-fleet/fleet.before-vocabulary-20261016.json`.
- Main application binary and stored user vocabulary were not modified. Servers are left running.

## Files and reproduction

`corpus.json`, `vocabulary.json`, generated WAVs, `inference.jsonl`, `server-matcher.jsonl`, `summary.json`, `route-check.jsonl`, `recorded-check.jsonl`, `realtime-repeats.jsonl`, and `second-worker-check.jsonl` retain inputs, references, transcripts, events, and scoring evidence. Deployment metadata is under `deployment/`; credentials remain outside the repository.

From the repository root, with a local authenticated tunnel to worker port 8090 and the isolated main-server matcher installed:

```bash
/tmp/speektome-vocab-venv/bin/python benchmark/vocabulary/run_probe.py
/tmp/speektome-vocab-venv/bin/python benchmark/vocabulary/run_probe.py --route-check
/tmp/speektome-vocab-venv/bin/python benchmark/vocabulary/check_recorded.py
/tmp/speektome-vocab-venv/bin/python benchmark/vocabulary/repeat_probe.py
/tmp/speektome-vocab-venv/bin/python benchmark/vocabulary/score.py
```

`run_probe.py` resumes successful primary rows; other check scripts append new trials. The local venv contains `websocket-client` and `edge-tts`. Auth token path defaults to `/tmp/speektome-vocab-audit/worker.token`; the primary runner supports `--token` and `--base`. No credentials are included in the saved results.
