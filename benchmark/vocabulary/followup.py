"""Compare explicit server policies and nearby inference strengths.
Original ASR baseline/0.5 transcripts are reused byte-for-byte; new inference
conditions and all new human recordings go through the real worker gateway.
"""
import argparse, collections, hashlib, json, pathlib, random, subprocess
import run_probe, score
ROOT=pathlib.Path(__file__).resolve().parent/'results'/'2026-10-16-followup'
OLD=ROOT.parent/'2026-10-16'
CONFIGS=[('baseline',None,'full'),('boost-0.25',0.25,'full'),('boost-0.5',0.5,'full'),('boost-0.75',0.75,'full'),('narrow-0.5',0.5,'engineering')]

def corpus():
 rows=json.loads((OLD/'corpus.json').read_text())
 for r in rows:r.update(cohort='original-calibration',audio_path=str(OLD/r['audio']))
 human=json.loads((ROOT/'human-corpus.json').read_text())
 for r in human:r.update(cohort='new-human',audio_path=str(ROOT/r['audio']))
 held=[]
 if (ROOT/'holdout-corpus.json').exists():
  held=json.loads((ROOT/'holdout-corpus.json').read_text())
  for r in held:r.update(cohort='new-synthetic',audio_path=str(ROOT/r['audio']))
 return rows+human+held

def inference():
 rows=corpus();(ROOT/'corpus.json').write_text(json.dumps(rows,indent=2)+'\n')
 vocab=json.loads((ROOT/'vocabulary.json').read_text());small=json.loads((ROOT/'engineering-vocabulary.json').read_text());token=pathlib.Path('/tmp/speektome-vocab-audit/worker.token').read_text().strip()
 output=ROOT/'inference.jsonl'
 if not output.exists():
  byid={r['id']:r for r in rows}
  with output.open('w') as f:
   for line in (OLD/'inference.jsonl').read_text().splitlines():
    r=json.loads(line)
    if r['condition'] in ['baseline','boost-0.5'] and r['status']=='ok':
     r.update(cohort='original-calibration',audio_path=byid[r['id']]['audio_path'],reused_from=str(OLD/'inference.jsonl'),vocabulary_profile='full');f.write(json.dumps(r)+'\n')
 done={(r['id'],r['condition']) for r in map(json.loads,output.read_text().splitlines()) if r['status']=='ok'}
 for row in rows:
  configs=list(CONFIGS);random.Random('followup-'+row['id']).shuffle(configs)
  for condition,boost,profile in configs:
   if (row['id'],condition) in done:continue
   row['_path']=pathlib.Path(row['audio_path']);result={k:v for k,v in row.items() if k!='_path'};terms=small if profile=='engineering' else vocab
   result.update(condition=condition,boost=boost,vocabulary_profile=profile,vocabulary_count=len(terms) if boost else 0)
   try:result.update(run_probe.transcribe('ws://127.0.0.1:18090',token,row,terms,boost),status='ok')
   except Exception as e:result.update(status='error',error=str(e))
   with output.open('a') as f:f.write(json.dumps(result)+'\n');f.flush()
   print(row['id'],condition,result['status'],result.get('text',result.get('error')),flush=True)
   if result['status']!='ok':raise SystemExit('Stopped on error; completed rows are resumable.')

def summarize():
 raw=[json.loads(s) for s in (ROOT/'inference.jsonl').read_text().splitlines()];raw=[r for r in raw if r['status']=='ok'];vocab=json.loads((ROOT/'vocabulary.json').read_text());aliases=json.loads((ROOT/'aliases.json').read_text());small=json.loads((ROOT/'engineering-vocabulary.json').read_text());by={(r['id'],r['condition']):r for r in raw}
 rows=[];requests=[];metadata={}
 specs=[('baseline','none','baseline'),('baseline','legacy','server-legacy'),('baseline','legacy-narrow','server-legacy-narrow'),('baseline','exact-narrow','server-exact-narrow'),('baseline','exact','server-exact'),('baseline','guarded','server-guarded'),('baseline','aliases','server-aliases'),('boost-0.5','none','inference-0.5'),('boost-0.5','legacy','0.5+legacy'),('boost-0.5','exact','0.5+exact'),('boost-0.5','guarded','0.5+guarded'),('boost-0.5','aliases','0.5+aliases'),('boost-0.25','exact','0.25+exact'),('boost-0.75','exact','0.75+exact'),('narrow-0.5','exact','narrow-0.5+exact')]
 for source,mode,condition in specs:
  for r in raw:
   if r['condition']!=source:continue
   new=dict(r);new.update(condition=condition,asr_condition=source,server_policy=mode,server_vocabulary_count=len(small) if mode.endswith('-narrow') else len(vocab))
   if mode=='none':rows.append(new);continue
   key=r['id']+'::'+condition;metadata[key]=new
   requests.append(dict(id=key,text=r['text'],terms=small if mode.endswith('-narrow') else vocab,mode='exact' if mode=='aliases' else mode.removesuffix('-narrow'),aliases=aliases if mode=='aliases' else {}))
 result=subprocess.run(['ssh','speektome-direct','/home/ec2-user/speektome/vocab-eval-policies'],input=''.join(json.dumps(r)+'\n' for r in requests),text=True,capture_output=True,check=True)
 (ROOT/'server-matcher.log').write_text(result.stderr)
 for item in map(json.loads,result.stdout.splitlines()):
  r=metadata.pop(item['id']);r.update(text=item['text'],replaced=item['replaced'],matcher_duration_us=item['duration_us']);rows.append(r)
 assert not metadata
 groups=collections.defaultdict(list)
 for r in rows:
  groups[(r['condition'],r['cohort'])].append(r)
  if r['cohort']=='original-calibration':groups[(r['condition'],'original-'+r['category'])].append(r)
 summaries=[]
 for (condition,cohort),rs in sorted(groups.items()):
  subs=dels=ins=n=hit=targets=changed=worse=better=contaminated=0;latencies=[]
  for r in rs:
   _,s,d,i=score.edits(r['reference'],r['text']);subs+=s;dels+=d;ins+=i;n+=len(score.words(r['reference']));targets+=len(r['targets']);hit+=sum(score.contains(r['text'],t) for t in r['targets'])
   base=by[(r['id'],'baseline')];base_errors=score.edits(r['reference'],base['text'])[0]
   changed+=score.words(r['text'])!=score.words(base['text']);worse+=s+d+i>base_errors;better+=s+d+i<base_errors
   injected=[t for t in vocab if score.contains(r['text'],t) and not score.contains(r['reference'],t) and not score.contains(base['text'],t)]
   r['new_unspoken_vocabulary']=injected;contaminated+=bool(injected)
   if 'matcher_duration_us' in r:latencies.append(r['matcher_duration_us'])
  summaries.append(dict(condition=condition,cohort=cohort,clips=len(rs),words=n,errors=subs+dels+ins,substitutions=subs,deletions=dels,insertions=ins,wer=(subs+dels+ins)/n if n else 0,target_hits=hit,target_total=targets,clips_changed=changed,clips_worse=worse,clips_better=better,clips_with_new_unspoken_vocabulary=contaminated,matcher_us_max=max(latencies) if latencies else None))
 (ROOT/'scored.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in rows));(ROOT/'summary.json').write_text(json.dumps(summaries,indent=2)+'\n')
 for r in summaries:
  if r['cohort'] in ['original-calibration','new-human','new-synthetic']:print(json.dumps(r))
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('phase',choices=['inference','score']);args=p.parse_args();inference() if args.phase=='inference' else summarize()
