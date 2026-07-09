use std::mem;
use std::ptr;
use std::sync::atomic::Ordering;
use std::thread;
use std::time::Duration;

use windows_sys::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM, RECT};
use windows_sys::Win32::UI::WindowsAndMessaging::{
    CallNextHookEx, SetWindowsHookExW, UnhookWindowsHookEx,
    WH_KEYBOARD_LL, KBDLLHOOKSTRUCT, MSG,
    GetMessageW, TranslateMessage, DispatchMessageW, PostQuitMessage, DefWindowProcW,
    RegisterClassW, CreateWindowExW, ShowWindow, GetSystemMetrics,
    WM_KEYDOWN, WM_KEYUP, WM_SYSKEYDOWN, WM_SYSKEYUP, WM_PAINT, WM_DESTROY,
    CS_HREDRAW, CS_VREDRAW, SW_HIDE, SW_SHOWNA, WS_POPUP,
    WS_EX_TOPMOST, WS_EX_TOOLWINDOW, WS_EX_LAYERED, WS_EX_TRANSPARENT, WS_EX_NOACTIVATE,
    WNDCLASSW, SetLayeredWindowAttributes, LWA_ALPHA, LWA_COLORKEY,
    SetTimer, KillTimer, WM_CREATE, WM_TIMER,
};
use windows_sys::Win32::Graphics::Gdi::{
    BeginPaint, EndPaint, CreateSolidBrush, RoundRect, SelectObject, DeleteObject,
    CreatePen, PAINTSTRUCT, PS_SOLID, UpdateWindow, InvalidateRect, FillRect,
    CreateCompatibleDC, CreateCompatibleBitmap, StretchBlt, SetStretchBltMode,
    SetBrushOrgEx, DeleteDC, SRCCOPY, HALFTONE,
};
use windows_sys::Win32::UI::Shell::{
    Shell_NotifyIconW, NOTIFYICONDATAW, NIM_ADD, NIM_DELETE, NIF_ICON, NIF_MESSAGE, NIF_TIP,
};

// GDI and Windows Constants
const SM_CXSCREEN: i32 = 0;

// Win32 Tray Message Identifier
const WM_TRAYICON: u32 = 0x8000 + 1; // WM_USER + 1

// Thread-safe wrapper for HWND
pub struct SendHWND(pub HWND);
unsafe impl Send for SendHWND {}
unsafe impl Sync for SendHWND {}

// Convert Rust &str to UTF-16 wide string
pub fn encode_wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(std::iter::once(0)).collect()
}

