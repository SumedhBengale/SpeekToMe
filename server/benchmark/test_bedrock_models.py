import urllib.request
import json
import time
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
    'Content-Type': 'application/json',
    'User-Agent': 'SpeekToMe-Benchmark/1.0'
}

# Try standard Bedrock model naming conventions
candidates = [
    # Google Gemma
    "google.gemma-4-e2b",
    "google.gemma-4-26b-a4b",
    "google.gemma-4-31b",
    "google.gemma-3-4b-it",
    "google.gemma-3-12b-it",
    "google.gemma-3-27b-it",
    # OpenAI
    "openai.gpt-5-6-luna",
    "openai.gpt-5-6-terra",
    "openai.gpt-oss-20b",
    "openai.gpt-oss-120b",
    # Mistral
    "mistral.ministral-3b",
    "mistral.ministral-3-8b",
    "mistral.ministral-14b-3-0",
    "mistral.voxtral-small-24b-2507",
    "mistral.voxtral-mini-3b-2507",
    # NVIDIA
    "nvidia.nemotron-nano-3-30b",
    "nvidia.nemotron-nano-9b-v2",
    # Z.AI
    "z-ai.glm-4-7-flash",
    # Qwen
    "qwen.qwen3-32b",
    "qwen.qwen3-coder-30b-a3b-instruct"
]

SYSTEM_PROMPT = """You are an expert speech-to-text post-processor and formatter. Output ONLY the polished verbatim text. Preserve: PostgreSQL, Kubernetes, SpeekTo.Me, FastAPI, tRPC."""
TEST_RAW = "we are modernizing our architecture by deploying kubernetes and connecting to postgress database to power fast api and t r p c inside speek to me"

print("🔍 Testing AWS Bedrock Mantle Model IDs & Measuring Latencies...")
print("="*85)

for model in candidates:
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": f"<raw_transcript>\n{TEST_RAW}\n</raw_transcript>"}
        ]
    }
    
    t0 = time.perf_counter()
    req = urllib.request.Request(
        'https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions',
        data=json.dumps(payload).encode('utf-8'),
        headers=headers,
        method="POST"
    )
    
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            elapsed = (time.perf_counter() - t0) * 1000.0
            body = json.loads(resp.read().decode('utf-8'))
            out = body['choices'][0]['message']['content'].strip()
            print(f"✅ {model:<36} | {elapsed:6.1f} ms | Out: {out[:45]}...")
    except Exception as e:
        err = str(e)
        if hasattr(e, 'read'):
            try:
                err += " " + e.read().decode('utf-8')[:60]
            except:
                pass
        print(f"❌ {model:<36} | {err[:50]}")

print("="*85)
