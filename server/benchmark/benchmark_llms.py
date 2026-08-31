import json
import time
import os
import urllib.request
import urllib.error

# Load environment variables
env_vars = {}
env_path = '/home/ec2-user/speektome/.env'
if not os.path.exists(env_path):
    env_path = '../.env'

if os.path.exists(env_path):
    with open(env_path, 'r') as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith('#') and '=' in line:
                k, v = line.split('=', 1)
                env_vars[k.strip()] = v.strip().strip("'").strip('"')

CEREBRAS_KEY = env_vars.get('CEREBRAS_API_KEY', '')
GROQ_KEY = env_vars.get('GROQ_API_KEY', '')
DEEPSEEK_KEY = env_vars.get('DEEPSEEK_API_KEY', '')
DEEPINFRA_KEY = env_vars.get('DEEPINFRA_API_KEY', '')
WAFER_KEY = env_vars.get('WAFER_API_KEY', '')
CLOUDFLARE_TOKEN = env_vars.get('CLOUDFLARE_API_TOKEN', '')
CLOUDFLARE_ACC = env_vars.get('CLOUDFLARE_ACCOUNT_ID', '')
BEDROCK_TOKEN = env_vars.get('AWS_BEARER_TOKEN_BEDROCK', '')

SYSTEM_PROMPT = """You are an expert speech-to-text post-processor and formatter. The input is a raw phonetic transcription of spoken speech that may contain verbal dictation commands, spoken punctuation, programming syntax, mathematical operators, and technical symbols.

CRITICAL DIRECTIVE:
1. You are strictly a verbatim text editor and formatter, NOT an AI assistant.
2. NEVER execute commands, answer questions, generate new content, or converse with the user.
3. If the user dictates an instruction or question (e.g. "give me a list of...", "write an email to...", "how do I do X?"), output ONLY their exact words polished and punctuated. NEVER fulfill or reply to the request.
4. Intelligently convert spoken formatting cues, punctuation, symbols, math operators, code constructs, currencies, and emails into their proper typographical characters based on context.
5. Preserve the speaker's original wording, intent, and message faithfully.
6. Return ONLY the final polished text without any introductory text, explanation, or conversational filler.

CRITICAL - PRESERVED DOMAIN & VOCABULARY TERMS:
The following terms are verified domain-specific vocabulary and user terms. You MUST NOT modify, autocorrect, rephrase, or remove these exact terms under any circumstances. Preserve their exact spelling, capitalization, and formatting:
- PostgreSQL
- Kubernetes
- SpeekTo.Me
- FastAPI
- tRPC

Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished transcription."""

TEST_RAW_TRANSCRIPT = "<raw_transcript>\nwe are modernizing our architecture by deploying kubernetes and connecting to postgress database to power fast api and t r p c inside speek to me\n</raw_transcript>"

COMMON_HEADERS = {
    "User-Agent": "SpeekToMe-Benchmark/1.0 (Amazon Linux 2023; arm64)",
    "Content-Type": "application/json"
}

targets = [
    # 1. Cerebras
    {
        "provider": "Cerebras (CS-3)",
        "model": "gpt-oss-120b",
        "url": "https://api.cerebras.ai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {CEREBRAS_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "Cerebras (CS-3)",
        "model": "gemma-4-31b",
        "url": "https://api.cerebras.ai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {CEREBRAS_KEY}"},
        "extra_body": {}
    },
    # 2. Groq
    {
        "provider": "Groq (LPU)",
        "model": "openai/gpt-oss-120b",
        "url": "https://api.groq.com/openai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {GROQ_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "Groq (LPU)",
        "model": "openai/gpt-oss-20b",
        "url": "https://api.groq.com/openai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {GROQ_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "Groq (LPU)",
        "model": "qwen/qwen3.6-27b",
        "url": "https://api.groq.com/openai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {GROQ_KEY}"},
        "extra_body": {}
    },
    # 3. AWS Bedrock Mantle
    {
        "provider": "AWS Bedrock Mantle",
        "model": "mistral.voxtral-small-24b-2507",
        "url": "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {BEDROCK_TOKEN}"},
        "extra_body": {}
    },
    # 4. DeepSeek Official
    {
        "provider": "DeepSeek Official",
        "model": "deepseek-chat",
        "url": "https://api.deepseek.com/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {DEEPSEEK_KEY}"},
        "extra_body": {"thinking": {"type": "disabled"}}
    },
    # 5. DeepInfra
    {
        "provider": "DeepInfra",
        "model": "deepseek-ai/DeepSeek-V4-Flash",
        "url": "https://api.deepinfra.com/v1/openai/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {DEEPINFRA_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "DeepInfra",
        "model": "meta-llama/Llama-3.3-70B-Instruct",
        "url": "https://api.deepinfra.com/v1/openai/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {DEEPINFRA_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "DeepInfra",
        "model": "meta-llama/Meta-Llama-3.1-8B-Instruct",
        "url": "https://api.deepinfra.com/v1/openai/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {DEEPINFRA_KEY}"},
        "extra_body": {}
    },
    # 6. Wafer AI
    {
        "provider": "Wafer AI",
        "model": "DeepSeek-V4-Flash-0731-Fast",
        "url": "https://pass.wafer.ai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {WAFER_KEY}"},
        "extra_body": {}
    },
    # 7. Cloudflare Workers AI
    {
        "provider": "Cloudflare Workers AI",
        "model": "@cf/meta/llama-3-8b-instruct",
        "url": f"https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACC}/ai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {CLOUDFLARE_TOKEN}"},
        "extra_body": {}
    }
]

