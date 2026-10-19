"""Use identical decoded Opus audio for direct worker and reference comparisons."""
import json,subprocess,wave,hashlib
from pathlib import Path
ROOT=Path(__file__).parent/'results'/'2026-10-17';rows=json.loads((ROOT/'corpus.json').read_text())
for row in rows:
 directory=ROOT/(row['id']+'-opus');directory.mkdir(exist_ok=True)
 subprocess.run(['ffmpeg','-v','error','-y','-i',row['audio'],'-af','apad=pad_dur=1.12','-ac','1','-ar','16000','-c:a','libopus','-b:a','48k','-f','segment','-segment_time','0.56','-reset_timestamps','1',str(directory/'chunk%04d.ogg')],check=True)
 pcm=b'';chunks=[]
 for p in sorted(directory.glob('chunk*.ogg')):
  out=subprocess.check_output(['ffmpeg','-v','error','-i',str(p),'-ar','16000','-ac','1','-f','s16le','-'])
  chunks.append({'path':str(p.resolve()),'duration':len(out)/32000});pcm+=out
 canonical=ROOT/(row['id']+'-canonical.wav')
 with wave.open(str(canonical),'wb') as w:w.setparams((1,2,16000,0,'NONE','not compressed'));w.writeframes(pcm)
 row.update(chunks=chunks,canonical_audio=str(canonical.resolve()),canonical_sha256=hashlib.sha256(canonical.read_bytes()).hexdigest(),canonical_duration=len(pcm)/32000)
 print(row['id'],len(chunks),round(len(pcm)/32000,3),flush=True)
(ROOT/'corpus.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2))
