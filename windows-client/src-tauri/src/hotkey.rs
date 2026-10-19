//! Shared, validated shortcut representation for Windows hooks and XDG portals.
#[derive(Debug, PartialEq, Eq)]
pub struct Hotkey {
    pub name: String,
    pub combo: u32,
    pub portal: String,
}

pub fn parse(value: &str) -> Result<Hotkey, String> {
    let mut mask = 0u32;
    let mut key = None;
    for part in value.split('+') {
        let part = part.trim().to_ascii_lowercase();
        let bit = match part.as_str() {
            "ctrl" | "control" => 1,
            "shift" => 2,
            "alt" | "option" => 4,
            "win" | "windows" | "meta" | "super" | "cmd" | "command" => 8,
            _ => 0,
        };
        if bit != 0 {
            if mask & bit != 0 {
                return Err("Each modifier can appear only once.".into());
            }
            mask |= bit;
        } else {
            if key.is_some() {
                return Err(
                    "Choose one key with optional Ctrl, Alt, Shift or Meta modifiers.".into(),
                );
            }
            key = Some(key_code(&part).ok_or(
                "Unsupported key. Use a letter, number, function key, Space or navigation key.",
            )?);
        }
    }
    let mut names = Vec::new();
    let mut portal = Vec::new();
    for (bit, name, token) in [
        (1, "Ctrl", "CTRL"),
        (2, "Shift", "SHIFT"),
        (4, "Alt", "ALT"),
        (8, "Win", "LOGO"),
    ] {
        if mask & bit != 0 {
            names.push(name.to_string());
            portal.push(token.to_string());
        }
    }
    let vk = if let Some((name, vk, keysym)) = key {
        if mask == 0 && !(0x70..=0x87).contains(&vk) && vk != 0xA5 && vk != 0x14 {
            return Err(
                "Add Ctrl, Alt, Shift or Meta to this key, or choose a function key on its own."
                    .into(),
            );
        }
        if vk == 0xA5 && mask != 0 {
            return Err("Use Right Alt on its own, or Alt with another key.".into());
        }
        if mask == 5 && vk == 0x2E {
            return Err("Ctrl + Alt + Delete is reserved by the operating system.".into());
        }
        names.push(name);
        portal.push(keysym);
        vk
    } else {
        // These modifier-only chords use the native Linux helper for real edges.
        let trigger = match mask {
            3 => "Shift_L",
            5 => "Alt_L",
            9 => "Super_L",
            _ => return Err("Add a key to this modifier combination.".into()),
        };
        portal = vec!["CTRL".into(), trigger.into()];
        0
    };
    Ok(Hotkey {
        name: names.join(" + "),
        combo: (vk << 16) | mask,
        portal: portal.join("+"),
    })
}

fn key_code(key: &str) -> Option<(String, u32, String)> {
    if key.len() == 1 {
        let b = key.as_bytes()[0];
        if b.is_ascii_alphanumeric() {
            return Some((
                key.to_ascii_uppercase(),
                b.to_ascii_uppercase() as u32,
                key.to_string(),
            ));
        }
    }
    if let Some(n) = key.strip_prefix('f').and_then(|s| s.parse::<u32>().ok()) {
        if (1..=24).contains(&n) {
            return Some((format!("F{n}"), 0x6F + n, format!("F{n}")));
        }
    }
    let (name, vk, sym) = match key {
        "space" => ("Space", 0x20, "space"),
        "tab" => ("Tab", 0x09, "Tab"),
        "escape" | "esc" => ("Escape", 0x1B, "Escape"),
        "enter" | "return" => ("Enter", 0x0D, "Return"),
        "backspace" => ("Backspace", 0x08, "BackSpace"),
        "delete" => ("Delete", 0x2E, "Delete"),
        "insert" => ("Insert", 0x2D, "Insert"),
        "home" => ("Home", 0x24, "Home"),
        "end" => ("End", 0x23, "End"),
        "pageup" | "page up" => ("Page Up", 0x21, "Prior"),
        "pagedown" | "page down" => ("Page Down", 0x22, "Next"),
        "arrowleft" | "left" => ("Left", 0x25, "Left"),
        "arrowup" | "up" => ("Up", 0x26, "Up"),
        "arrowright" | "right" => ("Right", 0x27, "Right"),
        "arrowdown" | "down" => ("Down", 0x28, "Down"),
        "right alt" | "ralt" => ("Right Alt", 0xA5, "Alt_R"),
        "caps lock" | "capslock" => ("Caps Lock", 0x14, "Caps_Lock"),
        _ => return None,
    };
    Some((name.into(), vk, sym.into()))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn translates_custom_chords_without_losing_modifiers() {
        let h = parse("meta + control + f12").unwrap();
        assert_eq!(h.name, "Ctrl + Win + F12");
        assert_eq!(h.combo, 0x7B0009);
        assert_eq!(h.portal, "CTRL+LOGO+F12");
        let h = parse("Ctrl + Alt + K").unwrap();
        assert_eq!(h.combo, 0x4B0005);
        assert_eq!(h.portal, "CTRL+ALT+k");
        assert_eq!(parse("Shift + 7").unwrap().portal, "SHIFT+7");
        assert_eq!(parse("Ctrl + PageDown").unwrap().portal, "CTRL+Next");
    }
    #[test]
    fn presets_remain_compatible() {
        for (s, c) in [
            ("Ctrl + Win", 9),
            ("Ctrl + Shift", 3),
            ("Ctrl + Alt", 5),
            ("Right Alt", 0xA50000),
            ("F8", 0x770000),
            ("Ctrl + Shift + Z", 0x5A0003),
            ("Alt + Space", 0x200004),
            ("Ctrl + Space", 0x200001),
            ("Shift + Space", 0x200002),
        ] {
            assert_eq!(parse(s).unwrap().combo, c, "{s}");
        }
    }
    #[test]
    fn rejects_ambiguous_unmodified_and_reserved_input() {
        for s in [
            "",
            "Ctrl +",
            "Ctrl + Ctrl + K",
            "Ctrl + K + L",
            "banana",
            "K",
            "Ctrl + Alt + Delete",
            "F25",
            "Alt",
            "Shift + Alt",
            "Ctrl + Right Alt",
        ] {
            assert!(parse(s).is_err(), "{s}");
        }
    }
}