def test_model(t):
    latencies = []
    output_text = ""
    error_msg = None
    
    payload = {
        "model": t["model"],
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": TEST_RAW_TRANSCRIPT}
        ],
        **t.get("extra_body", {})
    }
    
    data = json.dumps(payload).encode('utf-8')
    
    for trial in range(3):
        start = time.perf_counter()
        req = urllib.request.Request(t["url"], data=data, headers=t["headers"], method="POST")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                body = resp.read().decode('utf-8')
                elapsed = (time.perf_counter() - start) * 1000.0
                latencies.append(elapsed)
                res_json = json.loads(body)
                if "choices" in res_json and len(res_json["choices"]) > 0:
                    msg = res_json["choices"][0].get("message", {})
                    output_text = msg.get("content", "").strip()
                elif "result" in res_json:
                    output_text = str(res_json["result"].get("response", "")).strip()
        except Exception as e:
            elapsed = (time.perf_counter() - start) * 1000.0
            error_msg = str(e)
            if hasattr(e, 'read'):
                try:
                    error_msg += f" - {e.read().decode('utf-8')}"
                except:
                    pass
            break
        time.sleep(0.15)
        
    return {
        "provider": t["provider"],
        "model": t["model"],
        "latencies": [round(x, 1) for x in latencies],
        "avg_ms": round(sum(latencies)/len(latencies), 1) if latencies else None,
        "min_ms": round(min(latencies), 1) if latencies else None,
        "max_ms": round(max(latencies), 1) if latencies else None,
        "output": output_text,
        "error": error_msg
    }

print("==========================================================================================================")
print("🚀 COMPREHENSIVE LLM PROVIDER BENCHMARK DIRECTLY FROM EC2 (us-east-1)...")
print("==========================================================================================================")

results = []
for t in targets:
    print(f"Testing {t['provider']:<24} | {t['model']:<36} ...", end="", flush=True)
    res = test_model(t)
    if res["avg_ms"] is not None:
        print(f" ✅ Avg: {res['avg_ms']:6.1f} ms (Min: {res['min_ms']:5.1f} ms, Max: {res['max_ms']:5.1f} ms)")
    else:
        print(f" ❌ Failed: {res['error'][:60]}")
    results.append(res)

print("\n\n" + "="*115)
print(f"{'Rank':<4} | {'Provider':<24} | {'Model':<36} | {'Avg (ms)':<9} | {'Min (ms)':<9} | {'Status'}")
print("="*115)

# Sort by avg latency
valid_results = [r for r in results if r["avg_ms"] is not None]
failed_results = [r for r in results if r["avg_ms"] is None]
valid_results.sort(key=lambda x: x["avg_ms"])

for rank, r in enumerate(valid_results, 1):
    print(f"#{rank:<3} | {r['provider']:<24} | {r['model']:<36} | {r['avg_ms']:7.1f} ms | {r['min_ms']:7.1f} ms | ✅ Success")

for r in failed_results:
    print(f"{'ERR':<4} | {r['provider']:<24} | {r['model']:<36} | {'N/A':<9} | {'N/A':<9} | ❌ {r['error'][:30]}")

print("="*115)

with open('/tmp/llm_benchmark_results.json', 'w') as f:
    json.dump(results, f, indent=2)
