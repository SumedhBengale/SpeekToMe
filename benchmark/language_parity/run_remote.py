"""Authenticated test account plus direct production ASR; never write credentials to results."""
import argparse,json,threading,time,wave,ssl,urllib.request
from pathlib import Path
import websocket
ROOT=Path(__file__).parent/'results'/'2026-10-17'
p=argparse.ArgumentParser();p.add_argument('--mode',choices=['app','worker'],required=True);p.add_argument('--limit',type=int);p.add_argument('--unpaced',action='store_true');p.add_argument('--endpoint-ms',type=int,default=1200);args=p.parse_args()
rows=json.loads((ROOT/'corpus.json').read_text())
if args.limit:rows=rows[:args.limit]
cfg=json.loads((Path.home()/'.config'/'SpeekToMe'/'config.json').read_text())
secret=cfg['session_token'] if args.mode=='app' else (Path.home()/'.config'/'speektome'/'spot-worker.token').read_text().strip()
url='ws://127.0.0.1:18991/v1/stream' if args.mode=='app' else 'wss://127.0.0.1:18992/v1/realtime'
output=ROOT/(f'{args.mode}-e{args.endpoint_ms}.jsonl')
done={(x['id'],x['language']) for x in (json.loads(l) for l in output.read_text().splitlines())} if output.exists() else set()
for row in rows:
 for lang in (['auto'] if args.mode=='app' else ['auto','en-US','hi-IN']):
  if (row['id'],lang) in done:continue
  ws=websocket.create_connection(url,header={'Authorization':'Bearer '+secret},timeout=120,sslopt={'cert_reqs':ssl.CERT_NONE},enable_multithread=True)
  if args.mode=='app':
   ws.send(json.dumps({'type':'start','format':'ogg_opus','sample_rate':16000,'channels':1,'enhance':True,'capabilities':['continuous_stream']}))
   ready=json.loads(ws.recv());assert ready.get('provider')=='self_hosted_nemotron_3_5' and ready['type']=='session_ready'
   frames=[(Path(x['path']).read_bytes(),x['duration']) for x in row['chunks']]
  else:
   created=json.loads(ws.recv());assert created['type']=='session.created'
   ws.send(json.dumps({'type':'session.update','session':{'language':lang,'sample_rate':16000,'word_timestamps':True,'speaker_diarization':False,'endpointing_ms':args.endpoint_ms}}))
   updated=json.loads(ws.recv());assert updated['type']=='session.updated',updated
   with wave.open(row['canonical_audio']) as w:data=w.readframes(w.getnframes())
   frames=[(data[i:i+17920],len(data[i:i+17920])/32000) for i in range(0,len(data),17920)]
  timing={};errors=[];interims=[];completed=[];events=[];final=None;t0=time.perf_counter()
  def send():
   try:
    due=t0
    for data,duration in frames:
     due+=duration
     if not args.unpaced:time.sleep(max(0,due-time.perf_counter()))
     ws.send_binary(data)
    timing['stop']=time.perf_counter();ws.send(json.dumps({'type':'stop'} if args.mode=='app' else {'type':'input_audio_buffer.commit'}))
   except Exception as e:errors.append(type(e).__name__)
  sender=threading.Thread(target=send);sender.start()
  try:
   while True:
    e=json.loads(ws.recv());kind=e['type'];events.append(kind)
    if kind=='error':raise RuntimeError('remote returned error')
    if kind in ['interim','conversation.item.input_audio_transcription.delta']:interims.append({'time':time.perf_counter()-t0,'text':e.get('text',e.get('delta',''))})
    if kind=='conversation.item.input_audio_transcription.completed':completed.append(e)
    if kind=='final':final=e;break
    if kind=='input_audio_buffer.committed':break
   timing['final']=time.perf_counter()
  finally:ws.close();sender.join()
  assert not errors,errors
  if args.mode=='app':
   assert final and final.get('log_id') and final.get('vocabulary')
   audit=final['vocabulary'];raw=audit['proposal']['raw_transcript']
   saved=json.load(urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:18991/usage/history',headers={'Authorization':'Bearer '+secret}),timeout=15))
   record=next(x for x in saved if x['id']==final['log_id']);assert record['input_transcript']==raw
   extra={'log_id':final['log_id'],'selected_languages':['en','hi'],'raw_persisted_matches':True}
  else:
   assert completed and 'input_audio_buffer.committed' in events
   raw=' '.join(x.get('transcript','') for x in completed).strip();extra={'completed':completed}
  rec={'id':row['id'],'runtime':args.mode,'language':lang,'right_context':6,'realtime_pacing':not args.unpaced,'endpoint_ms':args.endpoint_ms,'audio_sha256':row['canonical_sha256'],'reference':row['reference'],'text':raw,'post_stop_ms':(timing['final']-timing['stop'])*1000,'interims':interims,**extra}
  with output.open('a') as f:f.write(json.dumps(rec,ensure_ascii=False)+'\n')
  print(args.mode,row['id'],lang,raw,flush=True)
