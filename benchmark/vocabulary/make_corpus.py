"""Controlled synthetic-speech probe; references and audio are retained."""
import asyncio, json, pathlib, subprocess, wave
import edge_tts
ROOT=pathlib.Path(__file__).resolve().parent
OUT=ROOT/'results'/'2026-10-16'; OUT.mkdir(parents=True,exist_ok=True)
VOCAB='Kubernetes Docker Redis Kafka PostgreSQL MySQL SQLite MongoDB DynamoDB Elasticsearch OpenSearch ClickHouse Snowflake Databricks TensorFlow PyTorch NumPy SciPy Pandas Polars Jupyter FastAPI Flask Django Starlette Pydantic SQLAlchemy Alembic Celery RabbitMQ Nginx Envoy Traefik HAProxy Prometheus Grafana Loki Tempo Jaeger OpenTelemetry Terraform Pulumi Ansible Packer Vagrant Helm ArgoCD Flux Istio Linkerd Consul Vault Nomad Spark Flink Hadoop Airflow Prefect Dagster dbt DuckDB Parquet Arrow Avro Protobuf gRPC GraphQL REST WebSocket OAuth SAML JWT OpenID Keycloak Auth0 Supabase Firebase Vercel Netlify Cloudflare Fastly Akamai AWS Azure GCP Lambda Fargate Sagemaker Bedrock S3 EC2 EKS ECS SQS SNS Kinesis Redshift Athena Glue React Vue Angular Svelte Next.js Nuxt TypeScript JavaScript Rust Golang Kotlin Swift Flutter Tauri Electron Capacitor WebAssembly Bun Deno Node.js pnpm Yarn npm Poetry uv Conda Pip CMake Ninja Bazel Gradle Maven GitHub GitLab Bitbucket Jenkins CircleCI Buildkite Linear Jira Notion Slack Discord Zoom Teams Twilio Stripe Plaid Brex Ramp Rampart Ollama vLLM TensorRT CUDA cuDNN Triton Nemotron NeMo Whisper DeepSeek OpenAI Anthropic Mistral Cohere Qwen Llama Gemini Inferock SpeekToMe'.split()
VOCAB+=['Hugging Face','Apache Arrow','Amazon Polly','Stable Diffusion','Visual Studio Code','Digital Ocean','Pinecone','Weaviate','Milvus','Qdrant','Chroma','Cache','Datadog','Sentry','Postman']
CASES=[
('target','We deploy Kubernetes with Docker and monitor Redis using Grafana.',['Kubernetes','Docker','Redis','Grafana']),
('target','The Kafka queue sends events to PostgreSQL and Elasticsearch.',['Kafka','PostgreSQL','Elasticsearch']),
('target','Our Python service uses FastAPI and Pydantic with SQLAlchemy.',['FastAPI','Pydantic','SQLAlchemy']),
('target','We train the model in PyTorch and export it with TensorRT for CUDA.',['PyTorch','TensorRT','CUDA']),
('target','Terraform provisions Cloudflare and GitHub connects to Vercel.',['Terraform','Cloudflare','GitHub','Vercel']),
('target','The desktop application uses Tauri and TypeScript with a WebSocket connection.',['Tauri','TypeScript','WebSocket']),
('confuser','Please read this document while the doctor brings me some coffee.',[]),
('confuser','I left the cash beside the notebook and closed the window.',[]),
('confuser','The communities organized a party near the old docks.',[]),
('confuser','He felt tired after carrying the torch across the bridge.',[]),
('confuser','The pie was warm and the flask was filled with fresh water.',[]),
('confuser','Ask the chef to bring a new tray and some celery for dinner.',[]),
('ordinary','Tomorrow morning we will take the train to the coast and visit my grandmother.',[]),
('ordinary','The children finished their homework before playing outside in the garden.',[]),
('ordinary','Could you move our appointment to Thursday afternoon and send me the address?',[]),
('ordinary','She opened the blue cupboard and put the clean plates on the wooden shelf.',[]),
('mixed','The doctor asked me to read this page before I opened Redis on my laptop.',['Redis']),
('mixed','We bought coffee for the team while Kafka processed the events in the background.',['Kafka']),
]
async def main():
 (OUT/'vocabulary.json').write_text(json.dumps(VOCAB,indent=2)+'\n')
 sem=asyncio.Semaphore(4)
 async def make(i,kind,text,targets,voice):
  key=f'{i:02d}-{voice.split("-")[-1]}';mp3=OUT/f'{key}.mp3';wav=OUT/f'{key}.wav'
  async with sem:
   if not mp3.exists(): await edge_tts.Communicate(text,voice).save(str(mp3))
   subprocess.run(['ffmpeg','-v','error','-y','-i',str(mp3),'-ac','1','-ar','16000','-c:a','pcm_s16le',str(wav)],check=True)
  with wave.open(str(wav)) as w: duration=w.getnframes()/w.getframerate()
  return dict(id=key,category=kind,reference=text,targets=targets,voice=voice,audio=wav.name,duration=duration,source='Microsoft Edge TTS, controlled synthetic speech')
 rows=await asyncio.gather(*(make(i,k,t,terms,v) for i,(k,t,terms) in enumerate(CASES) for v in ['en-US-AriaNeural','en-GB-RyanNeural']))
 human=pathlib.Path('/mnt/sumedh/AI/models/speech/NeMo-Speech.cpp/test_files/asr/wav/test/jfk.wav')
 if human.exists():
  wav=OUT/'human-jfk.wav';subprocess.run(['ffmpeg','-v','error','-y','-i',str(human),'-ac','1','-ar','16000','-c:a','pcm_s16le',str(wav)],check=True)
  with wave.open(str(wav)) as w: duration=w.getnframes()/w.getframerate()
  rows.append(dict(id='human-jfk',category='human',reference='And so my fellow Americans ask not what your country can do for you ask what you can do for your country',targets=[],voice='John F Kennedy archival recording',audio=wav.name,duration=duration,source=str(human)))
 (OUT/'corpus.json').write_text(json.dumps(rows,indent=2)+'\n');print(f'{len(rows)} clips, {len(VOCAB)} vocabulary phrases, {sum(r["duration"] for r in rows):.1f}s audio',flush=True)
asyncio.run(main())
