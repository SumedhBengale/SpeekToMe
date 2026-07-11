use std::mem;
use std::thread;
use std::time::Duration;
use arboard::Clipboard;

// Simulate pressing Ctrl+V via Win32 SendInput
pub fn simulate_paste() {
    unsafe {
        let mut inputs = [mem::zeroed::<INPUT>(); 4];
        
        // 1. Ctrl Down
        inputs[0].r#type = INPUT_KEYBOARD;
        let mut ki0: KEYBDINPUT = mem::zeroed();
        ki0.wVk = 0x11; // VK_CONTROL
        inputs[0].Anonymous.ki = ki0;

        // 2. V Down
        inputs[1].r#type = INPUT_KEYBOARD;
        let mut ki1: KEYBDINPUT = mem::zeroed();
        ki1.wVk = 0x56; // VK_V
        inputs[1].Anonymous.ki = ki1;

        // 3. V Up
        inputs[2].r#type = INPUT_KEYBOARD;
        let mut ki2: KEYBDINPUT = mem::zeroed();
        ki2.wVk = 0x56;
        ki2.dwFlags = KEYEVENTF_KEYUP;
        inputs[2].Anonymous.ki = ki2;

        // 4. Ctrl Up
        inputs[3].r#type = INPUT_KEYBOARD;
        let mut ki3: KEYBDINPUT = mem::zeroed();
        ki3.wVk = 0x11;
        ki3.dwFlags = KEYEVENTF_KEYUP;
        inputs[3].Anonymous.ki = ki3;

        windows_sys::Win32::UI::Input::KeyboardAndMouse::SendInput(
            4,
            inputs.as_ptr() as *const windows_sys::Win32::UI::Input::KeyboardAndMouse::INPUT,
            mem::size_of::<INPUT>() as i32
        );
    }
}

// Simulate a single key press and release natively (for forwarding short taps of swallowed keys)
pub fn simulate_single_key(vk: u32) {
    unsafe {
        let mut inputs = [mem::zeroed::<INPUT>(); 2];
        
        // 1. Key Down
        inputs[0].r#type = INPUT_KEYBOARD;
        let mut ki0: KEYBDINPUT = mem::zeroed();
        ki0.wVk = vk as u16;
        inputs[0].Anonymous.ki = ki0;

        // 2. Key Up
        inputs[1].r#type = INPUT_KEYBOARD;
        let mut ki1: KEYBDINPUT = mem::zeroed();
        ki1.wVk = vk as u16;
        ki1.dwFlags = KEYEVENTF_KEYUP;
        inputs[1].Anonymous.ki = ki1;

        windows_sys::Win32::UI::Input::KeyboardAndMouse::SendInput(
            2,
            inputs.as_ptr() as *const windows_sys::Win32::UI::Input::KeyboardAndMouse::INPUT,
            mem::size_of::<INPUT>() as i32
        );
    }
}

// Simulate a key down event natively
pub fn simulate_key_down(vk: u32) {
    unsafe {
        let mut input = mem::zeroed::<INPUT>();
        input.r#type = INPUT_KEYBOARD;
        let mut ki: KEYBDINPUT = mem::zeroed();
        ki.wVk = vk as u16;
        input.Anonymous.ki = ki;

        let inputs = [input];
        windows_sys::Win32::UI::Input::KeyboardAndMouse::SendInput(
            1,
            inputs.as_ptr() as *const windows_sys::Win32::UI::Input::KeyboardAndMouse::INPUT,
            mem::size_of::<INPUT>() as i32
        );
    }
}

// Simulate a key up event natively
pub fn simulate_key_up(vk: u32) {
    unsafe {
        let mut input = mem::zeroed::<INPUT>();
        input.r#type = INPUT_KEYBOARD;
        let mut ki: KEYBDINPUT = mem::zeroed();
        ki.wVk = vk as u16;
        ki.dwFlags = KEYEVENTF_KEYUP;
        input.Anonymous.ki = ki;

        let inputs = [input];
        windows_sys::Win32::UI::Input::KeyboardAndMouse::SendInput(
            1,
            inputs.as_ptr() as *const windows_sys::Win32::UI::Input::KeyboardAndMouse::INPUT,
            mem::size_of::<INPUT>() as i32
        );
    }
}

pub const KEYEVENTF_UNICODE: u32 = 0x0004;

// Simulate typing an entire string character by character using Unicode key inputs
pub fn simulate_typing(text: &str) {
    unsafe {
        let utf16_chars: Vec<u16> = text.encode_utf16().collect();
        for &c in &utf16_chars {
            let mut inputs = [mem::zeroed::<INPUT>(); 2];
            
            // 1. Key Down
            inputs[0].r#type = INPUT_KEYBOARD;
            let mut ki0: KEYBDINPUT = mem::zeroed();
            ki0.wVk = 0;
            ki0.wScan = c;
            ki0.dwFlags = KEYEVENTF_UNICODE;
            inputs[0].Anonymous.ki = ki0;

            // 2. Key Up
            inputs[1].r#type = INPUT_KEYBOARD;
            let mut ki1: KEYBDINPUT = mem::zeroed();
            ki1.wVk = 0;
            ki1.wScan = c;
            ki1.dwFlags = KEYEVENTF_UNICODE | KEYEVENTF_KEYUP;
            inputs[1].Anonymous.ki = ki1;

            windows_sys::Win32::UI::Input::KeyboardAndMouse::SendInput(
                2,
                inputs.as_ptr() as *const windows_sys::Win32::UI::Input::KeyboardAndMouse::INPUT,
                mem::size_of::<INPUT>() as i32
            );
            
            // A 2ms delay per character avoids buffering drops in fast text blocks
            thread::sleep(Duration::from_millis(2));
        }
    }
}

// Redefine Win32 INPUT structures manually for windows-sys safety and Copy implementation
#[repr(C)]
#[derive(Copy, Clone)]
#[allow(non_snake_case)]
pub struct INPUT {
    pub r#type: u32,
    pub Anonymous: INPUT_0,
}

#[repr(C)]
#[derive(Copy, Clone)]
#[allow(non_snake_case)]
pub union INPUT_0 {
    pub ki: KEYBDINPUT,
    pub mi: [u8; 32], // Padding
}

#[repr(C)]
#[derive(Copy, Clone)]
#[allow(non_snake_case)]
pub struct KEYBDINPUT {
    pub wVk: u16,
    pub wScan: u16,
    pub dwFlags: u32,
    pub time: u32,
    pub dwExtraInfo: usize,
}

pub const INPUT_KEYBOARD: u32 = 1;
pub const KEYEVENTF_KEYUP: u32 = 2;

// Copy text to clipboard and simulate paste command
pub fn paste_text(text: &str) {
    let mut ctx = match Clipboard::new() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("Failed to open clipboard: {:?}", e);
            return;
        }
    };

    // Safely backup user's original clipboard content if it is text
    let original_text = ctx.get_text().ok();
    
    if let Err(e) = ctx.set_text(text.to_owned()) {
        eprintln!("Failed to set clipboard text: {:?}", e);
        return;
    }
    
    // Give target application time to adapt to active focus
    thread::sleep(Duration::from_millis(80));
    
    simulate_paste();

    // Restore original clipboard text after a brief delay to allow target application to paste it first
    if let Some(orig) = original_text {
        thread::spawn(move || {
            thread::sleep(Duration::from_millis(250));
            if let Ok(mut ctx) = Clipboard::new() {
                let _ = ctx.set_text(orig);
            }
        });
    }
}
