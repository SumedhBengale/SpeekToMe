"""Real-time confirmation of human negative controls and one new target sentence."""
import json,pathlib,subprocess
import run_probe,followup
ROOT=followup.ROOT;run_probe.SEND_INTERVAL=.56
rows=followup.corpus();vocab=json.loads((ROOT/'vocabulary.json').read_text());token=pathlib.Path('/tmp/speektome-vocab-audit/worker.token').read_text().strip();output=ROOT/'realtime-confirmation.jsonl'
for row in rows:
 if row['id'] not in ['human-0600-0','human-0600-4','holdout-00']:continue
 for boost in [None,.5]:
  row['_path']=pathlib.Path(row['audio_path']);r={k:v for k,v in row.items() if k!='_path'};r.update(boost=boost,send_interval=.56)
  r.update(run_probe.transcribe('ws://127.0.0.1:18090',token,row,vocab,boost),status='ok')
  reqs=[dict(id=mode,text=r['text'],terms=vocab,mode=mode) for mode in ['exact','legacy']]
  result=subprocess.run(['ssh','speektome-direct','/home/ec2-user/speektome/vocab-eval-policies'],input=''.join(json.dumps(q)+'\n' for q in reqs),text=True,capture_output=True,check=True)
  r['server_outputs']={q['id']:q for q in map(json.loads,result.stdout.splitlines())}
  with output.open('a') as f:f.write(json.dumps(r)+'\n')
  print(row['id'],boost,'ASR:',r['text'],'EXACT:',r['server_outputs']['exact']['text'],'LEGACY:',r['server_outputs']['legacy']['text'],flush=True)
