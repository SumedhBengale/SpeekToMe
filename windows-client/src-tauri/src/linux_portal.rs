//! Desktop portal integration for the Linux client. The portal owns shortcuts and
//! keyboard injection on Wayland; neither XWayland grabs nor /dev/uinput are used.

use ashpd::desktop::global_shortcuts::{BindShortcutsOptions, GlobalShortcuts, NewShortcut};
use ashpd::desktop::remote_desktop::{
    DeviceType, KeyState, NotifyKeyboardKeysymOptions, RemoteDesktop, SelectDevicesOptions,
    StartOptions,
};
use ashpd::desktop::PersistMode;
use futures_util::StreamExt;
use serde::Serialize;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{mpsc, OnceLock};
use std::time::{Duration, Instant};
use tokio::sync::mpsc::{unbounded_channel, UnboundedSender};

static INPUT_TX: std::sync::Mutex<Option<UnboundedSender<InputRequest>>> =
    std::sync::Mutex::new(None);
static REBIND_TX: OnceLock<UnboundedSender<()>> = OnceLock::new();
static SHORTCUT_READY: AtomicBool = AtomicBool::new(false);
static INSERTION_READY: AtomicBool = AtomicBool::new(false);
static SHORTCUT_LABEL: std::sync::Mutex<Option<String>> = std::sync::Mutex::new(None);
static LAST_RELEASE: std::sync::Mutex<Option<Instant>> = std::sync::Mutex::new(None);
// ashpd shares one session-bus connection for the life of the process. Rebinds
// restart `run()`, but registering that same connection a second time is
// rejected by the host portal as already associated with an app ID.
static HOST_APP_REGISTERED: AtomicBool = AtomicBool::new(false);

struct InputRequest {
    keys: Vec<i32>,
    reply: mpsc::Sender<Result<(), String>>,
}

#[derive(Serialize)]
pub struct LinuxCapabilities {
    pub shortcut: bool,
    pub insertion: bool,
    pub detail: String,
}

pub fn capabilities() -> LinuxCapabilities {
    let shortcut = SHORTCUT_READY.load(Ordering::SeqCst);
    let insertion = INSERTION_READY.load(Ordering::SeqCst);
    let label = SHORTCUT_LABEL.lock().unwrap_or_else(|e| e.into_inner()).clone();
    LinuxCapabilities {
        shortcut,
        insertion,
        detail: if shortcut && insertion {
            format!("{} and keyboard access granted", label.unwrap_or_else(|| "Shortcut".into()))
        } else if !shortcut {
            "Grant the global shortcut in your desktop permission dialog".into()
        } else {
            "Grant keyboard control in your desktop permission dialog".into()
        },
    }
}

pub fn supported_hotkey(name: &str) -> &'static str {
    // XDG Shortcuts syntax is MODIFIER+keysym (for example CTRL+space), not
    // GTK's <Ctrl>space accelerator syntax. Modifier combinations use the
    // last pressed modifier as the keysym.
    match name {
        "Ctrl + Win" => "CTRL+Super_L",
        "Alt + Space" => "ALT+space",
        "Ctrl + Space" => "CTRL+space",
        "Right Alt" => "Alt_R",
        "F9" => "F9",
        "F12" => "F12",
        "Ctrl + Shift + Z" => "CTRL+SHIFT+z",
        "Ctrl + Shift" => "CTRL+Shift_L",
        "Ctrl + Alt" => "CTRL+Alt_L",
        "Shift + Space" => "SHIFT+space",
        _ => "F8",
    }
}

fn shortcut_id(name: &str) -> &'static str {
    // The portal remembers bindings by app ID and shortcut ID. Reusing one ID
    // for every settings choice makes it keep the first trigger (for example,
    // F8) even after the user selects Ctrl+Meta.
    match name {
        "Ctrl + Win" => "dictate-ctrl-super-v2",
        "Alt + Space" => "dictate-alt-space",
        "Ctrl + Space" => "dictate-ctrl-space",
        "Right Alt" => "dictate-right-alt",
        "Ctrl + Shift + Z" => "dictate-ctrl-shift-z",
        "Ctrl + Shift" => "dictate-ctrl-shift",
        "Ctrl + Alt" => "dictate-ctrl-alt",
        "Shift + Space" => "dictate-shift-space",
        "F9" => "dictate-f9",
        "F12" => "dictate-f12",
        _ => "dictate", // Preserve an existing F8 portal grant.
    }
}

