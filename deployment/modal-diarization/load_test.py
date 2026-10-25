"""Submit a mixed-length batch at once; every request gets its own single-use container.

    python load_test.py ../../server/.env ../../benchmark/modal-diarization/fixtures results.jsonl

Requests are sealed to the deployed diarizer's public key (from the coordinator .env), exactly
as the Go client does, and results are opened with each job's response key.

Runs `app.py` as an ephemeral app (nothing stays deployed). Records, per request: upload time
(spawn returns once Modal holds the input), wait until the container starts processing,
processing time, GPU type, and request -> response.
"""
import json
import base64
import random
import secrets
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import modal

import envelope
from app import Diarizer, app
from provision import read_env

MIX = {'m010.opus': 4, 'm017.opus': 3, 'm030.opus': 4, 'h1.opus': 3, 'h2.opus': 4, 'h4.opus': 2}
RATE = {'L4': 0.000222, 'A10': 0.000306, 'L40S': 0.000542}  # $/s GPU, Modal base price 2026-10-25
CPU_MEM = 4*0.0000131+8*0.00000222


def main(env_file, fixtures, out):
    jobs = [name for name, count in MIX.items() for _ in range(count)]
    random.Random(7).shuffle(jobs)
    payloads = {name: (Path(fixtures)/name).read_bytes() for name in MIX}
    public = base64.b64decode(read_env(Path(env_file))['SPEEKTOME_DIARIZATION_PUBLIC_KEY'])
    with modal.enable_output(), app.run():
        diarizer = Diarizer()
        t0 = time.time()

        def submit(index):
            name, job_id = jobs[index], secrets.token_hex(16)
            sealed, response_key = envelope.seal_request(payloads[name], public, job_id, int(time.time()))
            submitted = time.time()
            call = diarizer.diarize.spawn(sealed)
            return index, name, submitted, time.time(), (call, job_id, response_key)
        with ThreadPoolExecutor(len(jobs)) as pool:
            calls = list(pool.map(submit, range(len(jobs))))

        def collect(item):
            index, name, submitted, uploaded, (call, job_id, response_key) = item
            result = json.loads(envelope.open_response(call.get(timeout=3600), response_key, job_id))
            result['timings']['started_at'] = time.time()-result['timings']['total_s']  # approximate
            return dict(index=index, fixture=name, submitted=submitted, uploaded=uploaded, responded=time.time(),
                        gpu=result['gpu'], audio_seconds=result['audio_seconds'], timings=result['timings'],
                        segments=len(result['segments']), speakers=len({s['speaker'] for s in result['segments']}))
        with ThreadPoolExecutor(len(calls)) as pool:
            rows = list(pool.map(collect, calls))
        wall = time.time()-t0

    with open(out, 'w') as sink:
        sink.writelines(json.dumps(row)+'\n' for row in rows)
    total = 0.0
    print(f"{'#':>2} {'meeting':>8} {'gpu':6} {'upload':>7} {'wait':>6} {'decode':>7} {'diarize':>8} {'resp':>6} {'~$':>8}")
    for row in sorted(rows, key=lambda r: r['audio_seconds']):
        t = row['timings']
        gpu = next((k for k in RATE if k in row['gpu']), row['gpu'])
        # Billed ~= processing plus ~5 s of container start (reconciled against the billing report).
        cost = (t['total_s']+5)*(RATE.get(gpu, 0)+CPU_MEM)
        total += cost
        print(f"{row['index']:2} {row['audio_seconds']/60:7.1f}m {gpu:6} {row['uploaded']-row['submitted']:6.1f}s "
              f"{t['started_at']-row['uploaded']:5.1f}s {t['decode_s']:6.1f}s {t['diarize_s']:7.1f}s "
              f"{row['responded']-row['submitted']:5.1f}s {cost:8.4f}  {row['speakers']} spk")
    hours = sum(r['audio_seconds'] for r in rows)/3600
    print(f'\n{len(rows)} requests, {hours:.1f} h of audio, batch wall {wall:.0f}s, estimated cost ${total:.3f}')


if __name__ == '__main__':
    main(*sys.argv[1:4])
