"""CPU text matching benchmark on the live worker; no ASR or Jev request."""
import json,pathlib,shlex,subprocess
ROOT=pathlib.Path(__file__).parent/'results'/'2026-10-16-jev'
code='''import json,time,urllib.request,ssl,sys
terms=json.load(sys.stdin)
raw="We are using postgrsq as our database. Why do you not speak to me about the next meeting? "*500
payload=json.dumps({"raw_transcript":raw,"terms":terms}).encode()
key=open("/etc/speektome-fleet/worker.token").read().strip();out=[]
for i in range(6):
 start=time.perf_counter()
 request=urllib.request.Request("https://[WORKER_IPV6]:80/v1/vocabulary/final",data=payload,headers={"Authorization":"Bearer "+key,"Content-Type":"application/json"})
 result=json.load(urllib.request.urlopen(request,context=ssl._create_unverified_context(),timeout=5))
 assert result["raw_transcript"]==raw and result["schema_version"]==1 and not result.get("matcher_error")
 out.append({"round":i,"words":len(raw.split()),"terms":len(terms),"choices":len(result["choices"]),"matcher_version":result["matcher_version"],"match_ms":result["match_ms"],"request_ms":(time.perf_counter()-start)*1000})
print(json.dumps(out))
'''
output=subprocess.check_output(['ssh','speektome-direct','sudo python3 -c '+shlex.quote(code)],input=(ROOT/'vocabulary-1000.json').read_bytes())
p=ROOT/'hour-worker-benchmark.json'
if p.exists() and not (ROOT/'hour-worker-benchmark-v1.json').exists():p.rename(ROOT/'hour-worker-benchmark-v1.json')
p.write_bytes(output);print(output.decode())
