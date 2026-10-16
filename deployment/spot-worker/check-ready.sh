#!/usr/bin/env bash
# Refresh readiness after a reboot or model-process restart; test ASR once per PID.
set -euo pipefail
source "${1:?worker configuration file required}"
mkdir -p "/run/$SERVICE_PREFIX"
exec 8>"/run/$SERVICE_PREFIX/readiness.lock"; flock -n 8 || exit 0
READY="/run/$SERVICE_PREFIX/ready"
PID=$(systemctl show "$NEMO_SERVICE" -p MainPID --value)
if [[ $PID == 0 ]] || ! curl -fsS --max-time 3 "http://127.0.0.1:$NEMO_PORT/health" >/dev/null; then rm -f "$READY";exit 0;fi
if [[ -f $READY ]] && [[ $(cat "$READY") == "$PID" ]]; then exit 0;fi
rm -f "$READY"
nvidia-smi --query-compute-apps=pid --format=csv,noheader | tr -d ' ' | grep -Fxq "$PID"
"$ROOT/bin/gateway" -upstream "http://127.0.0.1:$NEMO_PORT" -smoke-wav "$ROOT/smoke.wav" -smoke-diarization=false > "$ROOT/smoke-stream.json"
printf '%s\n' "$PID" > "$READY"
