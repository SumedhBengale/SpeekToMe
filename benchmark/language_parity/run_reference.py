"""Official HF full-precision checkpoint through its documented Transformers streaming API."""
import argparse,json,time,os
from pathlib import Path
import numpy as np,soundfile as sf,torch
from transformers import AutoProcessor,AutoModelForRNNT
ROOT=Path(__file__).parent/'results'/'2026-10-17'
p=argparse.ArgumentParser();p.add_argument('--model',default=os.environ.get('SPEEKTOME_REFERENCE_MODEL_PATH'));p.add_argument('--limit',type=int);p.add_argument('--right',type=int,default=6);p.add_argument('--languages',nargs='+',default=['auto','en-US','hi-IN']);args=p.parse_args()
if not args.model:p.error('provide --model with the pinned local Hugging Face snapshot directory')
MODEL=args.model
torch.set_num_threads(4);torch.backends.cuda.matmul.allow_tf32=False
model=AutoModelForRNNT.from_pretrained(MODEL,dtype=torch.float32,attn_implementation='sdpa',local_files_only=True).eval().to('cuda')
processor=AutoProcessor.from_pretrained(MODEL,local_files_only=True);processor.set_num_lookahead_tokens(args.right)
print('loaded',torch.__version__,'right',args.right,'prompt_ids',{lang:processor._resolve_prompt_ids(lang,1).tolist() for lang in ['auto','en-US','hi-IN']},flush=True)
rows=json.loads((ROOT/'corpus.json').read_text())
if args.limit:rows=rows[:args.limit]
output=ROOT/f'reference-r{args.right}.jsonl'
done={(x['id'],x['language']) for x in (json.loads(l) for l in output.read_text().splitlines())} if output.exists() else set()
for row in rows:
 audio,sr=sf.read(row['canonical_audio'],dtype='float32');assert sr==16000 and audio.ndim==1
 for lang in args.languages:
  if (row['id'],lang) in done:continue
  chunks=[];first=True;start=0;hop=processor.feature_extractor.hop_length;nfft=processor.feature_extractor.n_fft
  while start<len(audio):
   needed=processor.num_samples_first_audio_chunk if first else processor.num_samples_per_audio_chunk
   piece=audio[start:start+needed]
   piece=np.pad(piece,(0,max(0,needed-len(piece))))
   ins=processor(piece,sampling_rate=sr,is_streaming=True,is_first_audio_chunk=first,language=lang,return_tensors='pt')
   frames=processor.num_mel_frames_first_audio_chunk if first else processor.num_mel_frames_per_audio_chunk
   chunks.append(ins.input_features[:,:frames,:].to('cuda'))
   if first:prompt=ins.prompt_ids.to('cuda');start=frames*hop-nfft//2;first=False
   else:start+=frames*hop
  def features():
   yield from chunks
  torch.cuda.synchronize();t=time.perf_counter()
  with torch.inference_mode(): result=model.generate(input_features=features(),prompt_ids=prompt,num_lookahead_tokens=args.right,max_new_tokens=1024,return_dict_in_generate=True)
  torch.cuda.synchronize();elapsed=time.perf_counter()-t
  text=processor.decode(result.sequences[0],skip_special_tokens=False)
  rec={'id':row['id'],'language':lang,'runtime':'transformers-fp32-sdpa','right_context':args.right,'model_revision':Path(MODEL).name,'audio_sha256':row['canonical_sha256'],'seconds':elapsed,'text':text,'tokens':result.sequences[0].tolist(),'chunks':len(chunks),'processed_audio_seconds':len(audio)/sr,'peak_cuda_bytes':torch.cuda.max_memory_allocated()}
  with output.open('a') as f:f.write(json.dumps(rec,ensure_ascii=False)+'\n')
  print(row['id'],lang,round(elapsed,2),text,flush=True)
