use std::sync::{OnceLock, RwLock};
use std::path::PathBuf;
use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct LanguageItem {
    pub code: String,
    pub name: String,
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
        LanguageItem { code: "af".into(), name: "Afrikaans".into(), tier: None },
        LanguageItem { code: "am".into(), name: "Amharic".into(), tier: None },
        LanguageItem { code: "ar".into(), name: "Arabic".into(), tier: None },
        LanguageItem { code: "as".into(), name: "Assamese".into(), tier: None },
        LanguageItem { code: "az".into(), name: "Azerbaijani".into(), tier: None },
        LanguageItem { code: "ba".into(), name: "Bashkir".into(), tier: None },
        LanguageItem { code: "be".into(), name: "Belarusian".into(), tier: None },
        LanguageItem { code: "bg".into(), name: "Bulgarian".into(), tier: None },
        LanguageItem { code: "bn".into(), name: "Bengali".into(), tier: None },
        LanguageItem { code: "bo".into(), name: "Tibetan".into(), tier: None },
        LanguageItem { code: "br".into(), name: "Breton".into(), tier: None },
        LanguageItem { code: "bs".into(), name: "Bosnian".into(), tier: None },
        LanguageItem { code: "ca".into(), name: "Catalan".into(), tier: None },
        LanguageItem { code: "cs".into(), name: "Czech".into(), tier: None },
        LanguageItem { code: "cy".into(), name: "Welsh".into(), tier: None },
        LanguageItem { code: "da".into(), name: "Danish".into(), tier: None },
        LanguageItem { code: "de".into(), name: "German".into(), tier: None },
        LanguageItem { code: "el".into(), name: "Greek".into(), tier: None },
        LanguageItem { code: "en".into(), name: "English".into(), tier: None },
        LanguageItem { code: "es".into(), name: "Spanish".into(), tier: None },
        LanguageItem { code: "et".into(), name: "Estonian".into(), tier: None },
        LanguageItem { code: "eu".into(), name: "Basque".into(), tier: None },
        LanguageItem { code: "fa".into(), name: "Persian".into(), tier: None },
        LanguageItem { code: "fi".into(), name: "Finnish".into(), tier: None },
        LanguageItem { code: "fo".into(), name: "Faroese".into(), tier: None },
        LanguageItem { code: "fr".into(), name: "French".into(), tier: None },
        LanguageItem { code: "gl".into(), name: "Galician".into(), tier: None },
        LanguageItem { code: "gu".into(), name: "Gujarati".into(), tier: None },
        LanguageItem { code: "ha".into(), name: "Hausa".into(), tier: None },
        LanguageItem { code: "haw".into(), name: "Hawaiian".into(), tier: None },
        LanguageItem { code: "he".into(), name: "Hebrew".into(), tier: None },
        LanguageItem { code: "hi".into(), name: "Hindi".into(), tier: None },
        LanguageItem { code: "hr".into(), name: "Croatian".into(), tier: None },
        LanguageItem { code: "ht".into(), name: "Haitian Creole".into(), tier: None },
        LanguageItem { code: "hu".into(), name: "Hungarian".into(), tier: None },
        LanguageItem { code: "hy".into(), name: "Armenian".into(), tier: None },
        LanguageItem { code: "id".into(), name: "Indonesian".into(), tier: None },
        LanguageItem { code: "is".into(), name: "Icelandic".into(), tier: None },
        LanguageItem { code: "it".into(), name: "Italian".into(), tier: None },
        LanguageItem { code: "ja".into(), name: "Japanese".into(), tier: None },
        LanguageItem { code: "jw".into(), name: "Javanese".into(), tier: None },
        LanguageItem { code: "ka".into(), name: "Georgian".into(), tier: None },
        LanguageItem { code: "kk".into(), name: "Kazakh".into(), tier: None },
        LanguageItem { code: "km".into(), name: "Khmer".into(), tier: None },
        LanguageItem { code: "kn".into(), name: "Kannada".into(), tier: None },
        LanguageItem { code: "ko".into(), name: "Korean".into(), tier: None },
        LanguageItem { code: "la".into(), name: "Latin".into(), tier: None },
        LanguageItem { code: "lb".into(), name: "Luxembourgish".into(), tier: None },
        LanguageItem { code: "ln".into(), name: "Lingala".into(), tier: None },
        LanguageItem { code: "lo".into(), name: "Lao".into(), tier: None },
        LanguageItem { code: "lt".into(), name: "Lithuanian".into(), tier: None },
        LanguageItem { code: "lv".into(), name: "Latvian".into(), tier: None },
        LanguageItem { code: "mg".into(), name: "Malagasy".into(), tier: None },
        LanguageItem { code: "mi".into(), name: "Maori".into(), tier: None },
        LanguageItem { code: "mk".into(), name: "Macedonian".into(), tier: None },
        LanguageItem { code: "ml".into(), name: "Malayalam".into(), tier: None },
        LanguageItem { code: "mn".into(), name: "Mongolian".into(), tier: None },
        LanguageItem { code: "mr".into(), name: "Marathi".into(), tier: None },
        LanguageItem { code: "ms".into(), name: "Malay".into(), tier: None },
        LanguageItem { code: "mt".into(), name: "Maltese".into(), tier: None },
        LanguageItem { code: "my".into(), name: "Burmese".into(), tier: None },
        LanguageItem { code: "ne".into(), name: "Nepali".into(), tier: None },
        LanguageItem { code: "nl".into(), name: "Dutch".into(), tier: None },
        LanguageItem { code: "nn".into(), name: "Nynorsk".into(), tier: None },
        LanguageItem { code: "no".into(), name: "Norwegian".into(), tier: None },
        LanguageItem { code: "oc".into(), name: "Occitan".into(), tier: None },
        LanguageItem { code: "pa".into(), name: "Punjabi".into(), tier: None },
        LanguageItem { code: "pl".into(), name: "Polish".into(), tier: None },
        LanguageItem { code: "ps".into(), name: "Pashto".into(), tier: None },
        LanguageItem { code: "pt".into(), name: "Portuguese".into(), tier: None },
        LanguageItem { code: "ro".into(), name: "Romanian".into(), tier: None },
        LanguageItem { code: "ru".into(), name: "Russian".into(), tier: None },
        LanguageItem { code: "sa".into(), name: "Sanskrit".into(), tier: None },
        LanguageItem { code: "sd".into(), name: "Sindhi".into(), tier: None },
        LanguageItem { code: "si".into(), name: "Sinhala".into(), tier: None },
        LanguageItem { code: "sk".into(), name: "Slovak".into(), tier: None },
        LanguageItem { code: "sl".into(), name: "Slovenian".into(), tier: None },
        LanguageItem { code: "sn".into(), name: "Shona".into(), tier: None },
        LanguageItem { code: "so".into(), name: "Somali".into(), tier: None },
        LanguageItem { code: "sq".into(), name: "Albanian".into(), tier: None },
        LanguageItem { code: "sr".into(), name: "Serbian".into(), tier: None },
        LanguageItem { code: "su".into(), name: "Sundanese".into(), tier: None },
        LanguageItem { code: "sv".into(), name: "Swedish".into(), tier: None },
        LanguageItem { code: "sw".into(), name: "Swahili".into(), tier: None },
        LanguageItem { code: "ta".into(), name: "Tamil".into(), tier: None },
        LanguageItem { code: "te".into(), name: "Telugu".into(), tier: None },
        LanguageItem { code: "tg".into(), name: "Tajik".into(), tier: None },
        LanguageItem { code: "th".into(), name: "Thai".into(), tier: None },
        LanguageItem { code: "tk".into(), name: "Turkmen".into(), tier: None },
        LanguageItem { code: "tl".into(), name: "Tagalog".into(), tier: None },
        LanguageItem { code: "tr".into(), name: "Turkish".into(), tier: None },
        LanguageItem { code: "tt".into(), name: "Tatar".into(), tier: None },
        LanguageItem { code: "uk".into(), name: "Ukrainian".into(), tier: None },
        LanguageItem { code: "ur".into(), name: "Urdu".into(), tier: None },
        LanguageItem { code: "uz".into(), name: "Uzbek".into(), tier: None },
        LanguageItem { code: "vi".into(), name: "Vietnamese".into(), tier: None },
        LanguageItem { code: "yi".into(), name: "Yiddish".into(), tier: None },
        LanguageItem { code: "yo".into(), name: "Yoruba".into(), tier: None },
        LanguageItem { code: "yue".into(), name: "Cantonese".into(), tier: None },
        LanguageItem { code: "zh".into(), name: "Chinese".into(), tier: None },
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
        self.languages.read().unwrap_or_else(|e| e.into_inner()).clone()
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
                    println!("[LANGUAGES] Loaded {} languages from local cache: {:?}", lock.len(), path);
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

    pub async fn sync_from_server(&self, api_url: &str) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
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
                    println!("[LANGUAGES] Successfully synced {} languages from server.", self.get_languages().len());
                    return Ok(());
                }
            } else if let Ok(langs) = serde_json::from_str::<Vec<LanguageItem>>(&body) {
                if !langs.is_empty() {
                    self.set_languages(langs);
                    println!("[LANGUAGES] Successfully synced {} languages from server.", self.get_languages().len());
                    return Ok(());
                }
            }
        }
        Ok(())
    }
}