// Win32 window callback procedure for painting the borderless overlay
pub unsafe extern "system" fn wnd_proc_overlay(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    match msg {
        WM_CREATE => {
            SetTimer(hwnd, 1, 30, None);
            0
        }
        WM_TIMER => {
            InvalidateRect(hwnd, ptr::null(), 0);
            0
        }
        WM_PAINT => {
            let mut ps: PAINTSTRUCT = mem::zeroed();
            let hdc = BeginPaint(hwnd, &mut ps);
            
            let width = 60;
            let height = 28;
            let scale = 4;
            
            let s_width = width * scale;
            let s_height = height * scale;
            
            // Create double-buffer memory DC for super-sampled anti-aliasing (SSAA)
            let mem_hdc = CreateCompatibleDC(hdc);
            let mem_bitmap = CreateCompatibleBitmap(hdc, s_width, s_height);
            let old_bitmap = SelectObject(mem_hdc, mem_bitmap);
            
            // Fill background of memory DC with black (color key for transparency)
            let black_brush = CreateSolidBrush(0x00000000);
            let mut r = RECT { left: 0, top: 0, right: s_width, bottom: s_height };
            FillRect(mem_hdc, &mut r as *mut RECT, black_brush);
            DeleteObject(black_brush);
            
            // Resolve theme colors dynamically (BGR format for GDI)
            let (bg_color, border_color, wave_color) = {
                let config = crate::get_active_config().lock().unwrap();
                match config.theme.as_str() {
                    "forest" => (0xFAF9FF, 0x120A24, 0xDCD1FF),   // Pinkish Cream BG, Burgundy border, Rose waveform
                    "lavender" => (0x2F101A, 0xFFD7F0, 0xFFD7F0), // Dark Purple BG, Lavender border & waveform
                    "mint" => (0x1C220A, 0xD0F3A7, 0xD0F3A7),     // Dark Mint BG, Mint border & waveform
                    _ => (0x1E1C1C, 0x76E600, 0x76E600),          // Classic Dark BG, Neon green border & waveform
                }
            };
            
            // 1. Draw rounded rectangle background (capsule shape) at 4x scale
            let bg_brush = CreateSolidBrush(bg_color);
            let old_brush = SelectObject(mem_hdc, bg_brush);
            
            // 2. Premium border at 4x scale (thickness 2 * 4 = 8)
            let border_pen = CreatePen(PS_SOLID, 8, border_color);
            let old_pen = SelectObject(mem_hdc, border_pen);
            
            RoundRect(mem_hdc, 0, 0, s_width, s_height, s_height, s_height);
            
            SelectObject(mem_hdc, old_pen);
            DeleteObject(border_pen);
            SelectObject(mem_hdc, old_brush);
            DeleteObject(bg_brush);
            
            // 3. Draw 5 vertical bars at 4x scale
            static SMOOTHED_VOLUME: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let raw_vol = crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed);
            let mut smoothed = SMOOTHED_VOLUME.load(Ordering::Relaxed);
            
            smoothed = ((smoothed as f32 * 0.8) + (raw_vol as f32 * 0.2)) as u32;
            SMOOTHED_VOLUME.store(smoothed, Ordering::Relaxed);
            
            let baseline = if smoothed > 2 { 6.5 * scale as f32 } else { 3.0 * scale as f32 };
            let dynamic_gain = (smoothed as f32 / 4.0).min(3.0) * scale as f32;
            let scaled_vol = baseline + dynamic_gain;
            
            static FRAME_COUNT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let frame = FRAME_COUNT.fetch_add(1, Ordering::Relaxed) as f32;
            
            let bar_brush = CreateSolidBrush(wave_color);
            let old_bar_brush = SelectObject(mem_hdc, bar_brush);
            let bar_pen = CreatePen(PS_SOLID, 1, wave_color);
            let old_bar_pen = SelectObject(mem_hdc, bar_pen);
            
            let center_y = s_height / 2; // 56
            let bar_width = 3 * scale;  // 12
            let gap = 2 * scale;        // 8
            let total_width = 5 * bar_width + 4 * gap; // 92
            let start_x = (s_width - total_width) / 2; // 74
            
            for i in 0..5 {
                let phase = frame * 0.1 + (i as f32) * 0.7;
                let multiplier = 0.35 + 0.65 * (phase.sin().abs());
                let bar_height = (scaled_vol * multiplier).clamp(2.0 * scale as f32, 11.0 * scale as f32) as i32;
                
                let x1 = start_x + i * (bar_width + gap);
                let x2 = x1 + bar_width;
                let y1 = center_y - bar_height;
                let y2 = center_y + bar_height;
                
                RoundRect(mem_hdc, x1, y1, x2, y2, 8, 8);
            }
            
            SelectObject(mem_hdc, old_bar_pen);
            DeleteObject(bar_pen);
            SelectObject(mem_hdc, old_bar_brush);
            DeleteObject(bar_brush);
            
            // 4. StretchBlt with HALFTONE mode down to screen DC (bilinear anti-aliasing)
            SetStretchBltMode(hdc, HALFTONE);
            SetBrushOrgEx(hdc, 0, 0, ptr::null_mut());
            StretchBlt(hdc, 0, 0, width, height, mem_hdc, 0, 0, s_width, s_height, SRCCOPY);
            
            // Cleanup memory DC resources
            SelectObject(mem_hdc, old_bitmap);
            DeleteObject(mem_bitmap);
            DeleteDC(mem_hdc);
            
            EndPaint(hwnd, &ps as *const PAINTSTRUCT);
            0
        }
        WM_DESTROY => {
            KillTimer(hwnd, 1);
            PostQuitMessage(0);
            0
        }
        _ => DefWindowProcW(hwnd, msg, wparam, lparam),
    }
}

