# Shortcut reservation policy

The desktop rejects a proposed shortcut if it matches the bundled platform catalog
or a native check confirms an occupied global binding. Both the recorder and
`save_config` call the same Rust policy; saving checks again before changing any
preferences, atomics, autostart entries or existing portal bindings. Previously
saved conflicting shortcuts are disabled on startup and Settings opens with the
reason, rather than silently choosing a replacement.

## Catalog

`windows-client/src-tauri/shortcut-reservations.json` is embedded in the client.
Version 1 was reviewed on 2026-10-18. Each entry contains platform tags, a key
combination, an action label and a source identifier; identifiers resolve to
primary documentation URLs in the same file. Maintain the data when upstream
references change. Do not fetch or scrape websites when the app starts.

The catalog covers common editing/navigation/window/browser commands within the
recorder's supported key vocabulary. It is a conservative product reservation
policy, not a claim that every listed action is a registered OS global shortcut.
Apply only entries tagged for the current platform. Command/Option on macOS are
separate from Control; aliases normalize before matching. Matching uses the exact
modifier set, so Ctrl+A does not reject Ctrl+Alt+A.

Sources include Microsoft Windows shortcuts, Microsoft Edge shortcuts, Apple's
Mac shortcuts, GNOME's standard and legacy system shortcuts, and KDE standard
shortcut documentation. The Windows Ctrl+Shift+Z reservation deliberately
protects the shared GNOME/GTK redo convention used by cross-platform applications.
Punctuation, number-pad keys, Fn/Globe and unsupported multi-key sequences are
outside this recorder's current vocabulary and are not represented as candidates.

## Native checks

- **KDE:** Query the running `org.kde.KGlobalAccel` service, including availability
  checks against shadowing sequences, and obtain the conflicting action and
  component when available. Exclude `me.speekto.SpeekToMe` ownership so the app's
  own remembered portal grants do not reject the current shortcut. No desktop
  shortcut configuration is rewritten and no global binding is stolen.
- **GNOME:** Read installed GNOME window-manager, shell, Mutter and media-key
  schemas through GSettings, including relocatable custom shortcut entries.
  Empty/disabled bindings do not count. This reads configured bindings; extension
  shortcuts or other compositor registries may not be exposed by these schemas.
- **Windows:** Temporarily attempt `RegisterHotKey` on a blocking task thread and
  immediately release successful registrations with a drop guard. Only
  `ERROR_HOTKEY_ALREADY_REGISTERED` proves occupancy; other errors are unknown.
  Modifier-only shortcuts cannot be checked with this API. F12 and documented
  OS shortcuts are covered by the catalog before registration is attempted.
- **macOS:** Read enabled symbolic system hotkeys with `CopySymbolicHotKeys`.
  Translate character keys using the current keyboard layout via `UCKeyTranslate`
  rather than assuming a US layout. The API does not enumerate all other apps'
  global registrations. The adapter and policy are present; native macOS runtime
  validation still requires a Mac, and this does not implement a complete macOS
  capture/overlay port of the existing Windows/Linux desktop client.

Native lookup is bounded to two seconds. If the lookup is unsupported, fails or
times out, the result is **unknown**, never “available.” The catalog continues to
block known reservations, and the UI explains the verification gap. Unknown
availability alone does not block a shortcut; unsupported Linux desktops would
otherwise have no usable selection. The desktop's binding permission dialog is
still required on Linux. System shortcuts may change after validation, so no
check can guarantee that a key will remain available indefinitely.

## UI and verification

Conflicting presets are omitted from the dropdown. Alt+Space and Ctrl+Shift+Z
are removed from the predefined options entirely; additional platform/system
conflicts are filtered when the dropdown opens. A previously saved blocked value
remains visible as the current selection with its rejection reason until replaced. Recording a conflicting custom shortcut shows
its reason and leaves the previous selection in place. Native save validation
also rejects conflicts if a caller bypasses the UI. Checks run at startup,
settings load and selection/save, not on a fixed refresh timer.

Read-only diagnostics, without loading preferences or claiming an app instance:

```sh
speektome --check-shortcut 'Ctrl + A'
speektome --check-shortcut 'Ctrl + F9'
speektome --check-shortcut 'Ctrl + Alt + K'
```

The JSON contains a report and an optional rejection reason. Local KDE validation
confirmed catalog refusal of Ctrl+A, live KWin occupancy of Ctrl+F9, and no reported
conflict for Ctrl+Alt+K. Tests validate catalog entries/source references/platform
isolation, shared recorder/save policy, Qt and GNOME encoding, and Carbon modifier
and special-key conversion. Windows is cross-compiled from Linux; Windows/GNOME/
macOS interactive runtime checks require those environments.
