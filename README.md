# SpeekTo.Me

Desktop dictation and meeting transcription for Windows, Linux, and macOS,
with a shared Rust/Tauri client and Go backend.

| Folder | Purpose |
| --- | --- |
| `windows-client/` | Shared desktop app; the directory retains its historical name. See [Linux](windows-client/LINUX.md) and [macOS](windows-client/MACOS.md) setup. |
| `server/` | Main API, authentication, billing, encrypted storage, fleet controller and tests. |
| `website/` | React/Vite website and account dashboard. Public static assets belong in `public/`; imported assets stay in `src/assets/`. |
| `deployment/` | Current GPU gateway, CPU diarization deployment, expiry timer and security operations. |
| `shared/` | Shared vocabulary package used by the server and GPU gateway. |
| `benchmark/` | Maintained evaluation tools and recorded research results. |
| `helpers/` | Development utilities for icons and administrator setup. |
| `docs/` | Current architecture/behaviour docs and clearly separated historical operations/research notes. |

## Local checks

```sh
(cd server && go test ./...)
(cd deployment/spot-worker/gateway && go test ./...)
(cd deployment/diarization-server && python3 -m unittest test_gateway_admission test_gateway_sealing)
(cd windows-client && cargo fmt --check && cargo test --locked)
(cd website && npm ci && npm run build)
```

The production server runs on ARM64; follow the
[deployment instructions](deployment/security/README.md) rather than uploading a
local desktop build. See [desktop/backend contracts](docs/desktop-backend-contracts.md)
, [encrypted personal storage](docs/encrypted-personal-storage.md) and
[snippets](docs/snippets.md) for application behaviour.

Generated Rust/Go binaries, dependency/build caches, Tauri schemas, credentials,
local databases and recordings are ignored. Keep intentional synthetic fixtures
under a component's `testdata/` directory. The standalone provider benchmark
requires `SPEEKTOME_BENCHMARK_AUDIO=/path/to/selected-audio`; it never selects a
user meeting implicitly.

## Configuration

Deployment-specific values (hosts, IP addresses, buckets, keys) are never
committed. Copy `server/.env.example` to `server/.env` and
`website/.env.example` to `website/.env`, then fill in your own values.

## License

Copyright (c) 2026 Sumedh Bengale. All rights reserved. See [LICENSE](LICENSE).
The source is visible for reference only; no right to use, copy, modify or
distribute it is granted.
