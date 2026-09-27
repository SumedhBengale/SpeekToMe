import json

with open('/tmp/llm_benchmark_results.json', 'r') as f:
    data = json.load(f)

for r in data:
    if r['avg_ms']:
        print(f"=== {r['provider']} | {r['model']} ({r['avg_ms']} ms) ===")
        print(r['output'])
        print()
