"""Native worker protocol acceptance with the full 1000-term dictionary."""
import hashlib,json,pathlib,threading,time,wave,websocket
ROOT=pathlib.Path(__file__).parent/'results'/'2026-10-16-jev'
terms=json.loads((ROOT/'vocabulary-1000.json').read_text());token=pathlib.Path('/tmp/speektome-vocab-audit/worker.token').read_text().strip()
for row in json.loads((ROOT/'acceptance-corpus.json').read_text())[:2]:
 ws=websocket.create_connection('ws://127.0.0.1:18090/v1/realtime',header={'Authorization':'Bearer '+token},timeout=25)
 ws.send(json.dumps({'type':'session.update','session':{'language':'en-US','sample_rate':16000,'word_timestamps':True,'endpointing_ms':1200,'vocabulary_matching':{'terms':terms}}}))
 with wave.open(row['audio']) as w:pcm=w.readframes(w.getnframes())
 timing={};errors=[]
 def send():
  try:
   for i in range(0,len(pcm),17920):ws.send_binary(pcm[i:i+17920]);time.sleep(.05)
   timing['commit']=time.perf_counter();ws.send(json.dumps({'type':'input_audio_buffer.commit'}))
  except Exception as e:errors.append(str(e))
 sender=threading.Thread(target=send);sender.start();events=[];result=None
 try:
  while True:
   e=json.loads(ws.recv());events.append(e)
   if e['type']=='vocabulary.final':result=e;break
 finally:ws.close();sender.join()
 assert result and result['raw_transcript'] and result['schema_version']==1 and not errors
 assert any(e['type']=='input_audio_buffer.committed' for e in events)
 for e in events:
  if e['type']=='session.updated':assert not e.get('session',{}).get('speech_contexts')
 output=dict(id=row['id'],raw=result['raw_transcript'],proposal=result,post_commit_ms=(time.perf_counter()-timing['commit'])*1000,events=events,audio_sha256=hashlib.sha256(pathlib.Path(row['audio']).read_bytes()).hexdigest())
 with (ROOT/'gateway-live.jsonl').open('a') as f:f.write(json.dumps(output)+'\n')
 print(row['id'],result['raw_transcript'],'->',result['vocabulary_transcript'],'match_ms',result['match_ms'],flush=True)
