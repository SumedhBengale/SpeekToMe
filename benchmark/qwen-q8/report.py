import json
import math
import statistics
from pathlib import Path

root = Path(__file__).resolve().parent / 'results/2026-10-16'
cases = {p.name.removesuffix('.summary.json'):json.loads(p.read_text()) for p in root.glob('*.summary.json')}
def fmt(v, digits=2):
    return '—' if v is None else f'{v:.{digits}f}'
def row(name):
    d=cases[name]
    success=d['exit_code']==0 and d['completed']==d.get('expected_measured',d['batch']*d['iterations'])
    return f"| {name} | {'OK' if success else 'Failed'} | {d['completed']} | {fmt(d['measured_audio_seconds'])} | {fmt(d['audio_seconds_per_wall_second'])} | {fmt(d['batch_latency_p95_seconds'])} | {d['device_peak_mib']} |"

lines = ['# Qwen3-ASR 1.7B Q8 on a 3 GiB L4-3Q', '',
'Live benchmark: 16 October 2026. The stock Q8 runtime failed to load. Removing a duplicate 714 MiB weight buffer let it load. Initial one-second fully GPU-resident calls still ran out of memory; with encoder buffers released between phases and a 320-token context, eight repeated one-second calls later succeeded at 2.62× real time. Fully GPU-resident 2.5- and 5-second calls failed. This tiny-window result is not a practical meeting-context configuration. Unified memory is unsupported on this virtual GPU. These are results for the pinned transcribe.cpp / CUDA 13.2 build, not a claim about every possible runtime.', '',
'The working configuration keeps the original Q8 tensors, places the 315.30 MiB tied embedding/output matrix on CPU, runs the encoder and transformer decoder on CUDA, and explicitly enables Flash Attention. Scheduler inspection confirmed 24 encoder and 28 decoder-prefill Flash Attention nodes on CUDA. Complete single-window runs also confirmed 28 CUDA Flash Attention nodes during decoder steps. The patch releases encoder buffers before decoding and the previous request\'s KV before the next encoder pass.', '',
'The worker has two vCPUs on one SMT physical core, approximately 7.4 GiB RAM, and an L4-3Q reporting 3,072 MiB. CUDA reports 2,856.25 MiB total. In the fully resident attempt it reported 2,203.75 MiB free before allocating 2,078.11 MiB of model tensors, then only 119.94 MiB free. Idle context probes using the existing worker CUDA 12.8 runtime and the experimental CUDA 13.2 runtime both reported the same 2,203.75 MiB free before weights, including after GGML backend initialization. NVML device-usage readings do not capture all CUDA reservation constraints; an apparently comfortable NVML number did not prevent an OOM.', '',
'## Throughput and latency', '',
'One model/session stays loaded per case; full warmup is excluded. Throughput counts successfully completed audio seconds divided by complete ASR-call wall time, with batch time counted once. It excludes model/file loading, network, gateway, vocabulary matching, LLM processing and diarization. Latency is batch service time, not request queueing latency. Peak memory is sampled at 100 ms and can miss short peaks, especially during failed allocations.', '',
'| Case | Status | Completed windows | Audio seconds | Audio seconds / wall second | p95 batch seconds | Sampled device peak MiB |',
'|---|---|---:|---:|---:|---:|---:|']
preferred=['stock-5s-b1','lowmem-5s-b1','trace-1s-b1','unified-5s-b1','final-gpuonly-1s-b1','final-gpuonly-2.5s-b1','cpuhead-5s-b1','cpuhead-10s-b1','cpuhead-20s-b1','cpuhead-30s-b1','cpuhead-5s-b2','cpuhead-10s-b2','cpuhead-5s-b4','cpuhead-5s-b8','release-5s-b3','reset-5s-b3','reset-5s-b3-t1','reset-mixed-b3','reset-7.5s-b2','reset-15s-b1','reset-2.5s-b4','reset-2.5s-b4-repeat','reset-2.5s-b5','reset-2.5s-b6','reset-2.5s-b8','hour-5s-b3']
lines += [row(n) for n in preferred if n in cases]
lines += ['', 'The maximum observed rate was 5.80× real time with four 2.5-second windows, confirmed over 16 measured batches / 64 windows. Five-item batches were slower and six/eight-item batches failed. This is the best tested configuration, not a universal maximum. Short windows sacrifice linguistic context and introduce more boundaries.', '',
'Three 5-second windows achieved 5.32× in the initial repeated test. Mixed 2.5/5-second windows achieved 4.48×. The batch path pads shorter items to the longest input and continues stepping finished rows while others decode. Grouping by duration would matter in an offline job queue. Two CPU threads outperformed one in the tested three-item batch. After releasing buffers between phases, one 15-second window also completed repeated calls at 3.04×; 20/30-second attempts failed. Enforce per-item duration and batch limits, not just a global connection count.', '',
'## One audio-hour endurance', '']
if 'hour-5s-b3' in cases:
    d=cases['hour-5s-b3']
    gpu=[json.loads(x) for x in (root/'hour-5s-b3.gpu.jsonl').read_text().splitlines()]
    # Compare maxima in four equal elapsed-time bins after the first 10s of initialization.
    active=[g for g in gpu if g['elapsed']>=10]
    lo,hi=active[0]['elapsed'],active[-1]['elapsed']
    peaks=[]
    for i in range(4):
        group=[g['memory_mib'] for g in active if lo+(hi-lo)*i/4<=g['elapsed']<=lo+(hi-lo)*(i+1)/4]
        peaks.append(max(group) if group else None)
    lines += [f"Completed {d['completed']}/720 windows, representing {d['measured_audio_seconds']:.0f} seconds of audio, in {d['measured_wall_seconds']/60:.2f} measured minutes ({d['audio_seconds_per_wall_second']:.2f}× real time). Failures: {d['failed_measured']}; missing windows: {d['missing_measured']}; process exit: {d['exit_code']}. p50/p95/p99 batch service times: {d['batch_latency_p50_seconds']:.3f}/{d['batch_latency_p95_seconds']:.3f}/{d['batch_latency_p99_seconds']:.3f} seconds. Sampled device peak: {d['device_peak_mib']} MiB. Four successive elapsed-time-bin peaks after initialization: {peaks} MiB. Sampled process RSS peak: {d['sampled_process_peak_rss_mib']:.1f} MiB.", '',
    'This workload repeats eight human-speech fixtures for 720 sequential five-second windows, submitted in 240 batches of three. It demonstrates one audio-hour of completed work and bounded retained state on this workload. It is not a genuine one-hour meeting, an uninterrupted attention context, or a validation of transcript stitching, overlap, diarization, WER or omission rate. There is no whole-hour GPU tensor or growing transcript/KV prefix. Every measured output has OK status and nonempty text.']
    (root/'hour-memory-analysis.json').write_text(json.dumps({'quarter_peaks_mib':peaks,'scope':'equal elapsed-time bins after first 10 seconds; NVML sampled every 100ms'},indent=2)+'\n')
