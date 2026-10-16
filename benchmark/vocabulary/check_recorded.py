"""Verify query vocabulary also reaches recorded mic/system ASR streams."""
import json,pathlib,urllib.request,urllib.parse
root=pathlib.Path(__file__).resolve().parent/'results'/'2026-10-16'
rows=json.loads((root/'corpus.json').read_text());terms=json.loads((root/'vocabulary.json').read_text());token=pathlib.Path('/tmp/speektome-vocab-audit/worker.token').read_text().strip()
for row in rows:
 if row['id'] not in ['00-AriaNeural','06-AriaNeural']:continue
 for stage in ['mic','system']:
  query=urllib.parse.urlencode({'boosted_words':','.join(terms),'vocabulary_boost':'1.5','language':'en-US'})
  req=urllib.request.Request('http://127.0.0.1:18090/v1/meeting/'+stage+'?'+query,data=(root/row['audio']).read_bytes(),headers={'Authorization':'Bearer '+token,'Content-Type':'audio/wav','X-SpeekToMe-Meeting-ID':'vocabulary-test-20261016'})
  with urllib.request.urlopen(req,timeout=60) as resp:body=json.load(resp)
  result=dict(id=row['id'],stage=stage,boost=1.5,response=body)
  with (root/'recorded-check.jsonl').open('a') as f:f.write(json.dumps(result)+'\n')
  print(row['id'],stage,body.get('text'),flush=True)
