//! Native macOS desktop adapter. Shared routing and gestures stay platform independent.
use std::ffi::{c_char, CString};
use std::sync::{
    atomic::{AtomicU8, Ordering},
    OnceLock,
};
use tauri::{
    menu::{CheckMenuItem, MenuBuilder, SubmenuBuilder},
    tray::TrayIconBuilder,
    Manager,
};

static STATE: AtomicU8 = AtomicU8::new(0);
unsafe extern "C" {
    fn s2m_permissions() -> u32;
    fn s2m_settings_observers(background: extern "C" fn());
    fn s2m_track_settings(view: *mut std::ffi::c_void);
    fn s2m_prepare_settings_close(view: *mut std::ffi::c_void);
    fn s2m_settings_snapshot() -> *mut c_char;
    fn s2m_browser_idle_cleanup();
    fn s2m_finish_settings_close();
    fn s2m_overlay_snapshot() -> *mut c_char;
    fn s2m_save_overlay_image(path: *const c_char) -> bool;
    fn s2m_free(value: *mut c_char);
    fn s2m_listener_ready() -> bool;
    fn s2m_request_permissions();
    fn s2m_request_notifications();
    fn s2m_set_shortcut(combo: u32);
    fn s2m_run_listener(edge: extern "C" fn(bool)) -> i32;
    fn s2m_chord(paste: bool) -> i32;
    fn s2m_type(text: *const u16, count: usize) -> i32;
    fn s2m_key(vk: u32, down: bool) -> i32;
    fn s2m_overlay(state: i32, text: *const c_char);
    fn s2m_overlay_style(position: i32, x: f64, y: f64);
    fn s2m_chime(start: bool);
    fn s2m_notify(title: *const c_char, body: *const c_char);
}
fn cstring(s: &str) -> CString {
    CString::new(s.replace('\0', "")).unwrap()
}

extern "C" fn settings_backgrounded() {
    let _ = std::panic::catch_unwind(|| {
        if let Some(app) = crate::TAURI_APP.get() {
            crate::settings_lifecycle::dismiss(app);
        }
    });
}

pub fn track_settings(window: &tauri::WebviewWindow) {
    let _ = window.with_webview(|view| unsafe { s2m_track_settings(view.inner()); });
}

pub fn prepare_settings_close(window: &tauri::WebviewWindow) {
    let _ = window.with_webview(|view| unsafe { s2m_prepare_settings_close(view.inner()); });
}

pub fn settings_snapshot() -> serde_json::Value {
    unsafe {
        let value = s2m_settings_snapshot();
        if value.is_null() { return serde_json::Value::Null; }
        let result = serde_json::from_str(&std::ffi::CStr::from_ptr(value).to_string_lossy()).unwrap_or_default();
        s2m_free(value);
        result
    }
}

pub fn release_idle_browser(app: &tauri::AppHandle) {
    unsafe { s2m_finish_settings_close(); }
    let app = app.clone();
    std::thread::spawn(move || {
        std::thread::sleep(std::time::Duration::from_millis(350));
        let services = crate::macos_browser::cached_services();
        let handle = app.clone();
        let _ = app.run_on_main_thread(move || {
            let snapshot = settings_snapshot();
            if handle.webview_windows().is_empty() && snapshot["live_webviews"] == 0 {
                let stopped = crate::macos_browser::terminate_cached(&services);
                unsafe { s2m_browser_idle_cleanup(); }
                eprintln!("[WINDOW] macOS Settings released; stopped {stopped} cached browser helpers: {}", settings_snapshot());
            }
        });
    });
}

