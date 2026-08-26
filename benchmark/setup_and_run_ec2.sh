#!/bin/bash
set -e

echo "=== SpeekTo.Me EC2 Benchmark Setup & Execution ==="

# 1. Install system prerequisites on Amazon Linux 2023
# Note: AL2023 has Python 3.9 as system default. Python 3.11/3.12 can also be installed.
sudo dnf install -y python3 python3-pip git tar xz gcc gcc-c++ make python3-devel || true

# Install static ffmpeg binary if ffmpeg is not already installed
if ! command -v ffmpeg &> /dev/null; then
    echo "Installing static FFmpeg build..."
    ARCH=$(uname -m)
    if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz"
    else
        FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"
    fi
    curl -sL "$FFMPEG_URL" | tar -xJ -C /tmp/
    sudo cp /tmp/ffmpeg-*-static/ffmpeg /usr/local/bin/
    sudo cp /tmp/ffmpeg-*-static/ffprobe /usr/local/bin/
    sudo chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe
    rm -rf /tmp/ffmpeg-*-static
    echo "FFmpeg installed successfully: $(ffmpeg -version | head -n 1)"
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

# 4. Upgrade pip and install python dependencies
pip install --upgrade pip
pip install --no-cache-dir \
    transformers \
    evaluate \
    datasets \
    jiwer \
    num2words \
    regex \
    soundfile \
    librosa \
    requests \
    tqdm \
    python-dotenv \
    kaldialign || true

# 5. Clone open_asr_leaderboard if not present
if [ ! -d "open_asr_leaderboard" ]; then
    git clone https://github.com/huggingface/open_asr_leaderboard.git
fi

# Copy speektome provider and register it
mkdir -p open_asr_leaderboard/api/providers
if [ -f "$BENCHMARK_DIR/speektome_provider.py" ]; then
    cp -f "$BENCHMARK_DIR/speektome_provider.py" open_asr_leaderboard/api/providers/
fi

# Ensure speektome_provider is imported in __init__.py
if ! grep -q "speektome_provider" open_asr_leaderboard/api/providers/__init__.py; then
    sed -i 's/from \. import (/from . import (\n    speektome_provider,/g' open_asr_leaderboard/api/providers/__init__.py
fi

echo "=== Environment Ready! ==="
