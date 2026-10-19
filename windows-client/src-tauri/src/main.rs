#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

pub mod config;
#[cfg(any(target_os = "windows", test))]
mod windows_autostart;
mod gestures;
mod shortcuts;
mod native_overlay;
#[cfg(target_os = "macos")]
mod macos_overlay;
mod hotkey;
mod hotkey_conflicts;
#[cfg(target_os = "linux")]
mod hotkey_linux;
#[cfg(any(target_os = "macos", test))]
#[cfg_attr(test, allow(dead_code))]
mod hotkey_macos;
#[cfg(any(target_os = "linux", test))]
mod portal_gestures;
#[cfg(target_os = "macos")]
mod macos_desktop;
#[cfg(target_os = "macos")]
mod macos_audio;
mod lifecycle;
mod settings_lifecycle;
#[cfg(any(target_os = "macos", test))]
mod macos_browser;
pub mod audio;
pub mod audio_processing;
pub mod meeting_echo;
pub mod api;
pub mod keyboard;
pub mod win32;
pub mod stream;
pub mod meeting_stream;
pub mod vad;
pub mod lang;
#[cfg(target_os = "linux")]
mod linux_portal;
#[cfg(target_os = "linux")]
mod linux_util;
#[cfg(target_os = "linux")]
mod linux_desktop;
#[cfg(target_os = "linux")]
mod linux_focus;
#[cfg(target_os = "linux")]
mod linux_keywatch;

use std::sync::{Mutex, OnceLock};
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicUsize, Ordering};
use std::thread;
use zeroize::Zeroize;

use tauri::{Manager, Emitter, PhysicalPosition, PhysicalSize};
use crate::config::AppConfig;
use crate::win32::SendHWND;

// Global static Tauri App handle
pub static TAURI_APP: OnceLock<tauri::AppHandle> = OnceLock::new();

// Global thread-safe flags
pub static OVERLAY_HWND: OnceLock<SendHWND> = OnceLock::new();
pub static ACTIVE_CONFIG: OnceLock<Mutex<AppConfig>> = OnceLock::new();
pub static SIMULATING_KEY: AtomicBool = AtomicBool::new(false);
pub static LAST_WINDOW_POSITION: OnceLock<Mutex<Option<PhysicalPosition<i32>>>> = OnceLock::new();
pub static LAST_WINDOW_SIZE: OnceLock<Mutex<Option<PhysicalSize<u32>>>> = OnceLock::new();

pub static ACTIVE_HOTKEY_COMBO: AtomicU32 = AtomicU32::new(0x00000009); // Ctrl (0x01) + Win (0x08) default
pub static HOTKEY_ALLOWED: AtomicBool = AtomicBool::new(false);
pub static ACTIVE_HOTKEY_MODE: std::sync::atomic::AtomicU8 = std::sync::atomic::AtomicU8::new(0); // 0 = hold, 1 = toggle
pub static ACTIVE_PILL_BG_COLOR: AtomicU32 = AtomicU32::new(0x27221F);
pub static ACTIVE_PILL_WAVE_COLOR: AtomicU32 = AtomicU32::new(0x57BF7C);
pub static ACTIVE_SOUND_FEEDBACK: AtomicBool = AtomicBool::new(true);
pub static ACTIVE_COMMAND_MODE: AtomicBool = AtomicBool::new(true);
pub static ACTIVE_HAS_SESSION_TOKEN: AtomicBool = AtomicBool::new(false);
pub static TRANSFORM_INPUT_WORD_LIMIT: AtomicUsize = AtomicUsize::new(10000);

pub fn get_transform_word_limit() -> usize {
    TRANSFORM_INPUT_WORD_LIMIT.load(Ordering::Relaxed)
}

pub fn set_transform_word_limit(limit: usize) {
    TRANSFORM_INPUT_WORD_LIMIT.store(limit, Ordering::Relaxed);
}

// A single expansion also emits the macOS embedded Info.plist exactly once.
pub fn desktop_context() -> tauri::Context<tauri::Wry> { tauri::generate_context!() }

pub fn sync_active_config_atomics(config: &AppConfig) {
    ACTIVE_HOTKEY_COMBO.store(config::parse_hotkey_combo(&config.hotkey), Ordering::SeqCst);
    let mode_byte = if config.hotkey_mode == "toggle" { 1 } else { 0 };
    ACTIVE_HOTKEY_MODE.store(mode_byte, Ordering::SeqCst);
    ACTIVE_COMMAND_MODE.store(config.command_mode, Ordering::SeqCst);
    ACTIVE_PILL_BG_COLOR.store(config.pill_bg_color, Ordering::SeqCst);
    ACTIVE_PILL_WAVE_COLOR.store(config.pill_wave_color, Ordering::SeqCst);
    ACTIVE_SOUND_FEEDBACK.store(config.sound_feedback, Ordering::SeqCst);
    ACTIVE_HAS_SESSION_TOKEN.store(config.session_token.is_some(), Ordering::SeqCst);
    shortcuts::settings_changed();
    #[cfg(target_os = "macos")]
    macos_desktop::sync_settings(config);
    #[cfg(target_os = "linux")]
    linux_desktop::sync_overlay_theme();
}

pub fn get_active_config() -> &'static Mutex<AppConfig> {
    ACTIVE_CONFIG.get_or_init(|| {
        let loaded = config::load_config();
        sync_active_config_atomics(&loaded);
        Mutex::new(loaded)
    })
}

// ==========================================
// Tauri Commands
// ==========================================

#[tauri::command]
fn get_config() -> AppConfig {
    settings_lifecycle::note_config_read();
    get_active_config().lock().unwrap_or_else(|e| e.into_inner()).clone()
}

#[tauri::command]
fn get_desktop_platform() -> &'static str {
    std::env::consts::OS
}

#[tauri::command]
fn get_linux_capabilities() -> serde_json::Value {
    #[cfg(target_os = "linux")]
    { serde_json::to_value(linux_portal::capabilities()).unwrap_or_default() }
    #[cfg(target_os = "macos")]
    { macos_desktop::capabilities() }
    #[cfg(not(any(target_os = "linux", target_os = "macos")))]
    { serde_json::json!({"shortcut": true, "insertion": true, "detail": ""}) }
}

