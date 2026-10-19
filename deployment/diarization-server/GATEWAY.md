# CPU diarization server API

Instance `i-REDACTED`, us-east-1a, t4g.medium, 4 GiB RAM, encrypted 8 GiB EBS. Gateway URL from coordinator: `http://DIARIZATION_PRIVATE_IP:8081` (private IPv4, no public IPv4). Security group `sg-REDACTED` permits SSH and TCP 8081 only from the coordinator's private `COORDINATOR_PRIVATE_IP/32`.

## SSH and live logs

```bash
ssh -i ~/.ssh/YOUR_SSH_KEY \
  -o 'ProxyCommand=ssh -i ~/.ssh/YOUR_SSH_KEY -W %h:%p ec2-user@BASTION_HOST_IP' \
  ubuntu@DIARIZATION_PRIVATE_IP
```

After connecting:

```bash
sudo journalctl -u speektome-diarization -n 40 -f -o short-iso
```

## Authentication and routes

All routes require `Authorization: Bearer <key>`. The existing fleet authentication key is used. It is stored at `/etc/speektome-diarization/token` on both coordinator and worker, with restricted permissions; it is not in the repository, logs or response bodies. The coordinator also has a protected `client.curl` file so curl need not put the key in process arguments.

| Method / route | Behaviour |
|---|---|
| GET `/healthz` | Authenticated readiness and busy state; 503 until model loaded |
| GET `/v1/metadata` | CPU/RAM/disk, process RSS, runtime details, capacity=1, active job and progress |
| POST `/v1/diarizations` | Raw Ogg/Opus file body, streamed to disk; 202 once full input is durably uploaded |
| GET `/v1/diarizations/{job_id}` | Upload/processing state, phase, audio duration, processed duration, completed/total windows, percent, estimated remaining time |
| GET `/v1/diarizations/{job_id}/result` | Saved JSON with speaker segments; 409 while processing, 422 on failure |
| GET `/v1/diarizations/{job_id}/result?format=rttm` | Saved RTTM |
| DELETE `/v1/diarizations/{job_id}` | Acknowledge and remove a finished job; 409 if active |

The optional `X-SpeekToMe-Job-ID` header supplies a 32-character lowercase hexadecimal coordinator identity. It is persisted as the job ID. Repeating the same identity returns 202 for the existing uploading/processing/terminal job without consuming the new body or rerunning inference. Poll its status to distinguish completion/failure. After a terminal failure, use a new identity for an explicit retry. Requests without the header retain random server-generated IDs.

No gateway queue. The single slot is reserved during upload as well as processing. A second POST returns 429, `Retry-After: 15`, and `queue_on_coordinator: true`. The coordinator should retain the request and retry later. Do not keep retrying a failed job without inspecting its error.

Submit from the coordinator:

```bash
curl --config /etc/speektome-diarization/client.curl \
  -H 'Content-Type: audio/ogg' \
  --data-binary @/path/to/meeting.opus \
  http://DIARIZATION_PRIVATE_IP:8081/v1/diarizations
```

The response supplies `job_id`, `status_url` and `result_url`. Poll metadata or job status, then fetch results, save them in the main server database, and DELETE the completed job after saving. The main durable-meeting integration owns a persistent FIFO and saves results before acknowledgment; see `server/orchestrator/DIARIZATION.md`.

## Runtime and long files

Model is resident in the gateway process: community Nemotron-3 ONNX INT8, ONNX Runtime 1.23.2, CPUExecutionProvider, two threads, published bounded offline window geometry. The original conversion wrapper is preserved. The gateway only wraps its feature/chunk calls to track progress.

Uploads are appended directly to disk; the HTTP body is never read into one RAM buffer. Default maximum upload is 2 GiB, subject to free space and a 512 MiB disk reserve. HTTP Content-Length and chunked transfer are both supported. Input must be a single mono Opus audio stream. ffmpeg explicitly decodes/resamples to 16 kHz mono; Opus container clock values need not be 16 kHz.

Decoded audio/features still occupy whole-recording RAM. Before decoding, admission estimates 512 MiB + 8 MiB per audio minute and rejects jobs exceeding the 3 GiB worker budget. This allows 1–4 hour recordings with headroom and rejects durations beyond roughly 5 hours 20 minutes with an explicit failed-job error. Larger workloads require a larger worker or a memory-mapped pipeline; the 8 GiB disk and 4 GiB RAM are not unlimited storage.

`progress_percent` describes diarization of the audio, not a weighted percentage of upload plus decode plus inference. Upload and decode have separate progress fields and the current `phase` distinguishes those steps. Estimated remaining time becomes available after diarization windows complete.

Input is durable before 202. Subsequent client disconnect does not cancel the worker. Completed results persist locally for seven days or until DELETE. Uploaded/decoded temporary audio is deleted after success or failure. The coordinator must retain its own source audio for retries. Result durability is EBS-local, not replicated storage.

## Automatic startup and recovery

`speektome-diarization.service` starts on boot and uses `Restart=always`, 3-second restart delay, a 3 GiB service memory ceiling, a dedicated unprivileged account, restricted filesystem writes, and private temporary storage. Gateway and resident model runtime start together; there is no second runtime daemon to coordinate.

A crash with a fully uploaded job recovers the same job ID and reruns diarization from the beginning once. A repeated runtime crash marks the job failed and releases capacity on startup, preventing an endless crash loop. Interrupted uploads are marked failed and their partial files removed. Live progress is in RAM; only admission and terminal state need durable persistence because recovery restarts the whole job.

## Log budget and storage

Gateway/runtime write only to journald; HTTP access logs and audio/segment content logging are disabled. Persistent journal budget 384 MiB, runtime journal 16 MiB, other `/var/log` files 64 MiB enforced by `speektome-log-budget.timer` every 60 seconds. Combined configured budgets are 464 MiB (486.5 MB), leaving headroom below 500 MB. Rotation/timer granularity can permit brief overshoot; this is log retention management, not a filesystem quota.

Keep only the selected model graph, frontend graph, constants, original wrapper and license/provenance, Python runtime dependencies, gateway source and persisted job results. Alternative GGUF/FP32 models, native build trees, benchmark inputs/outputs and helper scripts are unnecessary on the finalized worker. Benchmark evidence is retained in this repository.

## Deployment verification (2026-10-08)

- Six-minute Opus job: 25.44 seconds of processing after forced SIGKILL recovery; same job ID, one recovery attempt, 126 speaker segments. Result retrieval works after submitter disconnects.
- Four-hour repeated meeting fixture: 964.56 seconds (16m04.56s) processing, including 68.65s decode; 530 bounded windows, four speakers, 5,037 segments. JSON and RTTM retrieved successfully. This tests duration/resources, not natural four-hour diarization accuracy.
- Peak systemd service memory: 2,373,296,128 bytes (2.21 GiB), including final output processing. CPU reached approximately 100% of both cores. Metadata response sampled at 6ms during processing.
- Second simultaneous submission: 429 with Retry-After=15. Oversized declared upload: 413. Invalid chunked audio upload: asynchronous failed job and slot released. Missing/wrong authentication: 401.
- Finished test jobs acknowledged and removed. Alternative models, build trees, fixtures and benchmark helpers removed. Selected model directory approximately 100MB; root filesystem has approximately 3.4GiB free. System logs approximately 35MB at cleanup.
- systemd crash restart and uploaded-job recovery verified. Reboot startup verified separately in `validation/gateway-reboot.txt`.
