#!/bin/bash
set -euo pipefail
ROOT=/opt/speektome-diarization
SOURCE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
id speektome-diarization >/dev/null 2>&1 || useradd --system --home-dir "$ROOT" --shell /usr/sbin/nologin speektome-diarization
install -d -m 750 -o root -g speektome-diarization /etc/speektome-diarization
TOKEN_SOURCE=${1:?Pass the authentication token file path}
if [[ "$(readlink -f "$TOKEN_SOURCE")" != /etc/speektome-diarization/token ]]; then
  install -m 640 -o root -g speektome-diarization "$TOKEN_SOURCE" /etc/speektome-diarization/token
else
  chown root:speektome-diarization /etc/speektome-diarization/token
  chmod 640 /etc/speektome-diarization/token
fi
install -d -m 700 -o speektome-diarization -g speektome-diarization /var/lib/speektome-diarization
install -m 644 "$SOURCE/systemd/speektome-diarization.service" /etc/systemd/system/speektome-diarization.service
install -d /etc/systemd/journald.conf.d
install -m 644 "$SOURCE/journald/50-speektome-budget.conf" /etc/systemd/journald.conf.d/50-speektome-budget.conf
install -m 755 "$ROOT/log-budget.py" /usr/local/sbin/speektome-log-budget
install -m 644 "$SOURCE/systemd/speektome-log-budget.service" /etc/systemd/system/speektome-log-budget.service
install -m 644 "$SOURCE/systemd/speektome-log-budget.timer" /etc/systemd/system/speektome-log-budget.timer
systemctl daemon-reload
systemctl restart systemd-journald
journalctl --rotate
journalctl --vacuum-size=384M
systemctl enable --now speektome-log-budget.timer
systemctl start speektome-log-budget.service
systemctl enable --now speektome-diarization.service
