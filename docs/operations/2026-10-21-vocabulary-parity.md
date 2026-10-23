# Live vocabulary matcher comparison, 2026-10-21

Verified at 19:40 IST (14:10 UTC). Started the existing stopped main EC2 instance
`i-REDACTED` in us-east-1. Its deployed API and fleet controller started
successfully; API health returned `{"status":"ok"}`. No application binary or
fleet policy was deployed or changed. The local plan-policy edits remain local.

The existing controller targets two `g6f.large` Spot workers. Both target workers
were ACTIVE and ready, with zero active streams, at the final check:

| Worker | Region | Matcher RPC comparisons |
| --- | --- | --- |
| `i-REDACTED` | us-east-1 | 12/12 identical to main-side matcher |
| `i-REDACTED` | us-east-2 | 12/12 identical to main-side matcher |

An earlier Virginia worker, `i-REDACTED`, received a rebalance signal.
The controller created the replacement above and was draining the old worker,
with zero active streams. No On-Demand fallback or manual capacity change was made.

## Method and evidence

1. Checked the deployed main executable's Go build information: it imports
   `speektome/vocabulary`, replaced by `../shared/vocabulary`.
2. Built the repository's existing `server/cmd/vocabulary-probe` for Linux ARM64,
   uploaded it to a temporary directory, and executed it on the main instance.
   The probe uses `vocabulary.Compile` and `vocabulary.Propose`, the same entry
   points used by the main API's local fallback. It was run without `-live`.
3. Compared the probe with the deployed main executable using Go's embedded
   function/source-line tables. All 22 shared vocabulary functions present in
   the probe matched the deployed main's compiled function sizes and
   per-instruction source-line mappings. This is structural binary evidence,
   not a byte-for-byte comparison of executable machine code.
4. SSHed into both ready inference workers through the main instance. Confirmed
   their gateway and inference services were active and GPU bootstrap readiness
   had completed. Called each live gateway's authenticated localhost
   `/v1/vocabulary/final` endpoint with the same 12 synthetic inputs.
5. Compared the entire result JSON, excluding only `match_ms`. All 24 worker
   results matched the main-side probe exactly: final text, original text,
   proposals, occurrence IDs, UTF-8 offsets, similarity scores, match types,
   transcript/dictionary hashes, schema version and matcher version. There were
   no matcher errors. The 1,000-term case passed on both workers.

Both gateways reported matcher version `phonetic-symspell-jaro-v2` and had the
same installed executable SHA-256:
`379b1f7f5cd2cca7cb71061ff506f41b4df9e5b90cb93c809f6158ea16c44d59`.
The deployed main executable SHA-256 was
`2296a369b2ea90cdade19350ea265aa54ee022c07a32b42fb1af5c5b1b103a80`.

Cases cover spelling, canonical terms, ordinary/brand homophones, Unicode,
punctuation, speaker lines, an empty dictionary, unrelated text, repetition,
a match well before the final twelve words, and a 1,000-term dictionary.

These checks compare the matching algorithm. They did not force the running
main API into fallback, invoke Jev or the grammar LLM, use user recordings, or
measure ASR accuracy. The worker streaming finalizer and text RPC both call
`vocabulary.Propose`; the direct live calls here exercised the text RPC.
Main API, controller and inference workers were left running.

Raw synthetic inputs, outputs, comparisons, binary fingerprints and final fleet
status are retained at:
`/home/ssben/.local/state/SpeekToMe/verification/2026-10-21-vocabulary-parity/`.
