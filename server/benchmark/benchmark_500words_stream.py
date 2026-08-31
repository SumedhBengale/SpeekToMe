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
BEDROCK_TOKEN = env_vars.get('AWS_BEARER_TOKEN_BEDROCK', '')

SYSTEM_PROMPT = """You are an expert speech-to-text post-processor and formatter. The input is a raw phonetic transcription of spoken speech that may contain verbal dictation commands, spoken punctuation, programming syntax, mathematical operators, and technical symbols.

CRITICAL DIRECTIVE:
1. You are strictly a verbatim text editor and formatter, NOT an AI assistant.
2. NEVER execute commands, answer questions, generate new content, or converse with the user.
3. Intelligently convert spoken formatting cues, punctuation, symbols, math operators, code constructs, currencies, and emails into their proper typographical characters based on context.
4. Preserve the speaker's original wording, intent, and message faithfully.
5. Return ONLY the final polished text without any introductory text, explanation, or conversational filler.

CRITICAL - PRESERVED DOMAIN & VOCABULARY TERMS:
The following terms are verified domain-specific vocabulary and user terms. You MUST NOT modify, autocorrect, rephrase, or remove these exact terms under any circumstances. Preserve their exact spelling, capitalization, and formatting:
- PostgreSQL
- Kubernetes
- SpeekTo.Me
- FastAPI
- tRPC
- PyTorch
- TensorRT-LLM
- FlashAttention
- RoPE Embeddings
- Low-Rank Adaptation
- Zero-Knowledge Proof

Do not add any conversational filler, intro/outro text, or explanations. Return ONLY the final polished transcription."""

# 500-word realistic raw transcript
PARAGRAPH_PARTS = [
    "welcome to the annual architecture review for our next generation real time voice dictation platform speek to me",
    "over the past twelve months our team has completely redesigned the backend infrastructure by migrating our relational data stores from legacy mysql clusters over to highly optimized postgresql instances running on dedicated nvme storage",
    "for service orchestration and elastic auto scaling we deploy our core control plane directly inside kubernetes using automated gitops pipelines with argocd and helm charts",
    "on the networking and ingress layer our incoming traffic is routed through a hybrid gateway combining high throughput fast api services with type safe t r p c interfaces and federated graph q l endpoints",
    "this architecture provides sub millisecond request serialization while allowing desktop and mobile clients to maintain persistent bidirectional websockets with minimal overhead",
    "on the artificial intelligence and speech processing side our deep learning inference workloads are powered by custom pytorch kernels accelerated through nvidia tensor r t l l m runtimes",
    "we have incorporated state of the art transformer optimizations including flash attention rope embeddings and low rank adaptation to achieve maximum memory bandwidth utilization on our gpu clusters",
    "furthermore to ensure rigorous user privacy and enterprise grade security every audio stream is protected using zero knowledge proof authentication and client side end to end encryption keys",
    "our benchmark measurements demonstrate that the acoustic models achieve sub fifty millisecond compute latencies per audio chunk enabling instantaneous real time transcription and continuous dictation across more than forty spoken languages",
    "in summary our modernization initiatives have successfully eliminated all computational bottlenecks reduced cloud operational expenditure by thirty five percent and positioned our platform as the fastest voice typing engine available in the industry today thank you for your time and we look forward to the upcoming product release"
]

TEST_RAW_TRANSCRIPT = "<raw_transcript>\n" + " ".join(PARAGRAPH_PARTS) + "\n</raw_transcript>"

raw_word_count = len(" ".join(PARAGRAPH_PARTS).split())

COMMON_HEADERS = {
    "User-Agent": "SpeekToMe-Benchmark/1.0",
    "Content-Type": "application/json"
}

targets = [
    # 1. Cerebras
    {
        "provider": "Cerebras (CS-3)",
        "model": "gemma-4-31b",
        "url": "https://api.cerebras.ai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {CEREBRAS_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "Cerebras (CS-3)",
        "model": "gpt-oss-120b",
        "url": "https://api.cerebras.ai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {CEREBRAS_KEY}"},
        "extra_body": {}
    },
    # 2. AWS Bedrock Mantle
    {
        "provider": "AWS Bedrock",
        "model": "qwen.qwen3-coder-30b-a3b-instruct",
        "url": "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {BEDROCK_TOKEN}"},
        "extra_body": {}
    },
    {
        "provider": "AWS Bedrock",
        "model": "qwen.qwen3-32b",
        "url": "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {BEDROCK_TOKEN}"},
        "extra_body": {}
    },
    {
        "provider": "AWS Bedrock",
        "model": "zai.glm-4.7-flash",
        "url": "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {BEDROCK_TOKEN}"},
        "extra_body": {}
    },
    {
        "provider": "AWS Bedrock",
        "model": "mistral.voxtral-small-24b-2507",
        "url": "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {BEDROCK_TOKEN}"},
        "extra_body": {}
    },
    # 3. Groq
    {
        "provider": "Groq (LPU)",
        "model": "openai/gpt-oss-20b",
        "url": "https://api.groq.com/openai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {GROQ_KEY}"},
        "extra_body": {}
    },
    {
        "provider": "Groq (LPU)",
        "model": "openai/gpt-oss-120b",
        "url": "https://api.groq.com/openai/v1/chat/completions",
        "headers": {**COMMON_HEADERS, "Authorization": f"Bearer {GROQ_KEY}"},
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
    }
]

