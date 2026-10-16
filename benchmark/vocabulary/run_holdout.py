"""Run new synthetic holdout cases on worker two, isolated from worker-one trials."""
import json,pathlib,random,socket,subprocess,time
import followup,run_probe
ROOT=followup.ROOT
cmd=['ssh','-o','ExitOnForwardFailure=yes','-N','-L','127.0.0.1:18091:127.0.0.1:8090','-o','IdentitiesOnly=yes','-i',str(pathlib.Path.home()/'.ssh/YOUR_SSH_KEY'),'-J','speektome-direct','ubuntu@WORKER_IPV6']
tunnel=subprocess.Popen(cmd,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
try:
 for _ in range(120):
  try:
   with socket.create_connection(('127.0.0.1',18091),timeout=1):break
  except OSError:time.sleep(.25)
 output=ROOT/'inference-heldout.jsonl';done={(r['id'],r['condition']) for r in map(json.loads,output.read_text().splitlines()) if r['status']=='ok'} if output.exists() else set()
 token=pathlib.Path('/tmp/speektome-vocab-audit/worker.token').read_text().strip();full=json.loads((ROOT/'vocabulary.json').read_text());small=json.loads((ROOT/'engineering-vocabulary.json').read_text())
 for row in json.loads((ROOT/'holdout-corpus.json').read_text()):
  configs=list(followup.CONFIGS);random.Random('followup-'+row['id']).shuffle(configs)
  for condition,boost,profile in configs:
   if (row['id'],condition) in done:continue
   row['_path']=ROOT/row['audio'];r={k:v for k,v in row.items() if k!='_path'};terms=small if profile=='engineering' else full
   r.update(cohort='new-synthetic',audio_path=str(row['_path']),condition=condition,boost=boost,vocabulary_profile=profile,vocabulary_count=len(terms) if boost else 0,worker_id='i-REDACTED')
   try:r.update(run_probe.transcribe('ws://127.0.0.1:18091',token,row,terms,boost),status='ok')
   except Exception as e:r.update(status='error',error=str(e))
   with output.open('a') as f:f.write(json.dumps(r)+'\n')
   print(row['id'],condition,r['status'],r.get('text',r.get('error')),flush=True)
   if r['status']!='ok':raise SystemExit(1)
finally:tunnel.terminate();tunnel.wait(timeout=10)