pub fn start() {
    std::thread::spawn(|| {
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .expect("Linux portal runtime");
        let (rebind_tx, mut rebind_rx) = unbounded_channel();
        let _ = REBIND_TX.set(rebind_tx);
        loop {
            if let Err(err) = runtime.block_on(run(&mut rebind_rx)) {
                SHORTCUT_READY.store(false, Ordering::SeqCst);
                INSERTION_READY.store(false, Ordering::SeqCst);
                *SHORTCUT_LABEL.lock().unwrap_or_else(|e| e.into_inner()) = None;
                *INPUT_TX.lock().unwrap_or_else(|e| e.into_inner()) = None;
                eprintln!("[LINUX PORTAL] {err}");
                crate::win32::show_tray_notification(
                    "SpeekTo.Me needs desktop access",
                    &err,
                    crate::win32::NotificationLevel::Error,
                );
                if rebind_rx.blocking_recv().is_none() {
                    break;
                }
            }
        }
    });
}

pub fn rebind() {
    if let Some(tx) = REBIND_TX.get() {
        let _ = tx.send(());
    }
}

async fn run(rebind_rx: &mut tokio::sync::mpsc::UnboundedReceiver<()>) -> Result<(), String> {
    // Recent portal versions require an explicit identity for unsandboxed apps.
    // The same ID is used by the Linux desktop entry generated by Tauri.
    if !ashpd::is_sandboxed() {
        crate::linux_util::ensure_portal_desktop_entry()
            .map_err(|e| format!("Could not install the desktop portal launcher: {e}"))?;
    }
    let app_id: ashpd::AppID = crate::linux_util::APP_ID
        .parse()
        .map_err(|e: ashpd::Error| e.to_string())?;
    if !HOST_APP_REGISTERED.load(Ordering::SeqCst) {
        let mut registration_error = None;
        for attempt in 0..8 {
            match ashpd::register_host_app(app_id.clone()).await {
                Ok(()) => {
                    registration_error = None;
                    HOST_APP_REGISTERED.store(true, Ordering::SeqCst);
                    break;
                }
                Err(err) if err.to_string().contains("App info not found") && attempt < 7 => {
                    registration_error = Some(err);
                    // GIO notices a freshly written desktop entry asynchronously.
                    tokio::time::sleep(Duration::from_millis(350)).await;
                }
                Err(err) => {
                    registration_error = Some(err);
                    break;
                }
            }
        }
        if let Some(err) = registration_error {
            return Err(format!(
                "Could not register the Linux app with the desktop portal: {err}"
            ));
        }
    }
    eprintln!("[LINUX PORTAL] App identity registered for this D-Bus connection");
    let shortcuts = GlobalShortcuts::new().await.map_err(|e| e.to_string())?;
    let session = shortcuts
        .create_session(Default::default())
        .await
        .map_err(|e| e.to_string())?;
    let (configured_hotkey, hotkey_mode) = {
        let config = crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        (config.hotkey.clone(), config.hotkey_mode.clone())
    };
    let helper_chord = crate::linux_keywatch::chord_for(&configured_hotkey, &hotkey_mode);
    let using_helper = helper_chord.is_some();
    let hotkey = supported_hotkey(&configured_hotkey);
    let id = shortcut_id(&configured_hotkey);
    let activated = shortcuts
        .receive_activated()
        .await
        .map_err(|e| e.to_string())?;
    let deactivated = shortcuts
        .receive_deactivated()
        .await
        .map_err(|e| e.to_string())?;
    let (_unused_tx, mut keywatch_events) = unbounded_channel::<crate::linux_keywatch::Event>();
    let mut _keywatch_guard = None;
    if let Some(chord) = helper_chord {
        eprintln!("[LINUX KEYWATCH] Waiting for authorization to monitor {configured_hotkey}");
        let (guard, mut events) = crate::linux_keywatch::start(chord)?;
        match tokio::time::timeout(Duration::from_secs(90), events.recv()).await {
            Ok(Some(crate::linux_keywatch::Event::Ready)) => {
                eprintln!("[LINUX KEYWATCH] Modifier shortcut ready: {configured_hotkey}");
            }
            Ok(_) => return Err("Keyboard helper could not start. Check the desktop authorization prompt.".into()),
            Err(_) => return Err("Timed out waiting for keyboard helper authorization".into()),
        }
        keywatch_events = events;
        _keywatch_guard = Some(guard);
        *SHORTCUT_LABEL.lock().unwrap_or_else(|e| e.into_inner()) = Some(format!("{configured_hotkey} (keyboard helper)"));
    } else {
        let shortcut = NewShortcut::new(id, "Record or stop dictation").preferred_trigger(Some(hotkey));
        let result = shortcuts
            .bind_shortcuts(&session, &[shortcut], None, BindShortcutsOptions::default())
            .await
            .map_err(|e| e.to_string())?
            .response()
            .map_err(|e| e.to_string())?;
        let bound = result.shortcuts().iter().find(|shortcut| shortcut.id() == id)
            .ok_or("No global shortcut was granted")?;
        if bound.trigger_description().trim().is_empty() {
            return Err(format!("The desktop did not assign a trigger for {configured_hotkey}. Choose a shortcut in your desktop permission dialog."));
        }
        eprintln!("[LINUX PORTAL] Global shortcut granted: {} ({})", bound.id(), bound.trigger_description());
        *SHORTCUT_LABEL.lock().unwrap_or_else(|e| e.into_inner()) = Some(bound.trigger_description().to_owned());
    }
    SHORTCUT_READY.store(true, Ordering::SeqCst);

    let desktop = RemoteDesktop::new().await.map_err(|e| e.to_string())?;
    let input_session = desktop
        .create_session(Default::default())
        .await
        .map_err(|e| e.to_string())?;
    let restore_path = crate::config::get_config_path().with_file_name("remote_desktop_token");
    let restore_token = std::fs::read_to_string(&restore_path).ok();
    desktop
        .select_devices(
            &input_session,
            SelectDevicesOptions::default()
                .set_devices(Some(DeviceType::Keyboard.into()))
                .set_persist_mode(PersistMode::ExplicitlyRevoked)
                .set_restore_token(restore_token.as_deref()),
        )
        .await
        .map_err(|e| e.to_string())?
        .response()
        .map_err(|e| e.to_string())?;
    eprintln!("[LINUX PORTAL] Keyboard requested; waiting for desktop approval");
    let granted = desktop
        .start(&input_session, None, StartOptions::default())
        .await
        .map_err(|e| e.to_string())?
        .response()
        .map_err(|e| e.to_string())?;
    if !granted.devices().contains(DeviceType::Keyboard) {
        return Err("Keyboard control was not granted".into());
    }
    eprintln!("[LINUX PORTAL] Keyboard control granted");
    if let Some(token) = granted.restore_token() {
        let _ = std::fs::write(&restore_path, token);
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&restore_path, std::fs::Permissions::from_mode(0o600));
    }
    let (tx, mut rx) = unbounded_channel::<InputRequest>();
    *INPUT_TX.lock().unwrap_or_else(|e| e.into_inner()) = Some(tx);
    INSERTION_READY.store(true, Ordering::SeqCst);
    let mut activated = Box::pin(activated);
    let mut deactivated = Box::pin(deactivated);
    let mut pending_release: Option<Instant> = None;

    loop {
        tokio::select! {
            Some(event) = activated.next(), if !using_helper => {
                if event.shortcut_id() == id {
                    eprintln!("[LINUX PORTAL] activated {}", id);
                    on_shortcut(true);
                    // Some compositors deliver the two portal signal streams
                    // together. If deactivation was polled first, apply it
                    // after activation instead of leaving hold mode running.
                    if pending_release.take().is_some_and(|at| at.elapsed() < Duration::from_millis(250)) {
                        on_shortcut(false);
                    }
                }
            }
            Some(event) = deactivated.next(), if !using_helper => {
                if event.shortcut_id() == id {
                    eprintln!("[LINUX PORTAL] deactivated {}", id);
                    let toggle = crate::ACTIVE_HOTKEY_MODE.load(Ordering::SeqCst) == 1;
                    let active = crate::get_state().lock().unwrap_or_else(|e| e.into_inner()).is_key_held;
                    if !toggle && !active {
                        pending_release = Some(Instant::now());
                    } else {
                        on_shortcut(false);
                    }
                }
            }
            event = keywatch_events.recv(), if using_helper => {
                match event {
                    Some(crate::linux_keywatch::Event::Press) => {
                        eprintln!("[LINUX KEYWATCH] pressed");
                        on_shortcut(true);
                    }
                    Some(crate::linux_keywatch::Event::Release) => {
                        eprintln!("[LINUX KEYWATCH] released");
                        on_shortcut(false);
                    }
                    _ => {
                        abort_active_shortcut();
                        return Err("Keyboard helper stopped unexpectedly".into());
                    }
                }
            }
            Some(request) = rx.recv() => {
                let mut outcome = Ok(());
                for pair in request.keys.chunks_exact(2) {
                    if let Err(err) = desktop.notify_keyboard_keysym(
                        &input_session, pair[0],
                        if pair[1] == 1 { KeyState::Pressed } else { KeyState::Released },
                        NotifyKeyboardKeysymOptions::default(),
                    ).await {
                        outcome = Err(err.to_string());
                        break;
                    }
                }
                let _ = request.reply.send(outcome);
            }
            Some(()) = rebind_rx.recv() => break,
            else => break,
        }
    }
    abort_active_shortcut();
    SHORTCUT_READY.store(false, Ordering::SeqCst);
    INSERTION_READY.store(false, Ordering::SeqCst);
    *SHORTCUT_LABEL.lock().unwrap_or_else(|e| e.into_inner()) = None;
    *INPUT_TX.lock().unwrap_or_else(|e| e.into_inner()) = None;
    Ok(())
}