pub fn capabilities() -> serde_json::Value {
    let bits = unsafe { s2m_permissions() };
    serde_json::json!({
        "microphone": bits & 1 != 0,
        "shortcut": unsafe { s2m_listener_ready() } && bits & (2 | 4) != 0,
        "insertion": bits & 2 != 0,
        "input_monitoring": bits & 4 != 0,
        "system_audio": bits & 8 != 0,
        "detail": "Allow Microphone for speech, Accessibility for text insertion, and Input Monitoring for shortcuts. System audio access is requested when a meeting starts."
    })
}
pub fn request_permissions() {
    unsafe {
        s2m_request_permissions();
        s2m_request_notifications();
    }
}
/// Explicit Settings repair action. Reset only denied keyboard services for our
/// bundle, through Apple's supported utility. Approval still belongs to the user.
pub fn repair_permissions() -> Result<(), String> {
    let bits = unsafe { s2m_permissions() };
    for (service, granted) in [("Accessibility", bits & 2 != 0), ("ListenEvent", bits & 4 != 0)] {
        if granted { continue; }
        let output = std::process::Command::new("/usr/bin/tccutil")
            .args(["reset", service, "me.speekto.SpeekToMe"])
            .output().map_err(|error| format!("Could not reset {service}: {error}"))?;
        if !output.status.success() {
            return Err(format!("Could not reset {service}: {}", String::from_utf8_lossy(&output.stderr).trim()));
        }
        eprintln!("[MACOS PERMISSIONS] Reset denied {service} record; fresh user approval required.");
    }
    request_permissions();
    Ok(())
}
extern "C" fn shortcut_edge(pressed: bool) {
    let _ = std::panic::catch_unwind(|| {
        if !crate::SIMULATING_KEY.load(Ordering::SeqCst) {
            eprintln!("[MACOS SHORTCUT] {} (signed_in={}, allowed={})",
                if pressed { "pressed" } else { "released" },
                crate::ACTIVE_HAS_SESSION_TOKEN.load(Ordering::SeqCst),
                crate::HOTKEY_ALLOWED.load(Ordering::SeqCst));
            crate::shortcuts::on_edge(pressed);
        }
    });
}
pub fn sync_settings(config: &crate::config::AppConfig) {
    unsafe {
        s2m_set_shortcut(crate::config::parse_hotkey_combo(&config.hotkey));
    }
    let position = if config.pill_position_mode == "custom" {
        4
    } else {
        crate::native_overlay::position_code(&config.pill_predefined_position)
    };
    unsafe {
        s2m_overlay_style(
            position,
            config.pill_custom_x as f64,
            config.pill_custom_y as f64,
        );
    }
}
fn menu(app: &tauri::AppHandle) -> tauri::Result<tauri::menu::Menu<tauri::Wry>> {
    let active = crate::speaking_language();
    let mut languages = SubmenuBuilder::new(app, "Language");
    for (code, name) in crate::speaking_language_options() {
        languages = languages.item(&CheckMenuItem::with_id(
            app,
            format!("language:{code}"),
            name,
            true,
            code == active,
            None::<&str>,
        )?);
    }
    MenuBuilder::new(app)
        .item(&languages.build()?)
        .separator()
        .text("settings", "Open Settings")
        .text("permissions", "Grant Desktop Access…")
        .separator()
        .text("quit", "Quit SpeekTo.Me")
        .build()
}
fn menu_event(app: &tauri::AppHandle, id: &str) {
    match id {
        "settings" => crate::show_or_create_settings_window(app),
        "permissions" => {
            request_permissions();
            crate::show_or_create_settings_window(app);
        }
        "quit" => crate::lifecycle::quit(app),
        id => {
            if let Some(code) = id.strip_prefix("language:") {
                let _ = crate::set_active_language(code.to_string());
            }
        }
    }
}
pub fn refresh_language_menu(app: &tauri::AppHandle) {
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        if let Some(tray) = handle.tray_by_id("main-tray") {
            if let Ok(menu) = menu(&handle) {
                let _ = tray.set_menu(Some(menu));
            }
        }
    });
}
pub fn setup(app: &tauri::AppHandle) -> tauri::Result<()> {
    app.set_activation_policy(tauri::ActivationPolicy::Accessory)?;
    crate::macos_overlay::install();
    unsafe { s2m_settings_observers(settings_backgrounded); }
    let mut tray = TrayIconBuilder::with_id("main-tray")
        .menu(&menu(app)?)
        .tooltip("SpeekTo.Me");
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;
    // Accessory apps still need an explicit application menu and Cmd+Q.
    let application = SubmenuBuilder::new(app, "SpeekTo.Me")
        .text("settings", "Settings…")
        .text("permissions", "Grant Desktop Access…")
        .item(&tauri::menu::MenuItem::with_id(
            app,
            "quit",
            "Quit SpeekTo.Me",
            true,
            Some("Cmd+Q"),
        )?)
        .build()?;
    app.set_menu(MenuBuilder::new(app).item(&application).build()?)?;
    app.on_menu_event(|app, event| menu_event(app, event.id().as_ref()));
    sync_settings(
        &crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner()),
    );
    static STARTED: OnceLock<()> = OnceLock::new();
    STARTED.get_or_init(|| {
        std::thread::spawn(|| {
            let mut previous = None;
            while !crate::lifecycle::is_quitting() {
                let result = unsafe { s2m_run_listener(shortcut_edge) };
                if previous != Some(result) {
                    eprintln!("[MACOS SHORTCUT] {}", if result == 0 {
                        "Waiting for keyboard permission. Enable SpeekTo.Me in Privacy & Security → Accessibility or Input Monitoring. If already enabled for an older build, remove that entry and add this app again."
                    } else if result < 0 {
                        "macOS refused the keyboard listener. Reopen the app after granting Input Monitoring."
                    } else { "Keyboard listener stopped; retrying." });
                    previous = Some(result);
                }
                std::thread::sleep(std::time::Duration::from_secs(2));
            }
        });
    });
    if !std::env::args().any(|arg| arg == "--diagnose-macos")
        && crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .session_token
            .is_none()
    {
        crate::show_or_create_settings_window(app);
    }
    eprintln!(
        "[MACOS] Native menu bar ready; capabilities={}",
        capabilities()
    );
    Ok(())
}
pub fn set_preview_text(text: &str) {
    let text = cstring(text);
    unsafe {
        s2m_overlay(STATE.load(Ordering::SeqCst) as i32, text.as_ptr());
    }
}
pub fn overlay_state() -> u8 { STATE.load(Ordering::SeqCst) }

