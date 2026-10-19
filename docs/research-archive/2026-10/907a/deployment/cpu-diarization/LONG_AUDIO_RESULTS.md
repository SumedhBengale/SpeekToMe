# One-hour Opus CPU diarization test

Tested 2026-10-08 on the same t4g.medium (2 ARM vCPUs, 4 GiB RAM), ONNX INT8, ONNX Runtime 1.23.2, CPUExecutionProvider, two threads, published bounded-window geometry. No concurrent inference workloads.

The six-minute AMI ES2004a fixture was repeated ten times, then encoded as mono Opus with 16 kHz input, 24 kbps VBR, application `voip`. The file contains exactly 3,600 seconds of decoded audio. This is a duration/load test, not a one-hour natural-meeting accuracy evaluation.

| Metric | Observed |
|---|---:|
| Compressed file size | 7,770,913 bytes (7.77 MB / 7.41 MiB) |
| Opus decode/resample to 16 kHz mono | 17.11 s |
| Decode, audio reading, model load combined | 18.03 s |
| Diarization, including feature extraction | 226.09 s |
| Total process wall time | **244.46 s (4 min 4.46 s)** |
| Peak process RSS | **808.83 MiB** |
| Real-time factor, complete process | 0.06791 (~14.7x faster than audio duration) |
| Windows completed | 133 |
| Speaker identities / output segments | 4 / 1,288 |
| Probability matrix | [1, 360001, 8], all finite |
| Exit / safety cutoff | 0 / none |

Timings include process startup, decoding, loading, inference, output writing and shutdown, but exclude uploading the file, queue wait, instance boot and model download. Encoding the test fixture was also outside the timed process because clients provide already-compressed recordings.

The original sampler's CPU counter did not retain the decoder child's CPU time after that process exited, so its JSON CPU average should not be read as whole-pipeline CPU accounting. The sampler was updated for future runs to retain each observed process's CPU time. Peak RSS included active child processes; this limitation does not affect elapsed time or peak RAM. The model phase remained close to full use of both cores.

## Duration projections, not measurements

| Audio duration | Estimated processing time | Estimated peak process RAM |
|---|---:|---:|
| 1 hour | **4 min 4.5 s, measured** | **809 MiB, measured** |
| 2 hours | ~8 min 9 s | ~1.2 GiB |
| 4 hours | ~16 min 18 s | ~2.1 GiB |

Time projections linearly scale the one-hour complete-process result. RAM projections linearly extend the observed six-minute and one-hour INT8 peak-RSS measurements. They exclude OS RAM and are not validated for a four-hour file. The six-minute run did not include Opus decoding, but decoding was below the model-phase RAM peak in the one-hour run.

Bounded attention and its speaker cache have fixed window sizes, so the attention working set does not grow with full recording length. However, this wrapper holds decoded float audio, the whole recording's mel array, and output arrays in memory. Opus file size therefore does not describe inference RAM requirements. The existing 4 GiB instance has plausible headroom for these projections; the current whole-file wrapper should not be assumed safe for four hours on a 2 GiB instance.

Keep the recording as one logical diarization job, preserving the NumPy speaker cache across its windows. Restarting independent jobs for successive audio sections would reset speaker identities. These measurements cover diarization only; transcript text still requires separate ASR and subsequent timestamp-based association with speaker segments.

## Reproduce

After SSH to the test instance:

```bash
cd /opt/speektome-diarization
BENCH_AUDIO_SECONDS=3600 venv/bin/python -u benchmark.py repeat-1hr-opus \
  venv/bin/python -u onnx-test.py repeat-1hr-opus model.int8.onnx bounded \
  models/meeting-3600.opus | tee -a results/benchmark.log
```

Raw timings, stages and speaker segments are in `results/onnx-int8-1hr-opus.*`. Model weights, input Opus and frame probabilities remain on the instance. All tests finished; no inference test processes remain running. The instance remains running.
