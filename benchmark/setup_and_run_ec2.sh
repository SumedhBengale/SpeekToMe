#!/bin/bash
set -e

echo "=== SpeekTo.Me EC2 Benchmark Setup & Execution ==="

# 1. Install system prerequisites (ffmpeg, python3, pip, git)
if command -v dnf &> /dev/null; then
    sudo dnf update -y
    sudo dnf install -y python3 python3-pip ffmpeg git
elif command -v yum &> /dev/null; then
    sudo yum update -y
    sudo yum install -y python3 python3-pip ffmpeg git
elif command -v apt-get &> /dev/null; then
    sudo apt-get update -y
    sudo apt-get install -y python3 python3-pip ffmpeg git
fi

# 2. Setup benchmark directory
BENCHMARK_DIR="/home/ec2-user/speektome-benchmark"
mkdir -p "$BENCHMARK_DIR"
cd "$BENCHMARK_DIR"

# 3. Create virtual environment
if [ ! -d "venv" ]; then
    python3 -m venv venv
fi
source venv/bin/activate

# 4. Install python dependencies
pip install --no-cache-dir \
    datasets \
    evaluate \
    jiwer \
    num2words \
    regex \
    soundfile \
    librosa \
    requests \
    tqdm \
    python-dotenv \
    kaldialign

# 5. Clone open_asr_leaderboard if not present
if [ ! -d "open_asr_leaderboard" ]; then
    git clone https://github.com/huggingface/open_asr_leaderboard.git
fi

# Copy speektome provider and init into cloned leaderboard
mkdir -p open_asr_leaderboard/api/providers
cp "$BENCHMARK_DIR/speektome_provider.py" open_asr_leaderboard/api/providers/ 2>/dev/null || true

echo "=== Environment Ready! ==="
