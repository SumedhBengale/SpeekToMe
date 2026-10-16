"""Repeat selected paired probes at realtime input cadence to check batching effects."""
import json, pathlib, time
import run_probe
ROOT=pathlib.Path(__file__).resolve().parent/'results'/'2026-10-16'
rows=json.loads((ROOT/'corpus.json').read_text());vocab=json.loads((ROOT/'vocabulary.json').read_text());token=pathlib.Path('/tmp/speektome-vocab-audit/worker.token').read_text().strip()
# run_probe reads this setting for an ordinary 560-ms producer cadence.
run_probe.SEND_INTERVAL=0.56
out=ROOT/'realtime-repeats.jsonl'
for rep in range(2):
 for row in rows:
  if row['id'] not in ['00-AriaNeural','06-AriaNeural','02-RyanNeural']:continue
  for boost in [None,0.5,1.5]:
   row['_path']=ROOT/row['audio'];r={k:v for k,v in row.items() if k!='_path'};r.update(repetition=rep,boost=boost)
   try:r.update(run_probe.transcribe('ws://127.0.0.1:18090',token,row,vocab,boost),status='ok')
   except Exception as e:r.update(status='error',error=str(e))
   with out.open('a') as f:f.write(json.dumps(r)+'\n')
   print(row['id'],rep,boost,r.get('text',r.get('error')),flush=True)
   if r['status']=='error':raise SystemExit(1)
