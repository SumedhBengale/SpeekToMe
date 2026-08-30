#!/usr/bin/env bash
set -e

echo "=== SpeekToMe Golden AMI Bootstrap Script ==="

# 1. Create directory structure
sudo mkdir -p /opt/speektome-asr/models
sudo chown -R ubuntu:ubuntu /opt/speektome-asr

# 2. Setup Python Virtual Environment
sudo apt-get update && sudo apt-get install -y python3-venv python3-pip libsndfile1 ffmpeg
python3 -m venv /opt/speektome-asr/venv
source /opt/speektome-asr/venv/bin/activate

pip install --upgrade pip
pip install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu128
pip install Cython soundfile numpy websockets pynvml torchao
pip install nemo_toolkit['asr']

# 3. Copy service file
sudo cp /tmp/nemotron_streaming_worker.py /opt/speektome-asr/nemotron_streaming_worker.py
sudo cp /tmp/nemotron-asr.service /etc/systemd/system/nemotron-asr.service
sudo chown -R ubuntu:ubuntu /opt/speektome-asr

sudo systemctl daemon-reload
sudo systemctl enable nemotron-asr.service

echo "=== Bootstrap Complete ==="
