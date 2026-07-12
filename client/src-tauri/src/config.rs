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
    println!("[CONFIG] Loading config from path: {:?}", path);
    let mut config = match std::fs::read_to_string(&path) {
        Ok(data) => {
            match serde_json::from_str::<AppConfig>(&data) {
                Ok(config) => {
                    println!("[CONFIG] Successfully loaded config from {:?}", path);
                    config
                }
                Err(e) => {
                    eprintln!("[CONFIG] Failed to parse config JSON from {:?}: {:?}", path, e);
                    AppConfig::default()
                }
            }
        }
        Err(e) => {
            eprintln!("[CONFIG] Failed to read config file from {:?}: {:?}", path, e);
            // Migration fallback (search for the template config in CWD, exe folder, or target parent folders)
            let mut search_paths = vec![std::path::PathBuf::from("speektome_config.json")];
            if let Ok(exe_path) = std::env::current_exe() {
                if let Some(exe_dir) = exe_path.parent() {
                    search_paths.push(exe_dir.join("speektome_config.json"));
                    if let Some(p1) = exe_dir.parent() {
                        search_paths.push(p1.join("speektome_config.json"));
                        if let Some(p2) = p1.parent() {
                            search_paths.push(p2.join("speektome_config.json"));
                            if let Some(p3) = p2.parent() {
                                search_paths.push(p3.join("speektome_config.json"));
                            }
                        }
                    }
                }
            }

            let mut found_config = None;
            for template_path in search_paths {
                if let Ok(data) = std::fs::read_to_string(&template_path) {
                    if let Ok(config) = serde_json::from_str::<AppConfig>(&data) {
                        println!("[CONFIG] Found fallback template config at {:?}, saving to APPDATA", template_path);
                        found_config = Some(config);
                        break;
                    }
                }
            }
            found_config.unwrap_or_else(AppConfig::default)
        }
    };

    // Load session_token from secure keyring
    if let Ok(entry) = keyring::Entry::new("SpeekToMe", "session_token") {
        if let Ok(token) = entry.get_password() {
            config.session_token = Some(token);
        }
    }
    config
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

    // Save session_token to secure keyring
    if let Ok(entry) = keyring::Entry::new("SpeekToMe", "session_token") {
        if let Some(ref token) = config.session_token {
            let _ = entry.set_password(token);
        } else {
            let _ = entry.delete_password();
        }
    }

    let path = get_config_path();
    println!("[CONFIG] Saving config to path: {:?}", path);
    
    // Create copy without the token to serialize as plain JSON on disk
    let mut config_to_save = config.clone();
    config_to_save.session_token = None;

    if let Ok(data) = serde_json::to_string_pretty(&config_to_save) {
        if let Err(e) = std::fs::write(&path, &data) {
            eprintln!("[CONFIG] Failed to write config file to {:?}: {:?}", path, e);
        } else {
            println!("[CONFIG] Successfully saved config to {:?}", path);
        }
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

