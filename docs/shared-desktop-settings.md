# Shared Windows and Linux desktop client

Both targets compile `windows-client/src-tauri` and embed the same
`windows-client/frontend` directory. There is one settings page, stylesheet,
onboarding flow, and `config::AppConfig` schema. Build from the same Git commit
on each platform; do not maintain a separate Linux UI or copy settings files
between installations to select the UI version.

## Reference checkpoint

On 2026-10-19, the running Linux process was identified through `/proc` as
`/home/ssben/.codex/worktrees/d250/SpeekToMe/windows-client/target/debug/speektome`.
Its source worktree was based on `50d51ba`, with uncommitted client changes.
Commit `e9ec679` preserves those client changes, including custom shortcuts,
input gestures, account-limit feedback, meeting session handling, and the
existing native overlay. It also includes the client-related reference docs.
Uncommitted website and server changes in that worktree were left there.

The original executable's SHA-256 was
`060f25cc7ca88ed2785a77494c734fd8826aba9c47b8a3f90ef43ff7f38ec995`.
The imported source files predated the executable and were unchanged when
rechecked after import. This identifies the source snapshot accompanying the
running application; its build did not embed a Git/dirty-tree provenance record.
The running process and its saved preferences were left untouched.

## Shared behavior and native adapters

- `get_desktop_platform` identifies the compiled target. Browser user agents
  do not decide which operating-system labels or permission status appear.
- Shortcut values such as `Ctrl + Win` are identical in saved JSON. The UI
  displays Windows or Meta for the same key. Native adapters validate desktop
  reservations and register the selected shortcut.
- Both settings and onboarding use `gatherConfigFromUI`. Fields without a
  control on the current screen retain their saved values, including custom
  prompts, enhancement preferences, and onboarding's existing overlay position.
  Coordinates of zero and negative coordinates survive saving.
- Launch at sign-in uses the user's Windows `Run` registry entry or the Linux
  XDG autostart entry. The common toggle controls either implementation.
- The typing toggle has the same meaning on both platforms. Linux explains its
  clipboard fallback for characters unavailable through the keyboard map.
- Linux additionally shows shortcut and text-insertion permission status.
  These permissions depend on desktop portal support.
- Settings remain a Tauri webview on both platforms. The recording overlay uses
  Win32 on Windows and GTK/Cairo/Pango on Linux. Linux compositor placement
  constraints are documented in [LINUX.md](../windows-client/LINUX.md).

Preferences persist per installation in `%APPDATA%/SpeekToMe/config.json` on
Windows and `$XDG_CONFIG_HOME/SpeekToMe/config.json` (otherwise
`~/.config/SpeekToMe/config.json`) on Linux. The shared UI/schema does not sync
account tokens, machine-specific paths, or preferences between computers.

## Build and validation

From `windows-client`, on either target's development machine:

```sh
cargo build --locked
cargo test --locked -- --skip test_clipboard_lifecycle
```

The excluded existing test mutates the system clipboard. Linux dependencies
and packaging instructions are in [LINUX.md](../windows-client/LINUX.md).
On a Linux cross-compilation host with Windows GNU Rust libraries, MinGW,
and the native dependency toolchain installed:

```sh
cargo build --locked --target x86_64-pc-windows-gnu
```

For the DOM regression checks, from the repository root with Node 24 or later:

```sh
npm install --prefix /tmp/speektome-ui-tests --no-save jsdom@30.1.3
NODE_PATH=/tmp/speektome-ui-tests/node_modules node windows-client/tests/settings-parity.cjs
NODE_PATH=/tmp/speektome-ui-tests/node_modules node windows-client/tests/meeting-speaker-names.cjs
```

The parity test loads the actual HTML and controller for each native platform
with a deliberately mismatched browser user agent. It verifies identical
controls, platform key labels, Linux-only permission queries, custom-shortcut
round-trips, onboarding/settings payload parity, unchanged hidden preferences,
and zero/negative overlay coordinates. Native builds verify both platform code
paths; Windows runtime behavior and sign-in startup still need a Windows session.
