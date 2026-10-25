# Serverless GPU diarization (Modal)

`app.py` runs NVIDIA's official `nvidia/Nemotron-3-Diarization` Q8_0 GGUF (revision `f667ed7`) on NVIDIA NeMo-Speech.cpp (nightly `caa3f47`). Each request gets its own fresh GPU container, which diarizes one recording and exits, so nothing is billed between requests. Jobs from different users run in parallel, up to the configured limit.

The coordinator (`server/handlers/meeting_diarization.go`) drives this through the same job contract as the private CPU gateway in `deployment/diarization-server`: POST, poll, GET result, DELETE.

## Architecture and security

```
coordinator (AWS) ──HTTPS──▶ Modal edge ──▶ api (CPU, 0.25 core)  ──spawn──▶  Diarizer (GPU, single use)
   seals audio       proxy token   bearer key, envelope header,          opens envelope in RAM, diarizes,
   to diarizer key   required      freshness, job identity               seals segments to the job's key
```

- **Lock 1, Modal proxy auth.** `api` is deployed with `requires_proxy_auth=True`, so Modal's edge rejects any request without the workspace proxy token (`Modal-Key` / `Modal-Secret`) before a container starts. Unauthenticated callers can't run up the bill.
- **Lock 2, the coordinator's bearer key.** It's compared in constant time against a SHA-256 stored in the Modal Secret `speektome-diarization-api`. Modal never stores the key itself.
- **Application-layer encryption (`envelope.py` ⇄ `server/handlers/diarization_envelope.go`).**
  - Each recording is sealed with X25519 + HKDF-SHA256 + AES-256-GCM in 1 MiB records. Each record's AAD covers the header, its position and a final flag, so records can't be reordered, truncated, replayed into another job or edited.
  - Only the GPU function has the private key (Modal Secret `speektome-diarization-key`). The front endpoint can't decrypt anything.
  - Results are sealed to a per-job response key. The coordinator stores that key sealed under the account key, so a restart can still open a result.
  - Modal keeps function inputs and outputs for up to 7 days, but only ciphertext.
- **No plaintext on disk.** Decrypted audio and PCM live in `memfd`s. The CLI gets a `*.wav` symlink to the memfd, and the link holds no audio.
- **What this doesn't cover.** The audio is in plaintext in GPU-container memory on Modal hardware, which Modal operates. Protecting against the provider itself needs attested confidential computing, which Modal doesn't offer. A data processing agreement with Modal is still needed.
- **Latency cost of the encryption:** about 25 ms to seal and 43 ms to open a 4-hour recording (38 MB), plus 592 bytes of overhead.

## Image (all baked at build time)

- **GPU image:**
  - `debian_slim` with Python 3.12; ffmpeg, curl and ca-certificates installed without recommended packages; numpy and cryptography.
  - NeMo-Speech.cpp CUDA runtime, SHA-256 verified. Headers, CMake files, docs and the unused C-API shims are removed.
  - One model, `/models/Nemotron-3-Diarization.q8_0.gguf` (SHA-256 verified), with its OpenMDW-1.1 licence.
- **API image:** `debian_slim`, FastAPI and cryptography only.
- **`/opt/diar.yaml`:** the model card's 30.4 s long-form geometry (chunk 340, right context 40, FIFO 40, speaker cache 264, update period 300). No `preset` is set, because a preset silently replaces these keys. Raw 0.5 thresholds are used, because the CLI's default segment post-processing scored 27% DER on AMI.
- **GPU:** `["L4", "A10", "L40S"]`, cost first. 4 CPU cores, 8 GiB RAM, `single_use_containers=True`, `max_containers=10`. Your plan's GPU limit is 10.

## Setup

```bash
cd deployment/modal-diarization
python provision.py ../../server/.env               # key pair, Modal secrets, proxy token; prints no secret
modal deploy app.py
python provision.py ../../server/.env --url-only    # records SPEEKTOME_DIARIZATION_URL
```

