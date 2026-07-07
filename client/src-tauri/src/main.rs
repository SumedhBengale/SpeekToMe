// #![windows_subsystem = "windows"]

pub mod config;
pub mod audio;
pub mod api;
pub mod keyboard;
pub mod win32;

use std::sync::{Mutex, OnceLock};
use std::sync::atomic::AtomicBool;
use std::thread;

use tauri::{Manager, Emitter};
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

pub struct RecordingState {
    pub is_key_held: bool,
    pub press_time: Option<std::time::Instant>,
    pub stop_tx: Option<std::sync::mpsc::Sender<()>>,
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
    get_active_config().lock().unwrap().clone()
}

#[tauri::command]
fn save_config(config: AppConfig) {
    let mut active = get_active_config().lock().unwrap();
    *active = config.clone();
    config::save_config(&config);
}

#[tauri::command]
fn hide_window(window: tauri::WebviewWindow) {
    let _ = window.hide();
}

#[tauri::command]
fn start_login(window: tauri::WebviewWindow) {
    thread::spawn(move || {
        if let Some(token) = api::listen_for_token() {
            let mut active = get_active_config().lock().unwrap();
            active.session_token = Some(token.clone());
            config::save_config(&active);
            
            if let Some(user_info) = api::fetch_user_info(&token) {
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
fn logout() {
    let mut active = get_active_config().lock().unwrap();
    if let Some(ref token) = active.session_token {
        let token_clone = token.clone();
        thread::spawn(move || {
            let client = reqwest::blocking::Client::new();
            let _ = client.post(format!("{}/auth/logout", api::get_api_url()))
                .header("Authorization", format!("Bearer {}", token_clone))
                .send();
        });
    }
    active.session_token = None;
    config::save_config(&active);
}

#[tauri::command]
fn get_user_info() -> Option<api::UserInfo> {
    let config = get_active_config().lock().unwrap();
    if let Some(ref token) = config.session_token {
        api::fetch_user_info(token)
    } else {
        None
    }
}

#[tauri::command]
fn login_with_credentials(email: String, password: String) -> Result<api::UserInfo, String> {
    match api::login_with_credentials(&email, &password) {
        Ok((token, user_info)) => {
            let mut active = get_active_config().lock().unwrap();
            active.session_token = Some(token);
            config::save_config(&active);
            Ok(user_info)
        }
        Err(e) => Err(e),
    }
}

#[tauri::command]
fn signup_with_credentials(email: String, password: String, name: String) -> Result<String, String> {
    api::signup_with_credentials(&email, &password, &name)
}

#[tauri::command]
fn get_vocabulary() -> Result<Vec<api::ClientVocabularyTerm>, String> {
    let config = get_active_config().lock().unwrap();
    let token = config.session_token.as_ref().ok_or("Not logged in")?;
    api::fetch_vocabulary(token)
}

#[tauri::command]
fn add_vocabulary_term(term: String) -> Result<(), String> {
    let config = get_active_config().lock().unwrap();
    let token = config.session_token.as_ref().ok_or("Not logged in")?;
    api::add_vocab_term(token, &term)
}

#[tauri::command]
fn delete_vocabulary_term(term: String) -> Result<(), String> {
    let config = get_active_config().lock().unwrap();
    let token = config.session_token.as_ref().ok_or("Not logged in")?;
    api::delete_vocab_term(token, &term)
}

fn main() {
    // 1. Initialize global loaded config
    let _ = get_active_config();
    
    // 2. Spawn Keyboard Hook & System Tray Worker Thread
    thread::spawn(|| {
        unsafe {
            win32::run_win32_hook_and_tray_thread();
        }
    });
    
    // 3. Launch Tauri app
    tauri::Builder::default()
        .setup(|app| {
            // Apply Mica vibrancy effect in Windows 11
            #[cfg(target_os = "windows")]
            {
                use window_vibrancy::apply_mica;
                if let Some(window) = app.get_webview_window("main") {
                    let _ = apply_mica(&window, None);
                }
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                // Intercept close button and hide the window instead
                api.prevent_close();
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
            start_meeting,
            stop_meeting,
            get_meeting_levels,
            upload_meeting,
            get_api_url
        ])
        .build(tauri::generate_context!())
        .expect("error while running tauri application")
        .run(|app_handle, event| {
            if let tauri::RunEvent::Ready = event {
                let _ = TAURI_APP.set(app_handle.clone());
            }
        });
}

pub static CURRENT_MEETING_PATH: Mutex<Option<std::path::PathBuf>> = Mutex::new(None);

#[tauri::command]
fn start_meeting(title: String) -> Result<String, String> {
    println!("Starting meeting: {}", title);
    
    let temp_dir = std::env::temp_dir();
    let filename = format!("speektome_meeting_{}.ogg", std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_secs());
    let path = temp_dir.join(filename);
    
    if let Err(e) = crate::audio::start_meeting_recording(path.clone()) {
        return Err(format!("Failed to start recording: {}", e));
    }
    
    let mut path_lock = CURRENT_MEETING_PATH.lock().unwrap();
    *path_lock = Some(path.clone());
    
    Ok(path.to_string_lossy().to_string())
}

#[tauri::command]
fn stop_meeting() -> Result<String, String> {
    println!("Stopping meeting...");
    crate::audio::stop_meeting_recording();
    
    let path_lock = CURRENT_MEETING_PATH.lock().unwrap();
    if let Some(ref path) = *path_lock {
        Ok(path.to_string_lossy().to_string())
    } else {
        Err("No active meeting recording found".to_string())
    }
}

#[tauri::command]
fn get_meeting_levels() -> (u32, u32) {
    use std::sync::atomic::Ordering;
    let mic = crate::audio::MIC_VOLUME.load(Ordering::Relaxed);
    let loopback = crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed);
    (mic, loopback)
}

#[tauri::command]
fn upload_meeting(title: String, filepath: String) -> Result<String, String> {
    let config = get_active_config().lock().unwrap();
    let token = config.session_token.as_ref().ok_or("Not logged in")?;
    crate::api::upload_meeting(token, &title, &filepath)
}


