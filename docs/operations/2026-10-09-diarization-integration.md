# Dedicated CPU diarization integrated — 2026-10-09

Implemented in the requested source `.`, preserving its pre-existing edits. The Codex worktree contains this report/evidence; the authoritative implementation is in the requested source.

- Durable meeting jobs now send only mic/system ASR to the GPU fleet. Diarization uses the dedicated private CPU server at DIARIZATION_PRIVATE_IP:8081.
- SQL FIFO queue, SQL coordinator lease and a partial unique index enforce one running diarization item. Additional jobs remain queued. Gateway capacity=1 independently prevents concurrent inference.
- Remote identities persist before submission. The gateway now accepts a coordinator job-ID header and reattaches duplicate requests. Saved remote IDs survive main-server restarts and lost upload responses.
- Results are validated, normalized and committed before remote acknowledgment. Finished remote files are deleted after persistence. Busy/unavailable responses retry without advancing the head.
- GPU leases are released after ASR; waiting for CPU does not consume GPU capacity or failed-attempt allowance. Existing ASR checkpoints survive explicit diarization retries.
- Job status adds a diarization status/error object. Legacy synchronous/live meeting ingestion is unchanged; the current desktop durable upload path is integrated.

## Verification

Full `go test ./...` passed. Targeted race tests passed. Three gateway admission/upload tests passed. Real-server tests used JFK fixture audio and an isolated local SQL database on the coordinator:

1. `TestRealFleetDurableMeeting`: real mic/system GPU ASR plus CPU diarization, merged meeting persisted after upload client disconnected — passed (2.54 seconds in this small-fixture run).
2. `TestRealCPUQueueSerial`: two real CPU requests committed together; one active, second queued; both results persisted and acknowledged — passed (3.34 seconds total).
3. Runtime log: first queue job completed at 10:52:26.486 UTC; second uploaded at 10:52:27.778 UTC and completed at 10:52:28.254 UTC. These establish sequential handling for the fixture, not long-recording performance.

LLM summaries were not validated by the isolated tests because those test processes intentionally had no provider API keys. Main deployment retains its existing provider configuration.

## Deployed

CPU gateway restarted with coordinator-ID support, SHA256 `a2a14258dac0b72be77681c7ef54d3dd6df9535c78a04d1712786665fdddab57`. Previous gateway retained as `/opt/speektome-diarization/gateway.py.pre-integration-20261009`.

Main binary installed/restarted at 10:54:20 UTC, SHA256 `14fb06a5aeff128efdaf8ecb8d974cc64c0be6eb4b4a40895a6693bf81a164cf`. Previous binary retained under `/home/ec2-user/speektome/rollback-diarization-20261009/`. Systemd drop-in `/etc/systemd/system/speektome.service.d/30-diarization.conf` supplies the private URL and protected credential. Public API health returned 200; main and fleet services active; Valkey connected.

## Files and evidence

Implementation and operating contract: [DIARIZATION.md](server/orchestrator/DIARIZATION.md).

Queue/adapter: [meeting_diarization.go](server/handlers/meeting_diarization.go). ASR/finalization changes: [meeting_jobs.go](server/handlers/meeting_jobs.go).

The sibling `diarization-integration-2026-10-09` directory contains full-suite output, real-server test output, source hashes and a patch relative to the pre-task source snapshots. Tests did not create production account/meeting rows. The source and deployment changes do not remove or retry unrelated historical failed jobs automatically; use the existing meeting Retry action for those jobs.
