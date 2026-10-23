# Security dependency rollout, 22 October 2026

The API, fleet controller and AMD64/ARM64 worker gateways were rebuilt with the
upstream Go 1.26.10 toolchain. Go modules require that release or newer. Both API
and gateway test suites passed; the shared vocabulary tests also passed.

- API and controller binaries are deployed on the main EC2 host.
- Two existing idle fleet workers received the patched gateway. Their running
  executable hashes match the rebuilt artifact; both report ACTIVE and ready.
- The fleet configuration references a new content-addressed manifest and patched
  gateway objects. The ASR model, native runtime and fleet sizing were preserved.
- The artifact publisher accepts `--manifest` to preserve the selected runtime
  profiles and rejects gateways built before the eMudhra protection.
- The Ubuntu diarization service uses FastAPI 0.159.1 / Starlette 1.3.1 in a new
  versioned virtual environment. The previous environment is retained for rollback.
  Dependency consistency, authenticated readiness and resident CPU model readiness
  passed. Admission tests and production-route framework compatibility tests passed.
- Website lockfiles resolve Vite 8.3.5 with patched transitive dependencies. The
  production Pages deployment is `d013d692-7dda-4c2c-be7f-2c0a28e5fa12`.
- The desktop lockfile resolves webpki-roots 1.0.10. Cargo check and a Linux release
  build passed. Existing installed desktop apps and Windows/macOS release packages
  were not replaced or distributed by this server rollout.

The main host's available Amazon Linux CA package still trusted emSign G3.
`deployment/security/distrust-emudhra.py` applied the root-blocklist policy in
Red Hat RHSB-2026-012 / Debian DSA-6572-1. Both incident-related roots are absent
from its active PEM bundle; unrelated emSign roots are preserved. Ubuntu hosts
received the patched vendor CA package. Relevant OpenSSL and Nginx packages were
updated where vendor updates were available.

The main host's default Go is now upstream 1.26.10. On the development machine,
Fedora Go and CA packages were updated; Node 24.22.1 is installed under
`~/.local/share/nodejs/` with user-local command links, and uv is now 0.13.4.

Go source scanning reports no vulnerable imported packages or reachable symbols.
Module-inventory notices remain for unused OpenPGP and legacy AWS SDK S3 encryption
packages. They are not used by this application. Binary-only scanner results for
these notices must not be interpreted as proven call paths. No real paid inference
or exploit payload was submitted during this rollout.

Main API rollback: `/home/ec2-user/speektome/rollback-security-20261022T040000Z`.
Controller and configuration rollback: `/opt/speektome-fleet/rollback-security-20261022`.
Worker rollback: `/opt/speektome-worker/rollback-security-20261022`.
Diarizer rollback: `/opt/speektome-diarization/rollback-security-20261022`.

Production credentials and existing application settings were preserved. Existing
uncommitted source changes were preserved; this security rollout adds dependency,
lockfile, build-floor, release-manifest and security documentation changes.
