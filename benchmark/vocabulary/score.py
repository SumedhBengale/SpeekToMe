"""Score paired results and run the same production matcher on the main server."""
import collections, json, pathlib, re, subprocess
ROOT=pathlib.Path(__file__).resolve().parent/'results'/'2026-10-16'
def words(s):return re.findall(r"[a-z0-9]+",s.lower().replace("'",''))
def edits(ref,hyp):
 a,b=words(ref),words(hyp);prev=[(j,0,0,j) for j in range(len(b)+1)]
 for i,x in enumerate(a,1):
  cur=[(i,0,i,0)]
  for j,y in enumerate(b,1):
   if x==y:cur.append(prev[j-1]);continue
   v=prev[j-1]; sub=(v[0]+1,v[1]+1,v[2],v[3]);v=prev[j];delete=(v[0]+1,v[1],v[2]+1,v[3]);v=cur[j-1];insert=(v[0]+1,v[1],v[2],v[3]+1)
   cur.append(min([sub,delete,insert],key=lambda v:v[0]))
  prev=cur
 return prev[-1]
def contains(text,term):
 return bool(re.search(r'(?<![a-z0-9])'+re.escape(' '.join(words(term)))+r'(?![a-z0-9])',' '.join(words(text))))
def main():
 rows=[json.loads(s) for s in (ROOT/'inference.jsonl').read_text().splitlines()];rows=[r for r in rows if r['status']=='ok'];vocab=json.loads((ROOT/'vocabulary.json').read_text());baseline={r['id']:r for r in rows if r['condition']=='baseline'}
 requests=''.join(json.dumps({'id':k,'text':r['text'],'terms':vocab})+'\n' for k,r in baseline.items())
 result=subprocess.run(['ssh','speektome-direct','/home/ec2-user/speektome/vocab-eval'],input=requests,text=True,capture_output=True,check=True)
 (ROOT/'server-matcher.log').write_text(result.stderr)
 matched=[]
 for item in map(json.loads,result.stdout.splitlines()):
  r=dict(baseline[item['id']]);r.update(condition='server-matcher',text=item['text'],replaced=item['replaced'],matcher_duration_us=item['duration_us']);matched.append(r)
 (ROOT/'server-matcher.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in matched))
 rows+=matched
 groups=collections.defaultdict(list)
 for row in rows:
  groups[(row['condition'],'all')].append(row);groups[(row['condition'],row['category'])].append(row)
 summary=[]
 for (condition,category),rs in sorted(groups.items()):
  s=d=i=n=hit=targets=contam=contaminated=changed=worse=better=0
  for r in rs:
   _,ss,dd,ii=edits(r['reference'],r['text']);s+=ss;d+=dd;i+=ii;n+=len(words(r['reference']))
   targets+=len(r['targets']);hit+=sum(contains(r['text'],t) for t in r['targets'])
   base=baseline.get(r['id']); injected=[]
   if base:
    injected=[t for t in vocab if contains(r['text'],t) and not contains(r['reference'],t) and not contains(base['text'],t)]
    changed+=words(base['text'])!=words(r['text'])
    ebase=edits(r['reference'],base['text'])[0];e=ss+dd+ii;worse+=e>ebase;better+=e<ebase
   contaminated+=bool(injected);contam+=len(injected);r['new_unspoken_vocabulary']=injected
  summary.append(dict(condition=condition,category=category,clips=len(rs),reference_words=n,substitutions=s,deletions=d,insertions=i,wer=(s+d+i)/n if n else None,target_hits=hit,target_total=targets,new_unspoken_term_occurrences=contam,clips_with_new_unspoken_terms=contaminated,clips_changed_from_baseline=changed,clips_worse_than_baseline=worse,clips_better_than_baseline=better))
 (ROOT/'summary.json').write_text(json.dumps(summary,indent=2)+'\n');(ROOT/'scored.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in rows))
 for r in summary:
  if r['category'] in ['all','confuser','ordinary','human']:print(json.dumps(r))
if __name__=='__main__':main()
