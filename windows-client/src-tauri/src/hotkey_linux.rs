use crate::{hotkey, hotkey_conflicts::Conflict};
use std::time::Duration;

pub async fn check(combo: u32) -> Result<Vec<Conflict>, String> {
    let desktop = std::env::var("XDG_CURRENT_DESKTOP")
        .unwrap_or_default()
        .to_ascii_lowercase();
    if desktop.contains("kde") || desktop.contains("plasma") {
        tokio::time::timeout(Duration::from_secs(2), kde_check(combo))
            .await
            .map_err(|_| {
                "The KDE shortcut manager did not respond. Global availability is unverified."
                    .to_string()
            })?
    } else if desktop.contains("gnome") || desktop.contains("unity") {
        tokio::task::spawn_blocking(move || gnome_check(combo))
            .await
            .map_err(|_| "GNOME shortcut lookup failed.".to_string())?
    } else {
        Err("Live global shortcut checking is unavailable on this desktop. Standard reservations still apply.".into())
    }
}
fn qt_key(combo: u32) -> Option<i32> {
    let mut mask = combo & 0xFFFF;
    let vk = combo >> 16;
    let key = match vk {
        0 => {
            let (key, bit) = match mask {
                3 => (0x01000020, 2),
                5 => (0x01000023, 4),
                9 => (0x01000022, 8),
                _ => return None,
            };
            mask &= !bit;
            key
        }
        0x30..=0x39 | 0x41..=0x5A | 0x20 => vk,
        0x70..=0x87 => 0x01000030 + vk - 0x70,
        0x08 => 0x01000003,
        0x09 => 0x01000001,
        0x0D => 0x01000004,
        0x1B => 0x01000000,
        0x21 => 0x01000016,
        0x22 => 0x01000017,
        0x23 => 0x01000011,
        0x24 => 0x01000010,
        0x25 => 0x01000012,
        0x26 => 0x01000013,
        0x27 => 0x01000014,
        0x28 => 0x01000015,
        0x2D => 0x01000006,
        0x2E => 0x01000007,
        0x14 => 0x01000024,
        0xA5 => 0x01000023,
        _ => return None,
    };
    Some(
        (key | if mask & 1 != 0 { 0x04000000 } else { 0 }
            | if mask & 2 != 0 { 0x02000000 } else { 0 }
            | if mask & 4 != 0 { 0x08000000 } else { 0 }
            | if mask & 8 != 0 { 0x10000000 } else { 0 }) as i32,
    )
}
async fn kde_check(combo: u32) -> Result<Vec<Conflict>, String> {
    use ashpd::zbus::{Connection, Proxy};
    let key = qt_key(combo).ok_or("KDE cannot represent this shortcut.")?;
    let connection = Connection::session()
        .await
        .map_err(|_| "Could not connect to the desktop session bus.")?;
    let proxy = Proxy::new(
        &connection,
        "org.kde.kglobalaccel",
        "/kglobalaccel",
        "org.kde.KGlobalAccel",
    )
    .await
    .map_err(|_| "KDE shortcut manager is unavailable.")?;
    let available: bool = proxy
        .call(
            "globalShortcutAvailable",
            &((vec![key],), "me.speekto.SpeekToMe"),
        )
        .await
        .map_err(|_| "KDE could not verify global shortcut availability.")?;
    if available {
        return Ok(Vec::new());
    }
    let mut infos = Vec::new();
    // Availability includes our remembered bindings on some KDE versions,
    // despite the component argument. Check exact and shadowing owners before
    // rejecting; our own entries must not disable every previously used preset.
    for match_type in 0..=2i32 {
        let matches: Vec<KdeInfo> = proxy
            .call("globalShortcutsByKey", &((vec![key],), (match_type,)))
            .await
            .map_err(|_| "KDE could not determine shortcut ownership.")?;
        infos.extend(matches);
    }
    Ok(kde_owners(infos))
}
type KdeInfo = (
    String,
    String,
    String,
    String,
    String,
    String,
    Vec<i32>,
    Vec<i32>,
);
fn kde_owners(infos: Vec<KdeInfo>) -> Vec<Conflict> {
    if infos.is_empty() {
        return vec![Conflict {
            action: "A registered KDE global shortcut".into(),
            source: "KDE KGlobalAccel live registry".into(),
        }];
    }
    let mut conflicts: Vec<Conflict> = infos
        .into_iter()
        .filter(|i| i.2 != "me.speekto.SpeekToMe")
        .map(|i| Conflict {
            action: format!("{} ({})", i.1, i.3),
            source: "KDE KGlobalAccel live registry".into(),
        })
        .collect();
    conflicts.sort_by(|a, b| a.action.cmp(&b.action));
    conflicts.dedup_by(|a, b| a.action == b.action);
    conflicts
}

