# Open ASR Leaderboard - SpeekTo.Me Benchmark Suite

This directory contains the benchmarking harness integrating **SpeekTo.Me** (Speech model + LLM pipeline) with the Hugging Face **[Open ASR Leaderboard](https://github.com/huggingface/open_asr_leaderboard)**.

## Prerequisites

1. **Python dependencies**:
   ```bash
   pip install datasets evaluate jiwer num2words regex soundfile librosa requests tqdm python-dotenv kaldialign
   ```
2. **FFmpeg**: Must be available in your system `PATH` (used for Opus compression).

## Supported Model Variants

- `speektome/full-pipeline` - Evaluates end-to-end pipeline (`enhance=true`: Whisper + DeepSeek LLM enhancement).
- `speektome/raw-asr` - Evaluates speech recognition only (`enhance=false`).

## Quick Usage

### 1. Test Authentication & Connection
```bash
python benchmark/run_speektome_eval.py --server-url "http://127.0.0.1:8000" --token "<YOUR_SESSION_TOKEN>" --test-auth
```

### 2. Run a Quick Smoke Test (10 samples)
```bash
python benchmark/run_speektome_eval.py --server-url "http://127.0.0.1:8000" --token "<YOUR_SESSION_TOKEN>" --dataset "librispeech" --split "test.clean" --model-variant "full-pipeline" --max-samples 10
```

### 3. Run Benchmark on Full Standard Datasets
```bash
python benchmark/run_speektome_eval.py --server-url "http://127.0.0.1:8000" --token "<YOUR_SESSION_TOKEN>" --dataset "librispeech" --split "test.clean" --model-variant "full-pipeline" --max-workers 4
```

### Standard Leaderboard Datasets
- `librispeech:test.clean`
- `librispeech:test.other`
- `ami_cleaned:test`
- `gigaspeech_cleaned:test`
- `spgispeech:test`
- `voxpopuli_cleaned_aa:test`
