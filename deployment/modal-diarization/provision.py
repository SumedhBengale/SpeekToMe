"""Create (or rotate) the diarization credentials without printing any secret.

    python provision.py ../../server/.env            # first time: keys, secrets, proxy token
    python provision.py ../../server/.env --rotate   # new key pair (no jobs in flight), same bearer
    python provision.py ../../server/.env --url-only # after `modal deploy`: record the endpoint URL

Where each credential lives:
  * X25519 private key   Modal Secret `speektome-diarization-key`, attached to the GPU function only.
                         Never written locally: the coordinator needs only the public key.
  * bearer key           coordinator .env; Modal stores only its SHA-256 (`speektome-diarization-api`).
  * public key + key ID  coordinator .env (public key); the front endpoint accepts the key IDs.
  * proxy token          coordinator .env; Modal's edge requires it before any container starts.
"""
import base64
import hashlib
import json
import os
import secrets
import subprocess
import sys
import tempfile
from pathlib import Path

import modal
from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey

from envelope import key_id, raw_public

PREFIX = 'SPEEKTOME_DIARIZATION_'


def read_env(path):
    values = {}
    if path.exists():
        for line in path.read_text().splitlines():
            if '=' in line and not line.lstrip().startswith('#'):
                name, value = line.split('=', 1)
                values[name.strip()] = value.strip()
    return values


def write_env(path, updates):
    """Replace or append only our keys; keep every other line and the file's 0600 mode."""
    lines = path.read_text().splitlines() if path.exists() else []
    seen = set()
    for i, line in enumerate(lines):
        name = line.split('=', 1)[0].strip()
        if name in updates:
            lines[i] = f'{name}={updates[name]}'
            seen.add(name)
    missing = [name for name in updates if name not in seen]
    if missing:
        lines += ['', '# Serverless GPU diarization (deployment/modal-diarization/provision.py)']
        lines += [f'{name}={updates[name]}' for name in missing]
    path.touch(mode=0o600, exist_ok=True)
    path.write_text('\n'.join(lines)+'\n')
    os.chmod(path, 0o600)


def modal_secret(name, values):
    with tempfile.NamedTemporaryFile('w', suffix='.json', delete=False) as handle:
        os.chmod(handle.name, 0o600)
        json.dump(values, handle)
    try:
        subprocess.run(['modal', 'secret', 'create', name, '--from-json', handle.name, '--force'], check=True,
                       stdout=subprocess.DEVNULL)
    finally:
        os.unlink(handle.name)


def endpoint_url():
    return modal.Function.from_name('speektome-diarization', 'api').get_web_url().rstrip('/')


def main(env_path, *flags):
    env_path = Path(env_path)
    current = read_env(env_path)
    if '--url-only' in flags:
        write_env(env_path, {PREFIX+'URL': endpoint_url()})
        print(f'Recorded {PREFIX}URL in {env_path}')
        return
    rotate = '--rotate' in flags
    if current.get(PREFIX+'PUBLIC_KEY') and not rotate:
        sys.exit(f'{env_path} already has diarization credentials; pass --rotate to replace the key pair')

    private = X25519PrivateKey.generate()
    public = raw_public(private.public_key())
    private_b64 = base64.b64encode(private.private_bytes_raw()).decode()
    token = current.get(PREFIX+'TOKEN') or secrets.token_urlsafe(48)
    # Modal never returns a Secret's value, so the old private key is gone after a rotation:
    # rotate while no diarization job is in flight (README.md, "Rotating keys").
    modal_secret('speektome-diarization-key', {'DIARIZATION_PRIVATE_KEY': private_b64})
    modal_secret('speektome-diarization-api', {'DIARIZATION_TOKEN_SHA256': hashlib.sha256(token.encode()).hexdigest(),
                                                'DIARIZATION_KEY_IDS': key_id(public).hex()})
    updates = {PREFIX+'TOKEN': token, PREFIX+'PUBLIC_KEY': base64.b64encode(public).decode(),
               PREFIX+'CONCURRENCY': current.get(PREFIX+'CONCURRENCY', '8')}
    if not current.get(PREFIX+'MODAL_KEY'):
        proxy = modal.Workspace.from_context().proxy_tokens.create(name='speektome-coordinator')
        updates[PREFIX+'MODAL_KEY'], updates[PREFIX+'MODAL_SECRET'] = proxy.token_id, proxy.token_secret
    try:
        updates[PREFIX+'URL'] = endpoint_url()
    except Exception:
        pass  # not deployed yet; rerun with --url-only after `modal deploy app.py`
    write_env(env_path, updates)
    print(f'Wrote {", ".join(sorted(updates))} to {env_path} (values not shown); '
          f'Modal secrets speektome-diarization-key and speektome-diarization-api updated.')


if __name__ == '__main__':
    main(*sys.argv[1:])
