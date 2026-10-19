"""Controlled synthetic English/Hindi fixtures; not recordings of the user's voice."""
import asyncio,json,subprocess,wave,hashlib
from pathlib import Path
import edge_tts
ROOT=Path(__file__).parent/'results'/'2026-10-17'
EN='Okay, right now I am talking in English. Please keep these English words in the English alphabet.'
HI='अभी मैं हिंदी में बात कर रहा हूँ। कृपया मेरे शब्दों को हिंदी में ही लिखिए।'
TAIL='Now I am speaking English again. The transcription should return to English instead of writing English words in Hindi letters.'
CASES=[
 ('english-short-male',[('en-IN-PrabhatNeural','Okay, right now I am talking in English.')],['en']),
 ('english-short-female',[('en-IN-NeerjaNeural','Okay, right now I am talking in English.')],['en']),
 ('english-long',[('en-IN-PrabhatNeural','I have selected English and Hindi in my desktop application. I am from India and I speak English with an Indian accent. Please check the main server logs and the inference server logs to see whether the transcription is correct.')],['en']),
 ('hindi',[('hi-IN-MadhurNeural',HI)],['hi']),
 ('en-hi-en',[('hi-IN-MadhurNeural',EN),('hi-IN-MadhurNeural',HI),('hi-IN-MadhurNeural',TAIL)],['en','hi','en']),
 ('hi-en-hi',[('hi-IN-MadhurNeural',HI),('hi-IN-MadhurNeural',EN),('hi-IN-MadhurNeural','अब मैं फिर से हिंदी में बोल रहा हूँ। दोनों भाषाओं को ठीक से पहचानना ज़रूरी है।')],['hi','en','hi']),
 ('hinglish',[('hi-IN-MadhurNeural','Please कल की meeting का agenda भेज दीजिए और database का backup लेना मत भूलिए।')],['mixed']),
]
async def main():
 ROOT.mkdir(parents=True,exist_ok=True);rows=[]
 for name,segments,languages in CASES:
  pieces=[];bounds=[];offset=0
  for i,(voice,text) in enumerate(segments):
   mp3=ROOT/f'{name}-{i}.mp3';wav=ROOT/f'{name}-{i}.wav'
   if not mp3.exists(): await edge_tts.Communicate(text,voice).save(str(mp3))
   subprocess.run(['ffmpeg','-v','error','-y','-i',str(mp3),'-ac','1','-ar','16000','-c:a','pcm_s16le',str(wav)],check=True)
   with wave.open(str(wav)) as w: data=w.readframes(w.getnframes())
   bounds.append({'start':offset,'end':offset+len(data)/32000,'language':languages[i] if len(languages)>1 else languages[0],'text':text,'voice':voice})
   pieces.append(data);offset+=len(data)/32000
   if i<len(segments)-1: pieces.append(b'\0'*9600);offset+=.3
  out=ROOT/f'{name}.wav'
  with wave.open(str(out),'wb') as w:w.setparams((1,2,16000,0,'NONE','not compressed'));w.writeframes(b''.join(pieces))
  rows.append({'id':name,'audio':str(out.resolve()),'duration':offset,'reference':' '.join(x[1] for x in segments),'segments':bounds,'sha256':hashlib.sha256(out.read_bytes()).hexdigest(),'synthetic':True})
  print(name,round(offset,2),flush=True)
 (ROOT/'corpus.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2))
asyncio.run(main())
