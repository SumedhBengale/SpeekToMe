# SpeekToMe Spot inference worker

> Current g6f fleet deployment: see [OPTIMIZED_G6F.md](OPTIMIZED_G6F.md). It supersedes the early worker examples below for the managed g6f fleet. The fleet defaults are batch32, arena40, HTTP80, ingress40ms, 20 live slots and one meeting; x86 L4 uses the original BF16 diarization model and tested bounded-window geometry.

EC2 user-data installs a pinned NeMo-Speech runtime, the Go gateway, two GGUF models and systemd services. Normal worker boots download prebuilt artifacts; compilation is only for preparing a new runtime profile. Requires an Ubuntu AMI with an already working NVIDIA driver. Installing a CUDA toolkit on every worker is unnecessary.

## AWS resources

- Private S3 bucket, us-east-1: `speektome-inference-ACCOUNT_ID-us-east-1`. Public access blocked, SSE-S3 encryption and versioning enabled. No HF token needed.
- Token: SSM SecureString `/speektome/spot-worker/auth-token` in us-east-1. Local operator copy: `~/.config/speektome/spot-worker.token`, mode 0600. Do not put this token in user-data, S3, Git, logs or URLs.
- Instance profile: `SpeekToMeSpotWorkerBootstrap`; read access to this bucket and this SSM parameter only.
- Oregon security group: `sg-REDACTED`, reproducing the inbound rules of Virginia `sg-REDACTED`. Security group IDs cannot be reused across regions. Port 80 is limited to the original allowed IPs; SSH rules match the source group.
- Oregon ARM AMI: `ami-REDACTED`, AWS Deep Learning ARM64 Base AMI, Ubuntu 24.04, 20261002. GPU driver was verified on a T4G. AMIs, subnets, security groups and key pairs are regional.

## Current tested worker

`i-REDACTED`, g5g.xlarge **Spot**, us-west-2c, `https://SPOT_WORKER_IP:80`. Observed Spot compute price on 2026-10-06: $0.0932/hour, excluding EBS and public IPv4 charges; the request ceiling is $0.15/hour. The temporary build instance `i-REDACTED` is confirmed terminated. Both models, token rejection and public TLS/HTTP/PCM/Ogg tests passed, including automatic reboot recovery. Clean EC2 user-data boot took about four minutes. See `validation/` for evidence and exact x86 test scope.

## Start a worker

From this directory, with an authenticated AWS CLI:

```bash
python3 launch-worker.py --output /tmp/speektome-worker.json
```

This launches exactly one `g5g.xlarge` Spot worker in Oregon, with key pair `SpeekToMe`, IMDSv2, and a 50 GiB encrypted gp3 root disk that is deleted on termination. The default maximum Spot price is $0.15/hour. It never falls back to On-Demand. The launcher uploads immutable bootstrap and manifest objects and records instance IDs without printing secrets. Inspect `/var/log/speektome-bootstrap.log` and `cloud-init status --long` over SSH.

For x86 or another region, supply `--region`, `--ami`, `--instance-type`, `--subnet` and `--security-group`. Use a compatible NVIDIA-driver Ubuntu AMI. The x86 CUDA 12 artifact is built for SM75/86/89 (T4/A10/L4); it also contains SM120a code, but this bootstrap intentionally rejects unvalidated GPU profiles. The ARM SM75 artifact requires Ubuntu 24.04 or an ABI-compatible newer environment. There is no generic ARM binary for every GPU.

The script can also be run directly as root, with `BUCKET` set. Defaults are `/opt/speektome-worker`, gateway 8090, NeMo 8080, eight admitted inference requests/streams. `MAX_STREAMS` is a provisional admission limit, not a measured throughput guarantee. Change it only after a sustained streaming benchmark for the instance profile.

## Routing and authentication

Public endpoint is **HTTPS on port 80**, matching the requested firewall. NeMo and gateway bind to loopback; no direct unauthenticated model port is exposed. Every request, including readiness and WebSocket upgrades, requires:

```text
Authorization: Bearer <worker token>
```

Nginx uses a per-instance self-signed certificate with IP SANs. Copy `/etc/speektome-worker/tls.crt` through trusted SSH/SSM and configure the main server's TLS trust for that worker. Do not disable certificate verification. Certificates expire after 90 days; longer lived deployments need managed renewal or a trusted internal CA. The shared token authorizes requests; SG rules still restrict connectivity.

