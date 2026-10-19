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

The menu-bar app offers Open Settings, Language, Grant Desktop Access and Quit. Reopening the app opens Settings. Closing Settings leaves capture available. The floating overlay uses the shared Rust native design: a 62×24 waveform pill with a separate adjacent text card, on a transparent 420×140 surface. `native_overlay.rs` owns waveform animation, layout and COLORREF/BGR theme interpretation. Windows uses the shared waveform calculation in its Win32 compositor; Linux uses the shared bars/card layout through Cairo/Pango; macOS draws it in Rust through CoreGraphics/CoreText (`macos_overlay.rs`). The small AppKit bridge only hosts the nonactivating NSPanel, schedules redraws while visible and supplies the native graphics context. No overlay creates a WebView or takes keyboard focus. Settings uses the shared frontend with native macOS window controls and Command-key labels.

## Permissions

Use **Grant Desktop Access** in Settings or the menu-bar menu. macOS controls approval; the app does not alter the TCC database or bypass permission checks.

- Microphone: speech capture.
- Accessibility: insertion, selection copy, simulated typing and paste.
- Input Monitoring: global shortcut listening where required by macOS.
- Screen & System Audio Recording: requested when starting a meeting's ScreenCaptureKit system-audio capture.

The Settings status reflects actual grants and listener state. A missing system-audio grant prevents a meeting from silently starting with an empty system track. After a grant/revocation, the shortcut listener retries automatically. macOS may require an app restart for some recording permission changes. The local development signing helper embeds a stable identifier requirement so a rebuild does not identify itself solely by its changing executable hash. This identifier-only ad-hoc requirement does not authenticate a publisher and is for local testing only. Public distribution must use a stable Developer ID identity and notarization.

When upgrading from the original hash-identified development build, macOS retains an obsolete grant even if its switch is enabled. Remove the old SpeekTo.Me entry and add the current app bundle again under Accessibility (and Input Monitoring if an old entry exists). This one-time approval must be performed by the user; neither the app nor build scripts modify TCC records. Grant Desktop Access opens the missing permission pane. Settings polls actual permission/listener status, including Input Monitoring, and the app logs listener readiness and matched shortcut edges without logging other keys or transcript content.

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

## Shortcut permission follow-up, 19 October 2026

The reported missing hotkey pill was traced to a stale development signature grant: the system Accessibility record accepted an earlier executable hash, while the running app had a different hash and reported Accessibility/Input Monitoring unavailable even after a restart. The packaged diagnostic still rendered a visible native preview. The development signing helper now preserves the designated requirement between rebuilds; `python3 tests/macos-development-signing.py` verifies that two different executable hashes both satisfy the first build's requirement without changing any permission records. This check also runs in macOS CI. Native shortcut tests cover the reported Ctrl+Shift hold/release combination. A subsequent user approval only updated the old Accessibility record's timestamp, leaving its original hash requirement unchanged. The supported `tccutil reset Accessibility me.speekto.SpeekToMe` command successfully removed that obsolete record. Microphone approval already stored the new stable identifier requirement, confirming that fresh grants pick up the development identity.

Settings now offers **Repair saved permissions** when keyboard access is unavailable. This explicit action invokes the supported `/usr/bin/tccutil reset` command for this bundle's denied Accessibility/Input Monitoring services only, then requests fresh approval. It preserves existing valid keyboard grants, Microphone and other services. It neither edits TCC databases nor grants permissions. Input Monitoring is marked optional and does not keep Grant Desktop Access visible when the real listener, insertion and microphone are ready through Accessibility. Live global shortcut capture still requires fresh approval on the VM.

## Shared Rust overlay validation, 19 October 2026

The initial macOS text-only panel was replaced by the existing pill/waveform and adjacent preview design. macOS does not render text through NSTextField or draw its design in Objective-C. Rust owns drawing and caches CoreText shaping for multilingual text; hidden overlays stop their redraw timer and release cached text/frame objects. Linux now interprets saved BGR colors in the same order as Windows, fixing swapped red/blue channels.

The packaged Tahoe diagnostic exports `macos-overlay-pill.png`, `macos-overlay-preview.png` and `macos-overlay-processing.png`, plus per-state snapshots in `macos-diagnostic.json`. Actual output confirms a 62×24 five-bar pill, readable Hindi/Japanese/Arabic in the separate card, increasing draw-frame counts, zero WebView windows, no focus capture, and a hidden state with its redraw timer stopped. Microphone, Accessibility, Input Monitoring and the global shortcut listener all remained ready across the rebuild. The diagnostic does not capture or upload speech. Earlier normal-app logs also confirm user hotkey edges, capture and nonempty transcription on Tahoe; those are separate from this rendering diagnostic.

Shared geometry tests verify that the preview remains beside the pill and within the surface at each anchor, the waveform responds to audio and processing state, and theme colors follow Windows COLORREF ordering. Public release signing and physical multi-monitor/Spaces verification remain separate work.

Final checks for this rendering change: 55 macOS Rust tests and 59 Linux Rust tests pass; the Windows GNU target type-checks; Intel and Apple Silicon clients build on Tahoe (ARM execution remains untested on the Intel VM). The Linux production diagnostic was also run under Wayland in an isolated session socket/configuration, preserving the existing running client. Its native layer-shell surface reported `mapped=true width=420 height=140 pill=(179, 0)` and the captured desktop showed the pill and readable multilingual preview. Native Objective-C shortcut tests still pass. PNG pixel checks confirm exactly a 62×24 pill with five separate waveform bars and a transparent gap before the adjacent text card.
