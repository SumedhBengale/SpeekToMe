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
    "batch".to_string()
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

    pub typing_mode: bool,    // true/false
    pub sound_feedback: bool, // true/false
    pub custom_prompt: String,// String prompt instructions
    pub session_token: Option<String>,
    #[serde(default)]
    pub onboarding_version: u32,
    #[serde(default)]
    pub onboarding_step: u8,
    #[serde(default)]
    pub onboarding_pro_defaults_applied: bool,
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
            typing_mode: false,
            sound_feedback: true,
            custom_prompt: "".to_string(),
            session_token: None,
            onboarding_version: 0,
            onboarding_step: 1,
            onboarding_pro_defaults_applied: false,
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
    #[cfg(target_os = "macos")]
    {
        let root = std::env::var_os("HOME").map(std::path::PathBuf::from).unwrap_or_default();
        let directory = root.join("Library/Application Support/SpeekToMe");
        let _ = std::fs::create_dir_all(&directory);
        let path = directory.join("config.json");
        let legacy = root.join(".config/SpeekToMe/config.json");
        if !path.exists() && legacy.is_file() { let _ = std::fs::copy(legacy, &path); }
        path
    }
    #[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
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

fn decode_config(data: &str) -> Option<AppConfig> {
    let mut config = serde_json::from_str::<AppConfig>(data).ok()?;
    if serde_json::from_str::<serde_json::Value>(data)
        .ok()?
        .get("onboarding_version")
        .is_none()
        && config.session_token.is_some()
    {
        config.onboarding_version = 1;
    }
    config.normalize_speaking_language();
    Some(config)
}
pub fn load_config() -> AppConfig {
    std::fs::read_to_string(get_config_path())
        .ok()
        .and_then(|data| decode_config(&data))
        .unwrap_or_default()
}
pub fn save_config(config: &AppConfig) {
    if let Err(error) = try_save_config(config) {
        eprintln!("[CONFIG] {}", error);
    }
}

pub fn try_save_config(config: &AppConfig) -> Result<(), String> {
    let path = get_config_path();
    let data = serde_json::to_string_pretty(config).map_err(|_| "Settings could not be encoded".to_string())?;
    {
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
        if result.is_err() {
            let _ = std::fs::remove_file(&temporary);
            return Err("Settings could not be saved. Check available disk space and profile permissions.".into());
        }
    }
    #[cfg(target_os = "linux")]
    crate::linux_util::sync_autostart(config.autostart);
    #[cfg(target_os = "macos")]
    crate::macos_desktop::sync_autostart(config.autostart);
    #[cfg(target_os = "windows")]
    if let Err(error) = crate::windows_autostart::sync(config.autostart) {
        eprintln!("[CONFIG] Could not update launch at sign-in: {}", error);
    }
    Ok(())
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

#[cfg(test)]
mod onboarding_tests {
    use super::*;
    #[test]
    fn existing_accounts_do_not_repeat_setup() {
        let mut legacy = serde_json::to_value(AppConfig::default()).unwrap();
        legacy.as_object_mut().unwrap().remove("onboarding_version");
        legacy["session_token"] = serde_json::json!("test-session");
        assert_eq!(
            decode_config(&legacy.to_string())
                .unwrap()
                .onboarding_version,
            1
        );
    }
    #[test]
    fn new_accounts_resume_without_implicitly_finishing() {
        let mut cfg = AppConfig::default();
        cfg.session_token = Some("test-session".into());
        cfg.onboarding_step = 7;
        let restored = decode_config(&serde_json::to_string(&cfg).unwrap()).unwrap();
        assert_eq!(restored.onboarding_version, 0);
        assert_eq!(restored.onboarding_step, 7);
    }
}
