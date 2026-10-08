#!/bin/bash
# Run as root on Ubuntu ARM64 or x86_64. The token file is supplied out of band.
set -euo pipefail
SOURCE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
AUTH_FILE=${1:?Usage: sudo ./setup.sh /path/to/protected-token-file}
[[ $EUID -eq 0 ]] || { echo 'Run with sudo'; exit 1; }
[[ -s "$AUTH_FILE" ]] || { echo 'Token file missing'; exit 1; }
ROOT=/opt/speektome-diarization
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends python3-venv ffmpeg libsndfile1 curl ca-certificates
install -d "$ROOT/models"
python3 -m venv "$ROOT/venv"
"$ROOT/venv/bin/pip" install --no-cache-dir -r "$SOURCE/requirements.txt"
REV=fd1c2b082a83ea1ae82779ca30d165bd383c0eb6
URL="https://huggingface.co/wannaphong/Nemotron-3-Diarization-ONNX/resolve/$REV"
for file in model.int8.onnx preprocessor_core.onnx constants.npz diarize.py; do
 curl --fail --location --retry 3 "$URL/$file" --output "$ROOT/models/$file"
done
(cd "$ROOT/models" && sha256sum --check "$SOURCE/models.sha256")
install -m 644 "$SOURCE/MODEL_LICENSE" "$ROOT/models/LICENSE"
install -m 644 "$SOURCE/MODEL_NOTICE" "$ROOT/models/NOTICE"
install -m 644 "$SOURCE/gateway.py" "$ROOT/gateway.py"
install -m 644 "$SOURCE/log-budget.py" "$ROOT/log-budget.py"
install -m 644 "$SOURCE/GATEWAY.md" "$ROOT/GATEWAY.md"
bash "$SOURCE/install-gateway.sh" "$AUTH_FILE"
apt-get clean
printf 'Installed. Check: sudo journalctl -u speektome-diarization -n 30\n'
