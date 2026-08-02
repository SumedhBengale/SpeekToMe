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

#[derive(Serialize, Deserialize, Clone, Debug)]
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

pub fn hotkey_name_to_vk(name: &str) -> u32 {
    match name {
        "Left Control" => 0xA2,
        "Caps Lock" => 0x14,
        "Right Alt" => 0xA5,
        "Left Alt" => 0xA4,
        "Space" => 0x20,
        _ => 0xA3, // Right Control
    }
}