// Win32 window callback procedure for the invisible tray message handler
pub unsafe extern "system" fn wnd_proc_tray(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    match msg {
        WM_TRAYICON => {
            let event = lparam as u32;
            if event == windows_sys::Win32::UI::WindowsAndMessaging::WM_LBUTTONDBLCLK {
                if let Some(app) = crate::TAURI_APP.get() {
                    use tauri::Manager;
                    if let Some(window) = app.get_webview_window("main") {
                        let _ = window.show();
                        let _ = window.set_focus();
                    }
                }
            } else if event == windows_sys::Win32::UI::WindowsAndMessaging::WM_RBUTTONUP {
                // Render standard context menu at mouse cursor coordinates
                let menu = windows_sys::Win32::UI::WindowsAndMessaging::CreatePopupMenu();
                let settings_str = encode_wide("Settings");
                let exit_str = encode_wide("Exit");
                windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(menu, windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING, 1, settings_str.as_ptr());
                windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(menu, windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING, 2, exit_str.as_ptr());
                
                let mut pt = mem::zeroed();
                windows_sys::Win32::UI::WindowsAndMessaging::GetCursorPos(&mut pt);
                windows_sys::Win32::UI::WindowsAndMessaging::SetForegroundWindow(hwnd);
                
                let cmd = windows_sys::Win32::UI::WindowsAndMessaging::TrackPopupMenu(
                    menu,
                    windows_sys::Win32::UI::WindowsAndMessaging::TPM_RETURNCMD | windows_sys::Win32::UI::WindowsAndMessaging::TPM_RIGHTBUTTON,
                    pt.x,
                    pt.y,
                    0,
                    hwnd,
                    ptr::null()
                );
                windows_sys::Win32::UI::WindowsAndMessaging::DestroyMenu(menu);
                
                if cmd == 1 {
                    if let Some(app) = crate::TAURI_APP.get() {
                        use tauri::Manager;
                        if let Some(window) = app.get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        }
                    }
                } else if cmd == 2 {
                    if let Some(app) = crate::TAURI_APP.get() {
                        app.exit(0);
                    }
                    windows_sys::Win32::UI::WindowsAndMessaging::PostQuitMessage(0);
                }
            }
            0
        }
        WM_DESTROY => {
            0
        }
        _ => DefWindowProcW(hwnd, msg, wparam, lparam),
    }
}