#[tauri::command]
fn request_desktop_permissions() {
    #[cfg(target_os = "macos")]
    macos_desktop::request_permissions();
}

#[tauri::command]
async fn repair_desktop_permissions() -> Result<(), String> {
    #[cfg(target_os = "macos")]
    { tauri::async_runtime::spawn_blocking(macos_desktop::repair_permissions)
        .await.map_err(|error| error.to_string())? }
    #[cfg(not(target_os = "macos"))]
    { Err("Desktop permission repair is only available on macOS.".into()) }
}

#[tauri::command]
async fn validate_hotkey(value: String) -> Result<String, String> {
    let report = hotkey_conflicts::check(&value).await?;
    report.ensure_allowed()?;
    Ok(report.canonical)
}

#[tauri::command]
async fn check_hotkey_conflicts(value: String) -> Result<hotkey_conflicts::Report, String> {
    hotkey_conflicts::check(&value).await
}

#[tauri::command]
fn set_shortcut_capture(active: bool) {
    shortcuts::set_selecting(active);
}

#[tauri::command]
fn get_audio_level() -> u32 {
    audio::CURRENT_VOLUME.load(Ordering::Relaxed)
}

#[tauri::command]
async fn save_config(app: tauri::AppHandle, mut config: AppConfig) -> Result<(), String> {
    // Reject before touching memory, disk, autostart or existing portal grants.
    let report = hotkey_conflicts::check(&config.hotkey).await?;
    report.ensure_allowed()?;
    config.hotkey = report.canonical;
    #[cfg(any(target_os = "linux", target_os = "macos"))]
    let _ = &app;
    #[cfg(target_os = "linux")]
    let (old_hotkey, old_hotkey_mode) = {
        let active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        (active.hotkey.clone(), active.hotkey_mode.clone())
    };
    {
        let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        // Preserve tray changes and persist the whole config under the same lock.
        config.languages = active.languages.clone();
        config.active_language = active.speaking_language();
        *active = config.clone();
        config::save_config(&active);
    }
    HOTKEY_ALLOWED.store(true, Ordering::SeqCst);
    sync_active_config_atomics(&config);
    #[cfg(target_os = "linux")]
    if old_hotkey != config.hotkey || old_hotkey_mode != config.hotkey_mode || !linux_portal::capabilities().shortcut || !linux_portal::capabilities().insertion {
        linux_portal::rebind();
    }
    crate::win32::update_overlay_position();
    crate::win32::update_tray_icon_for_theme(&config.theme);
    #[cfg(target_os = "windows")]
    if let Some(window) = app.get_webview_window("main") {
        if let Ok(hwnd_ptr) = window.hwnd() {
            let hwnd = hwnd_ptr.0 as *mut std::ffi::c_void;
            crate::win32::set_window_icon_for_theme(hwnd, &config.theme);
        }
    }
    Ok(())
}

#[tauri::command]
fn update_tray_theme(app: tauri::AppHandle, theme: String) {
    #[cfg(any(target_os = "linux", target_os = "macos"))]
    let _ = &app;
    crate::win32::update_tray_icon_for_theme(&theme);
    #[cfg(target_os = "windows")]
    if let Some(window) = app.get_webview_window("main") {
        if let Ok(hwnd_ptr) = window.hwnd() {
            let hwnd = hwnd_ptr.0 as *mut std::ffi::c_void;
            crate::win32::set_window_icon_for_theme(hwnd, &theme);
        }
    }
}

// Destroy Settings on close so the web UI does not stay resident in the tray.
// Recording, shortcuts, tray actions and uploads run in native Rust code.
#[tauri::command]
fn hide_window(window: tauri::WebviewWindow) {
    settings_lifecycle::dismiss(window.app_handle());
}

#[tauri::command]
fn minimize_window(window: tauri::WebviewWindow) {
    settings_lifecycle::dismiss(window.app_handle());
}

static LAST_MAXIMIZE_TOGGLE: OnceLock<Mutex<Option<std::time::Instant>>> = OnceLock::new();

#[tauri::command]
fn is_window_maximized(window: tauri::WebviewWindow) -> bool {
    window.is_maximized().unwrap_or(false)
}

#[tauri::command]
fn toggle_maximize_window(window: tauri::WebviewWindow) -> bool {
    let mut toggle_lock = LAST_MAXIMIZE_TOGGLE.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner());
    let now = std::time::Instant::now();
    if let Some(last_time) = *toggle_lock {
        if now.duration_since(last_time) < std::time::Duration::from_millis(250) {
            return window.is_maximized().unwrap_or(false);
        }
    }
    *toggle_lock = Some(now);

    if let Ok(is_max) = window.is_maximized() {
        if is_max {
            let _ = window.unmaximize();
            false
        } else {
            let _ = window.maximize();
            true
        }
    } else {
        false
    }
}

#[tauri::command]
fn close_window(window: tauri::WebviewWindow) {
    hide_window(window);
}

#[tauri::command]
fn start_login(window: tauri::WebviewWindow) {
    thread::spawn(move || {
        if let Some(token) = api::listen_for_token() {
            {
                let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                active.session_token = Some(token.clone());
                sync_active_config_atomics(&active);
                config::save_config(&active);
            }
            
            if let Ok(user_info) = tauri::async_runtime::block_on(api::fetch_user_info(&api::get_api_url(), &token)) {
                let _ = window.emit("login-success", user_info);
            } else {
                let _ = window.emit("login-failed", "Failed to retrieve user info.");
            }
        } else {
            let _ = window.emit("login-failed", "Authentication listener closed.");
        }
    });
}

#[tauri::command]
fn get_api_url() -> String {
    api::get_api_url()
}

#[tauri::command]
fn open_external_url(url: String) {
    api::open_browser(&url);
}

