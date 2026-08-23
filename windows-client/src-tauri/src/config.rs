use serde::{Serialize, Deserialize};

fn default_pill_position_mode() -> String {
    "predefined".to_string()
}
fn default_pill_predefined_position() -> String {
    "Top Center".to_string()
}
fn default_pill_custom_x() -> i32 {
    0
}
fn default_pill_custom_y() -> i32 {
    60
}
fn default_pill_bg_color() -> u32 {
    0xFAF9FF // default to pale cream (forest theme)
}
fn default_pill_wave_color() -> u32 {
    0x120A24 // default to burgundy (forest theme)
}

fn default_transcription_mode() -> String {
    "stream".to_string()
}
fn default_hotkey() -> String {
    "Ctrl + Shift".to_string()
}
fn default_hotkey_mode() -> String {
    "hold".to_string()
}

pub const HOTKEY_CTRL: u32 = 0x01;
pub const HOTKEY_SHIFT: u32 = 0x02;
pub const HOTKEY_ALT: u32 = 0x04;
pub const HOTKEY_WIN: u32 = 0x08;

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct AppConfig {
    pub api_url: Option<String>,
    
    #[serde(default = "default_hotkey")]
    pub hotkey: String,       // "Ctrl + Shift", "Ctrl + Space", "Ctrl + Win", "Alt + Space", "Ctrl + Alt", "Shift + Space"
    
    #[serde(default = "default_hotkey_mode")]
    pub hotkey_mode: String,  // "hold" | "toggle"

    pub model: String,        // "whisper-large-v3-turbo", "whisper-large-v3"
    pub enhance: bool,        // true/false
    pub typing_mode: bool,    // true/false
    pub sound_feedback: bool, // true/false
    pub smart_spacing: bool,  // true/false
    pub custom_prompt: String,// String prompt instructions
    pub session_token: Option<String>,
    pub autostart: bool,      // true/false
    pub theme: String,        // "forest", "lavender", "mint", "classic_dark"
    
    #[serde(default = "default_transcription_mode")]
    pub transcription_mode: String, // "stream" | "batch"
    
    #[serde(default)]
    pub languages: Option<Vec<String>>,
    
    #[serde(default = "default_pill_position_mode")]
    pub pill_position_mode: String,
    
    #[serde(default = "default_pill_predefined_position")]
    pub pill_predefined_position: String,
    
    #[serde(default = "default_pill_custom_x")]
    pub pill_custom_x: i32,
    
    #[serde(default = "default_pill_custom_y")]
    pub pill_custom_y: i32,
    
    #[serde(default = "default_pill_bg_color")]
    pub pill_bg_color: u32,
    
    #[serde(default = "default_pill_wave_color")]
    pub pill_wave_color: u32,
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            api_url: Some("https://api.speekto.me".to_string()),
            hotkey: default_hotkey(),
            hotkey_mode: default_hotkey_mode(),
            model: "whisper-large-v3-turbo".to_string(),
            enhance: false,
            typing_mode: false,
            sound_feedback: true,
            smart_spacing: true,
            custom_prompt: "".to_string(),
            session_token: None,
            autostart: false,
            theme: "forest".to_string(),
            transcription_mode: default_transcription_mode(),
            languages: Some(vec!["en".to_string()]),
            pill_position_mode: default_pill_position_mode(),
            pill_predefined_position: default_pill_predefined_position(),
            pill_custom_x: default_pill_custom_x(),
            pill_custom_y: default_pill_custom_y(),
            pill_bg_color: default_pill_bg_color(),
            pill_wave_color: default_pill_wave_color(),
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
    AppConfig::default()
}

pub fn save_config(config: &AppConfig) {
    let path = get_config_path();
    if let Ok(data) = serde_json::to_string_pretty(config) {
        let _ = std::fs::write(path, data);
    }
}

pub fn parse_hotkey_combo(name: &str) -> u32 {
    let lower = name.to_lowercase();
    let mut mask: u32 = 0;
    let mut vk: u32 = 0;

    if lower.contains("ctrl") || lower.contains("control") {
        mask |= HOTKEY_CTRL;
    }
    if lower.contains("shift") {
        mask |= HOTKEY_SHIFT;
    }
    if lower.contains("alt") {
        mask |= HOTKEY_ALT;
    }
    if lower.contains("win") {
        mask |= HOTKEY_WIN;
    }
    if lower.contains("space") {
        vk = 0x20; // VK_SPACE
    }

    if mask == 0 && vk == 0 {
        // Fallback default: Ctrl + Shift
        mask = HOTKEY_CTRL | HOTKEY_SHIFT;
    }

    (vk << 16) | (mask & 0xFFFF)
}
