#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

pub mod config;
pub mod audio;
pub mod audio_processing;
pub mod api;
pub mod keyboard;
pub mod win32;
pub mod vocab;
pub mod stream;

use std::sync::{Mutex, OnceLock};
use std::sync::atomic::AtomicBool;
use std::thread;
use zeroize::Zeroize;

use tauri::{Manager, Emitter, PhysicalPosition, PhysicalSize};
use crate::config::AppConfig;
use crate::win32::SendHWND;

// Global static Tauri App handle
pub static TAURI_APP: OnceLock<tauri::AppHandle> = OnceLock::new();

// Global thread-safe flags
pub static STATE: OnceLock<Mutex<RecordingState>> = OnceLock::new();
pub static OVERLAY_HWND: OnceLock<SendHWND> = OnceLock::new();
pub static ACTIVE_CONFIG: OnceLock<Mutex<AppConfig>> = OnceLock::new();
pub static SIMULATING_KEY: AtomicBool = AtomicBool::new(false);
pub static LAST_RELEASE_TIME: OnceLock<Mutex<Option<std::time::Instant>>> = OnceLock::new();
pub static IS_CONSECUTIVE: AtomicBool = AtomicBool::new(false);
pub static LAST_WINDOW_POSITION: OnceLock<Mutex<Option<PhysicalPosition<i32>>>> = OnceLock::new();
pub static LAST_WINDOW_SIZE: OnceLock<Mutex<Option<PhysicalSize<u32>>>> = OnceLock::new();

pub struct RecordingState {
    pub is_key_held: bool,
    pub press_time: Option<std::time::Instant>,
    pub stop_tx: Option<std::sync::mpsc::Sender<bool>>,
}

pub fn get_state() -> &'static Mutex<RecordingState> {
    STATE.get_or_init(|| {
        Mutex::new(RecordingState {
            is_key_held: false,
            press_time: None,
            stop_tx: None,
        })
    })
}

pub fn get_active_config() -> &'static Mutex<AppConfig> {
    ACTIVE_CONFIG.get_or_init(|| Mutex::new(config::load_config()))
}

// ==========================================
// Tauri Commands
// ==========================================

#[tauri::command]
fn get_config() -> AppConfig {
    get_active_config().lock().unwrap_or_else(|e| e.into_inner()).clone()
}

#[tauri::command]
fn save_config(config: AppConfig) {
    {
        let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        *active = config.clone();
    }
    config::save_config(&config);
    crate::win32::update_overlay_position();
}

#[tauri::command]
fn hide_window(window: tauri::WebviewWindow) {
    let _ = window.close();
}

#[tauri::command]
fn start_login(window: tauri::WebviewWindow) {
    thread::spawn(move || {
        if let Some(token) = api::listen_for_token() {
            {
                let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                active.session_token = Some(token.clone());
                config::save_config(&active);
            }
            
            if let Ok(user_info) = tauri::async_runtime::block_on(api::fetch_user_info(&api::get_api_url(), &token)) {
                let token_clone = token.clone();
                let api_url = api::get_api_url();
                tauri::async_runtime::spawn(async move {
                    let _ = crate::vocab::get_vocab_manager().sync_from_server(&api_url, &token_clone).await;
                });
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
    config::save_config(&active);
}

#[tauri::command]
async fn get_user_info() -> Option<api::UserInfo> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone()
    };
    if let Some(ref t) = token {
        api::fetch_user_info(&api::get_api_url(), t).await.ok()
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
            let token_clone = token.clone();
            let api_url = api::get_api_url();
            tauri::async_runtime::spawn(async move {
                let _ = crate::vocab::get_vocab_manager().sync_from_server(&api_url, &token_clone).await;
            });
            let mut active = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
            active.session_token = Some(token);
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
    let terms = api::fetch_vocabulary(&api::get_api_url(), &token).await?;
    let canonical_list: Vec<String> = terms.iter().map(|t| t.canonical.clone()).collect();
    crate::vocab::get_vocab_manager().set_terms(canonical_list);
    Ok(terms)
}

#[tauri::command]
async fn add_vocabulary_term(term: String) -> Result<(), String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    crate::vocab::get_vocab_manager().add_term(&term);
    api::add_vocab_term(&api::get_api_url(), &token, &term).await
}

#[tauri::command]
async fn delete_vocabulary_term(term: String) -> Result<(), String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    crate::vocab::get_vocab_manager().delete_term(&term);
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
        config::save_config(&active);
    }
    Ok(res)
}

