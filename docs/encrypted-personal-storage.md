# Encrypted personal storage

The API fetches `MASTER_KEY_SECRET_ARN` from AWS Secrets Manager once at startup,
without SDK retries, disk caching, periodic refresh or key-bearing error messages.
The secret JSON contains only a base64 `master_key` of exactly 32 random bytes.
Do not rotate this value: existing account keys depend on it. A database marker
rejects an accidentally substituted master key. Preserve AWS recovery access.
`PRIVACY_USE_INSTANCE_ROLE=true` makes this retrieval use the EC2 instance role,
independently of legacy SES credentials. Grant that role only `GetSecretValue`
for the exact secret ARN. Disable core dumps and swap for the service.

Each account has a random AES-256 key wrapped with AES-GCM under the master.
Final history uses AES-GCM with random nonces and authenticated account/result
identities. Audio uses a separate random object key wrapped under the account
key; its authenticated identity includes account, meeting, object and length.
DARE v2 (`minio/sio`) authenticates AES-256-GCM audio packages and supports bounded
random access. R2 stores ciphertext with random object names and no meeting names.
A database or bucket dump alone does not reveal content. This is server-managed
encryption, not end-to-end encryption: the authorized running server can decrypt.
A live server/root compromise can read memory or use the instance role.

R2 credentials belong in the production server `.env` (mode 0600), not the master
secret. Required non-master configuration:

```
MASTER_KEY_SECRET_ARN=<AWS secret ARN, not its value>
PRIVACY_USE_INSTANCE_ROLE=true
R2_ACCOUNT_ID=<Cloudflare account ID>
R2_BUCKET=YOUR_R2_BUCKET
R2_ACCESS_KEY_ID=<bucket-scoped object read/write credential>
R2_SECRET_ACCESS_KEY=<R2 secret>
```

Keep the bucket private, without public domains or an r2.dev URL. Use Standard
storage and the Eastern North America location hint (`enam`); R2 placement is a
best-effort hint rather than a guaranteed AWS region. The production credential
has object read/write permission for this bucket only, without bucket administration.
R2's default lifecycle rule removes abandoned multipart uploads after seven days.

## Audio lifecycle

Meeting recording/upload requires active Pro access, preserving the existing
48-hour subscription grace boundary. Local files remain on the desktop. The
server archives the mixed recording, not the microphone/system processing tracks.
A transactional reservation counts encrypted object bytes against an account's
`user_cloud_storage.limit_bytes`, default **2,000,000,000 bytes**. Concurrent uploads
cannot oversubscribe that reservation. Full storage preserves existing objects and
local recordings; the meeting's `cloud_audio_status` reports `storage_full`.

A background worker retries pending archives every minute with per-meeting backoff.
It records ambiguous upload failures durably and retains reservations until object
cleanup succeeds. Interrupted upload leases expire after 30 minutes; worker upload
contexts expire after 20 minutes. Completed processing files are purged from the
server only after the encrypted mixed recording is ready. Local desktop recordings
are unaffected. The desktop can stream a decrypted cloud download directly to its
Downloads folder without loading the full recording into the webview. Audio retrieval authenticates meeting ownership before decrypting
R2 byte ranges. Local playback additionally verifies immutable account ownership,
exact server/client meeting linkage and the canonical recordings directory. Legacy
recordings without verifiable identity fail closed rather than matching timestamps.

Non-paying accounts get a persistent `unpaid_since` clock; an observed active
renewal clears it. After three calendar months, cleanup rechecks entitlement while
holding the database write lock, deletes cloud audio and terminal server processing
copies, and releases quota only after R2 deletion succeeds. Unknown historical
lapse dates are conservatively timed from first observation. Meeting notes and
transcription history remain. Tombstones prevent purged audio being reuploaded.
Downloads remain available to the owner during the lapse period. The Settings UI
shows storage use and the scheduled deletion date. Account deletion removes cloud
objects before removing encrypted records and account keys; interrupted deletion
blocks new encrypted writes and can be retried.

## Final history

The desktop reports the final result after insertion/clipboard delivery. It reports
transformed Command Mode output, never the spoken command or intermediate ASR.
The request identity, account token and backend are captured for the recording;
account switching cannot attach a result to a different account. Saves are
idempotent, retried three times, and failures produce a notification without
logging content. Pending retries are in memory; quitting before a successful save
can leave that result absent from history. The new desktop History tab uses `/v1/transcriptions/history`; the legacy
`/usage/history` analytics contract retains usage metadata with empty content
fields. History offers cursor pagination,
copying and deletion, with `no-store` responses. Deletion tombstones prevent a late
retry from resurrecting a deleted result. History reads have bounded concurrency.

Usage logs retain timing/model/quota metadata and no transcript, final text or
vocabulary payload. Content-bearing client/server diagnostics and provider error
bodies have been removed from the touched transcription paths. Meeting transcripts, summaries, action items, speaker segments/names, and vocabulary
audits are also encrypted in the database. Recovery checkpoints for microphone ASR,
system ASR, and diarization use authenticated account/job/field identities. Startup
migrates retained legacy content; completed checkpoints are removed. This does not
change providers' own retention policies or encrypt local desktop files. Meeting
titles, timestamps, durations, file paths, IDs, and job state remain metadata.

## Rollout

1. Provision the immutable secret, exact-ARN IAM read permission and private R2 bucket.
2. Install only the secret ARN on the host, and R2 credentials in its private `.env`.
3. Run the staged ARM64 binary with `--check-private-storage`. It tests encrypted
   PUT, range GET, authenticated decryption and DELETE using synthetic bytes only.
4. Stop old API writers before running `--migrate-private-data`. The additive schema
   preserves usage accounting; legacy final fields are retained encrypted, while
   raw ASR/vocabulary fields are cleared. Legacy results are not represented as
   verified delivered history because old clients never acknowledged delivery.
5. Install the tested binary and service settings, restart, verify health and use
   aggregate SQL counts to confirm migration. Do not dump transcripts or secrets.
6. Ship the updated desktop for final-history reporting. Older desktop clients can
   continue transcribing but do not report final delivered history.

Run `go -C server test ./...`, targeted race tests and desktop recording identity/
shortcut tests. Master-key substitution, cross-account access, corrupted audio,
concurrent quota reservations, failed deletion/recovery, retention/renewal and
history deletion/retry behavior have local regression coverage.

## Account quota and verification (20 October 2026)

The desktop Account page shows online meeting storage remaining against the
server-provided limit, plus stored bytes and upload reservations. Reservations
reduce available capacity immediately. Refresh failures hide the old meter rather
than displaying an assumed empty quota. Account changes invalidate pending reads.
The same account-authenticated `/v1/storage` API already powers Meeting Notes.

A read-only aggregate audit of the production database found zero plaintext rows
in the retained meeting-content columns and zero plaintext job-checkpoint rows.
The configured account quota remained 2,000,000,000 bytes. The live binary matched
the security release documented in `deployment/security/VERIFICATION.md`.
Regression tests verify all six retained content columns, authenticated read-back,
legacy migration, encrypted job recovery, cross-field/job substitution rejection,
and removal of completed checkpoints. No production migration or server deployment
was needed for this UI update and verification.
