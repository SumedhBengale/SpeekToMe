#!/usr/bin/env bash
# EC2 user-data entrypoint. Requires Ubuntu and a compatible NVIDIA driver.
set -euo pipefail
umask 027
BUCKET=${BUCKET:?S3 artifact bucket required}
MANIFEST_KEY=${MANIFEST_KEY:-deployment/20261006/manifest.json}
TOKEN_PARAMETER=${TOKEN_PARAMETER:-/speektome/spot-worker/auth-token}
ARTIFACT_REGION=${ARTIFACT_REGION:-us-east-1}
TOKEN_REGION=${TOKEN_REGION:-us-east-1}
ROOT=${ROOT:-/opt/speektome-worker}
SERVICE_PREFIX=${SERVICE_PREFIX:-speektome-worker}
NEMO_PORT=${NEMO_PORT:-8080}
GATEWAY_PORT=${GATEWAY_PORT:-8090}
MAX_STREAMS=${MAX_STREAMS:-20}
BATCH_SIZE=${BATCH_SIZE:-32}
STATE_SLOTS=${STATE_SLOTS:-40}
HTTP_THREADS=${HTTP_THREADS:-80}
INGRESS_COHORT_DELAY_US=${INGRESS_COHORT_DELAY_US:-120000}
FLEET_WORKER=${FLEET_WORKER:-0}
HTTP_PORT=${HTTP_PORT:-80}
SKIP_NGINX=${SKIP_NGINX:-0}
EXISTING_NEMO=${EXISTING_NEMO:-0}
ALLOW_NATIVE_BUILD=${ALLOW_NATIVE_BUILD:-0}
export DEBIAN_FRONTEND=noninteractive AWS_DEFAULT_REGION="$ARTIFACT_REGION" AWS_PAGER='' AWS_USE_DUALSTACK_ENDPOINT=true
[[ $EUID == 0 ]] || { echo 'Run as root' >&2; exit 1; }
install -d -m 0755 "$ROOT" "$ROOT/bin" "$ROOT/models" "$ROOT/config" "$ROOT/runtime" "/run/$SERVICE_PREFIX"
exec 9>"$ROOT/bootstrap.lock"; flock -n 9 || exit 0
rm -f "/run/$SERVICE_PREFIX/ready"
trap 'printf "bootstrap failed at line %s\n" "$LINENO" > "$ROOT/bootstrap-failed.txt"' ERR
# Keep package maintenance outside inference sessions on these ephemeral workers.
systemctl disable --now apt-daily.timer apt-daily-upgrade.timer update-notifier-download.timer update-notifier-motd.timer 2>/dev/null || true
systemctl stop apt-daily.service apt-daily-upgrade.service 2>/dev/null || true
cat > /etc/apt/apt.conf.d/99-speektome-maintenance <<'APTCONF'
APT::Periodic::Enable "0";
APTCONF
if [[ -f /etc/update-motd.d/90-updates-available ]]; then chmod a-x /etc/update-motd.d/90-updates-available; fi
# EC2 regional mirrors can lack IPv6; use Ubuntu's dual-stack public mirror.
python3 - <<'APT'
import pathlib,re
for p in list(pathlib.Path('/etc/apt/sources.list.d').glob('*.sources'))+list(pathlib.Path('/etc/apt/sources.list.d').glob('*.list'))+[pathlib.Path('/etc/apt/sources.list')]:
 if p.exists():p.write_text(re.sub(r'[a-z0-9-]+\.ec2\.archive\.ubuntu\.com','archive.ubuntu.com',p.read_text()))
APT
if [[ $FLEET_WORKER == 1 ]]; then
 cat > /etc/apt/apt.conf.d/99-speektome-ipv6 <<'APTCONF'
Acquire::ForceIPv6 "true";
Acquire::http::Timeout "10";
Acquire::https::Timeout "10";
Acquire::Retries "1";
APTCONF
fi
apt-get update -qq
apt-get install -y --no-install-recommends curl ca-certificates unzip nginx openssl python3 ffmpeg
if ! command -v aws >/dev/null; then
 case $(uname -m) in x86_64) CLI_ARCH=x86_64;; aarch64) CLI_ARCH=aarch64;; *) exit 1;; esac
 curl -fLsS "https://awscli.amazonaws.com/awscli-exe-linux-$CLI_ARCH.zip" -o /tmp/awscli-worker.zip
 unzip -q -o /tmp/awscli-worker.zip -d /tmp/worker-awscli
 /tmp/worker-awscli/aws/install --update
