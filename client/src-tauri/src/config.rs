use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Clone)]
pub struct AppConfig {
    pub api_url: Option<String>,
    pub hotkey: String,       // "Right Control", "Left Control", "Caps Lock", "Right Alt", "Left Alt", "Space"
    pub model: String,        // "whisper-large-v3-turbo", "whisper-large-v3"
    pub enhance: bool,        // true/false
    pub typing_mode: bool,    // true/false
    pub sound_feedback: bool, // true/false
    pub smart_spacing: bool,  // true/false
    pub custom_prompt: String,// String prompt instructions
    pub session_token: Option<String>,
    pub autostart: bool,      // true/false
    pub theme: String,        // "forest", "lavender", "mint", "classic_dark"
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            api_url: Some("https://api.speekto.me".to_string()),
            hotkey: "Right Control".to_string(),
            model: "whisper-large-v3-turbo".to_string(),
            enhance: false,
            typing_mode: false,
            sound_feedback: true,
            smart_spacing: true,
            custom_prompt: "".to_string(),
            session_token: None,
            autostart: false,
            theme: "forest".to_string(),
        }
    }
}

pub fn get_config_path() -> std::path::PathBuf {
    if let Ok(app_data) = std::env::var("APPDATA") {
        let mut path = std::path::PathBuf::from(app_data);
        path.push("SpeekToMe");
        let _ = std::fs::create_dir_all(&path);
        path.push("config.json");
        path
    } else if let Ok(home) = std::env::var("USERPROFILE").or_else(|_| std::env::var("HOME")) {
        let mut path = std::path::PathBuf::from(home);
        path.push(".config");
        path.push("SpeekToMe");
        let _ = std::fs::create_dir_all(&path);
        path.push("config.json");
        path
    } else {
        std::path::PathBuf::from("speektome_config.json")
    }
}

pub fn load_config() -> AppConfig {
    let path = get_config_path();
    if let Ok(data) = std::fs::read_to_string(&path) {
        if let Ok(config) = serde_json::from_str::<AppConfig>(&data) {
            return config;
        }
    }
    // Migration fallback (if config exists in current directory, load and move it to AppData)
    if let Ok(data) = std::fs::read_to_string("speektome_config.json") {
        if let Ok(config) = serde_json::from_str::<AppConfig>(&data) {
            save_config(&config);
            return config;
        }
    }
    AppConfig::default()
}

pub fn apply_autostart(enabled: bool) {
    use winreg::enums::{HKEY_CURRENT_USER, KEY_WRITE};
    use winreg::RegKey;
    
    let hkcu = RegKey::predef(HKEY_CURRENT_USER);
    if let Ok(run_key) = hkcu.open_subkey_with_flags(
        r"Software\Microsoft\Windows\CurrentVersion\Run",
        KEY_WRITE,
    ) {
        if enabled {
            if let Ok(exe_path) = std::env::current_exe() {
                let _ = run_key.set_value("SpeekTo.Me", &exe_path.to_string_lossy().as_ref());
            }
        } else {
            let _ = run_key.delete_value("SpeekTo.Me");
        }
    }
}

pub fn save_config(config: &AppConfig) {
    apply_autostart(config.autostart);
    if let Ok(data) = serde_json::to_string_pretty(config) {
        let _ = std::fs::write(get_config_path(), data);
    }
}

pub fn hotkey_name_to_vk(name: &str) -> u32 {
    match name {
        "Left Control" => 0xA2,
        "Caps Lock" => 0x14,
        "Right Alt" => 0xA5,
        "Left Alt" => 0xA4,
        "Space" => 0x20,
        _ => 0xA3, // Default to Right Control
    }
}

