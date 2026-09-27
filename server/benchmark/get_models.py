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

headers = {'User-Agent': 'SpeekToMe/1.0'}

# 1. Cerebras models
try:
    req = urllib.request.Request('https://api.cerebras.ai/v1/models', headers={**headers, 'Authorization': 'Bearer ' + env_vars['CEREBRAS_API_KEY']})
    with urllib.request.urlopen(req) as resp:
        m = json.loads(resp.read().decode('utf-8'))['data']
        print('Cerebras models:', [x['id'] for x in m])
except Exception as e:
    print('Cerebras err:', e)

# 2. Groq models
try:
    req = urllib.request.Request('https://api.groq.com/openai/v1/models', headers={**headers, 'Authorization': 'Bearer ' + env_vars['GROQ_API_KEY']})
    with urllib.request.urlopen(req) as resp:
        m = json.loads(resp.read().decode('utf-8'))['data']
        print('Groq models:', [x['id'] for x in m])
except Exception as e:
    print('Groq err:', e)