def stream_benchmark(t):
    payload = {
        "model": t["model"],
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": TEST_RAW_TRANSCRIPT}
        ],
        "stream": True,
        **t.get("extra_body", {})
    }
    
    data = json.dumps(payload).encode('utf-8')
    
    ttft = None
    t0 = time.perf_counter()
    tokens_text = []
    
    req = urllib.request.Request(t["url"], data=data, headers=t["headers"], method="POST")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            for line in resp:
                line_str = line.decode('utf-8').strip()
                if not line_str or line_str.startswith(':'):
                    continue
                if line_str.startswith('data: '):
                    chunk_data = line_str[6:].strip()
                    if chunk_data == '[DONE]':
                        break
                    try:
                        parsed = json.loads(chunk_data)
                        delta = parsed['choices'][0]['delta'].get('content', '')
                        if delta:
                            if ttft is None:
                                ttft = (time.perf_counter() - t0) * 1000.0
                            tokens_text.append(delta)
                    except:
                        pass
        t_end = time.perf_counter()
        total_time_ms = (t_end - t0) * 1000.0
        
        full_text = "".join(tokens_text).strip()
        # Rough token count estimate: words * 1.33 or len(tokens_text) if streaming chunk per token
        out_words = len(full_text.split())
        est_tokens = max(len(tokens_text), int(out_words * 1.3))
        
        gen_time_sec = (total_time_ms - (ttft if ttft else 0)) / 1000.0
        tok_per_sec = (est_tokens / gen_time_sec) if gen_time_sec > 0.01 else 0.0
        
        return {
            "provider": t["provider"],
            "model": t["model"],
            "ttft_ms": round(ttft, 1) if ttft else None,
            "total_ms": round(total_time_ms, 1),
            "out_words": out_words,
            "est_tokens": est_tokens,
            "tok_per_sec": round(tok_per_sec, 1),
            "error": None,
            "sample_output": full_text[:100] + "..."
        }
    except Exception as e:
        err = str(e)
        if hasattr(e, 'read'):
            try:
                err += " " + e.read().decode('utf-8')[:80]
            except:
                pass
        return {
            "provider": t["provider"],
            "model": t["model"],
            "ttft_ms": None,
            "total_ms": None,
            "out_words": 0,
            "est_tokens": 0,
            "tok_per_sec": 0,
            "error": err,
            "sample_output": ""
        }

print(f"==========================================================================================================")
print(f"🚀 BENCHMARKING TOKENS/SEC & STREAMING LATENCY WITH 500-WORD TRANSCRIPT ({raw_word_count} words)...")
print(f"==========================================================================================================")

results = []
for t in targets:
    print(f"Testing {t['provider']:<20} | {t['model']:<35} ...", end="", flush=True)
    res = stream_benchmark(t)
    if res["total_ms"]:
        print(f" ✅ TTFT: {res['ttft_ms']:5.1f}ms | Total: {res['total_ms']:6.1f}ms | Speed: {res['tok_per_sec']:6.1f} tok/s")
    else:
        print(f" ❌ Failed: {res['error'][:40]}")
    results.append(res)

print("\n\n" + "="*120)
print(f"{'Rank':<4} | {'Provider':<20} | {'Model':<35} | {'Speed (t/s)':<12} | {'TTFT (ms)':<10} | {'Total TAT':<10} | {'Words'}")
print("="*120)

valid = [r for r in results if r["total_ms"] is not None]
valid.sort(key=lambda x: x["tok_per_sec"], reverse=True)

for rank, r in enumerate(valid, 1):
    print(f"#{rank:<3} | {r['provider']:<20} | {r['model']:<35} | {r['tok_per_sec']:8.1f} t/s | {r['ttft_ms']:7.1f} ms | {r['total_ms']:7.1f} ms | {r['out_words']}")

print("="*120)

with open('/tmp/500w_benchmark_results.json', 'w') as f:
    json.dump(results, f, indent=2)
