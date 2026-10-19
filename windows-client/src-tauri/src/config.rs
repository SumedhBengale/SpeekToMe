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
    "Ctrl + Win".to_string()
}
fn default_hotkey_mode() -> String {
    "hold".to_string()
}
fn default_command_mode() -> bool {
    true
}

pub const HOTKEY_CTRL: u32 = 0x01;
pub const HOTKEY_SHIFT: u32 = 0x02;
pub const HOTKEY_ALT: u32 = 0x04;
pub const HOTKEY_WIN: u32 = 0x08;

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct AppConfig {
    pub api_url: Option<String>,
    
    #[serde(default = "default_hotkey")]
    pub hotkey: String,       // Validated shortcut name, shared by presets and custom combinations.
    
    #[serde(default = "default_hotkey_mode")]
    pub hotkey_mode: String,  // "hold" | "toggle"

    #[serde(default = "default_command_mode")]
    pub command_mode: bool,   // true/false - AI transformation command mode

    pub model: String,        // "whisper-large-v3-turbo", "whisper-large-v3"
    pub enhance: bool,        // true/false
    pub typing_mode: bool,    // true/false
    pub sound_feedback: bool, // true/false
    pub custom_prompt: String,// String prompt instructions
    pub session_token: Option<String>,
    pub autostart: bool,      // true/false
    pub theme: String,        // "forest", "lavender", "mint", "classic_dark"
    
    #[serde(default = "default_transcription_mode")]
    pub transcription_mode: String, // "stream" | "batch"
    
    #[serde(default)]
    pub languages: Option<Vec<String>>,

    #[serde(default)]
    pub active_language: String,
    
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

impl AppConfig {
    pub fn speaking_language(&self) -> String {
        let spoken = self.languages.as_deref().unwrap_or(&[]);
        if spoken.contains(&self.active_language) && self.active_language != "auto" {
            return self.active_language.clone();
        }
        spoken.first().cloned().unwrap_or_else(|| "en".into())
    }

    pub fn normalize_speaking_language(&mut self) {
        if self.languages.as_ref().map_or(true, |languages| languages.is_empty()) {
            self.languages = Some(vec!["en".into()]);
        }
        self.active_language = self.speaking_language();
    }
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            api_url: Some("https://api.speekto.me".to_string()),
            hotkey: default_hotkey(),
            hotkey_mode: default_hotkey_mode(),
            command_mode: default_command_mode(),
            model: "whisper-large-v3-turbo".to_string(),
            enhance: true,
            typing_mode: false,
            sound_feedback: true,
            custom_prompt: "".to_string(),
            session_token: None,
            autostart: false,
            theme: "forest".to_string(),
            transcription_mode: default_transcription_mode(),
            languages: Some(vec!["en".to_string()]),
            active_language: "en".into(),
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
    #[cfg(target_os = "windows")]
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
    #[cfg(target_os = "linux")]
    {
        let config_root = std::env::var_os("XDG_CONFIG_HOME")
            .filter(|p| !p.is_empty())
            .map(std::path::PathBuf::from)
            .or_else(|| std::env::var_os("HOME").map(|home| std::path::PathBuf::from(home).join(".config")));

        if let Some(root) = config_root {
            let directory = root.join("SpeekToMe");
            let path = directory.join("config.json");

            // Older Linux builds wrote to ~/.config/SpeekToMe regardless of
            // XDG_CONFIG_HOME. Preserve those settings when migrating.
            if !path.exists() {
                if let Some(home) = std::env::var_os("HOME") {
                    let legacy = std::path::PathBuf::from(home)
                        .join(".config")
                        .join("SpeekToMe")
                        .join("config.json");
                    if legacy != path && legacy.is_file() {
                        let _ = std::fs::create_dir_all(&directory);
                        let _ = std::fs::copy(legacy, &path);
                    }
                }
            }

            let _ = std::fs::create_dir_all(&directory);
            path
        } else {
            std::path::PathBuf::from("speektome_config.json")
        }
    }
    #[cfg(not(any(target_os = "windows", target_os = "linux")))]
    {
        if let Ok(home) = std::env::var("HOME") {
            let directory = std::path::PathBuf::from(home).join(".config").join("SpeekToMe");
            let _ = std::fs::create_dir_all(&directory);
            directory.join("config.json")
        } else {
            std::path::PathBuf::from("speektome_config.json")
        }
    }
}

pub fn load_config() -> AppConfig {
    let path = get_config_path();
    if let Ok(data) = std::fs::read_to_string(&path) {
        if let Ok(mut config) = serde_json::from_str::<AppConfig>(&data) {
            config.normalize_speaking_language();
            return config;
        }
    }
    AppConfig::default()
}

pub fn save_config(config: &AppConfig) {
    let path = get_config_path();
    if let Ok(data) = serde_json::to_string_pretty(config) {
        use std::io::Write;
        let temporary = path.with_extension(format!("json.{}.tmp", uuid::Uuid::new_v4()));
        let result = (|| -> std::io::Result<()> {
            let mut options = std::fs::OpenOptions::new();
            options.write(true).create_new(true);
            #[cfg(unix)]
            { use std::os::unix::fs::OpenOptionsExt; options.mode(0o600); }
            let mut file = options.open(&temporary)?;
            file.write_all(data.as_bytes())?;
            file.sync_all()?;
            drop(file);
            crate::meeting_upload::replace_file(&temporary, &path)
        })();
        if let Err(error) = result {
            let _ = std::fs::remove_file(&temporary);
            eprintln!("[CONFIG] Could not save settings: {}", error);
        }
    }
    #[cfg(target_os = "linux")]
    crate::linux_util::sync_autostart(config.autostart);
    #[cfg(target_os = "windows")]
    if let Err(error) = crate::windows_autostart::sync(config.autostart) {
        eprintln!("[CONFIG] Could not update launch at sign-in: {}", error);
    }
}

pub fn parse_hotkey_combo(name: &str) -> u32 {
    crate::hotkey::parse(name).map(|h| h.combo).unwrap_or(HOTKEY_CTRL | HOTKEY_WIN)
}

#[cfg(test)]
mod speaking_language_tests {
    use super::*;
    #[test]
    fn old_config_defaults_to_first_spoken_language() {
        let mut json = serde_json::to_value(AppConfig::default()).unwrap();
        json.as_object_mut().unwrap().remove("active_language");
        json["languages"] = serde_json::json!(["hi", "en"]);
        let mut config: AppConfig = serde_json::from_value(json).unwrap();
        config.normalize_speaking_language();
        assert_eq!(config.active_language, "hi");
    }
    #[test]
    fn empty_saved_languages_start_with_starred_english() {
        let mut config = AppConfig::default();
        for languages in [None, Some(vec![])] {
            config.languages = languages;
            config.active_language = String::new();
            config.normalize_speaking_language();
            assert_eq!(config.languages, Some(vec!["en".into()]));
            assert_eq!(config.active_language, "en");
        }
    }
    #[test]
    fn removing_the_star_uses_the_first_remaining_language() {
        let mut config = AppConfig::default();
        config.languages = Some(vec!["fr".into(), "hi".into()]);
        config.active_language = "en".into();
        config.normalize_speaking_language();
        assert_eq!(config.active_language, "fr");
        config.active_language = "auto".into();
        config.normalize_speaking_language();
        assert_eq!(config.active_language, "fr");
    }
    #[test]
    fn removed_or_automatic_languages_fall_back_to_first_saved_language() {
        let mut config = AppConfig::default();
        config.languages = Some(vec!["en".into(), "hi".into()]);
        assert_eq!(config.speaking_language(), "en");
        config.active_language = "hi".into();
        assert_eq!(config.speaking_language(), "hi");
        config.languages = Some(vec!["en".into()]);
        assert_eq!(config.speaking_language(), "en");
        config.active_language = "auto".into();
        assert_eq!(config.speaking_language(), "en");
        let restored: AppConfig = serde_json::from_str(&serde_json::to_string(&config).unwrap()).unwrap();
        assert_eq!(restored.speaking_language(), "en");
    }
}
