"""Run a CUDA-only benchmark case and sample device memory/utilization at 100ms."""
import argparse
import json
import math
import os
import resource
import subprocess
import threading
import time
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('name')
p.add_argument('--manifest', default='audio/mixed.tsv')
p.add_argument('--batch', type=int, default=1)
p.add_argument('--iterations', type=int, default=16)
p.add_argument('--context', type=int, default=1024)
p.add_argument('--threads', type=int, default=2)
p.add_argument('--timeout', type=int, default=1800)
p.add_argument('--unpacked', action='store_true')
args = p.parse_args()
resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
cpu_before = resource.getrusage(resource.RUSAGE_CHILDREN)
root = Path(__file__).resolve().parent
results = root / 'results'
results.mkdir(exist_ok=True)
env = {**os.environ, 'TRANSCRIBE_FORCE_FLASH':'1', 'CUDA_VISIBLE_DEVICES':'0',
       'TRANSCRIBE_VERIFY_FLASH':'1',
       'LD_LIBRARY_PATH': str(root/'runtime/lib')+':'+str(root/'cuda/usr/local/cuda-13.2/targets/x86_64-linux/lib')}
if args.unpacked:
    env['TRANSCRIBE_QWEN_LOW_MEMORY'] = '1'
cmd = [str(root/'runtime/bin/offline-bench'), str(root/'Qwen3-ASR-1.7B-Q8_0.gguf'),
       args.manifest, str(args.batch), str(args.iterations), str(args.context), str(args.threads)]
samples = []
rss_samples = []
process_holder = []
start = time.monotonic()
monitor = subprocess.Popen(['nvidia-smi','--query-gpu=memory.used,utilization.gpu,utilization.memory',
                            '--format=csv,noheader,nounits','--loop-ms=100'],
                           stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
def sample():
    with (results/f'{args.name}.gpu.jsonl').open('w') as f:
        for line in monitor.stdout:
            try:
                memory, gpu, mem_util = [int(x.strip()) for x in line.split(',')]
                row = {'elapsed':time.monotonic()-start,'memory_mib':memory,'gpu_percent':gpu,'memory_util_percent':mem_util}
                samples.append(row)
                if process_holder:
                    try:
                        status = Path(f'/proc/{process_holder[0].pid}/status').read_text()
                        hwm = next(line for line in status.splitlines() if line.startswith('VmHWM:'))
                        rss_samples.append(int(hwm.split()[1])/1024)
                    except (FileNotFoundError, StopIteration, ProcessLookupError):
                        pass
                f.write(json.dumps(row)+'\n')
                f.flush()
            except ValueError:
                pass
t = threading.Thread(target=sample,daemon=True)
t.start()
with (results/f'{args.name}.jsonl').open('w') as out, (results/f'{args.name}.stderr').open('w') as err:
    proc = subprocess.Popen(cmd,stdout=out,stderr=err,env=env,cwd=root)
    process_holder.append(proc)
    timed_out = False
    try:
        code = proc.wait(timeout=args.timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        proc.kill()
        code = proc.wait()
monitor.terminate()
monitor.wait()
t.join(timeout=2)
rows = [json.loads(line) for line in (results/f'{args.name}.jsonl').read_text().splitlines() if line.strip()]
measured = [r for r in rows if r.get('event')=='result' and not r['warmup']]
success = [r for r in measured if r['success']]
walls = {r['iteration']:r['batch_wall_seconds'] for r in measured}
latencies = sorted(walls.values())
def pct(q):
    return latencies[max(0,math.ceil(len(latencies)*q)-1)] if latencies else None
wall = sum(walls.values())
audio = sum(r['audio_seconds'] for r in success)
summary = {
    **vars(args),'exit_code':code,'timed_out':timed_out,'completed':len(success),
    'expected_measured':args.batch*args.iterations,
    'missing_measured':args.batch*args.iterations-len(measured),
    'warmup_failed':any(r.get('event')=='result' and r['warmup'] and not r['success'] for r in rows),
    'failed_measured':len(measured)-len(success),'measured_audio_seconds':audio,
    'measured_wall_seconds':wall,'audio_seconds_per_wall_second':audio/wall if wall else None,
    'batch_latency_p50_seconds':pct(.50),'batch_latency_p95_seconds':pct(.95),'batch_latency_p99_seconds':pct(.99),
    'device_peak_mib':max((s['memory_mib'] for s in samples), default=None),
    'process_wall_seconds':time.monotonic()-start,
    'child_cpu_seconds':resource.getrusage(resource.RUSAGE_CHILDREN).ru_utime + resource.getrusage(resource.RUSAGE_CHILDREN).ru_stime - cpu_before.ru_utime - cpu_before.ru_stime,
    'sampled_process_peak_rss_mib':max(rss_samples,default=None),
    'cuda_module_loading':env.get('CUDA_MODULE_LOADING'),
    'cpu_embedding_offload':bool(env.get('TRANSCRIBE_QWEN_CPU_EMBED')),
    'release_encoder_workspace':bool(env.get('TRANSCRIBE_QWEN_RELEASE_ENCODER')),
    'scope':'resident-model offline runtime; timed complete ASR calls including CPU mel, encoder and decoder; excludes model load, file loading, gateway and LLM',
}
(results/f'{args.name}.summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary),flush=True)
raise SystemExit(code)
