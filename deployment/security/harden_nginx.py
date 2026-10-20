from pathlib import Path
import gzip,os,re,subprocess
p=Path('/etc/nginx/nginx.conf');original=p.read_text()
s=original.replace('"$request"','"$request_method $uri $server_protocol"').replace('"$http_referer"','"-"').replace('"$http_user_agent" "$http_x_forwarded_for"','"-" "-"').replace('error_log /var/log/nginx/error.log notice;','error_log /var/log/nginx/error.log crit;')
if s==original and '"$request"' in s:raise SystemExit('Unexpected Nginx log configuration')
p.write_text(s)
r=subprocess.run(['nginx','-t'],capture_output=True,text=True)
if r.returncode:p.write_text(original);raise SystemExit('Nginx validation failed; original restored')
subprocess.run(['systemctl','reload','nginx'],check=True)
# Strip credentials from retained proxy logs, including rotated/gzipped logs.
pattern=re.compile(rb'([?&](?:token|temp_token|access_token|session_token|code|payment_url|client_secret)=)[^\s&"<>]*',re.I)
changed=0
for log in Path('/var/log/nginx').iterdir():
 if not log.is_file() or log.is_symlink() or not log.name.startswith(('access.log','error.log')):continue
 raw=log.read_bytes();data=gzip.decompress(raw) if log.suffix=='.gz' else raw
 clean,n=pattern.subn(rb'\1[redacted]',data)
 if n:
  # Keep the active log's inode; future writes already omit query strings.
  with log.open('r+b') as f:f.write(gzip.compress(clean) if log.suffix=='.gz' else clean);f.truncate()
  changed+=n
 os.chmod(log,0o640)
print('Nginx logging updated; credential fields scrubbed:',changed)
