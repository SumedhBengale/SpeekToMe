"""Unseen synthetic contexts, fixed before inspecting follow-up ASR results."""
import asyncio,json,pathlib,subprocess,wave
import edge_tts
ROOT=pathlib.Path(__file__).resolve().parent/'results'/'2026-10-16-followup'
CASES=[
('target','When the checkout service slows down, Grafana displays the Redis latency next to the PostgreSQL connection count.',['Grafana','Redis','PostgreSQL']),
('target',"Please compare the PyTorch training run with yesterday's TensorRT deployment before changing the CUDA version.",['PyTorch','TensorRT','CUDA']),
('target','Can you deploy the FastAPI service to Kubernetes and publish the TypeScript dashboard on Vercel?',['FastAPI','Kubernetes','TypeScript','Vercel']),
('target','We chose Tauri for the desktop app and WebSocket for updates, while Kafka handles background events.',['Tauri','WebSocket','Kafka']),
('confuser','Please read this page carefully. The doctor said my coffee should be cold before I drink it.',[]),
('confuser','Anna drew a graph while the children played on the blue carpet near the garden.',[]),
('confuser','We carried a torch to the old docks and opened the wooden door before sunset.',[]),
('confuser','The cook put celery in the soup, filled a flask with water, and asked the team to wash the plates.',[]),
]
async def main():
 rows=[]
 for i,(category,text,targets) in enumerate(CASES):
  key=f'holdout-{i:02d}';mp3=ROOT/(key+'.mp3');wav=ROOT/(key+'.wav')
  if not mp3.exists():await edge_tts.Communicate(text,'en-IN-NeerjaNeural').save(str(mp3))
  subprocess.run(['ffmpeg','-v','error','-y','-i',str(mp3),'-ac','1','-ar','16000','-c:a','pcm_s16le',str(wav)],check=True)
  with wave.open(str(wav)) as w:duration=w.getnframes()/w.getframerate()
  rows.append(dict(id=key,category=category,reference=text,targets=targets,audio=wav.name,duration=duration,source='Edge TTS en-IN-NeerjaNeural; new texts and voice held out from prior calibration'))
 (ROOT/'holdout-corpus.json').write_text(json.dumps(rows,indent=2)+'\n');print('Prepared',len(rows),'held-out synthetic clips',flush=True)
asyncio.run(main())