fn abort_active_shortcut() {
    let was_active = {
        let mut state = crate::get_state().lock().unwrap_or_else(|e| e.into_inner());
        if !state.is_key_held {
            false
        } else {
            state.is_key_held = false;
            state.press_time = None;
            if let Some(stop) = state.stop_tx.take() {
                let _ = stop.send(false);
            }
            true
        }
    };
    if was_active {
        crate::win32::hide_overlay();
    }
}

/// Pairs are XKB keysyms and press/release state (1/0).
pub fn send_keys(keys: Vec<i32>) -> Result<(), String> {
    let tx = INPUT_TX
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .clone()
        .ok_or("Keyboard permission is unavailable")?;
    let (reply, rx) = mpsc::channel();
    tx.send(InputRequest { keys, reply })
        .map_err(|e| e.to_string())?;
    rx.recv_timeout(Duration::from_secs(10))
        .map_err(|e| e.to_string())?
}

fn on_shortcut(pressed: bool) {
    if crate::ACTIVE_MEETING
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .is_some()
    {
        return;
    }
    let toggle = crate::ACTIVE_HOTKEY_MODE.load(Ordering::SeqCst) == 1;
    let mut state = crate::get_state().lock().unwrap_or_else(|e| e.into_inner());
    let now = Instant::now();
    eprintln!("[LINUX SHORTCUT] {} mode={} active={}", if pressed { "pressed" } else { "released" }, if toggle { "toggle" } else { "hold" }, state.is_key_held);
    if pressed {
        if state.is_key_held {
            if !toggle {
                return;
            }
            let is_command_double_press = LAST_RELEASE
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .take()
                .is_some_and(|last| now.duration_since(last) <= Duration::from_millis(1000));
            if is_command_double_press
                && !state.is_command_mode
                && crate::ACTIVE_COMMAND_MODE.load(Ordering::SeqCst)
            {
                state.is_command_mode = true;
                state.press_time = Some(now);
                crate::win32::set_preview_text("✨ Command Mode (Listening...)");
                std::thread::spawn(|| {
                    let selected = crate::keyboard::get_selected_text();
                    crate::get_state()
                        .lock()
                        .unwrap_or_else(|e| e.into_inner())
                        .selected_text = selected;
                });
                return;
            }
            state.is_key_held = false;
            state.press_time = None;
            if let Some(stop) = state.stop_tx.take() {
                let _ = stop.send(true);
            }
            crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
            return;
        }
        LAST_RELEASE.lock().unwrap_or_else(|e| e.into_inner()).take();
        state.is_key_held = true;
        state.press_time = Some(now);
        state.is_command_mode = false;
        state.selected_text = None;
        crate::win32::set_preview_text("");
        crate::win32::set_preview_state(crate::win32::PreviewState::Listening);
        crate::win32::show_overlay();
        crate::win32::play_chime(true);
        let (tx, rx) = mpsc::channel();
        state.stop_tx = Some(tx);
        std::thread::spawn(move || crate::audio::recording_thread(rx));
    } else if toggle && state.is_key_held {
        let elapsed = state.press_time.map(|t| t.elapsed()).unwrap_or_default();
        if elapsed >= Duration::from_millis(350) {
            LAST_RELEASE.lock().unwrap_or_else(|e| e.into_inner()).take();
            state.is_key_held = false;
            state.press_time = None;
            crate::win32::set_preview_text("⚠ Hands-Free is selected; Push-to-Talk gesture detected");
            if let Some(stop) = state.stop_tx.take() {
                let _ = stop.send(true);
            }
            crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
        } else {
            *LAST_RELEASE.lock().unwrap_or_else(|e| e.into_inner()) = Some(Instant::now());
        }
    } else if !toggle && state.is_key_held {
        let elapsed = state.press_time.map(|t| t.elapsed()).unwrap_or_default();
        state.is_key_held = false;
        state.press_time = None;
        if !state.is_command_mode {
            *LAST_RELEASE.lock().unwrap_or_else(|e| e.into_inner()) = Some(Instant::now());
        }
        let short_tap = elapsed < Duration::from_millis(350)
            && !state.is_command_mode
            && crate::ACTIVE_COMMAND_MODE.load(Ordering::SeqCst);
        if let Some(stop) = state.stop_tx.take() {
            let _ = stop.send(!short_tap);
        }
        if short_tap {
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
        } else {
            crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
        }
    }
}
