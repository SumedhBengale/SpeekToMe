"""Targeted speech regressions for the final vocabulary decision pipeline."""
import asyncio,json,pathlib,subprocess,edge_tts
ROOT=pathlib.Path(__file__).parent/'results'/'2026-10-16-jev'
CASES=[("ordinary-phrase","Why don't you speak to me about the problem?"),("named-app","We installed Speak to Me on the laptop to dictate emails."),("database","We are using PostgreSQL as our database.")]
async def main():
 rows=[]
 for name,text in CASES:
  mp3=ROOT/(name+'.mp3');wav=ROOT/(name+'.wav')
  if not mp3.exists():await edge_tts.Communicate(text,'en-IN-NeerjaNeural').save(str(mp3))
  subprocess.run(['ffmpeg','-v','error','-y','-i',str(mp3),'-ac','1','-ar','16000','-c:a','pcm_s16le',str(wav)],check=True)
  rows.append(dict(id=name,reference=text,audio=str(wav.resolve()),source='Synthetic Edge TTS en-IN-NeerjaNeural; targeted regression'))
 previous=pathlib.Path(__file__).parent/'results'/'2026-10-16-followup'
 for row in json.loads((previous/'holdout-corpus.json').read_text()):
  if row['id'] in ['holdout-00','holdout-04','holdout-07']:
   row['audio']=str((previous/row['audio']).resolve());rows.append(row)
 (ROOT/'acceptance-corpus.json').write_text(json.dumps(rows,indent=2)+'\n')
 print('Prepared',len(rows),'clips')
asyncio.run(main())
