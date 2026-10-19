# Linux client

The Rust/Tauri client in this directory also builds on Linux. The Linux
configuration uses the desktop ID `me.speekto.SpeekToMe`; the Windows package
configuration remains separate.

## Build

Install Rust, the [Tauri 2 Linux prerequisites](https://v2.tauri.app/start/prerequisites/),
the PulseAudio development library, the ALSA development library, and the
AT-SPI 2 development library. On Fedora these include `webkit2gtk4.1-devel`,
`libappindicator-gtk3-devel`, `alsa-lib-devel`, `pulseaudio-libs-devel`,
`at-spi2-core-devel`, and `opus-devel`.

From this directory:

```sh
cargo build --locked
cargo tauri build --bundles deb,rpm,appimage
```

The second command needs the Tauri CLI (`cargo install tauri-cli`). Build
AppImages on an older supported Linux base if they need to run across a wide
range of distribution versions. Native `.deb` and `.rpm` bundles install the
desktop entry, privileged shortcut helper, and its PolicyKit action. The
helper is built from `src-tauri/linux-keywatch.c` when packaging. AppImages
cannot use the bundled privileged helper; install the helper and its PolicyKit
action separately on the system to enable modifier-only
shortcuts in an AppImage. A standalone binary or AppImage creates a per-user
desktop entry on first run because the GlobalShortcuts portal requires an
application identity. Keep an AppImage at a stable path so its launcher
remains valid.

On KDE Plasma Wayland and compositors that implement `zwlr_layer_shell_v1`,
the pill uses GTK Layer Shell so it can follow the selected screen position
without taking keyboard focus. The native RPM and DEB packages depend on the
GTK Layer Shell runtime. AppImages and `cargo run` builds need that runtime
installed separately. GNOME Wayland does not implement this layer-shell
protocol; its compositor controls ordinary window placement, so the selected
position cannot be guaranteed there. The pill remains non-focusable and
click-through in the fallback path.

For an AppImage installation built from this repository, install the helper
after building the bundle:

```sh
sudo install -D -o root -g root -m 0755 target/release/speektome-keywatch /usr/libexec/speektome-keywatch
sudo install -D -o root -g root -m 0644 src-tauri/linux-keywatch.policy /usr/share/polkit-1/actions/me.speekto.SpeekToMe.policy
```

## Desktop integration

- Wayland is the primary target. The session must provide the
  `org.freedesktop.portal.GlobalShortcuts` and
  `org.freedesktop.portal.RemoteDesktop` portals. The app requests shortcut
  and keyboard-control permission at startup and shows their status in
  Settings. The Windows shortcut choices are retained, with Win shown as
  Meta on Linux. The default is Ctrl+Meta. Native packages install a
  PolicyKit helper for modifier-only shortcuts in both input modes. It asks for
  authorization when needed and keeps the keyboard devices open only while
  the app runs. Restart the app after connecting a new keyboard.
- Paste mode copies dictated text to the Wayland clipboard and sends Ctrl+V
  through the RemoteDesktop portal. Typing mode sends key events for ASCII
  text. For characters unavailable through the compositor's keyboard map,
  typing mode pastes the text to preserve it accurately.
- AT-SPI is used to detect a known non-editable focus. In that case the text
  stays on the clipboard. Some applications do not expose enough accessibility
  state; the app attempts insertion and leaves the text on the clipboard.
- Meeting recording captures the default microphone through CPAL and the
  default output monitor through the PulseAudio protocol (also provided by
  PipeWire). A running PipeWire PulseAudio or PulseAudio server is required.
- Autostart uses an XDG desktop entry. Configuration lives under
  `$XDG_CONFIG_HOME/SpeekToMe` or `~/.config/SpeekToMe`.

This port has been exercised on KDE Plasma Wayland. Other desktop environments
need portal support and should be validated on their own sessions before
release. The application displays a capability warning if either permission
is unavailable.

The Linux launcher disables WebKitGTK's DMA-BUF renderer by default. This
avoids a fatal Wayland explicit-sync protocol error observed on Fedora KDE.

## Input gestures (shared with Windows)

- Push to Talk: hold the shortcut to dictate and release to submit. For Command
  Mode, tap once, then press and hold again to speak the command; release to submit.
- Hands-Free: tap once to dictate, then tap again to submit. Double-tap to start
  Command Mode, then tap once to submit.
- The first tap of a double gesture must be shorter than 350 ms; the next press
  must arrive within 400 ms of that release. A completed hold or a stop tap never
  arms Command Mode. With Command Mode disabled, input routes directly to dictation.
- Capture starts on the first press and buffers audio while the gesture is
  classified. In Push to Talk, a lone short tap is discarded if no second press
  follows. Holding in Hands-Free submits on release and displays a mode warning.
- Key repeat and duplicate portal signals do not count as additional presses.
  The route and selected text stay attached to the recording through streaming,
  batch fallback and AI transformation. Inputs during processing are ignored
  until the current result finishes, so recordings cannot overwrite each other.

Gesture regression tests use synthetic timestamps and do not capture audio or
contact the server. Run the client suite without its clipboard-mutating test:

```sh
cargo test --locked -- --skip test_clipboard_lifecycle
```

### Custom shortcuts

General → Trigger Hotkey → **Record shortcut** captures a shortcut; release all keys,
then use **Save & Apply**. The same control is available during onboarding. Esc,
Cancel, losing focus, or a 25-second timeout leaves the existing selection unchanged.
Recording shortcut preferences temporarily pauses dictation gestures.

Use Ctrl, Alt, Shift or Meta with a letter, number, Space or navigation key, or
F1–F24 alone. Existing modifier-only presets and Right Alt remain available. The
main keyboard is supported; number-pad and punctuation keys are not currently
captured. Shortcuts use the active keyboard layout; a desktop-reserved shortcut
may need a different combination. Linux requests the selected binding through
its global-shortcut portal, so approve the desktop dialog if prompted. Existing
preset portal IDs are preserved, while each custom combination gets its own ID.

The validated shortcut name is also used by the Windows keyboard hook. Native
adapters feed the existing common hold/tap/double-tap recognizer.

Shortcut selection blocks documented reservations and confirmed global conflicts.
The catalog is bundled and tagged by platform. KDE uses its live shortcut registry;
GNOME reads configured GSettings bindings, including custom shortcuts. Blocked
presets are omitted, and the native save command validates independently of
the UI. An unavailable native check is shown as unverified, while catalog rules
remain enforced. See [the shortcut policy](../docs/hotkey-policy.md) for source
references, coverage, native checks and read-only diagnostics.