else:
    lines += ['Endurance result pending.']
accuracy_path=root/'accuracy-summary.json'
if accuracy_path.exists():
    d=json.loads(accuracy_path.read_text())
    lines += ['', '## Output sanity check', '', f"Eight complete synthetic Indian-English holdout clips / {d['reference_words']} reference words produced {d['word_edits']} word edits ({100*d['wer']:.2f}% normalized WER). No vocabulary prompt or LLM was used. This small clean set does not establish meeting or multilingual accuracy. Detailed references and outputs are in `accuracy-summary.json`. Separately, 23/24 five-second batched transcripts exactly matched the corresponding single-window output strings."]
lines += ['', '## State and reproducibility', '',
'Nemotron and the test worker gateway were stopped only on the east worker, with zero active requests observed beforehand. The fleet controller was paused during the experiment to prevent replacement of the test worker. The west worker remained running Nemotron and its gateway. Production restoration status is recorded in `final-state.json`.', '',
'Restoration required an ASR-only readiness probe: the existing smoke test requested GPU diarization although the worker loads only ASR. Added an opt-in ASR-only smoke flag (diarized mode remains the default), required a commit acknowledgment as well as nonempty text, and applied the flag to this worker\'s readiness script. Gateway tests passed. Both original workers were subsequently confirmed ACTIVE and ready with the controller running. The fix is local to the test worker; fleet launch artifacts were not republished.', '',
'The experimental runtime and Q8 weights remain in `/home/ubuntu/qwen-q8-test` on the east worker for follow-up work. No fleet launch configuration or main-server ASR route was changed to deploy Qwen. Temporary S3 staging objects were deleted. See `../../README.md`, the pinned model hash, the saved runtime patch, per-case JSONL transcripts, stderr logs, GPU samples and summaries.']
(root/'REPORT.md').write_text('\n'.join(lines)+'\n')
print(root/'REPORT.md')