// Low-level keyboard hook procedure. Captures configured global shortcut.
pub unsafe extern "system" fn keyboard_hook_proc(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if crate::SIMULATING_KEY.load(Ordering::SeqCst) {
        return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam);
    }

    if code >= 0 {
        let kb_struct = *(lparam as *const KBDLLHOOKSTRUCT);
        let vk_code = kb_struct.vkCode;
        
        let hotkey_vk = {
            let config = crate::get_active_config().lock().unwrap();
            crate::config::hotkey_name_to_vk(&config.hotkey)
        };
        
        if vk_code == hotkey_vk {
            let wparam_usize = wparam as usize;
            if wparam_usize == WM_KEYDOWN as usize || wparam_usize == WM_SYSKEYDOWN as usize {
                let mut state = crate::get_state().lock().unwrap();
                if !state.is_key_held {
                    // Enforce authentication check before starting dictation
                    let has_token = {
                        let config = crate::get_active_config().lock().unwrap();
                        config.session_token.is_some()
                    };
                    if !has_token {
                        unsafe {
                            windows_sys::Win32::System::Diagnostics::Debug::MessageBeep(0x00000010); // MB_ICONHAND (error chime)
                        }
                        println!("Please log in from SpeekTo.Me Settings before dictating!");
                        return 1;
                    }
                    
                    state.is_key_held = true;
                    state.press_time = Some(std::time::Instant::now());
                    
                    // Show indicator bar
                    if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
                        ShowWindow(send_hwnd.0, SW_SHOWNA);
                        UpdateWindow(send_hwnd.0);
                    }
                    
                    // Check if consecutive holding (less than 5 seconds since last release)
                    let consecutive = if let Some(last) = *crate::LAST_RELEASE_TIME.get_or_init(|| std::sync::Mutex::new(None)).lock().unwrap() {
                        std::time::Instant::now().duration_since(last) < std::time::Duration::from_secs(5)
                    } else {
                        false
                    };
                    crate::IS_CONSECUTIVE.store(consecutive, Ordering::SeqCst);
                    
                    // Audio feedback chime
                    let play_chime = {
                        let config = crate::get_active_config().lock().unwrap();
                        config.sound_feedback
                    };
                    if play_chime {
                        windows_sys::Win32::System::Diagnostics::Debug::MessageBeep(0x00000040); // MB_ICONASTERISK (pleasant info chime)
                    }
                    
                    // Setup channel and spawn audio recording
                    let (tx, rx) = std::sync::mpsc::channel();
                    state.stop_tx = Some(tx);
                    thread::spawn(move || {
                        crate::audio::recording_thread(rx);
                    });
                }
                return 1; // Swallow key-down event
            } else if wparam_usize == WM_KEYUP as usize || wparam_usize == WM_SYSKEYUP as usize {
                let mut state = crate::get_state().lock().unwrap();
                if state.is_key_held {
                    state.is_key_held = false;
                    
                    // Save key-release timestamp for smart spacing duration measurement
                    let mut last_release = crate::LAST_RELEASE_TIME.get_or_init(|| std::sync::Mutex::new(None)).lock().unwrap();
                    *last_release = Some(std::time::Instant::now());
                    
                    // Hide indicator bar
                    if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
                        ShowWindow(send_hwnd.0, SW_HIDE);
                    }
                    
                    let is_short_tap = if let Some(press_time) = state.press_time.take() {
                        press_time.elapsed() < Duration::from_millis(350)
                    } else {
                        false
                    };
                    
                    // Stop audio stream and trigger transcription
                    if let Some(tx) = state.stop_tx.take() {
                        let _ = tx.send(());
                    }
                    
                    if is_short_tap {
                        // Quick tap: simulate normal keypress so it toggles (Caps Lock) or inputs (Space) normally
                        thread::spawn(move || {
                            thread::sleep(Duration::from_millis(20));
                            crate::SIMULATING_KEY.store(true, Ordering::SeqCst);
                            crate::keyboard::simulate_single_key(hotkey_vk);
                            crate::SIMULATING_KEY.store(false, Ordering::SeqCst);
                        });
                    }
                }
                return 1; // Swallow key-up event
            }
        }
    }
    CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam)
}