#[tauri::command]
fn logout() {
    let token_opt = {
        let active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        active.session_token.clone()
    };
    if let Some(token) = token_opt {
        thread::spawn(move || {
            let client = reqwest::blocking::Client::new();
            let _ = client.post(format!("{}/auth/logout", api::get_api_url()))
                .header("Authorization", format!("Bearer {}", token))
                .send();
        });
    }
    let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
    active.session_token = None;
    sync_active_config_atomics(&active);
    config::save_config(&active);
}

#[tauri::command]
async fn get_user_info() -> Option<api::UserInfo> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone()
    };
    if let Some(ref t) = token {
        let user = api::fetch_user_info(&api::get_api_url(), t).await.ok();
        if let Some(ref user) = user {
            {
                let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                active.languages = user.languages.clone();
                active.normalize_speaking_language();
                config::save_config(&active);
            }
            notify_language_changed();
        }
        user
    } else {
        None
    }
}

#[tauri::command]
async fn login_with_credentials(email: String, mut password: String) -> Result<api::UserInfo, String> {
    let res = api::login_with_credentials(&api::get_api_url(), &email, &password).await;
    password.zeroize();
    match res {
        Ok((token, user_info)) => {
            let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
            active.session_token = Some(token);
            sync_active_config_atomics(&active);
            config::save_config(&active);
            Ok(user_info)
        }
        Err(e) => Err(e),
    }
}

#[tauri::command]
async fn signup_with_credentials(email: String, mut password: String, name: String) -> Result<String, String> {
    let res = api::signup_with_credentials(&api::get_api_url(), &email, &password, &name).await;
    password.zeroize();
    res
}

#[tauri::command]
async fn get_vocabulary() -> Result<Vec<api::ClientVocabularyTerm>, String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    api::fetch_vocabulary(&api::get_api_url(), &token).await
}

#[tauri::command]
async fn add_vocabulary_term(term: String) -> Result<(), String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    api::add_vocab_term(&api::get_api_url(), &token, &term).await
}

#[tauri::command]
async fn delete_vocabulary_term(term: String) -> Result<(), String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    api::delete_vocab_term(&api::get_api_url(), &token, &term).await
}

#[tauri::command]
async fn update_languages(languages: Vec<String>) -> Result<Vec<String>, String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    let res = api::update_user_languages(&api::get_api_url(), &token, languages.clone()).await?;
    {
        let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        active.languages = Some(res.clone());
        active.normalize_speaking_language();
        config::save_config(&active);
    }
    notify_language_changed();
    Ok(res)
}

pub fn speaking_language() -> String {
    get_active_config().lock().unwrap_or_else(|e| e.into_inner()).speaking_language()
}

pub fn speaking_language_options() -> Vec<(String, String)> {
    let spoken = get_active_config().lock().unwrap_or_else(|e| e.into_inner())
        .languages.clone().unwrap_or_else(|| vec!["en".into()]);
    let catalog = lang::get_language_manager().get_languages();
    let mut choices = Vec::new();
    for code in spoken {
        if code == "auto" || choices.iter().any(|(c, _)| c == &code) { continue; }
        let name = catalog.iter().find(|l| l.code == code).map(|l| l.name.clone()).unwrap_or_else(|| code.clone());
        choices.push((code, name));
    }
    choices
}

fn notify_language_changed() {
    if let Some(app) = TAURI_APP.get() {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner()).clone();
        let _ = app.emit("speaking-language-changed", serde_json::json!({"language": config.speaking_language(), "languages": config.languages}));
        #[cfg(target_os = "linux")]
        linux_desktop::refresh_language_menu(app);
        #[cfg(target_os = "macos")]
        macos_desktop::refresh_language_menu(app);
    }
}

#[tauri::command]
fn set_active_language(language: String) -> Result<String, String> {
    {
        let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        if language == "auto" || !active.languages.as_deref().unwrap_or(&[]).contains(&language) {
            return Err("Star a saved language in Languages I Speak".into());
        }
        active.active_language = language.clone();
        config::save_config(&active);
    }
    notify_language_changed();
    Ok(language)
}

#[tauri::command]
fn get_supported_languages() -> Vec<lang::LanguageItem> {
    lang::get_language_manager().get_languages()
}

pub fn handle_session_expired(reason: Option<&str>) {
    {
        let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        active.session_token = None;
        sync_active_config_atomics(&active);
        config::save_config(&active);
    }

    let msg = reason.unwrap_or("Your session has expired. Please sign in again to continue dictating.");
    crate::win32::show_tray_notification(
        "SpeekTo.Me — Session Expired",
        msg,
        crate::win32::NotificationLevel::Warning,
    );

    if let Some(app) = TAURI_APP.get() {
        let _ = app.emit("auth-session-expired", ());
        show_or_create_settings_window(app);
    }
}

pub fn show_or_create_settings_window(app: &tauri::AppHandle) {
    if lifecycle::is_quitting() { return; }
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
        let _ = window.emit("open-meeting-view", ());
    } else {
        let mut builder = tauri::WebviewWindowBuilder::new(app, "main", tauri::WebviewUrl::App("index.html".into()))
            .title("SpeekTo.Me Settings")
            // All durable preferences/auth live in Rust. Give each macOS
            // Settings view its own disposable WebKit website-data store.
            .incognito(cfg!(target_os = "macos"))
            .resizable(true)
            .decorations(cfg!(target_os = "macos"))
            .skip_taskbar(false)
            .min_inner_size(840.0, 650.0);

        let pos_opt = LAST_WINDOW_POSITION.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner()).clone();
        let size_opt = LAST_WINDOW_SIZE.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner()).clone();

        if let Some(pos) = pos_opt {
            builder = builder.position(pos.x as f64, pos.y as f64);
        }
        if let Some(size) = size_opt {
            builder = builder.inner_size(size.width as f64, size.height as f64);
        } else {
            builder = builder.inner_size(1200.0, 840.0);
        }

        match builder.build() {
            Ok(window) => {
                settings_lifecycle::watch(&window);
                #[cfg(target_os = "windows")]
                if let Ok(hwnd_ptr) = window.hwnd() {
                    let hwnd = hwnd_ptr.0 as *mut std::ffi::c_void;
                    let current_theme = get_active_config().lock().unwrap_or_else(|e| e.into_inner()).theme.clone();
                    crate::win32::set_window_icon_for_theme(hwnd, &current_theme);
                }
                let _ = window.show();
                let _ = window.set_focus();
                println!("[WINDOW] Settings window built and displayed successfully.");
            }
            Err(e) => {
                eprintln!("[WINDOW BUILD ERROR] Failed to build settings window: {}", e);
            }
        }
    }
}

