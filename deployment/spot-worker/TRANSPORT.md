# Transcription transport encryption

Audio between the fleet controller (on the main server) and each inference worker is sealed at the application layer, inside the pinned-TLS connection. The design is in `shared/transport/transport.go`.

- **Worker keys stay in memory.** Each gateway process creates an X25519 key pair at startup and never writes it to disk. It publishes the public key in `/worker/metadata` (`transport_key`, `transport_key_id`). The controller already fetches that endpoint over certificate-pinned TLS with the bearer key, so the key arrives authenticated. A restarted gateway gets a new key; requests sealed to the old one get a retryable `503` with `X-SpeekToMe-Admission: rejected`.
- **What's sealed:**
  - The controller makes fresh keys for every WebSocket session and every HTTP request.
  - Every stream message is sealed in both directions, with sequence numbers.
  - Batch and meeting bodies and their responses are sealed as authenticated 64 KiB records.
  - The query string, which can carry vocabulary terms, moves into sealed metadata.
  - nginx on the worker only ever relays ciphertext.
- **Plaintext exists only in memory.**
  - On the worker, the gateway decrypts as it reads and hands audio to the model runtime over loopback.
  - Meeting audio is decoded into memory and never written to the worker's disk.
  - On the main server, batch bodies spooled for retries are encrypted under a key held only in that request's memory (`transport.Spool`).
- **Gateways refuse unsealed `/v1/` requests.** `-transport-required` defaults to `true`.
- **Controller policy.** `transcription_encryption` in `/etc/speektome-fleet/fleet.json`:
  - `required` (the default) sends audio only to workers advertising a key, always sealed.
  - `preferred` also uses keyless workers, in plaintext over pinned TLS. It exists only to roll a fleet onto encrypting gateways; switch back to `required` once every worker advertises a key.
- **Measured cost** (main server, Graviton): about 10 µs to seal a 560 ms PCM chunk, 1 µs for a 560 ms Opus message, and 0.4 ms once per session.

Tests: `go test ./...` in `shared/transport`, `deployment/spot-worker/gateway` and `server`. To test the real gateway binary against the real controller:

```bash
go -C deployment/spot-worker/gateway build -o /tmp/gateway .
SPEEKTOME_GATEWAY_BINARY=/tmp/gateway go -C server test ./orchestrator -run RealGateway -v
```
