# macOS desktop port

The desktop client shares the settings frontend, Rust gesture recognizer, account/API code, microphone processing, transcription routing and meeting storage with Windows/Linux. Native macOS integration lives in `src-tauri/src/macos_desktop.rs`, `macos_audio.rs` and `src-tauri/native/macos.m`.

## Development build

Use macOS with Xcode command-line tools, Rust and CMake. The workspace Cargo environment aligns native dependencies with macOS 13 and permits the older vendored Opus project to build with CMake 4.

```sh
cd windows-client
cargo test --locked
sh scripts/build-macos-development.sh
open target/debug/bundle/macos/SpeekTo.Me.app
```

The development script creates an **ad-hoc signed local test bundle**. It is not a Developer ID signed/notarized public release. Release packaging should use Tauri's macOS bundle workflow with an appropriate signing identity and notarization credentials. No signing credentials are committed.

The menu-bar app offers Open Settings, Language, Grant Desktop Access and Quit. Reopening the app opens Settings. Closing Settings leaves capture available. The floating preview is an AppKit NSPanel, not a WebView, and does not take keyboard focus. Settings uses the shared frontend with native macOS window controls and Command-key labels.

## Permissions

Use **Grant Desktop Access** in Settings or the menu-bar menu. macOS controls approval; the app does not alter the TCC database or bypass permission checks.

- Microphone: speech capture.
- Accessibility: insertion, selection copy, simulated typing and paste.
- Input Monitoring: global shortcut listening where required by macOS.
- Screen & System Audio Recording: requested when starting a meeting's ScreenCaptureKit system-audio capture.

The Settings status reflects actual grants and listener state. A missing system-audio grant prevents a meeting from silently starting with an empty system track. After a grant/revocation, the shortcut listener retries automatically. macOS may require an app restart for some recording permission changes. Rebuilding with an ad-hoc signature can require granting permissions again; a stable Developer ID identity is needed for reliable distribution.

Settings and recordings use `~/Library/Application Support/SpeekToMe`. The previous `~/.config/SpeekToMe/config.json` is migrated if the new configuration does not exist. The shared autostart preference uses SMAppService login items and may require approval in System Settings.

## Native smoke test

Run the actual packaged app:

```sh
open -W target/debug/bundle/macos/SpeekTo.Me.app --args --diagnose-macos
```

It displays a native multilingual preview, enumerates audio inputs, records real permission status and exports its own preview view. It does not upload audio, authenticate an account, post keyboard events or change permissions. Results are written beside the configuration as `macos-diagnostic.json` and `.png`. The app exits after the diagnostic.

The native shortcut adapter can also be checked independently of permissions, without posting input to the desktop:

```sh
cd src-tauri
xcrun clang -fobjc-arc -fblocks -mmacosx-version-min=13.0 native/tests/macos_shortcuts.m \
  -framework AppKit -framework ApplicationServices -framework AVFoundation \
  -framework Carbon -framework UserNotifications -framework ScreenCaptureKit \
  -framework CoreMedia -framework CoreAudio -framework AudioToolbox \
  -framework ServiceManagement -o /tmp/speektome-native-tests
/tmp/speektome-native-tests
```

The tests cover modifier-only edges, repeat suppression, cancellation on extra modifiers, fresh presses after release and filtering of the app's own synthesized events.

## Scope and remaining validation

The initial port adds native shortcut listening, Command+C/V, Unicode typing, password-field protection, native preview, menu-bar actions, permission recovery, single-instance locking, login-item integration and ScreenCaptureKit system audio. Some desktop APIs still pass through the historical `win32` facade; the macOS adapter implements the relevant operations without linking Windows code.

A native build/smoke test is not a complete dictation or meeting acceptance test. Validate the following with permissions granted and a signed-in account before calling the port production-ready:

- Microphone → transcription → insertion into native applications and browsers, including multilingual text.
- Hold/toggle/double-tap gestures and Command Mode selection/replacement in real applications.
- System-audio/microphone capture, echo cancellation, long meetings and interrupted-capture recovery.
- Permission denial/revocation, secure input, clipboard restoration, sleep/wake, audio-device changes and fullscreen/Spaces/multiple monitors.
- Login-item approval, notifications, package updates and permission persistence with a stable signing identity.
- Actual Apple Silicon runtime behavior; cross-compilation alone does not validate it.

The existing shared session-token persistence has not been migrated to the macOS Keychain in this first port. Public release signing/notarization and distribution updating remain separate release work.

## Tahoe validation, 19 October 2026

Verified over `ssh macos` on macOS Tahoe 26.7.1 / Intel x86_64:

- Full local Rust suite: 52 passing tests.
- Native Objective-C shortcut adapter tests pass, including safe background reads of the main-thread layout cache.
- Intel and Apple Silicon Mach-O binaries both build on the VM. Apple Silicon execution was not tested on this Intel VM.
- The local development app bundle passes `codesign --verify --strict`.
- Packaged-app diagnostic confirms a visible AppKit NSPanel/S2MPanelView, multilingual text, no focus capture, BH900 PRO microphone enumeration, and successful main-thread shortcut checking initiated from a background task.
- Normal application launch reaches Settings and synchronizes the language catalog/configuration from the server. A second binary invocation is rejected by the single-instance lock.
- `Command + A` is rejected as Select All; `Ctrl + Alt + K` passes the native availability check.
- A Tahoe runtime crash in the pre-existing shortcut checker was reproduced and fixed: Carbon keyboard-layout APIs now run on the main thread; the event-tap callback uses a cached layout map.

The equivalent Linux Rust suite passes 56 tests, and the shared frontend settings/onboarding tests pass for Windows, Linux and macOS. Windows compilation/runtime was not exercised during this port. Actual microphone transcription/insertion and ScreenCaptureKit recording still require user-approved permissions and a signed-in account; the smoke-test results do not claim those end-to-end flows passed.