fn accelerator(value: &str) -> Option<u32> {
    let mut value = value.trim().to_string();
    for (token, name) in [
        ("<Primary>", "Ctrl + "),
        ("<Control>", "Ctrl + "),
        ("<Ctrl>", "Ctrl + "),
        ("<Shift>", "Shift + "),
        ("<Alt>", "Alt + "),
        ("<Super>", "Win + "),
        ("<Meta>", "Win + "),
    ] {
        value = value.replace(token, name);
    }
    value = value
        .replace("BackSpace", "Backspace")
        .replace("Page_Up", "Page Up")
        .replace("Page_Down", "Page Down")
        .replace("ISO_Left_Tab", "Shift + Tab");
    hotkey::parse(&value).ok().map(|h| h.combo)
}
fn gnome_check(combo: u32) -> Result<Vec<Conflict>, String> {
    use gtk::gio::{prelude::*, Settings, SettingsSchemaSource};
    let source = SettingsSchemaSource::default().ok_or("GNOME settings are unavailable.")?;
    let (schemas, _) = source.list_schemas(true);
    let mut found = Vec::new();
    let mut checked = 0;
    for id in schemas.iter().filter(|id| {
        id.starts_with("org.gnome.") && (id.contains("keybindings") || id.ends_with(".media-keys"))
    }) {
        let Some(schema) = source.lookup(id, true) else {
            continue;
        };
        let settings = Settings::new_full(&schema, None::<&gtk::gio::SettingsBackend>, None);
        checked += 1;
        for key in schema.list_keys() {
            if let Some(bindings) = settings.value(&key).get::<Vec<String>>() {
                if bindings.iter().any(|s| accelerator(s) == Some(combo)) {
                    let summary = schema
                        .key(&key)
                        .summary()
                        .map(|s| s.to_string())
                        .unwrap_or_else(|| key.to_string());
                    found.push(Conflict {
                        action: summary,
                        source: format!("GNOME configured binding: {id}"),
                    });
                }
                if key == "custom-keybindings" {
                    if let Some(custom) = source.lookup(
                        "org.gnome.settings-daemon.plugins.media-keys.custom-keybinding",
                        true,
                    ) {
                        if !custom.has_key("binding") || !custom.has_key("name") {
                            continue;
                        }
                        for path in bindings
                            .iter()
                            .filter(|p| p.starts_with('/') && p.ends_with('/'))
                        {
                            let custom_settings = Settings::new_full(
                                &custom,
                                None::<&gtk::gio::SettingsBackend>,
                                Some(path),
                            );
                            if accelerator(&custom_settings.string("binding")) == Some(combo) {
                                found.push(Conflict {
                                    action: custom_settings.string("name").to_string(),
                                    source: "GNOME custom global shortcut".into(),
                                });
                            }
                        }
                    }
                }
            } else if let Some(binding) = settings.value(&key).get::<String>() {
                if accelerator(&binding) == Some(combo) {
                    found.push(Conflict {
                        action: key.to_string(),
                        source: format!("GNOME configured binding: {id}"),
                    });
                }
            }
        }
    }
    if checked == 0 {
        Err("No GNOME shortcut schemas are installed. Global availability is unverified.".into())
    } else {
        Ok(found)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn qt_modifier_function_and_navigation_encoding() {
        assert_eq!(
            qt_key(hotkey::parse("Ctrl + F9").unwrap().combo),
            Some(83886136)
        );
        assert_eq!(
            qt_key(hotkey::parse("Ctrl + Shift").unwrap().combo),
            Some(0x05000020)
        );
        assert_eq!(
            qt_key(hotkey::parse("Ctrl + Win + K").unwrap().combo),
            Some(0x1400004B)
        );
        assert_eq!(
            qt_key(hotkey::parse("Shift + Left").unwrap().combo),
            Some(0x03000012)
        );
    }
    #[test]
    fn kde_current_and_historical_own_grants_do_not_block_selection() {
        fn info(component: &str, action: &str) -> KdeInfo {
            (
                "id".into(),
                action.into(),
                component.into(),
                component.into(),
                "default".into(),
                "Default".into(),
                vec![],
                vec![],
            )
        }
        assert!(kde_owners(vec![info("me.speekto.SpeekToMe", "Dictation")]).is_empty());
        let conflicts = kde_owners(vec![
            info("me.speekto.SpeekToMe", "Dictation"),
            info("kwin", "Window switch"),
        ]);
        assert_eq!(conflicts.len(), 1);
        assert!(conflicts[0].action.contains("Window switch"));
        assert_eq!(kde_owners(Vec::new()).len(), 1);
    }
    #[test]
    fn gnome_accelerators_match_aliases_and_exact_modifiers() {
        assert_eq!(
            accelerator("<Primary><Alt>k"),
            Some(hotkey::parse("Ctrl + Alt + K").unwrap().combo)
        );
        assert_eq!(
            accelerator("<Super>Page_Down"),
            Some(hotkey::parse("Win + Page Down").unwrap().combo)
        );
        assert_eq!(accelerator(""), None);
        assert_ne!(accelerator("<Primary>k"), accelerator("<Primary><Shift>k"));
    }
}
