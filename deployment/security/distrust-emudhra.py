#!/usr/bin/env python3
"""Block the two incident-related roots on RPM hosts pending vendor CA updates.

Policy: Red Hat RHSB-2026-012 / Debian DSA-6572-1. Unrelated emSign
roots are preserved. Run as root, then restart long-running TLS clients.
"""
import hashlib
from pathlib import Path
import re
import subprocess

BUNDLE = Path('/etc/pki/tls/certs/ca-bundle.crt')
BLOCKLIST = Path('/etc/pki/ca-trust/source/blocklist')
NAMES = {'emSign ECC Root CA - G3', 'AAA Certificate Services'}


def subject(pem):
    text = subprocess.check_output(['openssl', 'x509', '-noout', '-subject',
                                    '-nameopt', 'RFC2253'], input=pem.encode()).decode()
    match = re.search(r'(?:^|,)CN=([^,\n]+)', text.removeprefix('subject=').strip())
    return match.group(1) if match else ''


def certificates():
    return re.findall(r'-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----',
                      BUNDLE.read_text(), re.S)


if __name__ == '__main__':
    BLOCKLIST.mkdir(parents=True, exist_ok=True)
    for pem in certificates():
        name = subject(pem)
        if name in NAMES:
            der = subprocess.check_output(['openssl', 'x509', '-outform', 'DER'],
                                          input=pem.encode())
            fingerprint = hashlib.sha256(der).hexdigest()
            (BLOCKLIST / ('emudhra-incident-' + fingerprint + '.pem')).write_text(pem + '\n')
            print('Blocklisted', name, fingerprint)
    subprocess.run(['update-ca-trust', 'extract'], check=True)
    remaining = NAMES.intersection(subject(pem) for pem in certificates())
    if remaining:
        raise SystemExit('Roots still present: ' + ', '.join(sorted(remaining)))
    print('Both incident-related roots absent from the active PEM bundle')
