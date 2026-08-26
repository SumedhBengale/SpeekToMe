use std::thread;
use std::time::Duration;
use arboard::Clipboard;

// ==========================================
// Windows Implementation
// ==========================================
#[cfg(target_os = "windows")]
use windows_sys::Win32::UI::Input::KeyboardAndMouse::{INPUT, KEYBDINPUT};

#[cfg(target_os = "windows")]
pub const KEYEVENTF_UNICODE: u32 = 0x0004;
#[cfg(target_os = "windows")]
pub const INPUT_KEYBOARD: u32 = 1;
#[cfg(target_os = "windows")]
pub const KEYEVENTF_KEYUP: u32 = 2;

#[cfg(target_os = "windows")]
fn send_inputs(inputs: &[INPUT]) {
    unsafe {
        // Suppress our own synthesized input so the low-level keyboard hook
        // (win32::keyboard_hook_proc) never re-processes it as a user hotkey.
        crate::SIMULATING_KEY.store(true, std::sync::atomic::Ordering::SeqCst);
        windows_sys::Win32::UI::Input::KeyboardAndMouse::SendInput(
            inputs.len() as u32,
            inputs.as_ptr() as *const windows_sys::Win32::UI::Input::KeyboardAndMouse::INPUT,
            std::mem::size_of::<INPUT>() as i32
        );
        crate::SIMULATING_KEY.store(false, std::sync::atomic::Ordering::SeqCst);
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_paste() {
    release_modifier_keys();
    unsafe {
        let mut inputs = [std::mem::zeroed::<INPUT>(); 4];

        // 1. Ctrl Down
        inputs[0].r#type = INPUT_KEYBOARD;
        let mut ki0: KEYBDINPUT = std::mem::zeroed();
        ki0.wVk = 0x11; // VK_CONTROL
        inputs[0].Anonymous.ki = ki0;

        // 2. V Down
        inputs[1].r#type = INPUT_KEYBOARD;
        let mut ki1: KEYBDINPUT = std::mem::zeroed();
        ki1.wVk = 0x56; // VK_V
        inputs[1].Anonymous.ki = ki1;

        // 3. V Up
        inputs[2].r#type = INPUT_KEYBOARD;
        let mut ki2: KEYBDINPUT = std::mem::zeroed();
        ki2.wVk = 0x56;
        ki2.dwFlags = KEYEVENTF_KEYUP;
        inputs[2].Anonymous.ki = ki2;

        // 4. Ctrl Up
        inputs[3].r#type = INPUT_KEYBOARD;
        let mut ki3: KEYBDINPUT = std::mem::zeroed();
        ki3.wVk = 0x11;
        ki3.dwFlags = KEYEVENTF_KEYUP;
        inputs[3].Anonymous.ki = ki3;

        send_inputs(&inputs);
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_single_key(vk: u32) {
    unsafe {
        let mut inputs = [std::mem::zeroed::<INPUT>(); 2];

        // 1. Key Down
        inputs[0].r#type = INPUT_KEYBOARD;
        let mut ki0: KEYBDINPUT = std::mem::zeroed();
        ki0.wVk = vk as u16;
        inputs[0].Anonymous.ki = ki0;

        // 2. Key Up
        inputs[1].r#type = INPUT_KEYBOARD;
        let mut ki1: KEYBDINPUT = std::mem::zeroed();
        ki1.wVk = vk as u16;
        ki1.dwFlags = KEYEVENTF_KEYUP;
        inputs[1].Anonymous.ki = ki1;

        send_inputs(&inputs);
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_key_down(vk: u32) {
    unsafe {
        let mut input = std::mem::zeroed::<INPUT>();
        input.r#type = INPUT_KEYBOARD;
        let mut ki: KEYBDINPUT = std::mem::zeroed();
        ki.wVk = vk as u16;
        input.Anonymous.ki = ki;

        send_inputs(std::slice::from_ref(&input));
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_key_up(vk: u32) {
    unsafe {
        let mut input = std::mem::zeroed::<INPUT>();
        input.r#type = INPUT_KEYBOARD;
        let mut ki: KEYBDINPUT = std::mem::zeroed();
        ki.wVk = vk as u16;
        ki.dwFlags = KEYEVENTF_KEYUP;
        input.Anonymous.ki = ki;

        send_inputs(std::slice::from_ref(&input));
    }
}

#[cfg(target_os = "windows")]
pub fn release_modifier_keys() {
    unsafe {
        use windows_sys::Win32::UI::Input::KeyboardAndMouse::{keybd_event, KEYEVENTF_KEYUP, KEYEVENTF_EXTENDEDKEY};
        
        crate::SIMULATING_KEY.store(true, std::sync::atomic::Ordering::SeqCst);
        // Generic Control
        keybd_event(0x11, 0, KEYEVENTF_KEYUP, 0);
        // Left Control
        keybd_event(0xA2, 0, KEYEVENTF_KEYUP, 0);
        // Right Control (Extended)
        keybd_event(0xA3, 0, KEYEVENTF_KEYUP | KEYEVENTF_EXTENDEDKEY, 0);
        keybd_event(0xA3, 0, KEYEVENTF_KEYUP, 0);
        // Generic Alt
        keybd_event(0x12, 0, KEYEVENTF_KEYUP, 0);
        // Left Alt
        keybd_event(0xA4, 0, KEYEVENTF_KEYUP, 0);
        // Right Alt (Extended)
        keybd_event(0xA5, 0, KEYEVENTF_KEYUP | KEYEVENTF_EXTENDEDKEY, 0);
        keybd_event(0xA5, 0, KEYEVENTF_KEYUP, 0);
        // Shift
        keybd_event(0x10, 0, KEYEVENTF_KEYUP, 0);
        keybd_event(0xA0, 0, KEYEVENTF_KEYUP, 0);
        keybd_event(0xA1, 0, KEYEVENTF_KEYUP | KEYEVENTF_EXTENDEDKEY, 0);
        keybd_event(0xA1, 0, KEYEVENTF_KEYUP, 0);
        // Windows Key
        keybd_event(0x5B, 0, KEYEVENTF_KEYUP, 0);
        keybd_event(0x5C, 0, KEYEVENTF_KEYUP | KEYEVENTF_EXTENDEDKEY, 0);
        keybd_event(0x5C, 0, KEYEVENTF_KEYUP, 0);
        crate::SIMULATING_KEY.store(false, std::sync::atomic::Ordering::SeqCst);
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_typing(text: &str) {
    release_modifier_keys();
    unsafe {
        let utf16_chars: Vec<u16> = text.encode_utf16().collect();
        if utf16_chars.is_empty() {
            return;
        }

        let mut chunk_inputs: Vec<INPUT> = Vec::with_capacity(32);

        for &c in &utf16_chars {
            let mut in_down: INPUT = std::mem::zeroed();
            in_down.r#type = INPUT_KEYBOARD;
            let mut ki0: KEYBDINPUT = std::mem::zeroed();
            ki0.wVk = 0;
            ki0.wScan = c;
            ki0.dwFlags = KEYEVENTF_UNICODE;
            in_down.Anonymous.ki = ki0;

            let mut in_up: INPUT = std::mem::zeroed();
            in_up.r#type = INPUT_KEYBOARD;
            let mut ki1: KEYBDINPUT = std::mem::zeroed();
            ki1.wVk = 0;
            ki1.wScan = c;
            ki1.dwFlags = KEYEVENTF_UNICODE | KEYEVENTF_KEYUP;
            in_up.Anonymous.ki = ki1;

            chunk_inputs.push(in_down);
            chunk_inputs.push(in_up);

            let ch = char::from_u32(c as u32).unwrap_or(' ');
            let is_space = ch == ' ';
            let is_punct = matches!(ch, '.' | ',' | '!' | '?' | ';' | ':' | '\n');

            // Flush chunk at word boundaries, punctuation, or every ~8 characters
            if is_space || is_punct || chunk_inputs.len() >= 16 {
                send_inputs(&chunk_inputs);
                chunk_inputs.clear();

                let delay_ms = if is_punct {
                    4 + (rand::random::<u64>() % 4) // 4ms - 7ms punctuation pause
                } else if is_space {
                    1 + (rand::random::<u64>() % 3) // 1ms - 3ms word pause
                } else {
                    1 // 1ms micro-tick for long continuous words
                };

                thread::sleep(Duration::from_millis(delay_ms));
            }
        }

        if !chunk_inputs.is_empty() {
            send_inputs(&chunk_inputs);
        }
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_backspaces(count: usize) {
    if count == 0 {
        return;
    }
    unsafe {
        for _ in 0..count {
            let mut inputs = [std::mem::zeroed::<INPUT>(); 2];

            // 1. VK_BACK Down
            inputs[0].r#type = INPUT_KEYBOARD;
            let mut ki0: KEYBDINPUT = std::mem::zeroed();
            ki0.wVk = 0x08; // VK_BACK
            inputs[0].Anonymous.ki = ki0;

            // 2. VK_BACK Up
            inputs[1].r#type = INPUT_KEYBOARD;
            let mut ki1: KEYBDINPUT = std::mem::zeroed();
            ki1.wVk = 0x08;
            ki1.dwFlags = KEYEVENTF_KEYUP;
            inputs[1].Anonymous.ki = ki1;

            send_inputs(&inputs);

            thread::sleep(Duration::from_millis(1));
        }
    }
}

#[cfg(not(target_os = "windows"))]
pub fn simulate_paste() {
    println!("[KEYBOARD] simulate_paste is not supported on this platform.");
}

#[cfg(not(target_os = "windows"))]
pub fn release_modifier_keys() {}

#[cfg(not(target_os = "windows"))]
pub fn simulate_single_key(_vk: u32) {
    println!("[KEYBOARD] simulate_single_key is not supported on this platform.");
}

#[cfg(not(target_os = "windows"))]
pub fn simulate_key_down(_vk: u32) {
    println!("[KEYBOARD] simulate_key_down is not supported on this platform.");
}

#[cfg(not(target_os = "windows"))]
pub fn simulate_key_up(_vk: u32) {
    println!("[KEYBOARD] simulate_key_up is not supported on this platform.");
}

#[cfg(not(target_os = "windows"))]
pub fn simulate_typing(text: &str) {
    println!("[KEYBOARD] simulate_typing: {}", text);
}

#[cfg(not(target_os = "windows"))]
pub fn simulate_backspaces(count: usize) {
    println!("[KEYBOARD] simulate_backspaces: {}", count);
}

// ==========================================
// Shared Clipboard Injection Logic
// ==========================================
pub fn paste_text(text: &str) {
    let delays = [0, 50, 150];
    let mut clipboard_success = false;

    for (attempt, &delay_ms) in delays.iter().enumerate() {
        if delay_ms > 0 {
            thread::sleep(Duration::from_millis(delay_ms));
        }

        let mut ctx = match Clipboard::new() {
            Ok(c) => c,
            Err(e) => {
                eprintln!("[CLIPBOARD WARNING] Attempt {} failed to open clipboard: {:?}", attempt + 1, e);
                continue;
            }
        };

        let original_text = ctx.get_text().ok();

        if let Err(e) = ctx.set_text(text.to_owned()) {
            eprintln!("[CLIPBOARD WARNING] Attempt {} failed to set clipboard text: {:?}", attempt + 1, e);
            continue;
        }

        clipboard_success = true;
        thread::sleep(Duration::from_millis(80));
        simulate_paste();

        if let Some(orig) = original_text {
            thread::spawn(move || {
                thread::sleep(Duration::from_millis(250));
                for &restore_delay in &[0, 50, 150] {
                    if restore_delay > 0 {
                        thread::sleep(Duration::from_millis(restore_delay));
                    }
                    if let Ok(mut ctx) = Clipboard::new() {
                        if ctx.set_text(orig.clone()).is_ok() {
                            break;
                        }
                    }
                }
            });
        }
        break;
    }

    if !clipboard_success {
        eprintln!("[CLIPBOARD ERROR] All clipboard paste attempts failed.");
        crate::win32::show_tray_notification(
            "SpeekTo.Me — Clipboard Unavailable",
            "Another application locked the clipboard. To bypass clipboard locks, enable \"Keystroke Simulation (Typing Mode)\" in Settings.",
            crate::win32::NotificationLevel::Warning,
        );
    }
}