pub fn set_state(state: u8) {
    STATE.store(state, Ordering::SeqCst);
    let text = if state == 0 {
        String::new()
    } else {
        crate::win32::get_preview_text()
    };
    let text = cstring(&text);
    unsafe {
        s2m_overlay(state as i32, text.as_ptr());
    }
}
pub fn update_position() {
    sync_settings(
        &crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner()),
    );
}
pub fn chime(start: bool) {
    if crate::ACTIVE_SOUND_FEEDBACK.load(Ordering::SeqCst) {
        unsafe {
            s2m_chime(start);
        }
    }
}
pub fn notify(title: &str, body: &str) {
    let (title, body) = (cstring(title), cstring(body));
    unsafe {
        s2m_notify(title.as_ptr(), body.as_ptr());
    }
}
pub fn chord(paste: bool) -> Result<(), &'static str> {
    match unsafe { s2m_chord(paste) } {
        1 => Ok(()),
        -1 => Err("Text insertion is disabled in password fields. Your text remains on the clipboard."),
        _ => Err("Allow SpeekTo.Me in System Settings → Privacy & Security → Accessibility to insert text."),
    }
}
pub fn type_text(text: &str) -> Result<(), &'static str> {
    let text: Vec<u16> = text.encode_utf16().collect();
    match unsafe { s2m_type(text.as_ptr(), text.len()) } {
        1 => Ok(()),
        -1 => Err(
            "Text insertion is disabled in password fields. Your text remains on the clipboard.",
        ),
        _ => Err("Accessibility permission is required to type text."),
    }
}
pub fn key(vk: u32, down: bool) {
    unsafe {
        s2m_key(vk, down);
    }
}

pub fn show_overlay() {
    let state = STATE.load(Ordering::SeqCst);
    set_state(if state == 0 { 1 } else { state });
}

pub fn ensure_single_instance() -> bool {
    // BSD flock keeps the lock for the process lifetime and handles stale files.
    unsafe extern "C" {
        fn s2m_claim_instance(path: *const c_char) -> bool;
    }
    let path = crate::config::get_config_path().with_file_name("instance.lock");
    let path = cstring(&path.to_string_lossy());
    unsafe { s2m_claim_instance(path.as_ptr()) }
}

pub fn sync_autostart(enabled: bool) {
    unsafe extern "C" {
        fn s2m_login_item(enabled: bool) -> bool;
    }
    if !unsafe { s2m_login_item(enabled) } {
        eprintln!("[MACOS] Login item change requires a packaged app and may need approval in System Settings → General → Login Items.");
    }
}

