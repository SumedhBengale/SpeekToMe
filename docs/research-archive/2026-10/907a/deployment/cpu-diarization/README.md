# SpeekToMe ARM CPU diarization test host

Launched 2026-10-08 in us-east-1a as an On-Demand t4g.medium.

- Instance: i-REDACTED
- ARM64 Ubuntu24.04 AMI: ami-REDACTED
- Hardware: 2 vCPUs, 4GiB RAM
- Root: encrypted8GiB gp3; DeleteOnTermination=true
- PrivateIPv4:DIARIZATION_PRIVATE_IP; no publicIPv4
- IPv6:WORKER_IPV6
- Securitygroup:sg-REDACTED; SSH and authenticated gateway TCP8081 only from coordinator privateCOORDINATOR_PRIVATE_IP/32
- Key:SpeekToMe
- Credits:Unlimited for unthrottled performance experiments; sustained above-baseline CPU can incur surplus-credit charges.
- No Spot fleet membership tag, ASR model, GPU driver, or IAM instance profile. Dedicated CPU diarization gateway installed; see [GATEWAY.md](GATEWAY.md).

Connect via coordinator:
```bash
ssh -i ~/.ssh/YOUR_SSH_KEY -o 'ProxyCommand=ssh -i ~/.ssh/YOUR_SSH_KEY -W %h:%p ec2-user@BASTION_HOST_IP' ubuntu@DIARIZATION_PRIVATE_IP
```

CPU ONNX environment: /opt/speektome-diarization/venv. Bootstrap also installs ffmpeg and minimal C++ build tools for later native runtime experiments. ONNX Runtime's CPUExecutionProvider is available. Activate with:
```bash
source /opt/speektome-diarization/venv/bin/activate
```

The selected production runtime is the community ONNX INT8 export, with ONNX Runtime pinned to 1.23.2. Gateway and resident model start together under `speektome-diarization.service`. Alternative GGUF/FP32 models, native build trees, and benchmark audio are removed after verification. Benchmark evidence remains in this repository: [BENCHMARK_RESULTS.md](BENCHMARK_RESULTS.md), [LONG_AUDIO_RESULTS.md](LONG_AUDIO_RESULTS.md), [CONCURRENT_RESULTS.md](CONCURRENT_RESULTS.md).

Stop when testing finishes:
```bash
aws ec2 stop-instances --region us-east-1 --instance-ids i-REDACTED
```
Stopping preserves EBS and its storage charges. Terminating deletes the tagged root disk. The dedicated security group would remain and can be removed separately when the test host is no longer needed.

Launch input: user-data.sh. AWS launch/status/credit evidence: validation/.