use serde::{Serialize, Deserialize};
mod meeting_upload;
mod meeting_session;
mod account_errors;
static MEETING_INDEX_LOCK: Mutex<()> = Mutex::new(());

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct LocalMeetingRecord {
    #[serde(default)] pub language: Option<String>,
    pub id: String,
    pub title: String,
    pub created_at: u64,
    pub filepath: String,
    pub status: String, // "recording" | "recorded" | "uploaded" | "failed"
    pub error: Option<String>,
    #[serde(default)] pub owner_email: Option<String>,
    #[serde(default)] pub token_fingerprint: Option<String>,
}

fn get_meeting_index_path() -> Option<std::path::PathBuf> {
    config::get_config_path().parent().map(|p| p.join("recordings").join("meetings_index.json"))
}

fn load_meeting_index() -> Vec<LocalMeetingRecord> {
 let _guard=MEETING_INDEX_LOCK.lock().unwrap_or_else(|e|e.into_inner());
 load_meeting_index_unlocked()
}
fn load_meeting_index_unlocked() -> Vec<LocalMeetingRecord> {
    let mut records = Vec::new();
    if let Some(path) = get_meeting_index_path() {
        if let Ok(data) = std::fs::read_to_string(&path) {
            records = serde_json::from_str::<Vec<LocalMeetingRecord>>(&data).unwrap_or_default();
        }
        // Independent per-record manifests retain account identity and upload IDs
        // if the aggregate index is missing or damaged after a storage failure.
        if let Some(parent) = path.parent() {
            if let Ok(entries) = std::fs::read_dir(parent) {
                for entry in entries.flatten() {
                    let file = entry.path();
                    if !file.to_string_lossy().ends_with(".meeting.json") { continue; }
                    if let Ok(data) = std::fs::read(&file) {
                        if let Ok(record) = serde_json::from_slice::<LocalMeetingRecord>(&data) {
                            if std::path::Path::new(&record.filepath).exists() && !records.iter().any(|r| r.id == record.id) { records.push(record); }
                        }
                    }
                }
            }
        }
    }
    records
}

fn save_meeting_index(records: &[LocalMeetingRecord]) {
    for record in records {
        let path = std::path::Path::new(&record.filepath).with_extension("meeting.json");
        let temporary = path.with_extension("json.tmp");
        if let Ok(data) = serde_json::to_vec(record) {
            if let Ok(mut file) = std::fs::File::create(&temporary) {
                use std::io::Write;
                if file.write_all(&data).is_ok() && file.sync_all().is_ok() { drop(file); let _ = meeting_upload::replace_file(&temporary, &path); }
            }
        }
    }
    if let Some(path) = get_meeting_index_path() {
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        if let Ok(data) = serde_json::to_string_pretty(records) {
            let temporary=path.with_extension("tmp");
 if let Ok(mut file)=std::fs::File::create(&temporary) {
 use std::io::Write;
 if file.write_all(data.as_bytes()).is_ok() && file.sync_all().is_ok() {drop(file);let _=meeting_upload::replace_file(&temporary,&path);}
 }
        }
    }
}

fn update_meeting_status(filepath: &str, status: &str, err: Option<String>) {
    let _guard=MEETING_INDEX_LOCK.lock().unwrap_or_else(|e|e.into_inner());
    let mut records = load_meeting_index_unlocked();
    for r in &mut records {
        if r.filepath == filepath {
            r.status = status.to_string();
            r.error = err.clone();
            break;
        }
    }
    save_meeting_index(&records);
}

fn set_meeting_title(filepath: &str, title: &str) {
    if title.trim().is_empty() { return; }
    let _guard=MEETING_INDEX_LOCK.lock().unwrap_or_else(|e|e.into_inner());
    let mut records=load_meeting_index_unlocked();
    for record in &mut records { if record.filepath==filepath { record.title=title.trim().to_string(); break; } }
    save_meeting_index(&records);
}

fn cleanup_old_recordings() {
 // Retain all recordings until an explicit user deletion. Pending and queued
 // meeting sidecars must never be removed by an age-only cleanup policy.
}

// A bounded native rendering check without microphone capture, portal prompts,
// uploads or network calls. It uses the actual production pill/preview renderer.
#[cfg(target_os = "linux")]
fn run_native_overlay_diagnostic() {
    let app = tauri::Builder::default().setup(move |app| {
        let handle = app.handle().clone();
        let _ = TAURI_APP.set(handle.clone());
        linux_desktop::setup(&handle)?;
        win32::set_preview_text("Native Rust preview. नमस्ते दुनिया. No webview is running.");
        win32::set_preview_state(win32::PreviewState::Listening);
        gtk::glib::timeout_add_local_once(std::time::Duration::from_secs(7), move || lifecycle::quit(&handle));
        Ok(())
    }).build(desktop_context()).expect("native overlay diagnostic");
    app.run(|_, event| {
        if let tauri::RunEvent::ExitRequested { code, api, .. } = event {
            if lifecycle::keep_tray_alive(code) { api.prevent_exit(); }
        }
    });
}

fn settings_invoke_handler() -> impl Fn(tauri::ipc::Invoke) -> bool + Send + Sync + 'static {
    tauri::generate_handler![
            get_config,
            get_desktop_platform,
            get_linux_capabilities,
            request_desktop_permissions,
            repair_desktop_permissions,
            get_audio_level,
            save_config,
            validate_hotkey,
            check_hotkey_conflicts,
            set_shortcut_capture,
            hide_window,
            start_login,
            logout,
            get_user_info,
            login_with_credentials,
            signup_with_credentials,
            get_vocabulary,
            add_vocabulary_term,
            delete_vocabulary_term,
            update_languages,
            set_active_language,
            get_supported_languages,
            start_meeting,
            stop_meeting,
            cancel_meeting,
            get_active_meeting,
            get_meeting_levels,
            upload_meeting,
            list_local_meetings,
            get_local_meeting_audio,
            retry_upload_meeting,
            delete_local_meeting,
            get_api_url,
            open_external_url,
            validate_prompt,
            minimize_window,
            is_window_maximized,
            toggle_maximize_window,
            close_window,
            update_tray_theme
        ]
}