fn native_overlay_snapshot() -> serde_json::Value {
    unsafe {
        let value = s2m_overlay_snapshot();
        let result = if value.is_null() { serde_json::Value::Null } else {
            serde_json::from_str(&std::ffi::CStr::from_ptr(value).to_string_lossy()).unwrap_or_default()
        };
        if !value.is_null() { s2m_free(value); }
        result
    }
}

/// Bounded native smoke test. No uploads, account changes or synthesized input.
pub fn run_diagnostic() {
    let app = tauri::Builder::default().setup(|app| {
        let handle = app.handle().clone();
        let _ = crate::TAURI_APP.set(handle.clone());
        setup(&handle)?;
        crate::win32::set_preview_text("Native macOS preview · नमस्ते · 日本語 · مرحبا");
        crate::win32::set_preview_state(crate::win32::PreviewState::Listening);
        std::thread::spawn(move || {
            use cpal::traits::{DeviceTrait, HostTrait};
            let host = cpal::default_host();
            let devices = host.input_devices().map(|devices| devices.filter_map(|device| device.name().ok()).collect::<Vec<_>>()).unwrap_or_default();
            let shortcut_check = tauri::async_runtime::block_on(crate::hotkey_conflicts::check("Ctrl + Alt + K"));
            let shortcut_check = match shortcut_check {
                Ok(report) => serde_json::to_value(report).unwrap(),
                Err(error) => serde_json::json!({"error": error}),
            };
            let mut report = serde_json::json!({
                "platform": std::env::consts::OS, "architecture": std::env::consts::ARCH,
                "capabilities": capabilities(), "input_devices": devices,
                "native_shortcut_check": shortcut_check,
                "native_overlay": native_overlay_snapshot(),
                "webview_windows": handle.webview_windows().len(),
                "configuration": crate::config::get_config_path(),
            });
            let path = std::env::var_os("SPEEKTOME_DIAGNOSTIC_REPORT").map(std::path::PathBuf::from)
                .unwrap_or_else(|| crate::config::get_config_path().with_file_name("macos-diagnostic.json"));
            let screenshot = cstring(&path.with_extension("png").to_string_lossy());
            let _ = unsafe { s2m_save_overlay_image(screenshot.as_ptr()) };
            let mut states = Vec::new();
            for (name, text, state, volume) in [
                ("pill", "", crate::win32::PreviewState::Listening, 20),
                ("preview", "Native preview · नमस्ते · 日本語 · مرحبا", crate::win32::PreviewState::Listening, 20),
                ("processing", "Polishing the transcription…", crate::win32::PreviewState::Processing, 0),
                ("hidden", "", crate::win32::PreviewState::Hidden, 0),
            ] {
                crate::audio::CURRENT_VOLUME.store(volume, Ordering::Relaxed);
                crate::win32::set_preview_text(text);
                crate::win32::set_preview_state(state);
                std::thread::sleep(std::time::Duration::from_millis(250));
                let file = path.with_file_name(format!("macos-overlay-{name}.png"));
                let png = cstring(&file.to_string_lossy());
                let saved = unsafe { s2m_save_overlay_image(png.as_ptr()) };
                states.push(serde_json::json!({"name": name, "snapshot": native_overlay_snapshot(),
                    "image_saved": saved, "image": file, "webview_windows": handle.webview_windows().len()}));
            }
            report["overlay_states"] = serde_json::json!(states);
            crate::win32::set_preview_text("Native preview · नमस्ते · 日本語 · مرحبا");
            crate::win32::set_preview_state(crate::win32::PreviewState::Listening);
            let _ = std::fs::write(path, serde_json::to_vec_pretty(&report).unwrap());
            eprintln!("[MACOS DIAGNOSTIC] {report}");
            std::thread::sleep(std::time::Duration::from_secs(8));
            crate::lifecycle::quit(&handle);
        });
        Ok(())
    }).build(crate::desktop_context()).expect("macOS native diagnostic");
    app.run(|_, event| {
        if let tauri::RunEvent::ExitRequested { code, api, .. } = event {
            if crate::lifecycle::keep_tray_alive(code) {
                api.prevent_exit();
            }
        }
    });
}
