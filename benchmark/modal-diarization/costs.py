"""Per-job and monthly Modal cost from measured benchmark rows.

    python costs.py results/*.jsonl

Prices are Modal's published base rates (modal.com/pricing, read 2026-10-25), preemptible,
no region pinning. A job is billed for container start + model load + processing + the idle
`scaledown_window` that follows when no other job arrives (the worst case, assumed here).
"""
import json
import sys
from collections import defaultdict
from statistics import median

GPU_PER_S = {'T4': 0.000164, 'L4': 0.000222, 'A10': 0.000306, 'L40S': 0.000542, 'A100-40GB': 0.000583,
             'A100-80GB': 0.000694, 'H100': 0.001097}
CORE_PER_S = 0.0000131  # one physical core = 2 vCPU
GIB_PER_S = 0.00000222
MEMORY_GIB = 8
BOOT_S = 3.0  # container start before @enter, measured as cold queue_to_start minus load_s (rounded up)
FREE_CREDIT = 30.0
SCALEDOWN_S = 20  # proposed production scaledown_window
CPU_T4G_HOURLY = 0.0336  # current always-on t4g.medium, us-east-1 on-demand


def rate(gpu, cores):
    return GPU_PER_S[gpu]+cores*CORE_PER_S+MEMORY_GIB*GIB_PER_S


def main(paths):
    rows = [json.loads(line) for path in paths for line in open(path) if line.strip()]
    groups = defaultdict(list)
    for row in rows:
        if row['parallel'] == 1:
            groups[(row['requested_gpu'], row.get('cpu', 4), row.get('workers', 0), row['fixture'])].append(row)
    print(f"{'gpu':10} {'cores':>5} {'wrk':>3} {'fixture':10} {'audio':>6} {'cold':>6} {'warm':>6} {'x rt':>6} "
          f"{'$/job':>8} {'$/audio-h':>9} {'jobs/$30':>8}")
    for (gpu, cores, workers, fixture), group in sorted(groups.items(), key=lambda kv: (kv[0][3], kv[0][0])):
        warm = [r['timings']['total_in_container_s'] for r in group if r['warm']] or \
            [r['timings']['total_in_container_s'] for r in group]
        cold = [r['queue_to_start_s']+r['timings']['total_in_container_s'] for r in group if not r['warm']]
        load = median(r['load_s'] for r in group)
        processing = median(warm)
        audio_h = group[0]['audio_s']/3600
        cost = (BOOT_S+load+processing+SCALEDOWN_S)*rate(gpu, cores)
        print(f"{gpu:10} {cores:5g} {workers:3} {fixture:10} {audio_h*60:5.0f}m {(median(cold) if cold else float('nan')):5.1f}s "
              f"{processing:5.1f}s {audio_h*3600/processing:5.0f}x ${cost:7.4f} ${cost/audio_h:8.4f} {FREE_CREDIT/cost:8.0f}")
    print(f'\nBilled per job: {BOOT_S:.0f}s boot + model load + processing + '
          f'{SCALEDOWN_S}s scaledown_window idle, {MEMORY_GIB} GiB RAM. Current t4g.medium: ${CPU_T4G_HOURLY*730:.2f}/month always-on.')


if __name__ == '__main__':
    main(sys.argv[1:])