fn main() {
    #[cfg(target_os = "linux")]
    if std::env::var_os("WEBKIT_DISABLE_DMABUF_RENDERER").is_none() {
        // Fedora's WebKitGTK/Mesa combination can send an explicit-sync
        // Wayland surface without an acquire point, which KWin rejects with a
        // fatal protocol error. Keep the webview on its shared-memory path.
        std::env::set_var("WEBKIT_DISABLE_DMABUF_RENDERER", "1");
    }
    #[cfg(target_os = "windows")]
    unsafe {
        // Attach to parent console if running from terminal (PowerShell, CMD, Git Bash)
        // so that println! logs are visible even in --release builds.
        use windows_sys::Win32::System::Console::{AttachConsole, ATTACH_PARENT_PROCESS};
        AttachConsole(ATTACH_PARENT_PROCESS);
    }

    // Read-only policy diagnostic: no preferences, microphone or app instance.
    if std::env::args().nth(1).as_deref() == Some("--check-shortcut") {
        let value = std::env::args().nth(2).unwrap_or_default();
        let result = tauri::async_runtime::block_on(hotkey_conflicts::check(&value));
        match result {
            Ok(report) => { let error = report.ensure_allowed().err(); println!("{}", serde_json::json!({"report":report,"error":error})); }
            Err(error) => println!("{}", serde_json::json!({"error":error})),
        }
        return;
    }

    #[cfg(target_os = "macos")]
    if !macos_desktop::ensure_single_instance() { return; }
    #[cfg(target_os = "macos")]
    if std::env::args().any(|arg| arg == "--diagnose-macos") { macos_desktop::run_diagnostic(); return; }
    if std::env::args().any(|arg| arg == "--diagnose-settings-lifecycle") { settings_lifecycle::run_diagnostic(); return; }
    println!("[STARTUP 1] Starting SpeekTo.Me desktop client...");

    // Install process-level default CryptoProvider for rustls / tokio-tungstenite TLS
    let _ = rustls::crypto::ring::default_provider().install_default();
    println!("[STARTUP 2] CryptoProvider installed.");

    // 0. Ensure only one instance of the app is running
    #[cfg(target_os = "windows")]
    win32::ensure_single_instance();
    #[cfg(target_os = "linux")]
    if !linux_util::ensure_single_instance() {
        eprintln!("[STARTUP] SpeekTo.Me is already running or could not claim its session socket");
        return;
    }
    println!("[STARTUP 3] Single instance check passed.");

    // 1. Initialize global loaded config
    let _ = get_active_config();
    println!("[STARTUP 4] Active config initialized.");
    let startup_hotkey = get_active_config().lock().unwrap_or_else(|e| e.into_inner()).hotkey.clone();
    let hotkey_result = tauri::async_runtime::block_on(hotkey_conflicts::check(&startup_hotkey))
        .and_then(|report| report.ensure_allowed());
    HOTKEY_ALLOWED.store(hotkey_result.is_ok(), Ordering::SeqCst);
    if let Err(error) = &hotkey_result { eprintln!("[SHORTCUT] Saved shortcut disabled: {error}"); }


    #[cfg(target_os = "linux")]
    if std::env::args().any(|arg| arg == "--diagnose-native-overlay") {
        run_native_overlay_diagnostic();
        return;
    }

    // 2. Retain local meeting recordings for explicit deletion and retry.
    cleanup_old_recordings();
 meeting_upload::start_recovery();
    println!("[STARTUP 5] Cleanup recordings completed.");
    
    // 3. Spawn Keyboard Hook & System Tray Worker Thread
    #[cfg(target_os = "windows")]
    thread::spawn(|| {
        println!("[STARTUP] Win32 hook and system tray worker thread spawned.");
        unsafe {
            win32::run_win32_hook_and_tray_thread();
        }
    });
    
    // 4. Launch Tauri app
    println!("[STARTUP 6] Building Tauri application...");
    let app = tauri::Builder::default()
        .setup(move |app| {
            println!("[STARTUP 7a] Setting TAURI_APP handle...");
            let handle = app.handle().clone();
            let _ = TAURI_APP.set(handle.clone());
            #[cfg(target_os = "linux")]
            {
                linux_desktop::setup(&handle)?;
                linux_portal::start();
            }
            #[cfg(target_os = "macos")]
            macos_desktop::setup(&handle)?;
            shortcuts::start();
            println!("[STARTUP 7b] TAURI_APP set.");

            let token_opt = {
                let conf = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                conf.session_token.clone()
            };
            println!("[STARTUP 7c] Session token present: {}", token_opt.is_some());

            let api_url = api::get_api_url();
            let api_url_lang = api_url.clone();
            let api_url_cfg = api_url.clone();
            std::thread::spawn(move || {
                tauri::async_runtime::block_on(async move {
                    println!("[STARTUP] Syncing languages catalog and client config from server in background thread...");
                    let _ = crate::lang::get_language_manager().sync_from_server(&api_url_lang).await;
                    let _ = crate::api::fetch_client_config(&api_url_cfg).await;
                    println!("[STARTUP] Languages and config sync finished.");
                });
            });

            if let Err(error) = &hotkey_result {
                win32::show_tray_notification("Choose another shortcut", error, win32::NotificationLevel::Warning);
                show_or_create_settings_window(&handle);
            }
            if token_opt.is_none() {
                println!("[STARTUP] No session token found. Displaying onboarding settings window...");
                show_or_create_settings_window(&handle);
            }

            println!("[STARTUP 8] Setup completed successfully!");
            Ok(())
        })
        .on_window_event(settings_lifecycle::on_window_event)
        .invoke_handler(settings_invoke_handler());

    println!("[STARTUP 9] Running Tauri event loop...");
    let app = app
        .build(desktop_context())
        .expect("error while building tauri application");

    app.run(|_app_handle, event| {
        #[cfg(target_os = "macos")]
        if matches!(&event, tauri::RunEvent::Reopen { .. }) { show_or_create_settings_window(_app_handle); }
        if let tauri::RunEvent::ExitRequested { api, code, .. } = event {
            if lifecycle::keep_tray_alive(code) { api.prevent_exit(); }
        }
    });
}

