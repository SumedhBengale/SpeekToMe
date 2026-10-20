# Desktop first-run setup

Windows, Linux, and macOS embed the same onboarding HTML, CSS, and controller.
The sequence is:

1. Sign in or create an account using the existing authentication flow.
2. Review Pro, upgrade in the browser, or continue with Free.
3. Grant desktop access and check the default microphone.
4. Choose a shortcut and Push to Talk or Hands-Free behavior.
5. Choose simulated typing or immediate insertion.
6. Choose Command Mode (Pro).
7. Choose real-time transcription (Pro).
8. Choose launch at sign-in.
9. Choose spoken languages and the active language.
10. Practice real dictation, or explicitly defer it.
11. Explain tray/menu-bar operation and finish.

Theme, chimes, and overlay appearance remain in Settings. Setup preserves them.
Real-time transcription is selected once for a Pro account during setup;
returning to the wizard or refreshing account status does not override a later
choice to turn it off. Free accounts retain batch transcription and cannot
select the paid controls. Entitlements refresh when returning from the browser.

## Progress and access

The saved config includes `onboarding_version`, `onboarding_step`, and
`onboarding_pro_defaults_applied`. Version zero means incomplete. Saving an
authentication token alone does not complete setup. Each navigation save reads
the latest native config so it preserves the current session and other fields.
Save failures leave the user on the current page and show a retry message.
Existing configs that have a session but no onboarding-version field migrate to
completed version one, preserving existing installations without forcing setup.

Forward navigation requires authentication. The permissions page additionally
requires working shortcut and insertion capabilities and a successful microphone
check. macOS reports native Microphone and Accessibility access plus shortcut
listener readiness; Input Monitoring may be required for the listener. Linux
reports its native helper/portal readiness and offers reconnect. Windows uses
its native hooks; microphone privacy failures surface through the input check.
Meeting/system-audio access is deferred until the meeting feature is used.

The microphone check is user-initiated and bounded to three seconds. Native
callbacks reduce samples to a peak level; they do not store audio or send it to
the network. A successful stream must deliver samples without errors. Quiet
input produces a volume warning. System default input selection stays in OS
settings. Actual practice dictation uses the normal native pill, insertion, and
transcription service, with an explanation of encrypted account history.
Entering text manually is not presented as proof of a successful transcription.

Canceling browser login invalidates the pending native attempt. It has bounded
listener and exchange timeouts; canceled or superseded attempts cannot apply
late success events. Normal Settings close/hide behavior and native overlays
are unchanged: the settings webview can be destroyed and setup resumes when
Settings is reopened.

## Validation

`windows-client/tests/onboarding.cjs` exercises synthetic Free and Pro accounts
for all three platform adapters: login gates, paid defaults, permission gating,
practice skip, progress restoration, completion, and preservation of settings
not exposed in setup. It uses jsdom and mocked native commands. The existing
settings-parity suite also checks the shared config round trip.

Rust tests cover legacy config migration, incomplete-account resume, and browser
login attempt cancellation. Browser screenshots use synthetic data and mocked
native commands. These checks do not replace live microphone, OS permission,
shortcut, browser callback, or purchase testing on each platform.

## Setup visual design

The permissions, shortcut, practice, and completion pages follow a common
four-screen reference generated from the existing screenshots. Permission rows
use compact icons and text states; the microphone only becomes ready after its
check. Shortcut gestures use keyboard-accessible radio cards backed by the
existing native config. Keycaps use the compiled platform's labels. Completion
separates shortcut, Settings location, and background behavior into short rows.
The shared Back/Continue/Finish footer stays outside the scrolling body, including
in smaller windows. Appearance changes do not add another webview or change
recording, authentication, permissions, or saved setup progress.
