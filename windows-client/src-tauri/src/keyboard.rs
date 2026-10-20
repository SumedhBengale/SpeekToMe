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
pub fn simulate_copy() {
    release_modifier_keys();
    unsafe {
        let mut inputs = [std::mem::zeroed::<INPUT>(); 4];

        // 1. Ctrl Down
        inputs[0].r#type = INPUT_KEYBOARD;
        let mut ki0: KEYBDINPUT = std::mem::zeroed();
        ki0.wVk = 0x11; // VK_CONTROL
        inputs[0].Anonymous.ki = ki0;

        // 2. C Down
        inputs[1].r#type = INPUT_KEYBOARD;
        let mut ki1: KEYBDINPUT = std::mem::zeroed();
        ki1.wVk = 0x43; // 'C'
        inputs[1].Anonymous.ki = ki1;

        // 3. C Up
        inputs[2].r#type = INPUT_KEYBOARD;
        let mut ki2: KEYBDINPUT = std::mem::zeroed();
        ki2.wVk = 0x43;
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

pub fn get_selected_text() -> Option<String> {
    crate::SIMULATING_KEY.store(true, std::sync::atomic::Ordering::SeqCst);

    // 1. Backup existing clipboard content
    let original_clipboard = match Clipboard::new() {
        Ok(mut c) => c.get_text().ok(),
        Err(_) => None,
    };

    // 2. Clear clipboard text so we can reliably detect fresh selection
    if let Ok(mut c) = Clipboard::new() {
        let _ = c.clear();
    }

    // 3. Release physical modifiers so SendInput Ctrl+C is clean
    #[cfg(any(target_os = "windows", target_os = "linux", target_os = "macos"))]
    {
        release_modifier_keys();
        thread::sleep(Duration::from_millis(30));
        simulate_copy();
    }

    // 4. Poll clipboard up to 350ms for copied selection
    let mut selected: Option<String> = None;
    let start = std::time::Instant::now();
    while start.elapsed() < Duration::from_millis(350) {
        thread::sleep(Duration::from_millis(25));
        if let Ok(mut c) = Clipboard::new() {
            if let Ok(text) = c.get_text() {
                if !text.is_empty() {
                    selected = Some(text);
                    break;
                }
            }
        }
    }

    // 5. If nothing was selected, restore original clipboard
    if selected.is_none() {
        if let Some(orig) = original_clipboard {
            if let Ok(mut c) = Clipboard::new() {
                let _ = c.set_text(orig);
            }
        }
    }

    thread::sleep(Duration::from_millis(60));
    crate::SIMULATING_KEY.store(false, std::sync::atomic::Ordering::SeqCst);

    selected
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
pub fn suppress_win_key_start_menu() {
    unsafe {
        use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
            SendInput, INPUT, INPUT_KEYBOARD, KEYEVENTF_KEYUP,
        };
        crate::SIMULATING_KEY.store(true, std::sync::atomic::Ordering::SeqCst);
        // Virtual key 0xE8 is officially unassigned in Windows.
        // Sending 0xE8 tells the Windows Shell that a key combination was executed
        // with the Windows key, cleanly suppressing the Start Menu from popping up.
        let mut inputs: [INPUT; 2] = [std::mem::zeroed(), std::mem::zeroed()];
        inputs[0].r#type = INPUT_KEYBOARD;
        inputs[0].Anonymous.ki.wVk = 0xE8;
        inputs[1].r#type = INPUT_KEYBOARD;
        inputs[1].Anonymous.ki.wVk = 0xE8;
        inputs[1].Anonymous.ki.dwFlags = KEYEVENTF_KEYUP;

        SendInput(2, inputs.as_ptr(), std::mem::size_of::<INPUT>() as i32);
        crate::SIMULATING_KEY.store(false, std::sync::atomic::Ordering::SeqCst);
    }
}

#[cfg(not(target_os = "windows"))]
pub fn suppress_win_key_start_menu() {}

#[cfg(target_os = "windows")]
pub fn release_modifier_keys() {
    unsafe {
        use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
            GetAsyncKeyState, SendInput, INPUT, INPUT_KEYBOARD, KEYBDINPUT, KEYEVENTF_KEYUP, KEYEVENTF_EXTENDEDKEY,
        };

        crate::SIMULATING_KEY.store(true, std::sync::atomic::Ordering::SeqCst);

        // Check if Windows key is currently physically or logically down.
        let lwin_down = (GetAsyncKeyState(0x5B) as u16 & 0x8000) != 0;
        let rwin_down = (GetAsyncKeyState(0x5C) as u16 & 0x8000) != 0;
        let win_down = lwin_down || rwin_down;

        if win_down {
            // Mask with 0xE8 before releasing so Windows Shell never opens the Start menu
            let mut mask_inputs: [INPUT; 2] = [std::mem::zeroed(), std::mem::zeroed()];
            mask_inputs[0].r#type = INPUT_KEYBOARD;
            mask_inputs[0].Anonymous.ki.wVk = 0xE8;
            mask_inputs[1].r#type = INPUT_KEYBOARD;
            mask_inputs[1].Anonymous.ki.wVk = 0xE8;
            mask_inputs[1].Anonymous.ki.dwFlags = KEYEVENTF_KEYUP;
            SendInput(2, mask_inputs.as_ptr(), std::mem::size_of::<INPUT>() as i32);
        }

        // Only release modifiers that are ACTUALLY currently held down!
        // Never send an artificial KEYUP for an idle key, as sending KEYUP on an idle
        // Win or Alt key can cause the Windows Shell to pop open the Start Menu or Menu Bar.
        let mut inputs: Vec<INPUT> = Vec::with_capacity(8);

        let check_keys = [
            (0x11u16, 0u32, 0x11i32),                       // VK_CONTROL
            (0xA2u16, 0u32, 0xA2i32),                       // VK_LCONTROL
            (0xA3u16, KEYEVENTF_EXTENDEDKEY, 0xA3i32),      // VK_RCONTROL
            (0x12u16, 0u32, 0x12i32),                       // VK_MENU (Alt)
            (0xA4u16, 0u32, 0xA4i32),                       // VK_LMENU
            (0xA5u16, KEYEVENTF_EXTENDEDKEY, 0xA5i32),      // VK_RMENU
            (0x10u16, 0u32, 0x10i32),                       // VK_SHIFT
            (0xA0u16, 0u32, 0xA0i32),                       // VK_LSHIFT
            (0xA1u16, KEYEVENTF_EXTENDEDKEY, 0xA1i32),      // VK_RSHIFT
            (0x5Bu16, 0u32, 0x5Bi32),                       // VK_LWIN
            (0x5Cu16, KEYEVENTF_EXTENDEDKEY, 0x5Ci32),      // VK_RWIN
        ];

        for (vk, flags, vki) in check_keys {
            if (GetAsyncKeyState(vki) as u16 & 0x8000) != 0 {
                let mut input: INPUT = std::mem::zeroed();
                input.r#type = INPUT_KEYBOARD;
                let mut ki: KEYBDINPUT = std::mem::zeroed();
                ki.wVk = vk;
                ki.dwFlags = KEYEVENTF_KEYUP | flags;
                input.Anonymous.ki = ki;
                inputs.push(input);
            }
        }

        if !inputs.is_empty() {
            SendInput(
                inputs.len() as u32,
                inputs.as_ptr(),
                std::mem::size_of::<INPUT>() as i32,
            );
        }

        if win_down {
            // Mask again after release to ensure Windows Shell state is completely neutral
            let mut mask_inputs: [INPUT; 2] = [std::mem::zeroed(), std::mem::zeroed()];
            mask_inputs[0].r#type = INPUT_KEYBOARD;
            mask_inputs[0].Anonymous.ki.wVk = 0xE8;
            mask_inputs[1].r#type = INPUT_KEYBOARD;
            mask_inputs[1].Anonymous.ki.wVk = 0xE8;
            mask_inputs[1].Anonymous.ki.dwFlags = KEYEVENTF_KEYUP;
            SendInput(2, mask_inputs.as_ptr(), std::mem::size_of::<INPUT>() as i32);
        }

        crate::SIMULATING_KEY.store(false, std::sync::atomic::Ordering::SeqCst);

        // If any modifier was released, allow 25ms for the OS and active window to settle
        if !inputs.is_empty() {
            thread::sleep(Duration::from_millis(25));
        }
    }
}

#[cfg(target_os = "windows")]
pub fn simulate_typing(text: &str) {
    release_modifier_keys();
    // 25ms settling delay before typing the first character so the active target window
    // (e.g. Notepad, Word, Browser) is 100% steady and ready to ingest text without dropping the first word.
    thread::sleep(Duration::from_millis(25));
    unsafe {
        for ch in text.chars() {
            if ch == '\r' {
                continue;
            }
            if ch == '\n' {
                // Send true physical VK_RETURN (Enter key)
                let mut in_down: INPUT = std::mem::zeroed();
                in_down.r#type = INPUT_KEYBOARD;
                let mut ki0: KEYBDINPUT = std::mem::zeroed();
                ki0.wVk = 0x0D; // VK_RETURN
                ki0.dwFlags = 0;
                in_down.Anonymous.ki = ki0;

                let mut in_up: INPUT = std::mem::zeroed();
                in_up.r#type = INPUT_KEYBOARD;
                let mut ki1: KEYBDINPUT = std::mem::zeroed();
                ki1.wVk = 0x0D;
                ki1.dwFlags = KEYEVENTF_KEYUP;
                in_up.Anonymous.ki = ki1;

                send_inputs(&[in_down, in_up]);
                thread::sleep(Duration::from_millis(15));
                continue;
            }

            let mut utf16_buf = [0u16; 2];
            let encoded = ch.encode_utf16(&mut utf16_buf);

            let mut char_inputs: Vec<INPUT> = Vec::with_capacity(4);
            for &c in encoded.iter() {
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

                char_inputs.push(in_down);
                char_inputs.push(in_up);
            }

            // Send this character's Down + Up atomically so no key remains stuck
            send_inputs(&char_inputs);

            // Natural typing cadence with reliable pacing for modern Windows 11 apps (Notepad WinUI 3, RichEdit)
            let is_space = ch == ' ';
            let is_punct = matches!(ch, '.' | ',' | '!' | '?' | ';' | ':');

            let delay_ms = if is_punct {
                8 + (rand::random::<u64>() % 6) // 8ms - 13ms after punctuation
            } else if is_space {
                4 + (rand::random::<u64>() % 4) // 4ms - 7ms after word space
            } else {
                2 + (rand::random::<u64>() % 3) // 2ms - 4ms between letters
            };

            thread::sleep(Duration::from_millis(delay_ms));
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

#[cfg(target_os = "linux")]
pub fn simulate_copy() {
    let _ = send_linux_chord(0x63);
}

#[cfg(target_os = "linux")]
fn send_linux_chord(key: i32) -> Result<(), String> {
    // XKB keysyms: Control_L, requested key. The portal translates them in
    // the compositor, so this reaches native Wayland applications.
    crate::linux_portal::send_keys(vec![0xffe3, 1, key, 1, key, 0, 0xffe3, 0])
}

#[cfg(target_os = "linux")]
pub fn simulate_paste() {
    if crate::linux_focus::focused_target() == crate::linux_focus::Focus::NotEditable {
        crate::win32::show_tray_notification(
            "SpeekTo.Me copied your text",
            "No editable field is focused. Your text is on the clipboard.",
            crate::win32::NotificationLevel::Info,
        );
        return;
    }
    if let Err(err) = send_linux_chord(0x76) {
        eprintln!("[KEYBOARD] Portal paste failed: {err}");
        crate::win32::show_tray_notification(
            "SpeekTo.Me copied your text",
            "Desktop keyboard control is unavailable. Your text is on the clipboard.",
            crate::win32::NotificationLevel::Warning,
        );
    }
}

#[cfg(target_os = "linux")]
pub fn release_modifier_keys() {}

#[cfg(target_os = "linux")]
pub fn simulate_single_key(vk: u32) {
    let key = match vk { 0x08 => 0xff08, 0x0d => 0xff0d, _ => return };
    if let Err(err) = crate::linux_portal::send_keys(vec![key, 1, key, 0]) {
        eprintln!("[KEYBOARD] Portal key failed: {err}");
    }
}

#[cfg(target_os = "linux")]
pub fn simulate_key_down(vk: u32) {
    if let Err(err) = crate::linux_portal::send_keys(vec![vk as i32, 1]) {
        eprintln!("[KEYBOARD] Portal key down failed: {err}");
    }
}

#[cfg(target_os = "linux")]
pub fn simulate_key_up(vk: u32) {
    if let Err(err) = crate::linux_portal::send_keys(vec![vk as i32, 0]) {
        eprintln!("[KEYBOARD] Portal key up failed: {err}");
    }
}

#[cfg(target_os = "linux")]
pub fn simulate_typing(text: &str) {
    // Keep a usable copy if the focused surface has no editable widget.
    let clipboard_ready = Clipboard::new()
        .and_then(|mut clipboard| clipboard.set_text(text.to_owned()))
        .is_ok();
    if crate::linux_focus::focused_target() == crate::linux_focus::Focus::NotEditable {
        crate::win32::show_tray_notification(
            if clipboard_ready { "SpeekTo.Me copied your text" } else { "SpeekTo.Me could not copy your text" },
            if clipboard_ready { "No editable field is focused. Your text is on the clipboard." } else { "No editable field is focused and the clipboard is unavailable." },
            if clipboard_ready { crate::win32::NotificationLevel::Info } else { crate::win32::NotificationLevel::Warning },
        );
        return;
    }
    if !text.is_ascii() {
        // The RemoteDesktop portal can only map keysyms in the compositor's
        // current keyboard layout. Pasting preserves arbitrary dictation text.
        if clipboard_ready {
            simulate_paste();
        } else {
            crate::win32::show_tray_notification(
                "SpeekTo.Me could not insert your text",
                "The clipboard is unavailable, so these characters could not be pasted.",
                crate::win32::NotificationLevel::Warning,
            );
        }
        return;
    }
    for ch in text.chars() {
        if ch == '\r' { continue; }
        let (keysym, shifted) = match ch {
            '\n' => (0xff0d, false),
            'A'..='Z' => (ch.to_ascii_lowercase() as i32, true),
            '!' => (b'1' as i32, true),
            '@' => (b'2' as i32, true),
            '#' => (b'3' as i32, true),
            '$' => (b'4' as i32, true),
            '%' => (b'5' as i32, true),
            '^' => (b'6' as i32, true),
            '&' => (b'7' as i32, true),
            '*' => (b'8' as i32, true),
            '(' => (b'9' as i32, true),
            ')' => (b'0' as i32, true),
            '_' => (b'-' as i32, true),
            '+' => (b'=' as i32, true),
            '{' => (b'[' as i32, true),
            '}' => (b']' as i32, true),
            '|' => (b'\\' as i32, true),
            ':' => (b';' as i32, true),
            '"' => (b'\'' as i32, true),
            '<' => (b',' as i32, true),
            '>' => (b'.' as i32, true),
            '?' => (b'/' as i32, true),
            '~' => (b'`' as i32, true),
            c => (c as i32, false),
        };
        let keys = if shifted {
            vec![0xffe1, 1, keysym, 1, keysym, 0, 0xffe1, 0]
        } else {
            vec![keysym, 1, keysym, 0]
        };
        if let Err(err) = crate::linux_portal::send_keys(keys) {
            eprintln!("[KEYBOARD] Portal typing failed: {err}");
            crate::win32::show_tray_notification(
                "SpeekTo.Me copied your text",
                if clipboard_ready { "Typing stopped because desktop keyboard control is unavailable. The full text is on the clipboard." } else { "Typing stopped and the clipboard is unavailable." },
                crate::win32::NotificationLevel::Warning,
            );
            break;
        }
    }
}

#[cfg(target_os = "linux")]
pub fn simulate_backspaces(count: usize) {
    for _ in 0..count { simulate_single_key(0x08); }
}

#[cfg(target_os = "macos")]
pub fn simulate_copy() { let _ = crate::macos_desktop::chord(false); }

#[cfg(target_os = "macos")]
pub fn simulate_paste() {
    if let Err(error) = crate::macos_desktop::chord(true) {
        crate::win32::show_tray_notification("SpeekTo.Me copied your text", error, crate::win32::NotificationLevel::Warning);
    }
}

#[cfg(target_os = "macos")]
pub fn release_modifier_keys() {}

#[cfg(target_os = "macos")]
pub fn simulate_single_key(vk: u32) { simulate_key_down(vk); simulate_key_up(vk); }

#[cfg(target_os = "macos")]
pub fn simulate_key_down(vk: u32) { crate::macos_desktop::key(vk, true); }

#[cfg(target_os = "macos")]
pub fn simulate_key_up(vk: u32) { crate::macos_desktop::key(vk, false); }

#[cfg(target_os = "macos")]
pub fn simulate_typing(text: &str) {
    crate::SIMULATING_KEY.store(true, std::sync::atomic::Ordering::SeqCst);
    let result = crate::macos_desktop::type_text(text);
    crate::SIMULATING_KEY.store(false, std::sync::atomic::Ordering::SeqCst);
    if let Err(error) = result {
        let _ = Clipboard::new().and_then(|mut c| c.set_text(text.to_owned()));
        crate::win32::show_tray_notification("SpeekTo.Me could not insert your text", error, crate::win32::NotificationLevel::Warning);
    }
}

#[cfg(target_os = "macos")]
pub fn simulate_backspaces(count: usize) { for _ in 0..count { simulate_single_key(8); } }

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn simulate_paste() {
    println!("[KEYBOARD] simulate_paste is not supported on this platform.");
}

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn release_modifier_keys() {}

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn simulate_single_key(_vk: u32) {
    println!("[KEYBOARD] simulate_single_key is not supported on this platform.");
}

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn simulate_key_down(_vk: u32) {
    println!("[KEYBOARD] simulate_key_down is not supported on this platform.");
}

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn simulate_key_up(_vk: u32) {
    println!("[KEYBOARD] simulate_key_up is not supported on this platform.");
}

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn simulate_typing(text: &str) {
    println!("[PRIVATE DATA] Result processed; content omitted from logs.");
}

#[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
pub fn simulate_backspaces(count: usize) {
    println!("[KEYBOARD] simulate_backspaces: {}", count);
}

pub fn normalize_newlines(text: &str) -> String {
    let normalized = text.replace("\r\n", "\n");
    #[cfg(target_os = "windows")]
    { normalized.replace('\n', "\r\n") }
    #[cfg(not(target_os = "windows"))]
    { normalized }
}

// ==========================================
// Shared Clipboard Injection Logic
// ==========================================
pub fn paste_text(text: &str) {
    let normalized = normalize_newlines(text);
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

        #[cfg(not(target_os = "linux"))]
        let original_text = ctx.get_text().ok();

        if let Err(e) = ctx.set_text(normalized.clone()) {
            eprintln!("[CLIPBOARD WARNING] Attempt {} failed to set clipboard text: {:?}", attempt + 1, e);
            continue;
        }

        clipboard_success = true;
        thread::sleep(Duration::from_millis(80));
        simulate_paste();

        // On Linux, accessibility metadata may not identify a focused editor.
        // Keep the dictated text available if injection had nowhere to go.
        #[cfg(not(target_os = "linux"))]
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

/// Pastes transformed text to replace the active selection.
/// Crucially, this leaves the transformed text in the clipboard permanently
/// so that if the selection was in a read-only context (e.g. a browser webpage),
/// the user can paste it anywhere they want.
pub fn paste_transformed_text(text: &str) {
    let normalized = normalize_newlines(text);
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

        if let Err(e) = ctx.set_text(normalized.clone()) {
            eprintln!("[CLIPBOARD WARNING] Attempt {} failed to set clipboard text: {:?}", attempt + 1, e);
            continue;
        }

        clipboard_success = true;
        thread::sleep(Duration::from_millis(80));
        simulate_paste();
        // Intentionally DO NOT restore previous clipboard!
        // The transformed text is kept in the user's clipboard as a fallback.
        break;
    }

    if !clipboard_success {
        eprintln!("[CLIPBOARD ERROR] All clipboard paste attempts failed for transformed text.");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_clipboard_lifecycle() {
        if let Ok(mut c) = Clipboard::new() {
            let sample = "SpeekToMe Test Selection";
            let _ = c.set_text(sample.to_string());
            assert_eq!(c.get_text().ok(), Some(sample.to_string()));
            let _ = c.clear();
            assert_eq!(c.get_text().unwrap_or_default(), "");
        }
    }

    #[test]
    fn test_normalize_newlines() {
        let unix = "Line 1\nLine 2\n\nLine 3";
        #[cfg(target_os = "windows")]
        let expected = "Line 1\r\nLine 2\r\n\r\nLine 3";
        #[cfg(not(target_os = "windows"))]
        let expected = unix;
        assert_eq!(normalize_newlines(unix), expected);

        let windows = "Line 1\r\nLine 2\r\n\r\nLine 3";
        assert_eq!(normalize_newlines(windows), expected);
    }

    #[test]
    fn test_multibyte_unicode_slicing() {
        // Test Hindi text with multi-byte Devanagari characters including virama/halants (\u{94d})
        let hindi_text = "क्लाउड कंप्यूटिंग हमारे तकनीक तक पहुँचने और प्रबंधित करने के तरीके में क्रांति लाती है।";
        
        let preview_display = if let Some((start_byte, _)) = hindi_text.char_indices().rev().nth(31) {
            let tail = &hindi_text[start_byte..];
            format!("✨ ...{}", tail.trim_start())
        } else {
            format!("✨ {}", hindi_text)
        };

        assert!(preview_display.starts_with("✨ ..."));

        // Short string test (< 32 chars)
        let short_hindi = "नमस्ते दुनिया";
        let short_preview = if let Some((start_byte, _)) = short_hindi.char_indices().rev().nth(31) {
            let tail = &short_hindi[start_byte..];
            format!("✨ ...{}", tail.trim_start())
        } else {
            format!("✨ {}", short_hindi)
        };
        assert_eq!(short_preview, format!("✨ {}", short_hindi));
    }
}