pub static CURRENT_MEETING_PATH: Mutex<Option<std::path::PathBuf>> = Mutex::new(None);
pub static CURRENT_MEETING_STREAM: Mutex<Option<crate::meeting_stream::MeetingStreamHandle>> = Mutex::new(None);

#[derive(Clone, Serialize, Deserialize)]
pub struct ActiveMeetingInfo {
    pub is_recording: bool,
    pub title: String,
    pub filepath: String,
    pub elapsed_seconds: u64,
    pub start_time_unix: u64,
    pub turns: Vec<crate::meeting_stream::ClientMeetingTurn>,
}

pub struct ActiveMeetingState {
    pub title: String,
    pub filepath: String,
    pub start_instant: std::time::Instant,
    pub start_unix: u64,
    pub turns: std::sync::Arc<Mutex<Vec<crate::meeting_stream::ClientMeetingTurn>>>,
}

pub static ACTIVE_MEETING: Mutex<Option<ActiveMeetingState>> = Mutex::new(None);

#[tauri::command]
fn get_active_meeting() -> Option<ActiveMeetingInfo> {
    let lock = ACTIVE_MEETING.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(ref s) = *lock {
        let turns = s.turns.lock().unwrap_or_else(|e| e.into_inner()).clone();
        Some(ActiveMeetingInfo {
            is_recording: true,
            title: s.title.clone(),
            filepath: s.filepath.clone(),
            elapsed_seconds: s.start_instant.elapsed().as_secs(),
            start_time_unix: s.start_unix,
            turns,
        })
    } else {
        None
    }
}

#[tauri::command]
async fn start_meeting(title: String) -> Result<String, String> {
    let _starting = crate::meeting_session::begin()?;
    println!("[MEETING LOG] >>> start_meeting command called with title: '{}'", title);
    
    let temp_dir = crate::config::get_config_path()
        .parent()
        .ok_or("Unable to determine SpeekTo.Me data directory")?
        .join("recordings");
    std::fs::create_dir_all(&temp_dir).map_err(|e| format!("Failed to create recordings directory: {}", e))?;
    let now = std::time::SystemTime::now();
    let nonce = now
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|_| "System clock is before the Unix epoch")?
        .as_nanos();
    let filename = format!("meeting-{}-{}.ogg", std::process::id(), nonce);
    let path = temp_dir.join(filename);
    let path_str = path.to_string_lossy().to_string();
    println!("[MEETING LOG] Output path generated: {}", path_str);

    let start_instant = std::time::Instant::now();
    let start_unix = now.duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_secs();
    let turns_cache = std::sync::Arc::new(Mutex::new(Vec::new()));

    let recording_language = speaking_language();
    let recording_token={get_active_config().lock().unwrap_or_else(|e|e.into_inner()).session_token.clone()};
    let token_fingerprint=recording_token.as_ref().map(|token|{use sha2::Digest;format!("{:x}",sha2::Sha256::digest(token.as_bytes()))});
    let owner_email=tokio::time::timeout(std::time::Duration::from_secs(3),get_user_info()).await.ok().flatten().map(|user|user.email).or_else(||recording_token.as_deref().and_then(meeting_upload::cached_account));
    let stream_tx = None;
    println!("[MEETING LOG] Starting CPAL audio capture streams...");
    let permit = crate::meeting_session::acquire().await?;
    if let Err(e) = crate::audio::start_meeting_recording(path.clone(), stream_tx) {
        crate::meeting_session::abandon(permit);
        eprintln!("[MEETING LOG ERROR] start_meeting_recording failed: {}", e);
        return Err(format!("Failed to start recording: {}", e));
    }
    println!("[MEETING LOG] Audio capture successfully started!");
    
    {
        let mut path_lock = CURRENT_MEETING_PATH.lock().unwrap_or_else(|e| e.into_inner());
        *path_lock = Some(path.clone());
        println!("[MEETING LOG] Stored CURRENT_MEETING_PATH = Some({})", path_str);
    }

    {
        let mut act_lock = ACTIVE_MEETING.lock().unwrap_or_else(|e| e.into_inner());
        *act_lock = Some(ActiveMeetingState {
            title: title.clone(),
            filepath: path_str.clone(),
            start_instant,
            start_unix,
            turns: turns_cache,
        });
    }

    // Show native floating meeting overlay pill on desktop
    crate::win32::show_meeting_overlay();
    
    let _index_guard=MEETING_INDEX_LOCK.lock().unwrap_or_else(|e|e.into_inner());
    let mut records = load_meeting_index_unlocked();
    records.push(LocalMeetingRecord {
        language: Some(recording_language),
        id: uuid::Uuid::new_v4().to_string(),
        title: title.clone(),
        created_at: start_unix,
        filepath: path_str.clone(),
        status: "recording".to_string(),
        error: None,
        owner_email,
        token_fingerprint,
    });
    save_meeting_index(&records);
    crate::meeting_session::install(permit);
    println!("[MEETING LOG] Meeting record indexed locally. Returning path: {}", path_str);

    Ok(path_str)
}

