# Dedicated CPU diarization server

Production configuration for the existing Ubuntu 24.04 ARM64 `t4g.medium` in Virginia. See [aws-config.json](aws-config.json) for instance, networking and storage settings; [GATEWAY.md](GATEWAY.md) for API, authentication, limits, recovery and live log commands; [VERIFICATION.md](VERIFICATION.md) for measured results.

This folder contains the gateway, deployment installer, pinned Python requirements, model checksums, licensing, systemd units and journal budget. No model weights, uploaded meeting audio, authentication keys or SSH keys are committed.

## Reinstall or create a matching worker

Provision Ubuntu 24.04 with at least 2 vCPUs, 4GiB RAM and encrypted 8GiB gp3. The current AMI ID is ARM64 and region-specific. Use an architecture-appropriate image for another platform. Keep the instance private; TCP22 and8081 are permitted only from the coordinator's privateCOORDINATOR_PRIVATE_IP/32. That rule assumes the current same-VPC network; other regions need routed connectivity and corresponding allowed source addresses. Unlimited CPU credits can incur surplus charges under sustained load.

Copy this folder to the worker. Supply the existing fleet authentication key through a protected file, outside the repository (currently `/etc/speektome-diarization/token`). Then:

```bash
sudo bash setup.sh /etc/speektome-diarization/token
sudo systemctl status speektome-diarization --no-pager
sudo journalctl -u speektome-diarization -n 30 -f
```

On a clean machine, use another protected input token file instead. The installer stores it as root:service-group640 and does not delete your supplied file. If reinstalling with the existing destination token, the installer leaves that file in place.

The model is the community `wannaphong/Nemotron-3-Diarization-ONNX` export pinned at `fd1c2b082a83ea1ae82779ca30d165bd383c0eb6`. Setup downloads only INT8 graph, frontend graph, constants and original wrapper, then checks SHA256. ONNX Runtime1.23.2 uses CPUExecutionProvider with two threads; bounded offline geometry remains the publisher's default. No CUDA or NeMo native build is required.

`install-gateway.sh` installs `systemd/` and `journald/` configuration and enables services. Gateway and resident model start together at boot and restart on crash. `log-budget.py` caps other system logs. Combined configured log budgets are464MiB (486.5MB); rotation/timer granularity can cause brief overshoot.

The main application has not yet been integrated with this dedicated worker. Its protected client credentials are already available on the coordinator; see the API document for submitting, polling, saving output and acknowledging completed jobs. Large uploads are streamed to disk, but decoded waveform/features still use whole-file RAM; the tested worker accommodates one1–4hour job at a time, subject to size, duration, RAM and free-disk admission.
