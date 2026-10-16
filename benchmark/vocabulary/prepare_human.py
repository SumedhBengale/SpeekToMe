"""Download a fixed LibriSpeech test-clean sample with dataset-provided references.
No transcription output is used to choose recordings or references.
"""
import time
import concurrent.futures, hashlib, json, pathlib, subprocess, urllib.request, urllib.parse, wave
ROOT=pathlib.Path(__file__).resolve().parent/'results'/'2026-10-16-followup'
ROOT.mkdir(parents=True,exist_ok=True)
DATASET='openslr/librispeech_asr';OFFSETS=[0,200,400,600]
def get_rows(offset):
 query=urllib.parse.urlencode(dict(dataset=DATASET,config='clean',split='test',offset=offset,length=8))
 url='https://datasets-server.huggingface.co/rows?'+query
 for attempt in range(3):
  try:
   rows=json.load(urllib.request.urlopen(url,timeout=30))['rows'];return offset,rows[:8]
  except Exception:
   if attempt==2:raise
   time.sleep(1+attempt)
   url=url.replace('length=8','length=100')
def download(entry):
 row=entry['row'];name='libri-'+row['id'];flac=ROOT/(name+'.flac');wav=ROOT/(name+'.wav')
 if not flac.exists():
  with urllib.request.urlopen(row['audio'][0]['src'],timeout=30) as r:flac.write_bytes(r.read())
 subprocess.run(['ffmpeg','-v','error','-y','-i',str(flac),'-ac','1','-ar','16000','-c:a','pcm_s16le',str(wav)],check=True)
 with wave.open(str(wav)) as w:duration=w.getnframes()/w.getframerate()
 return dict(id=name,category='human-utterance',reference=row['text'],targets=[],audio=wav.name,duration=duration,speaker_id=row['speaker_id'],chapter_id=row['chapter_id'],row_idx=entry['row_idx'],source_id=row['id'],source_dataset=DATASET,source_revision=row['audio'][0]['src'].split('/--/')[1],audio_sha256=hashlib.sha256(wav.read_bytes()).hexdigest())
def main():
 with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:groups=list(pool.map(get_rows,OFFSETS))
 with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:entries=list(pool.map(download,[e for _,group in groups for e in group]))
 (ROOT/'human-source.json').write_text(json.dumps(entries,indent=2)+'\n');byid={r['row_idx']:r for r in entries};clips=[]
 # Four consecutive utterances form one longer test stream. Joins are explicit
 # synthetic 250-ms silences; these are not uninterrupted natural conversations.
 for off in OFFSETS:
  for group in [0,4]:
   sources=[byid[i] for i in range(off+group,off+group+4)];audio=bytearray()
   for j,row in enumerate(sources):
    with wave.open(str(ROOT/row['audio'])) as w:audio.extend(w.readframes(w.getnframes()))
    if j<3:audio.extend(bytes(8000))
   key=f'human-{off:04d}-{group}';wav=ROOT/(key+'.wav')
   with wave.open(str(wav),'wb') as w:w.setnchannels(1);w.setsampwidth(2);w.setframerate(16000);w.writeframes(audio)
   clips.append(dict(id=key,category='human-long',reference=' '.join(r['reference'] for r in sources),targets=[],audio=wav.name,duration=len(audio)/32000,source='LibriSpeech test-clean; 4 consecutive utterances joined with 250 ms silence',speaker_ids=sorted({r['speaker_id'] for r in sources}),source_ids=[r['source_id'] for r in sources]))
 (ROOT/'human-corpus.json').write_text(json.dumps(clips,indent=2)+'\n')
 print(json.dumps(dict(clips=len(clips),utterances=len(entries),duration=sum(c['duration'] for c in clips),speakers=sorted({e['speaker_id'] for e in entries}))),flush=True)
if __name__=='__main__':main()