`provision.py` writes `SPEEKTOME_DIARIZATION_{URL,TOKEN,PUBLIC_KEY,MODAL_KEY,MODAL_SECRET,CONCURRENCY}` into the coordinator `.env` (mode 0600). The private key never leaves Modal. Placeholders are in `server/.env.example`.

The coordinator switches to GPU mode whenever `SPEEKTOME_DIARIZATION_PUBLIC_KEY` is set (default concurrency 8). Remove it to fall back to the CPU gateway, which uses `SPEEKTOME_DIARIZATION_TOKEN_FILE` and runs one job at a time.

**Production server note:** `/etc/systemd/system/speektome.service.d/30-diarization.conf` used to hard-set `SPEEKTOME_DIARIZATION_URL` to the CPU worker, and environment variables set by systemd win over `.env`. That line was removed on 2026-10-25; the original is in `~/speektome/rollback-modal-diarization-20261025T084123Z/`.

## Tests

```bash
python -m unittest test_envelope                                          # format, tampering, Go vectors
go -C ../../server test ./handlers -run 'Envelope|GPU|Diarization'        # Go side, fake endpoint
SPEEKTOME_REAL_GPU_ENV=$PWD/../../server/.env SPEEKTOME_REAL_AUDIO=$PWD/../../benchmark/modal-diarization/fixtures/m010.opus \
  go -C ../../server test ./handlers -run RealGPU -v                       # live: both locks, 3 concurrent jobs
python load_test.py ../../server/.env ../../benchmark/modal-diarization/fixtures results.jsonl
```

The live test on 2026-10-25 rejected every incomplete credential combination with HTTP 401. Stale and mismatched envelopes got HTTP 400 without starting a GPU. Three 10-minute meetings ran at the same time, and their results were decrypted and saved in 15.8 s.

## Rotating keys

- **Bearer key or proxy token:** edit `.env` (delete the token lines to get a new proxy token), rerun `provision.py --rotate`, then update the production `.env` and restart `speektome`.
- **Diarizer key pair:** `provision.py --rotate`. Modal never returns a Secret's value, so the old private key is gone afterwards. Rotate when no diarization job is in flight, because a job sealed to the old key fails with "envelope rejected". `DIARIZATION_PREVIOUS_PRIVATE_KEY` in `speektome-diarization-key` is honoured for manual overlap rotations.

## Measured performance (2026-10-25)

| Meeting | GPU | Processing (decode + diarize) | DER vs AMI ground truth |
|---|---|---|---|
| ES2004a, 17.5 min | L40S | 5.1 s | **9.33%** (model card: 9.25%) |
| 4 h, 16 AMI meetings | L4 | 15.6–18.2 s | n/a, 8 speakers found |

- **Load test:** 20 requests (22.5 h of audio) submitted at once all landed on L4. 9 started within 3–6 s; 11 waited 44–76 s while Modal brought more L4s online.
- **Cost:** about $0.003 per average meeting, measured as processing plus ~5 s of billed startup.
- **Cold start:** typically 5–15 s for a single request. Waiting for a GPU isn't billed.

## Rebuilding

NVIDIA replaces the nightly asset over time, so a rebuild after that fails at the checksum on purpose. Until `app.py` changes, Modal reuses the built layer. When NeMo-Speech.cpp publishes a tagged release containing `caa3f47` (NVIDIA/NeMo-Speech.cpp#62), switch `RUNTIME` and `RUNTIME_SHA256` to it.

## Rollback (production)

```bash
cd ~/speektome && R=rollback-modal-diarization-20261025T084123Z
cp -p $R/speektome-server speektome-server && cp -p $R/.env .env
sudo install -o root -g root -m 644 $R/30-diarization.conf /etc/systemd/system/speektome.service.d/ && sudo systemctl daemon-reload
sudo systemctl restart speektome    # the CPU worker i-REDACTED must be running for that path
```
