//! macOS's enabled symbolic hotkeys, read without stealing registrations.
//! No registry enumerates every third-party application's private bindings.
use crate::hotkey_conflicts::Conflict;
use std::ffi::c_void;
type CF = *const c_void;
#[cfg_attr(target_os = "macos", link(name = "Carbon", kind = "framework"))]
extern "C" {
    fn CopySymbolicHotKeys(keys: *mut CF) -> i32;
    fn TISCopyCurrentKeyboardLayoutInputSource() -> CF;
    fn TISGetInputSourceProperty(source: CF, key: CF) -> CF;
    static kTISPropertyUnicodeKeyLayoutData: CF;
    fn LMGetKbdType() -> u8;
    fn UCKeyTranslate(
        layout: CF,
        key: u16,
        action: u16,
        modifiers: u32,
        keyboard_type: u32,
        options: u32,
        dead_state: *mut u32,
        max: u32,
        length: *mut u32,
        characters: *mut u16,
    ) -> i32;
}
#[cfg_attr(target_os = "macos", link(name = "CoreFoundation", kind = "framework"))]
extern "C" {
    fn CFRelease(value: CF);
    fn CFStringCreateWithCString(
        allocator: CF,
        value: *const std::ffi::c_char,
        encoding: u32,
    ) -> CF;
    fn CFArrayGetCount(value: CF) -> isize;
    fn CFArrayGetValueAtIndex(value: CF, index: isize) -> CF;
    fn CFDictionaryGetValue(value: CF, key: CF) -> CF;
    fn CFBooleanGetValue(value: CF) -> u8;
    fn CFNumberGetValue(value: CF, kind: isize, result: *mut c_void) -> u8;
    fn CFDataGetBytePtr(value: CF) -> *const u8;
}
struct Owned(CF);
impl Drop for Owned {
    fn drop(&mut self) {
        if !self.0.is_null() {
            unsafe { CFRelease(self.0) }
        }
    }
}
unsafe fn number(value: CF) -> Option<i64> {
    if value.is_null() {
        return None;
    }
    let mut n = 0i64;
    (CFNumberGetValue(value, 4, (&mut n as *mut i64).cast()) != 0).then_some(n)
}
fn modifier_mask(modifiers: u32) -> u32 {
    u32::from(modifiers & 4096 != 0)
        | (u32::from(modifiers & 512 != 0) << 1)
        | (u32::from(modifiers & 2048 != 0) << 2)
        | (u32::from(modifiers & 256 != 0) << 3)
}
fn special_key(code: u16) -> Option<u32> {
    Some(match code {
        36 | 76 => 0x0D,
        48 => 0x09,
        49 => 0x20,
        51 => 0x08,
        53 => 0x1B,
        57 => 0x14,
        114 => 0x2D,
        115 => 0x24,
        116 => 0x21,
        117 => 0x2E,
        119 => 0x23,
        121 => 0x22,
        123 => 0x25,
        124 => 0x27,
        125 => 0x28,
        126 => 0x26,
        122 => 0x70,
        120 => 0x71,
        99 => 0x72,
        118 => 0x73,
        96 => 0x74,
        97 => 0x75,
        98 => 0x76,
        100 => 0x77,
        101 => 0x78,
        109 => 0x79,
        103 => 0x7A,
        111 => 0x7B,
        105 => 0x7C,
        107 => 0x7D,
        113 => 0x7E,
        106 => 0x7F,
        64 => 0x80,
        79 => 0x81,
        80 => 0x82,
        90 => 0x83,
        61 => 0xA5,
        _ => return None,
    })
}
fn semantic_function_modifier(modifiers: u32, code: u16) -> bool {
    // Carbon Fn is bit 17. macOS synthesizes it for function/navigation keys;
    // on ordinary keys it is a real extra modifier that our recorder cannot
    // express. Do not turn Fn+Control+K into a false conflict with Control+K.
    modifiers & (1 << 17) != 0
        && !special_key(code).is_some_and(|vk| {
            (0x70..=0x83).contains(&vk) || (0x21..=0x28).contains(&vk) || vk == 0x2E
        })
}
pub fn is_main_thread() -> bool {
    unsafe extern "C" {
        fn pthread_main_np() -> i32;
    }
    unsafe { pthread_main_np() != 0 }
}