pub fn show_or_create_settings_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.set_focus();
    } else {
        let mut builder = tauri::WebviewWindowBuilder::new(app, "main", tauri::WebviewUrl::App("index.html".into()))
            .title("SpeekTo.Me Settings")
            .resizable(true)
            .transparent(true)
            .decorations(true)
            .skip_taskbar(false);

        let pos_opt = LAST_WINDOW_POSITION.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner()).clone();
        let size_opt = LAST_WINDOW_SIZE.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner()).clone();

        if let Some(pos) = pos_opt {
            builder = builder.position(pos.x as f64, pos.y as f64);
        }
        if let Some(size) = size_opt {
            builder = builder.inner_size(size.width as f64, size.height as f64);
        } else {
            builder = builder.inner_size(800.0, 600.0);
        }

        match builder.build() {
            Ok(window) => {
                #[cfg(target_os = "windows")]
                {
                    use window_vibrancy::apply_mica;
                    let _ = apply_mica(&window, None);
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

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct LocalMeetingRecord {
    pub id: String,
    pub title: String,
    pub created_at: u64,
    pub filepath: String,
    pub status: String, // "recording" | "recorded" | "uploaded" | "failed"
    pub error: Option<String>,
}

fn get_meeting_index_path() -> Option<std::path::PathBuf> {
    config::get_config_path().parent().map(|p| p.join("recordings").join("meetings_index.json"))
}

fn load_meeting_index() -> Vec<LocalMeetingRecord> {
    if let Some(path) = get_meeting_index_path() {
        if let Ok(data) = std::fs::read_to_string(path) {
            if let Ok(records) = serde_json::from_str::<Vec<LocalMeetingRecord>>(&data) {
                return records;
            }
        }
    }
    Vec::new()
}

fn save_meeting_index(records: &[LocalMeetingRecord]) {
    if let Some(path) = get_meeting_index_path() {
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        if let Ok(data) = serde_json::to_string_pretty(records) {
            let _ = std::fs::write(path, data);
        }
    }
}

fn update_meeting_status(filepath: &str, status: &str, err: Option<String>) {
    let mut records = load_meeting_index();
    for r in &mut records {
        if r.filepath == filepath {
            r.status = status.to_string();
            r.error = err.clone();
            break;
        }
    }
    save_meeting_index(&records);
}

fn cleanup_old_recordings() {
    let config_path = config::get_config_path();
    if let Some(parent) = config_path.parent() {
        let rec_dir = parent.join("recordings");
        let records = load_meeting_index();
        let uploaded_set: std::collections::HashSet<String> = records
            .iter()
            .filter(|r| r.status == "uploaded")
            .map(|r| r.filepath.clone())
            .collect();
        let protected_set: std::collections::HashSet<String> = records
            .iter()
            .filter(|r| r.status == "failed" || r.status == "recorded" || r.status == "recording")
            .map(|r| r.filepath.clone())
            .collect();

        if let Ok(entries) = std::fs::read_dir(rec_dir) {
            let cutoff = std::time::SystemTime::now() - std::time::Duration::from_secs(24 * 3600);
            for entry in entries.flatten() {
                let path = entry.path();
                let path_str = path.to_string_lossy().to_string();

                // Do not delete failed or un-uploaded recordings
                if protected_set.contains(&path_str) {
                    continue;
                }

                if let Ok(metadata) = entry.metadata() {
                    if metadata.is_file() {
                        if let Ok(modified) = metadata.modified() {
                            if modified < cutoff && (uploaded_set.contains(&path_str) || !path_str.ends_with(".json")) {
                                let _ = std::fs::remove_file(&path);
                            }
                        }
                    }
                }
            }
        }
    }
}

fn main() {
    println!("[STARTUP] Starting SpeekTo.Me Windows Client...");

    // Install process-level default CryptoProvider for rustls / tokio-tungstenite TLS
    let _ = rustls::crypto::ring::default_provider().install_default();

    // 0. Ensure only one instance of the app is running
    win32::ensure_single_instance();

    // 1. Initialize global loaded config
    let _ = get_active_config();
    let _ = crate::vocab::get_vocab_manager();

    // 2. Cleanup old meeting recordings from previous runs (if older than 24 hours)
    cleanup_old_recordings();
    
    // 3. Spawn Keyboard Hook & System Tray Worker Thread
    thread::spawn(|| {
        println!("[STARTUP] Win32 hook and system tray worker thread spawned.");
        unsafe {
            win32::run_win32_hook_and_tray_thread();
        }
    });
    
    // 4. Launch Tauri app
    tauri::Builder::default()
        .setup(|app| {
            let handle = app.handle().clone();
            let _ = TAURI_APP.set(handle.clone());

            #[cfg(target_os = "windows")]
            {
                use window_vibrancy::apply_mica;
                if let Some(window) = app.get_webview_window("main") {
                    let _ = apply_mica(&window, None);
                }
            }

            if let Some(token) = get_active_config().lock().unwrap_or_else(|e| e.into_inner()).session_token.clone() {
                let api_url = api::get_api_url();
                tauri::async_runtime::spawn(async move {
                    let _ = crate::vocab::get_vocab_manager().sync_from_server(&api_url, &token).await;
                });
            }

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                if let Ok(pos) = window.outer_position() {
                    let mut pos_lock = LAST_WINDOW_POSITION.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner());
                    *pos_lock = Some(pos);
                }
                if let Ok(size) = window.inner_size() {
                    let mut size_lock = LAST_WINDOW_SIZE.get_or_init(|| Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner());
                    *size_lock = Some(size);
                }
                let _ = window.hide();
            }
        })
        .invoke_handler(tauri::generate_handler![
            get_config,
            save_config,
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
            start_meeting,
            stop_meeting,
            cancel_meeting,
            get_meeting_levels,
            upload_meeting,
            list_local_meetings,
            retry_upload_meeting,
            delete_local_meeting,
            get_api_url,
            open_external_url,
            validate_prompt
        ])
        .build(tauri::generate_context!())
        .expect("error while running tauri application")
        .run(|app_handle, event| {
            match event {
                tauri::RunEvent::Ready => {
                    let _ = TAURI_APP.set(app_handle.clone());
                }
                tauri::RunEvent::ExitRequested { api, .. } => {
                    api.prevent_exit();
                }
                _ => {}
            }
        });
}

pub static CURRENT_MEETING_PATH: Mutex<Option<std::path::PathBuf>> = Mutex::new(None);

#[tauri::command]
fn start_meeting(title: String) -> Result<String, String> {
    println!("Starting meeting: {}", title);
    
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
    
    if let Err(e) = crate::audio::start_meeting_recording(path.clone()) {
        return Err(format!("Failed to start recording: {}", e));
    }
    
    let mut path_lock = CURRENT_MEETING_PATH.lock().unwrap_or_else(|e| e.into_inner());
    *path_lock = Some(path.clone());
    
    let mut records = load_meeting_index();
    records.push(LocalMeetingRecord {
        id: format!("meeting-{}-{}", std::process::id(), nonce),
        title: title.clone(),
        created_at: now.duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_secs(),
        filepath: path_str.clone(),
        status: "recording".to_string(),
        error: None,
    });
    save_meeting_index(&records);

    Ok(path_str)
}

#[tauri::command]
fn stop_meeting() -> Result<String, String> {
    println!("Stopping meeting...");
    crate::audio::stop_meeting_recording();
    
    let path_lock = CURRENT_MEETING_PATH.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(ref path) = *path_lock {
        let path_str = path.to_string_lossy().to_string();
        update_meeting_status(&path_str, "recorded", None);
        Ok(path_str)
    } else {
        Err("No active meeting recording found".to_string())
    }
}

#[tauri::command]
fn cancel_meeting() -> Result<(), String> {
    println!("Cancelling meeting...");
    crate::audio::stop_meeting_recording();
    
    let mut path_lock = CURRENT_MEETING_PATH.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(path) = path_lock.take() {
        let path_str = path.to_string_lossy().to_string();
        let mut records = load_meeting_index();
        records.retain(|r| r.filepath != path_str);
        save_meeting_index(&records);

        let _ = std::fs::remove_file(&path);
        let _ = std::fs::remove_file(path.with_extension("mic.ogg"));
        let _ = std::fs::remove_file(path.with_extension("loopback.ogg"));
        let _ = std::fs::remove_file(path.with_extension("energy.json"));
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
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };

    let path = std::path::PathBuf::from(&filepath);
    if !path.exists() {
        return Err(format!("Meeting audio file not found on disk: {}", filepath));
    }

    let mixed_bytes = std::fs::read(&path)
        .map_err(|e| format!("Failed to read meeting audio: {}", e))?;
    let mic_bytes = std::fs::read(path.with_extension("mic.ogg")).ok();
    let loopback_bytes = std::fs::read(path.with_extension("loopback.ogg")).ok();
    let energy_json = std::fs::read_to_string(path.with_extension("energy.json")).ok();

    match crate::api::upload_meeting(&api::get_api_url(), &token, &title, mixed_bytes, mic_bytes, loopback_bytes, energy_json).await {
        Ok(res) => {
            update_meeting_status(&filepath, "uploaded", None);
            Ok(res)
        }
        Err(e) => {
            eprintln!("[MEETING UPLOAD FAILED] Audio safely preserved at {}. Error: {}", filepath, e);
            update_meeting_status(&filepath, "failed", Some(e.clone()));
            Err(e)
        }
    }
}

#[tauri::command]
fn list_local_meetings() -> Result<Vec<LocalMeetingRecord>, String> {
    Ok(load_meeting_index())
}

#[tauri::command]
async fn retry_upload_meeting(filepath: String) -> Result<String, String> {
    let records = load_meeting_index();
    let record = records.iter().find(|r| r.filepath == filepath)
        .cloned()
        .ok_or_else(|| format!("No local meeting record found for: {}", filepath))?;

    upload_meeting(record.title, filepath).await
}

#[tauri::command]
fn delete_local_meeting(filepath: String) -> Result<(), String> {
    let mut records = load_meeting_index();
    records.retain(|r| r.filepath != filepath);
    save_meeting_index(&records);

    let path = std::path::PathBuf::from(&filepath);
    let _ = std::fs::remove_file(&path);
    let _ = std::fs::remove_file(path.with_extension("mic.ogg"));
    let _ = std::fs::remove_file(path.with_extension("loopback.ogg"));
    let _ = std::fs::remove_file(path.with_extension("energy.json"));
    Ok(())
}

#[tauri::command]
async fn validate_prompt(prompt: String) -> Result<api::PromptValidationResult, String> {
    let token = {
        let config = get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        config.session_token.clone().ok_or("Not logged in")?
    };
    api::validate_custom_prompt(&api::get_api_url(), &token, &prompt).await
}


