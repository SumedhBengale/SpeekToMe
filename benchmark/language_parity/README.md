> Historical research source recovered on 2026-10-19. Deployment status, quotas,
> instance IDs and runtime observations below belong to the original experiment.
> Recovery did not launch resources or repeat the benchmarks.

These scripts compare raw ASR from the production application, its Q8 inference worker, and the official full-precision Nemotron 3.5 checkpoint through NVIDIA's documented Transformers integration.

`prepare.py` generates seven synthetic fixtures with Microsoft Edge TTS voices. `encode.py` creates the same independent 560 ms Ogg Opus messages used by the desktop, adds 1.12 seconds of final silence, and decodes them into a canonical PCM waveform for direct-worker/reference comparisons. The waveform hash is recorded per run. This is a controlled reproduction suite, not a human-speech accuracy benchmark.

`run_remote.py --mode app` reads the user's session token directly from `~/.config/SpeekToMe/config.json`, sends the fixtures to the main server, and checks raw ASR against persisted history. It does not change account language preferences. The mandatory downstream pipeline still executes, but its output is not scored. The token and acknowledgement tokens are never written to results. Real test-account usage/history entries are created.

`run_remote.py --mode worker --unpaced` reads the existing worker credential from `~/.config/speektome/spot-worker.token`. It sends canonical PCM using 560 ms frames, with explicit `auto`, `en-US`, and `hi-IN` controls. `--endpoint-ms 60000` suppresses mid-recording silence endpointing as a diagnostic control. Unpaced results measure accuracy, not realistic latency or capacity.

The scripts expect SSH forwards from local 18991 to main-server localhost:8000 and local 18992 to the current inference worker's TLS port 80. Worker TLS certificate verification is disabled only inside this authenticated SSH tunnel. The public endpoint remains the desktop's normal route; the harness uses the main server tunnel to avoid an HTTP client rejection at the public edge.

`run_reference.py --model /path/to/pinned/hf/snapshot --right 6` uses the locally cached official checkpoint and the processor/model's supported streaming API, FP32, SDPA, TF32 disabled, and bounded streaming caches. It runs all three language settings. Right-context values 0, 3, 6, and 13 correspond to 80, 320, 560, and 1120 ms chunks in this pinned checkpoint. The pinned processor rejects right context 1. The generator pads the final incomplete chunk with silence; native EOS and endpointing can differ, so this is a behavioral comparison rather than a tensor-parity claim.

The private results and generated media are ignored by Git. `summarize.py` writes raw-ASR script counts and literal Unicode word error rates. Literal WER penalizes transliteration and legitimate spelling/loanword variants; it is diagnostic only. The mixed-sentence script check requires actual Latin letters after removing emitted language tags. Intrasentence Hinglish loanwords are excluded from that check because their expected script is ambiguous.

Tested Python environment: Python 3.12, PyTorch 2.13.0+cu130, Transformers 5.18.0, NumPy 2.4.6, websocket-client 1.9.2, edge-tts 7.3.1, SoundFile 0.14.0, and FFmpeg. The test created an isolated dependency environment and reused the existing local CUDA PyTorch installation.