fi
id -u speektome-worker >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin speektome-worker
install -d -o speektome-worker -g speektome-worker -m 0750 "$ROOT/signals"
nvidia-smi >/dev/null
META_TOKEN=$(curl --fail --silent --max-time 3 -X PUT -H 'X-aws-ec2-metadata-token-ttl-seconds: 21600' http://169.254.169.254/latest/api/token)
metadata(){ curl --fail --silent --max-time 3 -H "X-aws-ec2-metadata-token: $META_TOKEN" "http://169.254.169.254/latest/$1"; }
INSTANCE_ID=$(metadata meta-data/instance-id)
PUBLIC_IP=$(metadata meta-data/public-ipv4 || true)
PRIVATE_IP=$(metadata meta-data/local-ipv4 || true)
MAC=$(metadata meta-data/mac)
IPV6=$(metadata "meta-data/network/interfaces/macs/$MAC/ipv6s" | head -1 || true)
if [[ $FLEET_WORKER == 1 && -z $IPV6 ]]; then echo "Fleet worker requires IPv6" >&2;exit 1;fi
REGION=$(metadata dynamic/instance-identity/document | python3 -c 'import json,sys;print(json.load(sys.stdin)["region"])')
case $(uname -m) in x86_64) ARCH=amd64;; aarch64) ARCH=arm64;; *) exit 1;; esac
GPU_NAME=$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1)
if [[ $ARCH == arm64 && $GPU_NAME == *T4* ]]; then PROFILE=arm64-sm75
elif [[ $ARCH == amd64 && ($GPU_NAME == *T4* || $GPU_NAME == *A10* || $GPU_NAME == *L4*) ]]; then PROFILE=amd64-cuda12
else echo "Unvalidated GPU/CPU profile: $ARCH / $GPU_NAME" >&2; exit 1; fi
aws s3 cp "s3://$BUCKET/$MANIFEST_KEY" "$ROOT/manifest.json" --only-show-errors
if [[ -n ${MANIFEST_SHA:-} ]]; then echo "$MANIFEST_SHA  $ROOT/manifest.json" | sha256sum --check --status;fi
fetch(){
 local key=$1 dest=$2 expected=$3
 if [[ -f $dest ]] && echo "$expected  $dest" | sha256sum --check --status; then return; fi
 aws s3 cp "s3://$BUCKET/$key" "$dest.part" --only-show-errors
 echo "$expected  $dest.part" | sha256sum --check --status
 mv "$dest.part" "$dest"
}
# Manifest values are passed as NUL-separated data, never sourced as shell code.
mapfile -d '' -t FIELDS < <(python3 - "$ROOT/manifest.json" "$PROFILE" "$ARCH" "$GPU_NAME" <<'PY'
import json,sys
m=json.load(open(sys.argv[1]));p=m['profiles'][sys.argv[2]];g=m['gateways'][sys.argv[3]]
for v in [p.get('key',''),p.get('sha256',''),g['key'],g['sha256'],m['asr']['key'],m['asr']['sha256'],'','',m['smoke']['key'],m['smoke']['sha256'],m['source']['key'],m['source']['sha256'],m['helpers']['build']['key'],m['helpers']['build']['sha256'],m['helpers']['ready']['key'],m['helpers']['ready']['sha256'],g.get('compression','none')]:
 sys.stdout.write(str(v)+'\0')
PY
)
[[ ${#FIELDS[@]} == 17 ]] || { echo 'Manifest profile missing' >&2; exit 1; }
if [[ -n ${FIELDS[0]} ]]; then
 fetch "${FIELDS[0]}" "$ROOT/runtime.tar.gz" "${FIELDS[1]}"
 if [[ ! -f $ROOT/runtime/.artifact-sha ]] || [[ $(cat "$ROOT/runtime/.artifact-sha") != "${FIELDS[1]}" ]]; then
  rm -rf "$ROOT/runtime.new"; mkdir "$ROOT/runtime.new"
  tar --no-same-owner -xzf "$ROOT/runtime.tar.gz" -C "$ROOT/runtime.new"
  echo "${FIELDS[1]}" > "$ROOT/runtime.new/.artifact-sha"
  rm -rf "$ROOT/runtime.old"; mv "$ROOT/runtime" "$ROOT/runtime.old"; mv "$ROOT/runtime.new" "$ROOT/runtime"; rm -rf "$ROOT/runtime.old"
 fi
elif [[ $ALLOW_NATIVE_BUILD == 1 && $ARCH == arm64 ]]; then
 fetch "${FIELDS[10]}" "$ROOT/source.tar.gz" "${FIELDS[11]}"
 mkdir -p "$ROOT/source";tar --no-same-owner -xzf "$ROOT/source.tar.gz" -C "$ROOT/source"
 fetch "${FIELDS[12]}" "$ROOT/build-runtime.sh" "${FIELDS[13]}"
 bash "$ROOT/build-runtime.sh" "$ROOT/source" "$ROOT/runtime" 75
else echo 'No prebuilt runtime for this profile; refusing boot-time compilation' >&2;exit 1;fi
if [[ ${FIELDS[16]} == gzip ]]; then
 fetch "${FIELDS[2]}" "$ROOT/bin/gateway.gz" "${FIELDS[3]}"
 gzip -dc "$ROOT/bin/gateway.gz" > "$ROOT/bin/gateway.part"
 mv "$ROOT/bin/gateway.part" "$ROOT/bin/gateway"
else
 fetch "${FIELDS[2]}" "$ROOT/bin/gateway" "${FIELDS[3]}"
fi
chmod 0755 "$ROOT/bin/gateway"
fetch "${FIELDS[4]}" "$ROOT/models/asr.gguf" "${FIELDS[5]}"
# Remove diarization weights inherited from older inference AMIs.
for model_root in "$ROOT/models" /opt/speektome/models; do
 if [[ -d $model_root ]]; then find "$model_root" \( -type f -o -type l \) -iname '*diar*.gguf' -delete; fi
done
rm -f "$ROOT/config/diar-offline.yaml"
fetch "${FIELDS[8]}" "$ROOT/smoke.wav" "${FIELDS[9]}"
install -d -m 0750 "/etc/$SERVICE_PREFIX"
aws ssm get-parameter --name "$TOKEN_PARAMETER" --with-decryption --region "$TOKEN_REGION" --query Parameter.Value --output text > "/etc/$SERVICE_PREFIX/worker.token"
chgrp speektome-worker "/etc/$SERVICE_PREFIX" "/etc/$SERVICE_PREFIX/worker.token"
chmod 0640 "/etc/$SERVICE_PREFIX/worker.token"
cat > "$ROOT/config/server.yaml" <<YAML
http:
  host: 127.0.0.1
  port: $NEMO_PORT
  threads: $HTTP_THREADS
  read-timeout: 60
  write-timeout: 60
  playground: false
asr:
  backend:
    gpu: 0
  model:
    path: $ROOT/models/asr.gguf
  streaming:
    rnnt_right_context: 6
  endpointing:
    enable: true
    vad_based: false
    stop_history_eou_ms: 800
  batching:
    enabled: true
    state_arena_slots: $STATE_SLOTS
    max_batch_size: $BATCH_SIZE
    max_queue_delay_us: 5000
    ingress_cohort_delay_us: $INGRESS_COHORT_DELAY_US
YAML
chmod -R a+rX "$ROOT/runtime" "$ROOT/models" "$ROOT/config"
cat > "/etc/$SERVICE_PREFIX/identity.env" <<ENV
INSTANCE_ID=$INSTANCE_ID
AWS_REGION=$REGION
WORKER_PROFILE=$PROFILE
ENV
if [[ $EXISTING_NEMO == 0 ]]; then
# Disable inherited inference services only on NEW fleet clones.
if [[ $FLEET_WORKER == 1 ]]; then
 for legacy in speektome-nemo speektome-gateway speektome-nemo-gateway; do systemctl disable --now "$legacy" 2>/dev/null || true;done
fi
cat > "/etc/systemd/system/$SERVICE_PREFIX-nemo.service" <<UNIT
[Unit]
Description=SpeekToMe GPU inference
After=network-online.target
Wants=network-online.target
[Service]
User=speektome-worker
Group=speektome-worker
WorkingDirectory=$ROOT
Environment=CUDA_VISIBLE_DEVICES=0
UnsetEnvironment=GGML_CUDA_DISABLE_GRAPHS
Environment=CUDA_DEVICE_SCHEDULE=2
Environment=LD_LIBRARY_PATH=$ROOT/runtime/lib
ExecStart=$ROOT/runtime/bin/nemo-speech serve --config $ROOT/config/server.yaml
Restart=on-failure
RestartSec=3
LimitNOFILE=65535
NoNewPrivileges=true
[Install]
WantedBy=multi-user.target
UNIT
fi
cat > "/etc/systemd/system/$SERVICE_PREFIX-gateway.service" <<UNIT
[Unit]
Description=SpeekToMe authenticated worker gateway
After=network-online.target
[Service]
User=speektome-worker
Group=speektome-worker
EnvironmentFile=/etc/$SERVICE_PREFIX/identity.env
ExecStart=$ROOT/bin/gateway -listen 127.0.0.1:$GATEWAY_PORT -upstream http://127.0.0.1:$NEMO_PORT -meeting-exclusive=${MEETING_EXCLUSIVE:-false} -max-streams $MAX_STREAMS -drain-file $ROOT/signals/draining -spot-file $ROOT/signals/spot-signal.json -token-file /etc/$SERVICE_PREFIX/worker.token -ready-file /run/$SERVICE_PREFIX/ready
Restart=on-failure
RestartSec=3
LimitNOFILE=65535
NoNewPrivileges=true
[Install]
WantedBy=multi-user.target
UNIT
if [[ $SKIP_NGINX == 0 ]]; then
 if [[ ! -f /etc/$SERVICE_PREFIX/tls.key ]]; then
  TLS_SAN="IP:${IPV6:-${PUBLIC_IP:-$PRIVATE_IP}}"
  [[ -z $PRIVATE_IP ]] || TLS_SAN="$TLS_SAN,IP:$PRIVATE_IP"
  openssl req -x509 -newkey rsa:2048 -nodes -days 90 -keyout "/etc/$SERVICE_PREFIX/tls.key" -out "/etc/$SERVICE_PREFIX/tls.crt" -subj "/CN=$INSTANCE_ID" -addext "subjectAltName=$TLS_SAN" >/dev/null 2>&1
 fi
 chmod 0600 "/etc/$SERVICE_PREFIX/tls.key"
 cat > "/etc/nginx/sites-available/$SERVICE_PREFIX" <<NGINX
server {
 listen $HTTP_PORT ssl default_server;
 listen [::]:$HTTP_PORT ssl default_server;
 server_name _;
 ssl_certificate /etc/$SERVICE_PREFIX/tls.crt;
 ssl_certificate_key /etc/$SERVICE_PREFIX/tls.key;
 ssl_protocols TLSv1.2 TLSv1.3;
 client_max_body_size 512m;
 location / {
  proxy_pass http://127.0.0.1:$GATEWAY_PORT;
  proxy_http_version 1.1;
  proxy_set_header Upgrade \$http_upgrade;
  proxy_set_header Connection "upgrade";
  proxy_set_header Host \$host;
  proxy_set_header Authorization \$http_authorization;
  proxy_buffering off;
  proxy_request_buffering off;
  proxy_read_timeout 21600s;
  proxy_send_timeout 21600s;
 }
}
NGINX
 if [[ $FLEET_WORKER == 1 ]]; then
  install -d -m 0700 /etc/nginx/legacy-fleet-sites
  for site in /etc/nginx/sites-enabled/*; do
   [[ -e $site || -L $site ]] || continue
   [[ $(basename "$site") == "$SERVICE_PREFIX" ]] || mv "$site" /etc/nginx/legacy-fleet-sites/
  done
 fi
 rm -f /etc/nginx/sites-enabled/default
 ln -sf "/etc/nginx/sites-available/$SERVICE_PREFIX" "/etc/nginx/sites-enabled/$SERVICE_PREFIX"
 nginx -t
fi
systemctl daemon-reload
if [[ $EXISTING_NEMO == 0 ]]; then systemctl enable "$SERVICE_PREFIX-nemo";systemctl restart "$SERVICE_PREFIX-nemo";fi
systemctl enable "$SERVICE_PREFIX-gateway";systemctl restart "$SERVICE_PREFIX-gateway"
if [[ $SKIP_NGINX == 0 ]]; then systemctl enable nginx;systemctl restart nginx;fi
for attempt in $(seq 1 180); do
 if curl -fsS "http://127.0.0.1:$NEMO_PORT/health" >/dev/null; then break;fi
 sleep 2
done
NEMO_SERVICE="$SERVICE_PREFIX-nemo"
if [[ $EXISTING_NEMO == 1 ]]; then NEMO_SERVICE=${EXISTING_NEMO_SERVICE:-speektome-nemo};fi
cat > "/etc/$SERVICE_PREFIX/readiness.env" <<ENV
ROOT=$ROOT
SERVICE_PREFIX=$SERVICE_PREFIX
NEMO_PORT=$NEMO_PORT
NEMO_SERVICE=$NEMO_SERVICE
ENV
fetch "${FIELDS[14]}" "$ROOT/check-ready.sh" "${FIELDS[15]}"
chmod 0755 "$ROOT/check-ready.sh"
bash "$ROOT/check-ready.sh" "/etc/$SERVICE_PREFIX/readiness.env"
[[ -f /run/$SERVICE_PREFIX/ready ]] || { echo 'GPU readiness failed' >&2;exit 1; }
cat > "/etc/systemd/system/$SERVICE_PREFIX-readiness.service" <<UNIT
[Unit]
Description=Refresh SpeekToMe readiness
[Service]
Type=oneshot
ExecStart=$ROOT/check-ready.sh /etc/$SERVICE_PREFIX/readiness.env
UNIT
cat > "/etc/systemd/system/$SERVICE_PREFIX-readiness.timer" <<UNIT
[Unit]
Description=Check worker readiness periodically
[Timer]
OnBootSec=30
OnUnitActiveSec=10
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload;systemctl enable --now "$SERVICE_PREFIX-readiness.timer"
# A model smoke test alone is insufficient: verify the authenticated gateway
# actually parsed its service arguments and is exposing the expected capacity.
GATEWAY_PORT="$GATEWAY_PORT" TOKEN_FILE="/etc/$SERVICE_PREFIX/worker.token" EXPECTED_MAX="$MAX_STREAMS" python3 - <<'PYCHECK'
import json,os,pathlib,time,urllib.request
key=pathlib.Path(os.environ['TOKEN_FILE']).read_text().strip()
for attempt in range(60):
 try:
  req=urllib.request.Request('http://127.0.0.1:'+os.environ['GATEWAY_PORT']+'/worker/metadata',headers={'Authorization':'Bearer '+key})
  with urllib.request.urlopen(req,timeout=3) as response: metadata=json.load(response)
  if metadata.get('ready') and metadata.get('max_streams')==int(os.environ['EXPECTED_MAX']): break
 except Exception: pass
 time.sleep(2)
else: raise SystemExit('authenticated gateway readiness failed')
PYCHECK
if [[ $FLEET_WORKER == 1 ]]; then
 aws s3 cp "/etc/$SERVICE_PREFIX/tls.crt" "s3://$BUCKET/registration/$INSTANCE_ID/tls.crt" --only-show-errors
 python3 - "$ROOT/manifest.json" "$ROOT/spot-watcher.py" <<'WATCH'
import json,sys,subprocess,os,hashlib
h=json.load(open(sys.argv[1]))['helpers']['watcher']
subprocess.run(['aws','s3','cp','s3://'+os.environ['BUCKET']+'/'+h['key'],sys.argv[2],'--only-show-errors'],check=True)
assert hashlib.sha256(open(sys.argv[2],'rb').read()).hexdigest()==h['sha256']
WATCH
 install -d -o speektome-worker -g speektome-worker -m 0750 "$ROOT/signals"
 cat > "/etc/systemd/system/$SERVICE_PREFIX-spot-watcher.service" <<UNIT
[Unit]
Description=Spot interruption metadata watcher
After=network-online.target
[Service]
Environment=SIGNAL_DIR=$ROOT/signals
ExecStart=/usr/bin/python3 $ROOT/spot-watcher.py
Restart=always
RestartSec=3
[Install]
WantedBy=multi-user.target
UNIT
 systemctl daemon-reload;systemctl enable --now "$SERVICE_PREFIX-spot-watcher"
fi
WORKER_ADDRESS=${PUBLIC_IP:-$PRIVATE_IP}
[[ -z $IPV6 ]] || WORKER_ADDRESS="[$IPV6]"
python3 - "$ROOT" "$INSTANCE_ID" "$REGION" "$PROFILE" "$WORKER_ADDRESS" "$HTTP_PORT" "$MAX_STREAMS" "$SKIP_NGINX" "$GATEWAY_PORT" <<'PY'
import json,sys,pathlib
p=pathlib.Path(sys.argv[1]);result=dict(ready=True,instance_id=sys.argv[2],region=sys.argv[3],profile=sys.argv[4],endpoint=('http://127.0.0.1:'+sys.argv[9] if sys.argv[8]=='1' else 'https://'+sys.argv[5]+':'+sys.argv[6]),max_streams=int(sys.argv[7]))
(p/'bootstrap-result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
PY
rm -f "$ROOT/bootstrap-failed.txt"
