"""Paired inference-only benchmark through the authenticated worker gateway."""
import argparse, hashlib, json, pathlib, random, subprocess, threading, time, urllib.parse, wave
import websocket
ROOT=pathlib.Path(__file__).resolve().parent
SEND_INTERVAL=0.04

def transcribe(base, token, row, vocab, boost, route='/v1/realtime'):
 q={}
 if boost is not None:q={'boosted_words':','.join(vocab),'vocabulary_boost':str(boost)}
 url=base+route+('?' + urllib.parse.urlencode(q) if q else '')
 ws=websocket.create_connection(url,header={'Authorization':'Bearer '+token},timeout=65)
 started=time.monotonic(); events=[]; finals=[];errors=[];committed=False
 ws.send(json.dumps({'type':'session.update','session':{'sample_rate':16000,'language':'en-US','automatic_punctuation':True,'endpointing_ms':1200}}))
 pcm=None
 if route.endswith('/opus'):
  audio=subprocess.check_output(['ffmpeg','-v','error','-i',str(row['_path']),'-ac','1','-ar','16000','-c:a','libopus','-b:a','48k','-f','ogg','-'])
  chunks=[audio[i:i+4096] for i in range(0,len(audio),4096)]
 else:
  with wave.open(str(row['_path'])) as w:
   assert w.getframerate()==16000 and w.getnchannels()==1 and w.getsampwidth()==2
   pcm=w.readframes(w.getnframes())
  chunks=[pcm[i:i+17920] for i in range(0,len(pcm),17920)]
 def send():
  try:
   for chunk in chunks:ws.send_binary(chunk);time.sleep(SEND_INTERVAL)
   ws.send(json.dumps({'type':'input_audio_buffer.commit'}))
  except Exception as e:errors.append(str(e));ws.close()
 sender=threading.Thread(target=send,daemon=True);sender.start()
 try:
  while True:
   raw=ws.recv()
   if not raw:break
   e=json.loads(raw);events.append(e)
   if e.get('type')=='error':errors.append(e.get('error',e))
   if e.get('type')=='conversation.item.input_audio_transcription.completed':finals.append(e.get('transcript',''))
   if e.get('type')=='input_audio_buffer.committed':committed=True;break
 finally:ws.close();sender.join(timeout=5)
 text=' '.join(t.strip() for t in finals if t.strip())
 if not committed or not text or errors:raise RuntimeError(json.dumps({'committed':committed,'text':text,'errors':errors,'events':events}))
 return dict(text=text,events=events,elapsed=time.monotonic()-started,committed=committed,audio_sha256=hashlib.sha256(row['_path'].read_bytes()).hexdigest())

def main():
 p=argparse.ArgumentParser();p.add_argument('--base',default='ws://127.0.0.1:18090');p.add_argument('--token',default='/tmp/speektome-vocab-audit/worker.token');p.add_argument('--out',type=pathlib.Path,default=ROOT/'results'/'2026-10-16');p.add_argument('--route-check',action='store_true');args=p.parse_args()
 rows=json.loads((args.out/'corpus.json').read_text());vocab=json.loads((args.out/'vocabulary.json').read_text());token=pathlib.Path(args.token).read_text().strip()
 output=args.out/('route-check.jsonl' if args.route_check else 'inference.jsonl')
 done={(r['id'],r['condition']) for r in map(json.loads,output.read_text().splitlines()) if r.get('status')=='ok'} if output.exists() else set()
 cases=[]
 for row in rows:
  if args.route_check:
   if row['id'] not in ['00-AriaNeural','06-AriaNeural','human-jfk']:continue
   configs=[('route-'+r,1.5,r) for r in ['/v1/audio/realtime','/v1/audio/transcriptions/realtime','/v1/realtime/opus']]
  else: configs=[('baseline',None,'/v1/realtime'),('boost-0.5',0.5,'/v1/realtime'),('boost-1.5',1.5,'/v1/realtime'),('boost-3.0',3.0,'/v1/realtime')]
  random.Random(row['id']).shuffle(configs)
  cases.extend((row,c,b,r) for c,b,r in configs)
 for row,condition,boost,route in cases:
  if (row['id'],condition) in done:continue
  row['_path']=args.out/row['audio'];result={k:v for k,v in row.items() if k!='_path'};result.update(condition=condition,boost=boost,route=route,vocabulary_count=len(vocab) if boost else 0)
  try:result.update(transcribe(args.base,token,row,vocab,boost,route),status='ok')
  except Exception as e:result.update(status='error',error=str(e))
  with output.open('a') as f:f.write(json.dumps(result)+'\n');f.flush()
  print(row['id'],condition,result['status'],result.get('text',result.get('error')),flush=True)
  if result['status']=='error': raise SystemExit('Stopping on inference error; completed rows are resumable.')
if __name__=='__main__':main()
