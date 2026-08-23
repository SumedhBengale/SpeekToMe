// ==========================================
// Windows Implementation
// ==========================================
#[cfg(target_os = "windows")]
mod win32_impl {
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
        WNDCLASSW, SetTimer, KillTimer, WM_CREATE, WM_TIMER,
        SetWindowPos, SWP_NOZORDER, SWP_NOACTIVATE, UpdateLayeredWindow,
    };
    use windows_sys::Win32::Graphics::Gdi::{
        BeginPaint, EndPaint, SelectObject, DeleteObject,
        PAINTSTRUCT, CreateCompatibleDC, DeleteDC,
        CreateDIBSection, GetDC, ReleaseDC,
    };
    use windows_sys::Win32::UI::Shell::{
        Shell_NotifyIconW, NOTIFYICONDATAW, NIM_ADD, NIM_DELETE, NIF_ICON, NIF_MESSAGE, NIF_TIP, ExtractIconW,
    };
    use windows_sys::Win32::System::LibraryLoader::GetModuleFileNameW;

    const SM_CXSCREEN: i32 = 0;
    const SM_CYSCREEN: i32 = 1;

    pub struct SendHWND(pub HWND);
    unsafe impl Send for SendHWND {}
    unsafe impl Sync for SendHWND {}

    #[link(name = "winmm")]
    extern "system" {
        fn PlaySoundW(pszsound: *const u8, hmod: *mut std::ffi::c_void, fdwsound: u32) -> i32;
    }

    const SND_ASYNC: u32 = 0x0001;
    const SND_MEMORY: u32 = 0x0004;
    const SND_NODEFAULT: u32 = 0x0002;

    const WM_TRAYICON: u32 = 0x8000 + 1;

    pub fn encode_wide(s: &str) -> Vec<u16> {
        s.encode_utf16().chain(std::iter::once(0)).collect()
    }

    pub fn calculate_pill_position(config: &crate::config::AppConfig, width: i32, height: i32) -> (i32, i32) {
        if config.pill_position_mode == "custom" {
            (config.pill_custom_x, config.pill_custom_y)
        } else {
            unsafe {
                let screen_width = GetSystemMetrics(SM_CXSCREEN);
                let screen_height = GetSystemMetrics(SM_CYSCREEN);
                
                match config.pill_predefined_position.as_str() {
                    "Top Left" => (60, 60),
                    "Top Right" => (screen_width - width - 60, 60),
                    "Bottom Center" => ((screen_width - width) / 2, screen_height - height - 100),
                    "Bottom Left" => (60, screen_height - height - 100),
                    "Bottom Right" => (screen_width - width - 60, screen_height - height - 100),
                    _ => ((screen_width - width) / 2, 60), // Top Center
                }
            }
        }
    }

    pub fn update_overlay_position() {
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            let hwnd = send_hwnd.0;
            let (x, y) = {
                let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                calculate_pill_position(&config, 76, 32)
            };
            unsafe {
                SetWindowPos(
                    hwnd,
                    std::ptr::null_mut(),
                    x,
                    y,
                    76,
                    32,
                    SWP_NOZORDER | SWP_NOACTIVATE,
                );
                update_layered_overlay(hwnd);
            }
        }
    }

    pub fn play_chime(start: bool) {
        if !crate::ACTIVE_SOUND_FEEDBACK.load(Ordering::Relaxed) {
            return;
        }

        static START_CHIME: &[u8] = include_bytes!("chimes/start.wav");
        static END_CHIME: &[u8] = include_bytes!("chimes/end.wav");

        let bytes = if start { START_CHIME } else { END_CHIME };
        unsafe {
            PlaySoundW(bytes.as_ptr(), std::ptr::null_mut(), SND_ASYNC | SND_MEMORY | SND_NODEFAULT);
        }
    }

    pub unsafe extern "system" fn wnd_proc_overlay(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
        match msg {
            WM_CREATE => {
                SetTimer(hwnd, 1, 30, None);
                0
            }
            WM_TIMER => {
                update_layered_overlay(hwnd);
                0
            }
            WM_PAINT => {
                let mut ps: PAINTSTRUCT = mem::zeroed();
                let _hdc = BeginPaint(hwnd, &mut ps);
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

    pub unsafe fn update_layered_overlay(hwnd: HWND) {
        let mut rect: RECT = mem::zeroed();
        windows_sys::Win32::UI::WindowsAndMessaging::GetClientRect(hwnd, &mut rect as *mut RECT);
        let width = rect.right - rect.left;
        let height = rect.bottom - rect.top;
        if width <= 0 || height <= 0 {
            return;
        }

        let hdc_screen = GetDC(std::ptr::null_mut());
        let hdc_mem = CreateCompatibleDC(hdc_screen);

        let mut bmi: windows_sys::Win32::Graphics::Gdi::BITMAPINFO = mem::zeroed();
        bmi.bmiHeader.biSize = mem::size_of::<windows_sys::Win32::Graphics::Gdi::BITMAPINFOHEADER>() as u32;
        bmi.bmiHeader.biWidth = width;
        bmi.bmiHeader.biHeight = -height;
        bmi.bmiHeader.biPlanes = 1;
        bmi.bmiHeader.biBitCount = 32;
        bmi.bmiHeader.biCompression = 0;

        let mut bits: *mut std::ffi::c_void = ptr::null_mut();
        let dib = CreateDIBSection(
            hdc_mem,
            &bmi as *const _,
            0,
            &mut bits,
            ptr::null_mut(),
            0,
        );

        if !dib.is_null() && !bits.is_null() {
            let old_bitmap = SelectObject(hdc_mem, dib);
            let pixel_count = (width * height) as usize;
            let pixel_slice = std::slice::from_raw_parts_mut(bits as *mut u32, pixel_count);
            for pixel in pixel_slice.iter_mut() {
                *pixel = 0;
            }

            let bg_color = crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed);
            let wave_color = crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed);

            let r = height as f32 / 2.0;
            let c1_x = r;
            let c1_y = r;
            let c2_x = width as f32 - r;
            let c2_y = r;

            let bg_r = bg_color & 0xFF;
            let bg_g = (bg_color >> 8) & 0xFF;
            let bg_b = (bg_color >> 16) & 0xFF;

            for y in 0..height {
                for x in 0..width {
                    let px = x as f32 + 0.5;
                    let py = y as f32 + 0.5;

                    let d = if px < c1_x {
                        ((px - c1_x).powi(2) + (py - c1_y).powi(2)).sqrt()
                    } else if px > c2_x {
                        ((px - c2_x).powi(2) + (py - c2_y).powi(2)).sqrt()
                    } else {
                        (py - c1_y).abs()
                    };

                    let alpha = if d <= r - 0.5 {
                        235
                    } else if d >= r + 0.5 {
                        0
                    } else {
                        let factor = (r + 0.5 - d).clamp(0.0, 1.0);
                        (235.0 * factor) as u32
                    };

                    if alpha > 0 {
                        let pr = (bg_r * alpha) / 255;
                        let pg = (bg_g * alpha) / 255;
                        let pb = (bg_b * alpha) / 255;
                        pixel_slice[(y * width + x) as usize] = (alpha << 24) | (pr << 16) | (pg << 8) | pb;
                    }
                }
            }

            static SMOOTHED_VOLUME: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let raw_vol = crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed);
            let mut smoothed = SMOOTHED_VOLUME.load(Ordering::Relaxed);
            smoothed = ((smoothed as f32 * 0.8) + (raw_vol as f32 * 0.2)) as u32;
            SMOOTHED_VOLUME.store(smoothed, Ordering::Relaxed);

            let baseline = if smoothed > 2 { 6.5 } else { 3.0 };
            let dynamic_gain = (smoothed as f32 / 4.0).min(3.0);
            let scaled_vol = baseline + dynamic_gain;

            static FRAME_COUNT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let frame = FRAME_COUNT.fetch_add(1, Ordering::Relaxed) as f32;

            let bar_r = wave_color & 0xFF;
            let bar_g = (wave_color >> 8) & 0xFF;
            let bar_b = (wave_color >> 16) & 0xFF;

            let center_y = height as f32 / 2.0;
            let bar_width = 3.0;
            let gap = 2.0;
            let total_width = 5.0 * bar_width + 4.0 * gap;
            let start_x = (width as f32 - total_width) / 2.0;

            for i in 0..5 {
                let phase = frame * 0.1 + (i as f32) * 0.7;
                let multiplier = 0.35 + 0.65 * (phase.sin().abs());
                let bar_height = (scaled_vol * multiplier).clamp(2.0, 11.0);

                let x_center = start_x + (i as f32) * (bar_width + gap) + bar_width / 2.0;
                let y_start = center_y - bar_height + 1.0;
                let y_end = center_y + bar_height - 1.0;

                for y in 0..height {
                    for x in 0..width {
                        let px = x as f32 + 0.5;
                        let py = y as f32 + 0.5;

                        if (px - x_center).abs() > 2.0 {
                            continue;
                        }

                        let d = if py < y_start {
                            ((px - x_center).powi(2) + (py - y_start).powi(2)).sqrt()
                        } else if py > y_end {
                            ((px - x_center).powi(2) + (py - y_end).powi(2)).sqrt()
                        } else {
                            (px - x_center).abs()
                        };

                        let bar_alpha = if d <= 1.0 {
                            255
                        } else if d >= 2.0 {
                            0
                        } else {
                            ((2.0 - d) * 255.0) as u32
                        };

                        if bar_alpha > 0 {
                            let idx = (y * width + x) as usize;
                            let dst = pixel_slice[idx];
                            let dst_a = dst >> 24;

                            let src_r = (bar_r * bar_alpha) / 255;
                            let src_g = (bar_g * bar_alpha) / 255;
                            let src_b = (bar_b * bar_alpha) / 255;

                            if dst_a == 0 {
                                pixel_slice[idx] = (bar_alpha << 24) | (src_r << 16) | (src_g << 8) | src_b;
                            } else {
                                let out_r = src_r + (dst >> 16 & 0xFF) * (255 - bar_alpha) / 255;
                                let out_g = src_g + (dst >> 8 & 0xFF) * (255 - bar_alpha) / 255;
                                let out_b = src_b + (dst & 0xFF) * (255 - bar_alpha) / 255;
                                let out_a = bar_alpha + dst_a * (255 - bar_alpha) / 255;
                                pixel_slice[idx] = (out_a << 24) | (out_r.min(255) << 16) | (out_g.min(255) << 8) | out_b.min(255);
                            }
                        }
                    }
                }
            }

            let psize = windows_sys::Win32::Foundation::SIZE { cx: width, cy: height };
            let ppt_src = windows_sys::Win32::Foundation::POINT { x: 0, y: 0 };
            
            let mut blend: windows_sys::Win32::Graphics::Gdi::BLENDFUNCTION = mem::zeroed();
            blend.BlendOp = 0;
            blend.BlendFlags = 0;
            blend.SourceConstantAlpha = 255;
            blend.AlphaFormat = 1;

            UpdateLayeredWindow(
                hwnd,
                std::ptr::null_mut(),
                std::ptr::null(),
                &psize as *const _,
                hdc_mem,
                &ppt_src as *const _,
                0,
                &blend as *const _,
                2,
            );

            SelectObject(hdc_mem, old_bitmap);
            DeleteObject(dib);
        }

        DeleteDC(hdc_mem);
        ReleaseDC(std::ptr::null_mut(), hdc_screen);
    }

    pub unsafe extern "system" fn wnd_proc_tray(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
        match msg {
            WM_TRAYICON => {
                let event = lparam as u32;
                if event == windows_sys::Win32::UI::WindowsAndMessaging::WM_LBUTTONDBLCLK {
                    if let Some(app) = crate::TAURI_APP.get() {
                        crate::show_or_create_settings_window(app);
                    }
                } else if event == windows_sys::Win32::UI::WindowsAndMessaging::WM_RBUTTONUP {
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
                            crate::show_or_create_settings_window(app);
                        }
                    } else if cmd == 2 {
                        if let Some(app) = crate::TAURI_APP.get() {
                            app.exit(0);
                        }
                        windows_sys::Win32::System::Threading::ExitProcess(0);
                    }
                }
                0
            }
            WM_DESTROY => {
                windows_sys::Win32::System::Threading::ExitProcess(0);
            }
            _ => DefWindowProcW(hwnd, msg, wparam, lparam),
        }
    }

    pub unsafe extern "system" fn keyboard_hook_proc(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
        if crate::SIMULATING_KEY.load(Ordering::SeqCst) {
            return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam);
        }

        if code >= 0 {
            let kb_struct = *(lparam as *const KBDLLHOOKSTRUCT);
            let vk_code = kb_struct.vkCode;
            
            let hotkey_vk = crate::ACTIVE_HOTKEY_VK.load(Ordering::Relaxed);
            
            if vk_code == hotkey_vk {
                let wparam_usize = wparam as usize;
                if wparam_usize == WM_KEYDOWN as usize || wparam_usize == WM_SYSKEYDOWN as usize {
                    let mut state = crate::get_state().lock().unwrap_or_else(|e| e.into_inner());
                    if !state.is_key_held {
                        let has_token = crate::ACTIVE_HAS_SESSION_TOKEN.load(Ordering::Relaxed);
                        if !has_token {
                            unsafe {
                                windows_sys::Win32::System::Diagnostics::Debug::MessageBeep(0x00000010);
                            }
                            println!("Please log in from SpeekTo.Me Settings before dictating!");
                            return 1;
                        }
                        
                        state.is_key_held = true;
                        state.press_time = Some(std::time::Instant::now());
                        
                        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
                            ShowWindow(send_hwnd.0, SW_SHOWNA);
                        }
                        
                        let consecutive = if let Some(last) = *crate::LAST_RELEASE_TIME.get_or_init(|| std::sync::Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner()) {
                            std::time::Instant::now().duration_since(last) < std::time::Duration::from_secs(5)
                        } else {
                            false
                        };
                        crate::IS_CONSECUTIVE.store(consecutive, Ordering::SeqCst);
                        
                        play_chime(true);
                        
                        let (tx, rx) = std::sync::mpsc::channel();
                        state.stop_tx = Some(tx);
                        thread::spawn(move || {
                            crate::audio::recording_thread(rx);
                        });
                    }
                    return 1;
                } else if wparam_usize == WM_KEYUP as usize || wparam_usize == WM_SYSKEYUP as usize {
                    let mut state = crate::get_state().lock().unwrap_or_else(|e| e.into_inner());
                    if state.is_key_held {
                        state.is_key_held = false;
                        
                        let mut last_release = crate::LAST_RELEASE_TIME.get_or_init(|| std::sync::Mutex::new(None)).lock().unwrap_or_else(|e| e.into_inner());
                        *last_release = Some(std::time::Instant::now());
                        
                        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
                            ShowWindow(send_hwnd.0, SW_HIDE);
                        }
                        
                        play_chime(false);
                        
                        let is_short_tap = if let Some(press_time) = state.press_time.take() {
                            press_time.elapsed() < Duration::from_millis(350)
                        } else {
                            false
                        };
                        
                        if let Some(tx) = state.stop_tx.take() {
                            let _ = tx.send(true);
                        }
                        
                        // Ensure OS modifier states are released
                        crate::keyboard::release_modifier_keys();
                        
                        if is_short_tap {
                            thread::spawn(move || {
                                thread::sleep(Duration::from_millis(20));
                                crate::keyboard::simulate_single_key(hotkey_vk);
                            });
                        }
                        return 1;
                    }
                }
            } else {
                let wparam_usize = wparam as usize;
                if wparam_usize == WM_KEYDOWN as usize || wparam_usize == WM_SYSKEYDOWN as usize {
                    let mut state = crate::get_state().lock().unwrap_or_else(|e| e.into_inner());
                    if state.is_key_held {
                        state.is_key_held = false;
                        state.press_time = None;
                        if let Some(tx) = state.stop_tx.take() {
                            let _ = tx.send(false);
                        }
                        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
                            ShowWindow(send_hwnd.0, SW_HIDE);
                        }
                        play_chime(false);
                        crate::keyboard::release_modifier_keys();
                    }
                }
            }
        }
        CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam)
    }

    pub unsafe fn run_win32_hook_and_tray_thread() {
        PlaySoundW(std::ptr::null(), std::ptr::null_mut(), 0);
        let hinstance = windows_sys::Win32::System::LibraryLoader::GetModuleHandleW(ptr::null());
        
        let overlay_class = encode_wide("SpeekTo.MeOverlayClass");
        let wnd_class_overlay = WNDCLASSW {
            style: CS_HREDRAW | CS_VREDRAW,
            lpfnWndProc: Some(wnd_proc_overlay),
            cbClsExtra: 0,
            cbWndExtra: 0,
            hInstance: hinstance,
            hIcon: std::ptr::null_mut(),
            hCursor: windows_sys::Win32::UI::WindowsAndMessaging::LoadCursorW(std::ptr::null_mut(), 32512 as *const u16),
            hbrBackground: std::ptr::null_mut(),
            lpszMenuName: ptr::null(),
            lpszClassName: overlay_class.as_ptr(),
        };
        RegisterClassW(&wnd_class_overlay);
        
        let width = 76;
        let height = 32;
        let (x, y) = {
            let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
            calculate_pill_position(&config, width, height)
        };
        
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
        
        update_layered_overlay(hwnd_overlay);
        crate::OVERLAY_HWND.set(SendHWND(hwnd_overlay)).ok();
        
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
        
        let mut nid: NOTIFYICONDATAW = mem::zeroed();
        nid.cbSize = mem::size_of::<NOTIFYICONDATAW>() as u32;
        nid.hWnd = hwnd_tray;
        nid.uID = 1;
        nid.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
        nid.uCallbackMessage = WM_TRAYICON;
        let mut exe_path = [0u16; 260];
        let len = GetModuleFileNameW(hinstance, exe_path.as_mut_ptr(), 260);
        let mut h_icon = std::ptr::null_mut();
        if len > 0 {
            let extracted = ExtractIconW(hinstance, exe_path.as_ptr(), 0);
            if !extracted.is_null() && (extracted as usize) != 1 {
                h_icon = extracted as *mut std::ffi::c_void;
            }
        }
        if h_icon.is_null() {
            h_icon = windows_sys::Win32::UI::WindowsAndMessaging::LoadIconW(std::ptr::null_mut(), 32512 as *const u16);
        }
        nid.hIcon = h_icon;
        
        let tip = encode_wide("SpeekTo.Me (Hold Shortcut to transcribe)");
        for (i, &c) in tip.iter().enumerate() {
            if i < nid.szTip.len() {
                nid.szTip[i] = c;
            }
        }
        let tray_ok = Shell_NotifyIconW(NIM_ADD, &nid);
        println!("[WIN32] System tray icon registered (success: {}).", tray_ok != 0);
        
        let hook_id = SetWindowsHookExW(
            WH_KEYBOARD_LL,
            Some(keyboard_hook_proc),
            hinstance,
            0
        );
        
        if hook_id == std::ptr::null_mut() {
            eprintln!("[WIN32 ERROR] Failed to register global keyboard hook!");
        } else {
            println!("[WIN32] Global keyboard hook successfully registered.");
        }
        
        let mut msg: MSG = mem::zeroed();
        while GetMessageW(&mut msg, std::ptr::null_mut(), 0, 0) > 0 {
            TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }
        
        println!("[WIN32] Win32 message loop exited.");
        UnhookWindowsHookEx(hook_id);
        Shell_NotifyIconW(NIM_DELETE, &nid);
    }
    pub fn ensure_single_instance() {
        use windows_sys::Win32::Foundation::GetLastError;
        use windows_sys::Win32::Foundation::ERROR_ALREADY_EXISTS;
        use windows_sys::Win32::System::Threading::CreateMutexW;

        static SINGLE_INSTANCE_MUTEX: std::sync::atomic::AtomicIsize = std::sync::atomic::AtomicIsize::new(0);
        let mutex_name = encode_wide("Local\\SpeekToMeSingleInstanceMutex");
        unsafe {
            let h_mutex = CreateMutexW(std::ptr::null(), 1, mutex_name.as_ptr());
            if h_mutex.is_null() || GetLastError() == ERROR_ALREADY_EXISTS {
                eprintln!("[SpeekTo.Me] Another instance is already running. Exiting.");
                windows_sys::Win32::System::Threading::ExitProcess(0);
            }
            SINGLE_INSTANCE_MUTEX.store(h_mutex as isize, std::sync::atomic::Ordering::SeqCst);
        }
    }
}

#[cfg(target_os = "windows")]
pub use win32_impl::{SendHWND, calculate_pill_position, update_overlay_position, play_chime, run_win32_hook_and_tray_thread, ensure_single_instance};

// ==========================================
// Non-Windows Implementation Stubs
// ==========================================
#[cfg(not(target_os = "windows"))]
mod win32_stub {
    pub struct SendHWND(pub *mut std::ffi::c_void);
    unsafe impl Send for SendHWND {}
    unsafe impl Sync for SendHWND {}

    pub fn calculate_pill_position(_config: &crate::config::AppConfig, _width: i32, _height: i32) -> (i32, i32) {
        (0, 0)
    }

    pub fn update_overlay_position() {}

    pub fn play_chime(_start: bool) {}

    pub fn ensure_single_instance() {}

    pub unsafe fn run_win32_hook_and_tray_thread() {
        // Simple sleep loop to keep helper thread alive without consuming CPU resources
        loop {
            std::thread::sleep(std::time::Duration::from_secs(3600));
        }
    }
}

#[cfg(not(target_os = "windows"))]
pub use win32_stub::{SendHWND, calculate_pill_position, update_overlay_position, play_chime, run_win32_hook_and_tray_thread, ensure_single_instance};