- `GET /ready` or `/health`: authenticated JSON with instance ID, region, profile, active streams, admission limit and readiness; 503 while unavailable or draining.
- `POST /worker/drain`: authenticated, stops admitting new requests, existing streams continue. Drain resets when the gateway restarts.
- `WSS /v1/realtime` or `/v1/audio/transcriptions/realtime`: PCM16, mono, 16 kHz binary messages; NeMo JSON control/events.
- `WSS /v1/realtime/opus`: mono Ogg Opus decoded by the pure-Go gateway. The assembler accepts packets/pages split over arbitrary transport messages, resets the decoder for independent Ogg chunks and trims codec padding using end granules. Raw Opus and WebM are not accepted by this endpoint.
- Other `/v1/*` HTTP APIs are proxied to NeMo; upload limit 100 MiB. The gateway removes the worker token before forwarding.
- Capacity exhausted: 429; not ready/draining: 503; absent/incorrect token: 401 before any model request or upgrade.

Readiness sends 560 ms PCM chunks with diarization enabled and requires a completed nonempty transcript with speaker labels. A systemd timer repeats this test after a model-process restart. `/opt/speektome-worker/bootstrap-result.json` is written only after that test passes. Services survive reboot and use cached assets. The main server should poll `/ready` and route only after a 200 result. There is no automatic registration callback, fleet controller, Spot interruption watcher or reconnection replay yet.

## Verify from this PC

Copy the certificate first (substitute the worker IP):

```bash
ssh -i ~/.ssh/YOUR_SSH_KEY ubuntu@WORKER_IP 'sudo cat /etc/speektome-worker/tls.crt' > /tmp/worker.crt
python3 -m venv /tmp/worker-test-env
/tmp/worker-test-env/bin/pip install websockets
aws s3 cp s3://speektome-inference-ACCOUNT_ID-us-east-1/deployment/20261006/smoke-2s.wav /tmp/smoke.wav --region us-east-1
/tmp/worker-test-env/bin/python test-worker.py --endpoint https://WORKER_IP:80 --certificate /tmp/worker.crt --token-file ~/.config/speektome/spot-worker.token --wav /tmp/smoke.wav --output /tmp/worker-test.json
```

The PC also needs ffmpeg for generating the Ogg test input. The test checks certificate verification, missing/wrong token rejection, authenticated readiness, unauthenticated inference/WS rejection, and completed PCM, continuous Ogg and independently encoded 560 ms Ogg chunk transcripts with speaker labels and matching processed audio duration. It is a functionality test, not a concurrency or latency benchmark.

## Models and provenance

NeMo-Speech.cpp v0.2.0, commit `6a3ca369370782acee0dd155e34394ba60b920c4`.

Models were copied directly from the existing server `GPU_HOST_IP`:

- ASR: `nemotron-3.5-asr-streaming-0.6b.q8_0.spm.gguf` (embedded tokenizer), 742,090,464 bytes.
- Diarization original: `Nemotron-3-Diarization.bf16.gguf`, 199,018,528 bytes, retained on the reference server; its redundant S3 copies were removed during cleanup.
- Worker diarization: F16 conversion of the original; same size. T4G does not have native BF16 support. `convert_diar_fp16.py` preserves metadata and checks finite converted values. Functional smoke tests validate loading/inference, not numerical parity or diarization quality.

`manifest.json` records all artifact SHA256 values. The bootstrap checks hashes before installation. Rebuild the gateway for Linux amd64/arm64 with `CGO_ENABLED=0`; both were built on the existing x86 server. Run `go test -race ./...` in `gateway/`.

`build-runtime.sh SOURCE DEST 75` builds the ARM CUDA runtime once, with SM75 explicitly targeted and the bundled cuBLAS shim. The first build used CUDA 13.2 on T4G, not the incompatible official ARM SM87 release. Runtime libraries are packaged with it; the compatible host NVIDIA driver remains an AMI responsibility.

## Next phase

Fleet selection, cost-aware scaling, Spot interruption/rebalance handling and main-server token/certificate integration are separate work. Sudden worker loss cannot migrate an active WebSocket; the main server needs an audio replay buffer and explicit reconnect behavior. Current workers expose the readiness/load/drain controls needed for that work.

Regional configuration: see [REGIONAL.md](REGIONAL.md) and `regional-stack.json` for the reusable, AWS-validated IAM/SG/launch-template configuration. No regional fleet or replicas have been deployed. The current bootstrap uses IAM download rather than an expiring URL, separates token/model regions, and omits unused worker-side ffmpeg. Its revised cold boot time is not yet measured.
