#!/usr/bin/env python3
"""Publish a content-addressed gateway release. Does not change live fleet state."""
import argparse,base64,gzip,hashlib,json,pathlib,re,subprocess
BASE=pathlib.Path(__file__).resolve().parent
BUCKET='speektome-inference-ACCOUNT_ID-us-east-1'
def publish(data,name,prefix):
    sha=hashlib.sha256(data).hexdigest();key=f'{prefix}/{sha}/{name}'
    path=pathlib.Path('/tmp')/('speektome-publish-'+sha);path.write_bytes(data)
    subprocess.run(['aws','s3api','put-object','--bucket',BUCKET,'--key',key,'--body',str(path),'--checksum-algorithm','SHA256','--metadata','sha256='+sha,'--region','us-east-1'],check=True,stdout=subprocess.DEVNULL)
    head=json.loads(subprocess.check_output(['aws','s3api','head-object','--bucket',BUCKET,'--key',key,'--checksum-mode','ENABLED','--region','us-east-1']))
    assert head['ChecksumSHA256']==base64.b64encode(bytes.fromhex(sha)).decode()
    path.unlink();return {'key':key,'sha256':sha}
def main():
    p=argparse.ArgumentParser();p.add_argument('--fleet-config',required=True);p.add_argument('--output',required=True);p.add_argument('--manifest',type=pathlib.Path,default=BASE/'manifest.json');a=p.parse_args()
    m=json.loads(a.manifest.read_text())
    for arch in ['amd64','arm64']:
        binary=pathlib.Path('/tmp/speektome-gateway-'+arch)
        info=subprocess.check_output(['go','version','-m',str(binary)],text=True)
        version=re.search(r': go(\d+)\.(\d+)\.(\d+)',info.splitlines()[0])
        if not version:raise SystemExit('Missing Go build version: '+str(binary))
        major,minor,patch=map(int,version.groups())
        if not (major>1 or major==1 and (minor>27 or minor==27 and patch>=3 or minor==26 and patch>=10)):
            raise SystemExit('Gateway predates eMudhra fix: '+info.splitlines()[0])
        data=binary.read_bytes()
        m['gateways'][arch]=dict(publish(gzip.compress(data,mtime=0),'speektome-gateway-'+arch+'.gz','gateway'),compression='gzip')
    m['helpers']['ready']=publish((BASE/'check-ready.sh').read_bytes(),'check-ready.sh','deployment/helpers')
    m['capabilities']=sorted(set(m.get('capabilities',[])+['final_vocabulary_matching_v1','live_vocabulary_matching_v1']))
    manifest=(json.dumps(m,indent=2)+'\n').encode();(BASE/'fleet-manifest.json').write_bytes(manifest)
    artifacts={'manifest':publish(manifest,'fleet-manifest.json','deployment/fleet-v1'),'bootstrap':publish((BASE/'bootstrap.sh').read_bytes(),'bootstrap.sh','deployment/fleet-v1')}
    fleet=json.loads(pathlib.Path(a.fleet_config).read_text());regions=[]
    for region in fleet['regions']:
        data=region['user_data']
        data=re.sub(r'MANIFEST_KEY=[^\s]+',"MANIFEST_KEY="+artifacts['manifest']['key'],data)
        data=re.sub(r'MANIFEST_SHA=[^\s]+',"MANIFEST_SHA="+artifacts['manifest']['sha256'],data)
        data=re.sub(r'deployment/fleet-v1/[a-f0-9]+/bootstrap.sh',artifacts['bootstrap']['key'],data)
        # The bootstrap checksum appears quoted as the first sha256sum input.
        oldsha=hashlib.sha256((BASE/'bootstrap.sh').read_bytes()).hexdigest()
        original=region['user_data']
        match=re.search(r'deployment/fleet-v1/([a-f0-9]+)/bootstrap.sh',original)
        if match:data=data.replace(match.group(1),artifacts['bootstrap']['sha256'])
        region['user_data']=data
        regionname=region['region']
        regions.append(dict(region=regionname,**publish(data.encode(),'user-data.sh','deployment/launch-configs/'+regionname)))
    output=pathlib.Path(a.output);output.write_text(json.dumps(fleet,indent=2)+'\n');output.chmod(0o600)
    artifacts['regional_user_data']=regions
    artifacts['fleet_config']=publish(output.read_bytes(),'fleet.json','deployment/fleet-config')
    (BASE/'vocabulary-artifacts.json').write_text(json.dumps(artifacts,indent=2)+'\n');print(json.dumps(artifacts,indent=2))
if __name__=='__main__':main()
