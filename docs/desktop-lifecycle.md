# Desktop Settings lifecycle and native status UI

Closing or hiding force-destroys Settings and its webview. Minimizing keeps Settings and its webview in memory. Switching to another application while Settings remains visible keeps the form open. The tray, keyboard shortcuts,
audio capture and durable meeting upload run in Rust and remain available.
Settings is recreated on demand with its last in-session geometry.

Explicit Quit is different from closing Settings. Linux and Windows tray actions
use the shared `lifecycle::quit` path. Programmatic Tauri exit requests are allowed;
only implicit window-close exits are prevented. A 250 ms watchdog exits the
process even if GTK, an audio device or pending network work fails to shut down.
Audio finalization is attempted off the UI thread within that deadline. A forced
Quit can interrupt the tail of an active recording; saved meeting recovery is
unchanged. Quit never waits for uploads or inference requests.

The Linux pill and preview are native GTK/Cairo/Pango surfaces drawn from Rust.
They use the configured colors, a 62 x 24 pill, five animated bars, Unicode text
with content-driven height, and the existing positioning/layer-shell rules.
Processing uses the original Windows violet/pink/amber/cyan palette on an animated
border and diffuse halo around both the pill and preview. There is no fixed
four-line/104-pixel preview cap: text is measured in full and the surface grows
to the available display bounds. Custom anchors retain their pill position.
Hidden overlays stop animation and discard cached text; expanded surfaces shrink.
There is no HTML or JavaScript renderer for the overlay. Its drawing timer runs
only while visible. Windows retains the existing native Win32/GDI renderer.

Tauri 2.12 intentionally retains WebKit contexts on Linux because of the upstream
[WebKit network-helper lifecycle issue](https://github.com/tauri-apps/tauri/issues/14626).
After Settings destruction, a short main-thread callback verifies that no webview
has reopened and terminates only this application's direct WebKit helper children.
It also trims the idle glibc heap. Other applications' browsers and native audio
workers are untouched. Reopening Settings recreates its browser helpers.
The pinned Linux Tauri dependency enables its native WindowBuilder API.

The packaged keyboard helper re-arms its parent-death signal after dropping root;
Linux clears that signal during the UID transition. Parent identity is checked
across the transition so a helper cannot outlive a parent that exits meanwhile.

## Local verification (2026-10-17)

- Native pill plus English/Hindi preview displayed with zero WebKit subprocesses.
- Five Settings open/close cycles left zero WebKit subprocesses after each close.
- Tray Quit with Settings closed and open took approximately 102 ms and 99 ms
  respectively, including D-Bus test dispatch. An additional check with an
  authorized keyboard helper confirmed both parent and helper gone in 374 ms.
  Final Wayland checks took 120–122 ms with all observed children released.
- Fresh normal tray host: about 40 MiB PSS / 76 MiB RSS; after five Settings cycles:
  about 90 MiB PSS. Browser-helper memory was zero in both tray states. These are
  local samples, not a sub-10-MiB guarantee: GTK/WebKit libraries and native host
  caches still contribute to the parent process after Settings has been used.
  RSS counts shared library pages in each process; the memory probe reports PSS
  as well as RSS so the two are not confused.
- Rust tests (excluding the test that writes the real clipboard), release Linux
  build, Windows cross-check, and strict C helper compilation passed.

A bounded rendering diagnostic uses the production native renderer, exits after
seven seconds, and does not start capture, portals, uploads or server calls. Quit
the regular instance before running it:

```
speektome --diagnose-native-overlay
python3 windows-client/tests/linux-memory-probe.py <PID>
```

No main-server, inference-worker or S3 launch-config changes are needed.

## Settings teardown on macOS and Linux

`settings_lifecycle.rs` owns the production close/hide policy and geometry retention. Native macOS application Hide and GTK hide notifications use the same teardown. Native minimize events preserve Settings. Pending GTK callbacks carry a window generation so an old window cannot close a freshly reopened Settings window.

Minimizing uses the operating system's minimize action on every platform. No minimize-state detection or focus-loss dismissal is needed to enforce teardown. Switching applications preserves the visible form.

macOS Settings uses a disposable nonpersistent website-data store; durable settings and authentication remain in Rust. Teardown stops loading, clears native delegates and responder references, detaches the webview/content hierarchy, and advances the AppKit event/autorelease cycle. Weak native tracking verifies actual WKWebView, data-store and process-pool deallocation. Cleanup only proceeds with no live Settings webview. It reads this application's own `launchctl print pid/<host-pid>` service namespace, validates active WebKit service PIDs and their system WebKit executable paths, rechecks membership and signals those same-user helpers. It does not use root privileges, private WebKit APIs or global kill-by-name commands. A freshly reopened Settings window cancels idle cleanup. Because native handles may be released after the Destroyed callback returns, cleanup retries at 350 ms intervals (up to 20 attempts) and advances subsequent AppKit autorelease turns while no Settings window is open; it never kills helpers while a live webview exists. Idle malloc pressure relief returns unused native heap pages.

The bounded `--diagnose-settings-lifecycle` mode uses the actual Settings frontend, production invoke handlers and teardown. It checks repeated close, Hide, retained Settings after app/native minimize, macOS application Hide, and rapid reopen during the old window's delayed cleanup. It records webview counts, successful configuration IPC reads, retained account state, native object counts and macOS RSS/physical footprint in `settings-lifecycle-diagnostic.json` beside the app configuration. It performs normal Settings reads but does not record audio, start uploads or alter permissions. Native capture is not started during the diagnostic.

Browser teardown and the native host's remaining RAM are separate measurements. Destroying Settings does not unload every shared framework/code page from the operating-system process. The Windows under-10-MB idle benchmark must not be claimed for macOS/Linux without comparable release-build measurements.

Previous teardown-policy validation on 2026-10-19 (before minimize was changed to retain Settings): seven macOS Settings cycles (including application Hide, native minimize and rapid reopen) ended with zero tracked WKWebView/data-store/process-pool objects and zero owned WebKit helpers. The Rust host and account state survived; the normal Tahoe client was restarted with keyboard permissions intact. The final debug host sample was about 25.2 MiB physical footprint / 69.5 MiB RSS, so the Windows under-10-MB benchmark is not yet achieved. Linux Wayland close/hide/app-minimize and rapid reopen passed with no final WebKit helpers. Rust tests passed on Linux (60) and macOS (56); Intel/ARM macOS builds, Windows GNU cross-check and native shortcut tests passed. Windows runtime behavior was not exercised.

Updated minimize-policy validation: the Tahoe lifecycle diagnostic retained one live Settings webview after both app and native minimize, then released it on explicit dismissal. Close/Hide and rapid reopen checks passed. Linux and Windows GNU compile checks passed; the updated normal Tahoe client was restarted.

Revalidated from master on 2026-10-20: a one-shot cleanup occasionally ran before delayed native release completed. Bounded idle retries now cover that timing. Two consecutive Tahoe lifecycle runs passed all 18 snapshots each, retaining exactly one live view while minimized and no views/data stores/process pools after Close/Hide. Independently sampled owned WebKit helpers were absent in final idle. Native preview/permission checks, 56 macOS Rust tests, Intel/ARM builds, native shortcut tests and development-signing identity checks passed. Linux passed 60 Rust tests and lifecycle checks; Windows GNU compile and shared Settings parity checks passed. The normal Linux and Tahoe clients were left running with account state intact. No live speech/transcription or ARM/Windows runtime test was performed.
