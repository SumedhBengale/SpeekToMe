import urllib.request
import json
import os

env_vars = {}
with open('/home/ec2-user/speektome/.env', 'r') as f:
    for line in f:
        line = line.strip()
        if line and not line.startswith('#') and '=' in line:
            k, v = line.split('=', 1)
            env_vars[k.strip()] = v.strip().strip("'").strip('"')

headers = {
    'Authorization': 'Bearer ' + env_vars['AWS_BEARER_TOKEN_BEDROCK'],
    'User-Agent': 'SpeekToMe/1.0'
}

req = urllib.request.Request('https://bedrock-mantle.us-east-1.api.aws/v1/models', headers=headers)
try:
    with urllib.request.urlopen(req) as resp:
        m = json.loads(resp.read().decode('utf-8'))
        print("Bedrock Mantle models count:", len(m.get('data', [])))
        for x in sorted([x['id'] for x in m.get('data', [])]):
            print(f" - {x}")
except Exception as e:
    print("Error:", e)
