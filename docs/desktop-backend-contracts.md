# Paired desktop, API and diarization gateway

The shared desktop client requires the matching backend in this commit. Building
the client alone does not verify the recording-permit or account-usage APIs.

## Integrated dependencies

- The API registers `POST /v1/meetings/session` and
  `DELETE /v1/meetings/session/{id}`. The desktop persists its owner ID before
  requesting a permit and requires authorization before starting audio capture.
  Account-wide leases survive lost responses and can be released on reconnect.
- Account-limit storage, command-token reservations/settlement, confirmed billing
  periods, account usage responses and subscription-expiry maintenance are
  included with their existing tests. `GET /auth/me` supplies `command_usage` to
  the desktop's percentage meter and reset status. See
  [account-limits.md](account-limits.md) and
  [subscription-expiry](../deployment/subscription-expiry/README.md).
- The CPU coordinator and gateway agree on `X-SpeekToMe-Job-ID`. Duplicate
  uploads reattach to the saved identity. The coordinator saves validated output
  before acknowledging and deleting the gateway job. The main-server systemd
  drop-in is included under `deployment/diarization-server/systemd`.
- `windows-client/.cargo/config.toml` sets `CMAKE_POLICY_VERSION_MINIMUM=3.5`
  early enough for dependency build scripts, including bundled Opus on CMake 4.
  The application's `build.rs` no longer attempts to set it too late.

The server changes were recovered from the running application's `d250` source
worktree. Gateway support, its admission tests/configuration, and the Cargo fix
were recovered selectively from the preserved pre-merge stash. Older client
implementations, generated schemas, deployment snapshots and the retired webview
overlay were not restored.

## Regression checks

From `server`:

```sh
go test ./...
go test -race . ./db ./handlers ./accountlimits ./pipeline
go build -o /tmp/speektome-server-integration .
```

`newAPIMux` supplies the production application's routes and the HTTP tests.
There is no parallel route list in the test setup. The authenticated desktop
contract test verifies the actual usage response, repeated permit acquisition
with a saved ID, account ownership, stop/release and the next recording.

`TestCoordinatorWithProductionPythonGateway` starts a loopback-only Python
fixture from the standard library. It executes admission, persistence, status,
result and acknowledgment functions extracted from production `gateway.py`;
only model inference is synthetic. The real Go coordinator verifies the saved
identity, a retried upload, recovery with a fresh coordinator object, result
persistence and remote deletion. The test requires Python 3 and reports a skip
if it is unavailable. It does not load model weights or contact cloud services.

From the repository root:

```sh
python3 deployment/diarization-server/test_gateway_admission.py
```

From `windows-client`:

```sh
cargo test --locked -- --skip test_clipboard_lifecycle
cargo build --locked
cargo build --locked --target x86_64-pc-windows-gnu
```

The Windows cross-build requires its GNU Rust target and MinGW toolchain. Run
Cargo from `windows-client` so it discovers that directory's `.cargo/config.toml`.
Settings/meeting DOM checks remain documented in
[shared-desktop-settings.md](shared-desktop-settings.md).

To test the CMake fix without reusing previously built Opus artifacts, use an
empty target directory and bypass pkg-config. Run these from `windows-client`:

```sh
CARGO_TARGET_DIR=/tmp/speektome-opus-fresh OPUS_NO_PKG=1 cargo build --locked -p audiopus_sys
CARGO_TARGET_DIR=/tmp/speektome-opus-fresh OPUS_NO_PKG=1 cargo build --locked -p audiopus_sys --target x86_64-pc-windows-gnu
```

Both fresh bundled-Opus builds passed with CMake 4.3.0 on the integration host.
These isolate the dependency compatibility fix; they are not fresh builds of
every desktop dependency.

## Rollout boundary

The fixes are local source changes. No server/gateway deployment, cloud startup,
production inference, service installation or desktop restart is part of their
validation. Install the compatible CPU gateway before enabling its coordinator,
and deploy the matching API before distributing this client. Install/enable the
subscription-expiry timer separately using its deployment guide.

Native Windows runtime behavior still requires a Windows session. Synthetic
gateway inference and local HTTP tests verify contracts and persistence, not
model accuracy, production credentials, acoustic recording or cloud capacity.
