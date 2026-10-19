"""Summarize raw-ASR script failures and literal WER; no LLM output is scored."""
import json,re,unicodedata
from pathlib import Path
ROOT=Path(__file__).parent/'results'/'2026-10-17'
corpus={x['id']:x for x in json.loads((ROOT/'corpus.json').read_text())}
def clean(s):
 s=re.sub(r'<[A-Za-z]{2,3}(?:-[A-Za-z0-9]+)?>','',s)
 return ''.join(c.lower() if unicodedata.category(c)[0] in 'LMN' or c.isspace() else ' ' for c in unicodedata.normalize('NFKC',s))
def distance(a,b):
 row=list(range(len(b)+1))
 for i,x in enumerate(a,1):
  new=[i]
  for j,y in enumerate(b,1):new.append(min(new[-1]+1,row[j]+1,row[j-1]+(x!=y)))
  row=new
 return row[-1]
summary={};details=[]
for p in sorted(ROOT.glob('*.jsonl')):
 if not p.name.startswith(('reference-r','worker-e','app-e')):continue
 rs=[json.loads(l) for l in p.read_text().splitlines() if l.strip()]
 seen=set()
 for r in rs:
  key=(r['id'],r['language']);assert key not in seen,(p,key);seen.add(key)
  c=corpus[r['id']];ref=clean(c['reference']).split();hyp=clean(r['text']).split()
  r['literal_wer']=distance(ref,hyp)/len(ref)
  r['devanagari_chars']=sum('\u0900'<=x<='\u097f' and unicodedata.category(x)[0] in 'LM' for x in clean(r['text']))
  r['latin_chars']=sum('a'<=x<='z' for x in clean(r['text']))
  details.append({k:v for k,v in r.items() if k not in ['tokens','interims','completed']})
 auto=[r for r in rs if r['language']=='auto'];english=[r for r in auto if r['id'].startswith('english-')];mixed=[r for r in auto if r['id'] in ['en-hi-en','hi-en-hi']]
 summary[p.name]={'completed':len(rs),'auto_english_clips':len(english),'auto_english_with_devanagari':sum(r['devanagari_chars']>0 for r in english),'mixed_sentence_clips':len(mixed),'mixed_without_any_latin':sum(r['latin_chars']==0 for r in mixed),'forced_english_monolingual_wer':{r['id']:r['literal_wer'] for r in rs if r['language']=='en-US' and r['id'].startswith('english-')}}
(ROOT/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
(ROOT/'details.json').write_text(json.dumps(details,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(summary,indent=2))