pub fn check(combo: u32) -> Result<Vec<Conflict>, String> {
    if !is_main_thread() {
        return Err("macOS keyboard-layout queries require the main thread.".into());
    }

    unsafe {
        let mut array = std::ptr::null();
        if CopySymbolicHotKeys(&mut array) != 0 || array.is_null() {
            return Err("macOS system shortcut lookup failed.".into());
        }
        let array = Owned(array);
        let input = Owned(TISCopyCurrentKeyboardLayoutInputSource());
        let layout = if input.0.is_null() {
            std::ptr::null()
        } else {
            TISGetInputSourceProperty(input.0, kTISPropertyUnicodeKeyLayoutData)
        };
        let layout: CF = if layout.is_null() {
            std::ptr::null()
        } else {
            CFDataGetBytePtr(layout).cast()
        };
        let mut unavailable_layout = false;
        // Carbon defines these keys as CFSTR macros, not exported symbols.
        // Declaring extern statics compiles but fails to link on a real Mac.
        let key = |value: &'static [u8]| {
            Owned(CFStringCreateWithCString(
                std::ptr::null(),
                value.as_ptr().cast(),
                0x08000100,
            ))
        };
        let enabled_key = key(b"kHISymbolicHotKeyEnabled\0");
        let code_key = key(b"kHISymbolicHotKeyCode\0");
        let modifiers_key = key(b"kHISymbolicHotKeyModifiers\0");
        if enabled_key.0.is_null() || code_key.0.is_null() || modifiers_key.0.is_null() {
            return Err("macOS shortcut key allocation failed.".into());
        }
        for index in 0..CFArrayGetCount(array.0) {
            let dict = CFArrayGetValueAtIndex(array.0, index);
            let enabled = CFDictionaryGetValue(dict, enabled_key.0);
            if enabled.is_null() || CFBooleanGetValue(enabled) == 0 {
                continue;
            }
            let (Some(code), Some(modifiers)) = (
                number(CFDictionaryGetValue(dict, code_key.0)),
                number(CFDictionaryGetValue(dict, modifiers_key.0)),
            ) else {
                continue;
            };
            if !(0..=u16::MAX as i64).contains(&code) {
                continue;
            }
            if semantic_function_modifier(modifiers as u32, code as u16) {
                continue;
            }
            let mask = modifier_mask(modifiers as u32);
            let wanted = combo >> 16;
            let vk = if let Some(vk) = special_key(code as u16) {
                Some(vk)
            } else if !layout.is_null() {
                let (mut dead, mut length, mut chars) = (0u32, 0u32, [0u16; 4]);
                if UCKeyTranslate(
                    layout,
                    code as u16,
                    0,
                    0,
                    LMGetKbdType() as u32,
                    1,
                    &mut dead,
                    4,
                    &mut length,
                    chars.as_mut_ptr(),
                ) == 0
                    && length == 1
                {
                    char::from_u32(chars[0] as u32)
                        .filter(|c| c.is_ascii_alphanumeric())
                        .map(|c| c.to_ascii_uppercase() as u32)
                } else {
                    None
                }
            } else {
                unavailable_layout = true;
                None
            };
            let matches = if wanted == 0 {
                let bit = match code {
                    56 | 60 => 2,
                    58 | 61 => 4,
                    55 | 54 => 8,
                    _ => 0,
                };
                bit != 0 && (mask | bit) == (combo & 0xFFFF)
            } else {
                vk == Some(wanted) && mask == (combo & 0xFFFF)
            };
            if matches {
                return Ok(vec![Conflict {
                    action: "An enabled macOS system shortcut".into(),
                    source: "macOS CopySymbolicHotKeys".into(),
                }]);
            }
        }
        if unavailable_layout && (0x30..=0x5A).contains(&(combo >> 16)) {
            Err("The current macOS keyboard layout could not be checked. Global availability is unverified.".into())
        } else {
            Ok(Vec::new())
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[cfg(target_os = "macos")]
    #[test]
    fn background_layout_query_is_rejected_before_calling_carbon() {
        let result = std::thread::spawn(|| check(0x4b0005)).join().unwrap();
        assert!(result.unwrap_err().contains("main thread"));
    }
    #[test]
    fn carbon_modifiers_and_special_keys_match_shared_representation() {
        assert_eq!(modifier_mask(4096 | 2048), 5);
        assert_eq!(modifier_mask(256 | 512), 10);
        assert_eq!(special_key(122), Some(0x70));
        assert_eq!(special_key(123), Some(0x25));
        assert!(semantic_function_modifier(1 << 17, 40)); // Fn+K is distinct.
        assert!(!semantic_function_modifier(1 << 17, 122)); // Synthesized on F1.
        assert_eq!(special_key(0), None); // Letters must use the active layout.
    }
}
