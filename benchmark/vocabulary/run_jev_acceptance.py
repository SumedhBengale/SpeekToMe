"""Audio from this PC -> actual handler -> fleet ASR -> worker matcher -> Jev -> LLM.
Uses an isolated SQL database on the main server, never production accounts.
"""
import hashlib,json,pathlib,subprocess,tempfile,threading,time,urllib.request,websocket,wave
ROOT=pathlib.Path(__file__).parent/'results'/'2026-10-16-jev'
token=pathlib.Path('/tmp/speektome-vocab-audit/jev-acceptance-session.token').read_text().strip()
for row in json.loads((ROOT/'acceptance-corpus.json').read_text()):
 with tempfile.TemporaryDirectory(prefix='speektome-jev-opus-') as directory:
  subprocess.run(['ffmpeg','-v','error','-y','-i',row['audio'],'-ac','1','-ar','16000','-c:a','libopus','-b:a','48k','-f','segment','-segment_time','0.56','-reset_timestamps','1',directory+'/chunk%04d.ogg'],check=True)
  chunks=[p.read_bytes() for p in sorted(pathlib.Path(directory).glob('*.ogg'))]
 with wave.open(row['audio']) as audio:duration=audio.getnframes()/audio.getframerate()
 ws=websocket.create_connection('ws://127.0.0.1:18001/v1/stream',header={'Authorization':'Bearer '+token},timeout=60)
 ws.send(json.dumps({'type':'start','format':'ogg_opus','sample_rate':16000,'channels':1,'enhance':True,'capabilities':['continuous_stream']}))
 ready=json.loads(ws.recv());assert ready.get('type')=='session_ready' and ready.get('provider')=='self_hosted_nemotron_3_5',ready
 timing={};errors=[];interims=[]
 def send():
  try:
   for i,chunk in enumerate(chunks):
    time.sleep(min(.56,max(.001,duration-.56*i)))
    ws.send_binary(chunk)
   timing['stop']=time.perf_counter();ws.send(json.dumps({'type':'stop'}))
  except Exception as e:errors.append(str(e))
 sender=threading.Thread(target=send);sender.start();final=None
 try:
  while True:
   e=json.loads(ws.recv())
   if e['type']=='error':raise RuntimeError(e)
   if e['type']=='interim':interims.append(e['text'])
   if e['type']=='final':timing['final']=time.perf_counter();final=e;break
 finally:ws.close();sender.join()
 assert final and final['text'] and final.get('log_id') and not errors,final
 audit=final['vocabulary'];assert audit['proposal_source']=='inference_worker',audit
 assert not audit['proposal'].get('matcher_error'),audit
 assert all(d['source'] in ['asr','jev'] for d in audit['decisions']),audit
 history=json.load(urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:18001/audit',headers={'Authorization':'Bearer '+token}),timeout=5))
 saved=next(x for x in history if x['id']==final['log_id'])
 assert saved['input_transcript']==audit['proposal']['raw_transcript'] and saved['final_output']==final['text'] and saved['vocabulary']==audit
 assert saved['llm_provider'],saved
 output=dict(id=row['id'],reference=row['reference'],audio_sha256=hashlib.sha256(pathlib.Path(row['audio']).read_bytes()).hexdigest(),chunks=len(chunks),send_schedule='capture duration before each frame; stop immediately after last frame',post_stop_ms=(timing['final']-timing['stop'])*1000,final=final,interims=interims,history=saved)
 with (ROOT/'acceptance-live.jsonl').open('a') as f:f.write(json.dumps(output)+'\n')
 print(row['id'],'ASR:',audit['proposal']['raw_transcript'],'RESOLVED:',audit['resolved_transcript'],'FINAL:',final['text'],'post_stop_ms',round(output['post_stop_ms'],1),flush=True)
