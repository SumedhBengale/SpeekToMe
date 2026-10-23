use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::sync::{OnceLock, RwLock};

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct LanguageItem {
    pub code: String,
    pub name: String,
    #[serde(default)]
    pub locale: Option<String>,
    #[serde(default)]
    pub tier: Option<String>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct LanguagesResponse {
    pub languages: Vec<LanguageItem>,
    #[serde(default)]
    pub model_support: std::collections::HashMap<String, Vec<String>>,
}

pub struct LanguageManager {
    languages: RwLock<Vec<LanguageItem>>,
}

static GLOBAL_LANGUAGE_MANAGER: OnceLock<LanguageManager> = OnceLock::new();

pub fn get_language_manager() -> &'static LanguageManager {
    GLOBAL_LANGUAGE_MANAGER.get_or_init(LanguageManager::new)
}

fn get_cache_path() -> PathBuf {
    if let Ok(app_data) = std::env::var("APPDATA") {
        let mut p = PathBuf::from(app_data);
        p.push("SpeekToMe");
        let _ = std::fs::create_dir_all(&p);
        p.push("languages_cache.json");
        p
    } else {
        PathBuf::from("languages_cache.json")
    }
}

fn default_languages() -> Vec<LanguageItem> {
    vec![
        LanguageItem {
            code: "en".into(),
            name: "English".into(),
            locale: Some("en-US".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "es".into(),
            name: "Spanish".into(),
            locale: Some("es-ES".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "de".into(),
            name: "German".into(),
            locale: Some("de-DE".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "fr".into(),
            name: "French".into(),
            locale: Some("fr-FR".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "it".into(),
            name: "Italian".into(),
            locale: Some("it-IT".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "pt".into(),
            name: "Portuguese".into(),
            locale: Some("pt-BR".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "nl".into(),
            name: "Dutch".into(),
            locale: Some("nl-NL".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "ru".into(),
            name: "Russian".into(),
            locale: Some("ru-RU".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "tr".into(),
            name: "Turkish".into(),
            locale: Some("tr-TR".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "ar".into(),
            name: "Arabic".into(),
            locale: Some("ar-AR".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "hi".into(),
            name: "Hindi".into(),
            locale: Some("hi-IN".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "ja".into(),
            name: "Japanese".into(),
            locale: Some("ja-JP".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "ko".into(),
            name: "Korean".into(),
            locale: Some("ko-KR".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "vi".into(),
            name: "Vietnamese".into(),
            locale: Some("vi-VN".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "uk".into(),
            name: "Ukrainian".into(),
            locale: Some("uk-UA".into()),
            tier: Some("transcription-ready".into()),
        },
        LanguageItem {
            code: "zh".into(),
            name: "Chinese".into(),
            locale: Some("zh-CN".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "pl".into(),
            name: "Polish".into(),
            locale: Some("pl-PL".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "sv".into(),
            name: "Swedish".into(),
            locale: Some("sv-SE".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "cs".into(),
            name: "Czech".into(),
            locale: Some("cs-CZ".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "no".into(),
            name: "Norwegian".into(),
            locale: Some("nb-NO".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "da".into(),
            name: "Danish".into(),
            locale: Some("da-DK".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "bg".into(),
            name: "Bulgarian".into(),
            locale: Some("bg-BG".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "fi".into(),
            name: "Finnish".into(),
            locale: Some("fi-FI".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "hr".into(),
            name: "Croatian".into(),
            locale: Some("hr-HR".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "sk".into(),
            name: "Slovak".into(),
            locale: Some("sk-SK".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "hu".into(),
            name: "Hungarian".into(),
            locale: Some("hu-HU".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "ro".into(),
            name: "Romanian".into(),
            locale: Some("ro-RO".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "et".into(),
            name: "Estonian".into(),
            locale: Some("et-EE".into()),
            tier: Some("broad-coverage".into()),
        },
        LanguageItem {
            code: "el".into(),
            name: "Greek".into(),
            locale: Some("el-GR".into()),
            tier: Some("adaptation-ready".into()),
        },
        LanguageItem {
            code: "lt".into(),
            name: "Lithuanian".into(),
            locale: Some("lt-LT".into()),
            tier: Some("adaptation-ready".into()),
        },
        LanguageItem {
            code: "lv".into(),
            name: "Latvian".into(),
            locale: Some("lv-LV".into()),
            tier: Some("adaptation-ready".into()),
        },
        LanguageItem {
            code: "mt".into(),
            name: "Maltese".into(),
            locale: Some("mt-MT".into()),
            tier: Some("adaptation-ready".into()),
        },
        LanguageItem {
            code: "sl".into(),
            name: "Slovenian".into(),
            locale: Some("sl-SI".into()),
            tier: Some("adaptation-ready".into()),
        },
        LanguageItem {
            code: "he".into(),
            name: "Hebrew".into(),
            locale: Some("he-IL".into()),
            tier: Some("adaptation-ready".into()),
        },
        LanguageItem {
            code: "th".into(),
            name: "Thai".into(),
            locale: Some("th-TH".into()),
            tier: Some("adaptation-ready".into()),
        },
    ]
}

impl LanguageManager {
    pub fn new() -> Self {
        let manager = Self {
            languages: RwLock::new(default_languages()),
        };
        manager.load_from_local_cache();
        manager
    }

    pub fn get_languages(&self) -> Vec<LanguageItem> {
        self.languages
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }

    pub fn set_languages(&self, langs: Vec<LanguageItem>) {
        if langs.is_empty() {
            return;
        }
        {
            let mut lock = self.languages.write().unwrap_or_else(|e| e.into_inner());
            *lock = langs;
        }
        self.save_to_local_cache();
    }

    pub fn load_from_local_cache(&self) {
        let path = get_cache_path();
        if let Ok(content) = std::fs::read_to_string(&path) {
            if let Ok(cached) = serde_json::from_str::<Vec<LanguageItem>>(&content) {
                if !cached.is_empty() {
                    let mut lock = self.languages.write().unwrap_or_else(|e| e.into_inner());
                    *lock = cached;
                    println!(
                        "[LANGUAGES] Loaded {} languages from local cache: {:?}",
                        lock.len(),
                        path
                    );
                }
            }
        }
    }

    pub fn save_to_local_cache(&self) {
        let path = get_cache_path();
        let langs = self.get_languages();
        if let Ok(json) = serde_json::to_string_pretty(&langs) {
            let _ = std::fs::write(&path, json);
        }
    }

    pub async fn sync_from_server(
        &self,
        api_url: &str,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        let url = format!("{}/v1/languages", api_url.trim_end_matches('/'));
        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(5))
            .build()?;

        let resp = client.get(&url).send().await?;
        if resp.status().is_success() {
            let body = resp.text().await?;
            if let Ok(res) = serde_json::from_str::<LanguagesResponse>(&body) {
                if !res.languages.is_empty() {
                    self.set_languages(res.languages);
                    println!(
                        "[LANGUAGES] Successfully synced {} languages from server.",
                        self.get_languages().len()
                    );
                    return Ok(());
                }
            } else if let Ok(langs) = serde_json::from_str::<Vec<LanguageItem>>(&body) {
                if !langs.is_empty() {
                    self.set_languages(langs);
                    println!(
                        "[LANGUAGES] Successfully synced {} languages from server.",
                        self.get_languages().len()
                    );
                    return Ok(());
                }
            }
        }
        Ok(())
    }
}
