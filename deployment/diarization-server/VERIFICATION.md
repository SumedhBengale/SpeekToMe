# Verified deployment, 2026-10-08

- CPU INT8 bounded offline inference, two threads; ONNX Runtime 1.23.2.
- Six-minute Opus upload: 25.44 seconds processing after forced crash recovery, same job ID, 126 segments.
- Four-hour repeated meeting fixture: 964.56 seconds processing including 68.65s decode, 530 windows, 5,037 segments, four speakers. Peak service memory 2,373,296,128 bytes (2.21 GiB). Repeated audio validates resources/duration, not natural four-hour accuracy.
- Metadata sampled at 6ms during processing. Second submission 429/Retry-After15; oversized declaration413; missing/wrong key401; invalid chunked upload failed and released slot.
- JSON and RTTM fetched after submitter disconnected; finished jobs acknowledged and removed.
- SIGKILL restart recovered the uploaded job once; reboot returned to ready automatically.
- Selected model directory ~100MB; root filesystem ~3.4GiB free after cleanup. Log retention budget464MiB, with possible brief rotation/timer overshoot.

No transcription model or production main-server application changes are part of this deployment.