// Win32 Background Worker Thread (Hook + Tray message pump)
pub unsafe fn run_win32_hook_and_tray_thread() {
    let hinstance = windows_sys::Win32::System::LibraryLoader::GetModuleHandleW(ptr::null());
    
    // 1. Register and Create Overlay Window
    let overlay_class = encode_wide("SpeekTo.MeOverlayClass");
    let wnd_class_overlay = WNDCLASSW {
        style: CS_HREDRAW | CS_VREDRAW,
        lpfnWndProc: Some(wnd_proc_overlay),
        cbClsExtra: 0,
        cbWndExtra: 0,
        hInstance: hinstance,
        hIcon: std::ptr::null_mut(),
        hCursor: windows_sys::Win32::UI::WindowsAndMessaging::LoadCursorW(std::ptr::null_mut(), 32512 as *const u16), // IDC_ARROW
        hbrBackground: std::ptr::null_mut(),
        lpszMenuName: ptr::null(),
        lpszClassName: overlay_class.as_ptr(),
    };
    RegisterClassW(&wnd_class_overlay);
    
    let width = 60;
    let height = 28;
    let screen_width = GetSystemMetrics(SM_CXSCREEN);
    let x = (screen_width - width) / 2;
    let y = 60;
    
    let hwnd_overlay = CreateWindowExW(
        WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOACTIVATE,
        overlay_class.as_ptr(),
        encode_wide("SpeekTo.Me Overlay").as_ptr(),
        WS_POPUP,
        x, y, width, height,
        std::ptr::null_mut(), std::ptr::null_mut(), hinstance, ptr::null()
    );
    
    if hwnd_overlay == std::ptr::null_mut() {
        panic!("Failed to create SpeekTo.Me overlay window!");
    }
    
    // Set color key (black = 0x00000000) for transparency + global alpha (235)
    SetLayeredWindowAttributes(hwnd_overlay, 0x00000000, 235, LWA_ALPHA | LWA_COLORKEY);
    crate::OVERLAY_HWND.set(SendHWND(hwnd_overlay)).ok();
    
    // 2. Register and Create Invisible Helper Tray Window
    let tray_class = encode_wide("SpeekTo.MeTrayClass");
    let wnd_class_tray = WNDCLASSW {
        style: 0,
        lpfnWndProc: Some(wnd_proc_tray),
        cbClsExtra: 0,
        cbWndExtra: 0,
        hInstance: hinstance,
        hIcon: std::ptr::null_mut(),
        hCursor: std::ptr::null_mut(),
        hbrBackground: std::ptr::null_mut(),
        lpszMenuName: ptr::null(),
        lpszClassName: tray_class.as_ptr(),
    };
    RegisterClassW(&wnd_class_tray);
    
    let hwnd_tray = CreateWindowExW(
        0,
        tray_class.as_ptr(),
        encode_wide("SpeekTo.Me Tray Helper Window").as_ptr(),
        0,
        0, 0, 0, 0,
        std::ptr::null_mut(), std::ptr::null_mut(), hinstance, ptr::null()
    );
    
    if hwnd_tray == std::ptr::null_mut() {
        panic!("Failed to create SpeekTo.Me tray helper window!");
    }
    
    // 3. Register System Tray Icon
    let mut nid: NOTIFYICONDATAW = mem::zeroed();
    nid.cbSize = mem::size_of::<NOTIFYICONDATAW>() as u32;
    nid.hWnd = hwnd_tray;
    nid.uID = 1;
    nid.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
    nid.uCallbackMessage = WM_TRAYICON;
    let mut h_icon = windows_sys::Win32::UI::WindowsAndMessaging::LoadIconW(hinstance, 32512 as *const u16);
    if h_icon == std::ptr::null_mut() {
        h_icon = windows_sys::Win32::UI::WindowsAndMessaging::LoadIconW(std::ptr::null_mut(), 32512 as *const u16);
    }
    nid.hIcon = h_icon;
    
    let tip = encode_wide("SpeekTo.Me (Hold Shortcut to transcribe)");
    for (i, &c) in tip.iter().enumerate() {
        if i < nid.szTip.len() {
            nid.szTip[i] = c;
        }
    }
    Shell_NotifyIconW(NIM_ADD, &nid);
    
    // 4. Install Keyboard Hook
    let hook_id = SetWindowsHookExW(
        WH_KEYBOARD_LL,
        Some(keyboard_hook_proc),
        hinstance,
        0
    );
    
    if hook_id == std::ptr::null_mut() {
        panic!("Failed to register global keyboard hook!");
    }
    
    // Run background Win32 message pump
    let mut msg: MSG = mem::zeroed();
    while GetMessageW(&mut msg, std::ptr::null_mut(), 0, 0) > 0 {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    
    // Cleanup on thread exit
    UnhookWindowsHookEx(hook_id);
    Shell_NotifyIconW(NIM_DELETE, &nid);
}