#[tauri::command]
async fn stop_meeting(title: Option<String>) -> Result<String, String> {
    println!("[MEETING LOG] >>> stop_meeting command called!");
    crate::win32::hide_meeting_overlay();
    *ACTIVE_MEETING.lock().unwrap_or_else(|e| e.into_inner()) = None;

    let finalize_error=crate::audio::stop_meeting_recording();
    crate::meeting_session::finish();
    if let Err(e)=finalize_error {
        if let Some(path)=CURRENT_MEETING_PATH.lock().unwrap_or_else(|error|error.into_inner()).take() {
            update_meeting_status(&path.to_string_lossy(), "failed", Some(e.clone()));
        }
        return Err(e);
    }
    println!("[MEETING LOG] Audio recording stopped in audio engine.");
    
    let stream_handle = {
        let mut lock = CURRENT_MEETING_STREAM.lock().unwrap_or_else(|e| e.into_inner());
        lock.take()
    };
    println!("[MEETING LOG] Live stream handle present: {}", stream_handle.is_some());

    let path_str = {
        let mut path_lock = CURRENT_MEETING_PATH.lock().unwrap_or_else(|e| e.into_inner());
        if let Some(path) = path_lock.take() {
            path.to_string_lossy().to_string()
        } else {
            eprintln!("[MEETING LOG ERROR] CURRENT_MEETING_PATH is None! Cannot stop meeting.");
            return Err("No active meeting recording found".to_string());
        }
    };
    if let Some(title)=title { set_meeting_title(&path_str,&title); }
    println!("[MEETING LOG] Found active meeting path: {}", path_str);

    if let Some(handle) = stream_handle {
        println!("[MEETING LOG] Waiting up to 30s for live stream final result from server...");
        match handle.rx_final.recv_timeout(std::time::Duration::from_secs(30)) {
            Ok(Ok(final_json)) => {
                println!("[MEETING LOG] Received finalized meeting json from server (len: {} bytes).", final_json.len());
                print_meeting_review(&final_json);
                update_meeting_status(&path_str, "uploaded", None);
                return Ok(final_json);
            }
            Ok(Err(e)) => {
                eprintln!("[MEETING LOG ERROR] Live stream finalize error: {}.", e);
                update_meeting_status(&path_str, "failed", Some(e.clone()));
                return Err(format!("Live stream finalize error: {}", e));
            }
            Err(e) => {
                eprintln!("[MEETING LOG WARNING] Timed out waiting for live stream ({:?}).", e);
                update_meeting_status(&path_str, "failed", Some(e.to_string()));
                return Err(format!("Timed out waiting for server meeting finalization: {}", e));
            }
        }
    }

    println!("[MEETING LOG] Returning local filepath for batch upload: {}", path_str);
    update_meeting_status(&path_str, "recorded", None);
    Ok(path_str)
}

pub fn print_meeting_review(json_str: &str) {
    println!("\n================================================================================");
    println!("🎯 COMPLETE MEETING DIARIZATION & TRANSCRIPTION DATA REVIEW");
    println!("================================================================================");

    if let Ok(v) = serde_json::from_str::<serde_json::Value>(json_str) {
        let title = v.get("title").and_then(|t| t.as_str()).unwrap_or("Untitled Meeting");
        let id = v.get("id").map(|i| i.to_string()).unwrap_or_else(|| "N/A".to_string());
        let duration = v.get("duration").and_then(|d| d.as_f64()).unwrap_or(0.0);
        let created_at = v.get("created_at").map(|c| c.to_string()).unwrap_or_else(|| "".to_string());
        let audio_url = v.get("audio_url").and_then(|u| u.as_str()).unwrap_or("N/A");

        println!("📋 Title:        {}", title);
        println!("🆔 Meeting ID:   {}", id);
        println!("⏱️  Duration:     {:.2} seconds", duration);
        println!("📅 Created At:   {}", created_at);
        println!("🎵 Audio URL:    {}", audio_url);
        println!("--------------------------------------------------------------------------------");

        // Diarized Segments / Turns
        println!("\n👥 DIARIZED TRANSCRIPTION (SPEAKER SEPARATED):");
        println!("--------------------------------------------------------------------------------");
        
        let mut segments_found = false;
        if let Some(t_json) = v.get("transcript_json") {
            let segs_val: Option<serde_json::Value> = if let Some(s) = t_json.as_str() {
                serde_json::from_str(s).ok()
            } else {
                Some(t_json.clone())
            };

            if let Some(serde_json::Value::Array(segs)) = segs_val {
                if !segs.is_empty() {
                    segments_found = true;
                    for (idx, seg) in segs.iter().enumerate() {
                        let start = seg.get("start").and_then(|s| s.as_f64()).unwrap_or(0.0);
                        let end = seg.get("end").and_then(|e| e.as_f64()).unwrap_or(0.0);
                        let spk = seg.get("speaker").and_then(|s| s.as_str()).unwrap_or("Unknown");
                        let text = seg.get("text").and_then(|t| t.as_str()).unwrap_or("");
                        
                        let start_min = (start / 60.0) as u32;
                        let start_sec = (start % 60.0) as u32;
                        let end_min = (end / 60.0) as u32;
                        let end_sec = (end % 60.0) as u32;

                        println!("{:3}. [{:02}:{:02} - {:02}:{:02}] {:<18} : \"{}\"", 
                            idx + 1, start_min, start_sec, end_min, end_sec, spk, text);
                    }
                }
            }
        }

        if !segments_found {
            if let Some(raw) = v.get("raw_transcript").and_then(|r| r.as_str()) {
                println!("{}", raw);
            } else if let Some(tr) = v.get("transcript").and_then(|t| t.as_str()) {
                println!("{}", tr);
            } else {
                println!("(No dialogue segments available)");
            }
        }

        println!("\n--------------------------------------------------------------------------------");
        println!("📝 AI SUMMARY & ACTION ITEMS:");
        println!("--------------------------------------------------------------------------------");
        if let Some(summary) = v.get("summary").and_then(|s| s.as_str()) {
            println!("\n📌 Summary:\n{}", summary);
        }
        if let Some(actions) = v.get("action_items").and_then(|a| a.as_str()) {
            println!("\n✅ Action Items:\n{}", actions);
        }

        println!("\n--------------------------------------------------------------------------------");
        println!("📦 FULL RAW JSON PAYLOAD:");
        println!("--------------------------------------------------------------------------------");
        if let Ok(pretty) = serde_json::to_string_pretty(&v) {
            println!("{}", pretty);
        } else {
            println!("{}", json_str);
        }
    } else {
        println!("{}", json_str);
    }

    println!("================================================================================\n");
}

