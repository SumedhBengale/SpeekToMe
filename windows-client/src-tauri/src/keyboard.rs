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
pub fn simulate_typing(text: &str) {
    unsafe {
        let utf16_chars: Vec<u16> = text.encode_utf16().collect();
        for &c in &utf16_chars {
            let mut inputs = [std::mem::zeroed::<INPUT>(); 2];

            // 1. Key Down
            inputs[0].r#type = INPUT_KEYBOARD;
            let mut ki0: KEYBDINPUT = std::mem::zeroed();
            ki0.wVk = 0;
            ki0.wScan = c;
            ki0.dwFlags = KEYEVENTF_UNICODE;
            inputs[0].Anonymous.ki = ki0;

            // 2. Key Up
            inputs[1].r#type = INPUT_KEYBOARD;
            let mut ki1: KEYBDINPUT = std::mem::zeroed();
            ki1.wVk = 0;
            ki1.wScan = c;
            ki1.dwFlags = KEYEVENTF_UNICODE | KEYEVENTF_KEYUP;
            inputs[1].Anonymous.ki = ki1;

            send_inputs(&inputs);

            thread::sleep(Duration::from_millis(2));
        }
    }
}

// ==========================================
// Non-Windows Stubs (macOS, iOS, Android)
// ==========================================
#[cfg(target_os = "windows")]
pub fn release_modifier_keys() {
    unsafe {
        let mut inputs = [std::mem::zeroed::<INPUT>(); 6];
        let vks = [0x11, 0xA2, 0xA3, 0x12, 0xA4, 0xA5]; // VK_CONTROL, VK_LCONTROL, VK_RCONTROL, VK_MENU, VK_LMENU, VK_RMENU

        for (i, &vk) in vks.iter().enumerate() {
            inputs[i].r#type = INPUT_KEYBOARD;
            let mut ki: KEYBDINPUT = std::mem::zeroed();
            ki.wVk = vk;
            ki.dwFlags = KEYEVENTF_KEYUP;
            inputs[i].Anonymous.ki = ki;
        }

        send_inputs(&inputs);
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

// ==========================================
// Shared Clipboard Injection Logic
// ==========================================
pub fn paste_text(text: &str) {
    let mut ctx = match Clipboard::new() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("Failed to open clipboard: {:?}", e);
            return;
        }
    };

    let original_text = ctx.get_text().ok();
    
    if let Err(e) = ctx.set_text(text.to_owned()) {
        eprintln!("Failed to set clipboard text: {:?}", e);
        return;
    }
    
    thread::sleep(Duration::from_millis(80));
    
    simulate_paste();

    if let Some(orig) = original_text {
        thread::spawn(move || {
            thread::sleep(Duration::from_millis(250));
            if let Ok(mut ctx) = Clipboard::new() {
                let _ = ctx.set_text(orig);
            }
        });
    }
}
