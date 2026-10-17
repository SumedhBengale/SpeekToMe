# Desktop tray lifecycle and native Linux status UI

Closing or hiding Settings destroys its webview. The tray, keyboard shortcuts,
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
with four-line truncation, and the existing positioning/layer-shell rules.
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
