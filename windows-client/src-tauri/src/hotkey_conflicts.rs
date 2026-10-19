//! Sourced reservation policy plus native, bounded global availability checks.
use crate::hotkey;
use serde::{Deserialize, Serialize};
use std::{collections::HashMap, sync::OnceLock};

#[derive(Deserialize)]
struct Catalog {
    version: u32,
    sources: HashMap<String, String>,
    entries: Vec<Entry>,
}
#[derive(Deserialize)]
struct Entry {
    platforms: Vec<String>,
    combination: String,
    action: String,
    source: String,
}
#[derive(Debug, Serialize, Clone)]
pub struct Conflict {
    pub action: String,
    pub source: String,
}
#[derive(Debug, Serialize)]
pub struct Report {
    pub canonical: String,
    pub conflicts: Vec<Conflict>,
    pub system_check: String,
    pub detail: String,
}
impl Report {
    pub fn ensure_allowed(&self) -> Result<(), String> {
        if self.conflicts.is_empty() {
            return Ok(());
        }
        Err(format!(
            "{} is reserved for {}. Choose another shortcut.",
            display_name(&self.canonical),
            self.conflicts
                .iter()
                .take(3)
                .map(|c| c.action.as_str())
                .collect::<Vec<_>>()
                .join("; ")
        ))
    }
}
pub fn display_name(name: &str) -> String {
    name.replace(
        "Win",
        if cfg!(target_os = "macos") {
            "Command"
        } else if cfg!(target_os = "linux") {
            "Meta"
        } else {
            "Windows"
        },
    )
}
fn catalog() -> &'static Catalog {
    static CATALOG: OnceLock<Catalog> = OnceLock::new();
    CATALOG.get_or_init(|| {
        serde_json::from_str(include_str!("../shortcut-reservations.json"))
            .expect("Bundled shortcut catalog must be valid")
    })
}
pub fn platform() -> &'static str {
    if cfg!(target_os = "windows") {
        "windows"
    } else if cfg!(target_os = "macos") {
        "macos"
    } else {
        "linux"
    }
}
pub fn reserved(value: &str, platform: &str) -> Result<Vec<Conflict>, String> {
    let combo = hotkey::parse(value)?.combo;
    let c = catalog();
    debug_assert_eq!(c.version, 1);
    Ok(c.entries
        .iter()
        .filter(|e| {
            e.platforms.iter().any(|p| p == platform)
                && hotkey::parse(&e.combination).is_ok_and(|h| h.combo == combo)
        })
        .map(|e| Conflict {
            action: e.action.clone(),
            source: c.sources[&e.source].clone(),
        })
        .collect())
}
pub async fn check(value: &str) -> Result<Report, String> {
    let hotkey = hotkey::parse(value)?;
    let mut conflicts = reserved(value, platform())?;
    // A catalog refusal does not need an OS registration probe.
    let (system_check, detail) = if !conflicts.is_empty() {
        (
            "not_needed".into(),
            "Reserved by the standard shortcut catalog.".into(),
        )
    } else {
        let result = tokio::time::timeout(
            std::time::Duration::from_secs(2),
            native_check(hotkey.combo),
        )
        .await
        .unwrap_or_else(|_| {
            Err("The global shortcut check timed out. Availability is unverified.".into())
        });
        match result {
            Ok(found) => {
                let status = if found.is_empty() {
                    "available"
                } else {
                    "occupied"
                };
                conflicts.extend(found);
                (
                    status.into(),
                    "Native global shortcut check completed.".into(),
                )
            }
            Err(reason) => ("unknown".into(), reason),
        }
    };
    Ok(Report {
        canonical: hotkey.name,
        conflicts,
        system_check,
        detail,
    })
}
async fn native_check(combo: u32) -> Result<Vec<Conflict>, String> {
    #[cfg(target_os = "linux")]
    {
        return crate::hotkey_linux::check(combo).await;
    }
    #[cfg(target_os = "windows")]
    {
        return tokio::task::spawn_blocking(move || windows_check(combo))
            .await
            .map_err(|_| "Windows shortcut check failed.".to_string())?;
    }
    #[cfg(target_os = "macos")]
    {
        return tokio::task::spawn_blocking(move || crate::hotkey_macos::check(combo))
            .await
            .map_err(|_| "macOS shortcut check failed.".to_string())?;
    }
    #[allow(unreachable_code)]
    Err("This desktop does not expose a supported global shortcut check.".into())
}
#[cfg(target_os = "windows")]
fn windows_check(combo: u32) -> Result<Vec<Conflict>, String> {
    use windows_sys::Win32::{
        Foundation::{GetLastError, ERROR_HOTKEY_ALREADY_REGISTERED},
        UI::Input::KeyboardAndMouse::{
            RegisterHotKey, UnregisterHotKey, MOD_ALT, MOD_CONTROL, MOD_NOREPEAT, MOD_SHIFT,
            MOD_WIN,
        },
    };
    let key = combo >> 16;
    if key == 0 || key == 0xA5 {
        return Err(
            "Windows cannot check modifier-only shortcuts. Standard reservations still apply."
                .into(),
        );
    }
    let mask = combo & 0xFFFF;
    let modifiers = MOD_NOREPEAT
        | if mask & 1 != 0 { MOD_CONTROL } else { 0 }
        | if mask & 2 != 0 { MOD_SHIFT } else { 0 }
        | if mask & 4 != 0 { MOD_ALT } else { 0 }
        | if mask & 8 != 0 { MOD_WIN } else { 0 };
    // This runs on a dedicated blocking task thread. No event loop is pumped;
    // successful registrations are immediately released, including on unwind.
    struct Registration;
    impl Drop for Registration {
        fn drop(&mut self) {
            unsafe {
                UnregisterHotKey(std::ptr::null_mut(), 0x5354);
            }
        }
    }
    unsafe {
        if RegisterHotKey(std::ptr::null_mut(), 0x5354, modifiers, key) != 0 {
            let _release = Registration;
            Ok(Vec::new())
        } else if GetLastError() == ERROR_HOTKEY_ALREADY_REGISTERED {
            Ok(vec![Conflict {
                action: "A registered Windows global shortcut".into(),
                source: "Windows RegisterHotKey".into(),
            }])
        } else {
            Err("Windows could not verify this shortcut. Try another combination.".into())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn catalog_is_valid_sourced_and_platform_specific() {
        for e in &catalog().entries {
            assert!(hotkey::parse(&e.combination).is_ok(), "{}", e.combination);
            assert!(catalog()
                .sources
                .get(&e.source)
                .unwrap()
                .starts_with("https://"));
            assert!(e
                .platforms
                .iter()
                .all(|p| ["windows", "linux", "macos"].contains(&p.as_str())));
        }
        assert!(!reserved("Ctrl + A", "windows").unwrap().is_empty());
        assert!(!reserved("Ctrl + A", "linux").unwrap().is_empty());
        assert!(!reserved("Command + A", "macos").unwrap().is_empty());
        assert!(reserved("Ctrl + Alt + A", "linux").unwrap().is_empty());
        assert!(reserved("Win + L", "linux").unwrap().is_empty());
        assert!(!reserved("Win + L", "windows").unwrap().is_empty());
        assert!(reserved("Ctrl + Alt + K", "macos").unwrap().is_empty());
    }
    #[tokio::test]
    async fn recorder_command_enforces_the_same_native_policy_as_save() {
        let result = crate::validate_hotkey("Ctrl + A".into()).await;
        assert!(result.unwrap_err().contains("Select All"));
    }
    #[test]
    fn native_save_validation_rejects_conflicts_but_unknown_is_not_claimed_free() {
        let report = Report {
            canonical: "Ctrl + A".into(),
            conflicts: reserved("Ctrl + A", "linux").unwrap(),
            system_check: "not_needed".into(),
            detail: String::new(),
        };
        assert!(report.ensure_allowed().unwrap_err().contains("Select All"));
        let report = Report {
            canonical: "Ctrl + Alt + K".into(),
            conflicts: Vec::new(),
            system_check: "unknown".into(),
            detail: "Unsupported desktop".into(),
        };
        assert!(report.ensure_allowed().is_ok());
        assert_eq!(report.system_check, "unknown");
    }
}