#[tauri::command]
async fn cancel_meeting() -> Result<(), String> {
    println!("[MEETING LOG] >>> cancel_meeting command called!");
    crate::win32::hide_meeting_overlay();
    *ACTIVE_MEETING.lock().unwrap_or_else(|e| e.into_inner()) = None;

    let stopped=crate::audio::stop_meeting_recording();
    crate::meeting_session::finish();
    stopped?;
    
    {
        let mut stream_lock = CURRENT_MEETING_STREAM.lock().unwrap_or_else(|e| e.into_inner());
        *stream_lock = None;
    }

    let mut path_lock = CURRENT_MEETING_PATH.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(path) = path_lock.take() {
        let path_str = path.to_string_lossy().to_string();
        println!("[MEETING LOG] Cancelling and cleaning files for: {}", path_str);
        let _index_guard=MEETING_INDEX_LOCK.lock().unwrap_or_else(|e|e.into_inner());
        let mut records = load_meeting_index_unlocked();
        records.retain(|r| r.filepath != path_str);
        save_meeting_index(&records);

        let _ = std::fs::remove_file(&path);
        let _ = std::fs::remove_file(path.with_extension("mic.ogg"));
        let _ = std::fs::remove_file(path.with_extension("loopback.ogg"));
        let _ = std::fs::remove_file(path.with_extension("energy.json"));
    let _ = std::fs::remove_file(path.with_extension("meeting.json"));
    let _ = std::fs::remove_file(path.with_extension("wav"));
    } else {
        println!("[MEETING LOG] cancel_meeting: CURRENT_MEETING_PATH was already None.");
    }
    Ok(())
}

#[tauri::command]
fn get_meeting_levels() -> (u32, u32) {
    use std::sync::atomic::Ordering;
    let mic = crate::audio::MIC_VOLUME.load(Ordering::Relaxed);
    let loopback = crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed);
    (mic, loopback)
}

#[tauri::command]
async fn upload_meeting(title: String, filepath: String) -> Result<String, String> {
 set_meeting_title(&filepath,&title);
 let path=filepath.clone();
 tauri::async_runtime::spawn_blocking(move ||meeting_upload::upload(&path)).await.map_err(|e|e.to_string())?
}

#[tauri::command]
fn list_local_meetings() -> Result<Vec<LocalMeetingRecord>, String> {
    use sha2::{Digest, Sha256};
    let token = get_active_config().lock().unwrap_or_else(|e|e.into_inner()).session_token.clone();
    let Some(token) = token else { return Ok(Vec::new()); };
    let email = meeting_upload::cached_account(&token);
    let fingerprint = format!("{:x}", Sha256::digest(token.as_bytes()));
    Ok(load_meeting_index().into_iter().filter(|record| {
        if let Some(owner) = &record.owner_email { email.as_ref() == Some(owner) }
        else { record.token_fingerprint.as_ref() == Some(&fingerprint) }
    }).collect())
}

#[tauri::command]
async fn retry_upload_meeting(filepath: String) -> Result<String, String> {
    let records = load_meeting_index();
    let record = records.iter().find(|r| r.filepath == filepath)
        .cloned()
        .ok_or_else(|| format!("No local meeting record found for: {}", filepath))?;

    let _=record;
 tauri::async_runtime::spawn_blocking(move||meeting_upload::retry(&filepath)).await.map_err(|e|e.to_string())?
}

#[tauri::command]
fn delete_local_meeting(filepath: String) -> Result<(), String> {
    let _index_guard=MEETING_INDEX_LOCK.lock().unwrap_or_else(|e|e.into_inner());
    let mut records = load_meeting_index_unlocked();
    records.retain(|r| r.filepath != filepath);
    save_meeting_index(&records);

    let path = std::path::PathBuf::from(&filepath);
    let _ = std::fs::remove_file(&path);
    let _ = std::fs::remove_file(path.with_extension("mic.ogg"));
    let _ = std::fs::remove_file(path.with_extension("loopback.ogg"));
    let _ = std::fs::remove_file(path.with_extension("energy.json"));
    let _ = std::fs::remove_file(path.with_extension("meeting.json"));
    let _ = std::fs::remove_file(path.with_extension("wav"));
    Ok(())
}

#[tauri::command]
async fn get_local_meeting_audio(_meeting_id: Option<i64>, timestamp: Option<u64>) -> Result<String, String> {
    use base64::Engine;

    let records = load_meeting_index();
    if records.is_empty() {
        return Err("No local meeting records found on disk".to_string());
    }

    // Try finding matching record by timestamp or take the most recent record
    let record = if let Some(ts) = timestamp {
        records.iter().min_by_key(|r| (r.created_at as i64 - ts as i64).abs())
    } else {
        records.last()
    }.ok_or_else(|| "No matching local meeting record found".to_string())?;

    let ogg_path = std::path::PathBuf::from(&record.filepath);
    let wav_path = ogg_path.with_extension("wav");

    // 1. If WAV file already exists on disk, read and return base64 data URI
    if wav_path.exists() {
        if let Ok(wav_bytes) = std::fs::read(&wav_path) {
            let b64 = base64::engine::general_purpose::STANDARD.encode(&wav_bytes);
            return Ok(format!("data:audio/wav;base64,{}", b64));
        }
    }

    // 2. If only OGG file exists, decode it to WAV and save WAV on disk for instant future loading
    if ogg_path.exists() {
        let wav_bytes = crate::audio::decode_ogg_to_wav(&ogg_path)?;
        let _ = std::fs::write(&wav_path, &wav_bytes);
        let b64 = base64::engine::general_purpose::STANDARD.encode(&wav_bytes);
        return Ok(format!("data:audio/wav;base64,{}", b64));
    }

    Err(format!("Local recording file not found at: {}", record.filepath))
}

#[tauri::command]
async fn validate_prompt(prompt: String) -> Result<api::PromptValidationResult, String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    api::validate_custom_prompt(&api::get_api_url(), &token, &prompt).await
}


