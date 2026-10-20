// ==========================================
// Windows Implementation
// ==========================================
#[cfg(target_os = "windows")]
mod win32_impl {
    use std::mem;
    use std::ptr;
    use std::sync::atomic::Ordering;
    use std::sync::Mutex;
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
        SetWindowPos, SWP_NOZORDER, SWP_NOACTIVATE, SWP_NOSIZE, SWP_SHOWWINDOW, SWP_NOMOVE,
        HWND_TOPMOST, UpdateLayeredWindow, BringWindowToTop, WINDOWPOS, WM_WINDOWPOSCHANGING,
        CreateIconIndirect, DestroyIcon, ICONINFO, SendMessageW,
    };
    use windows_sys::Win32::Graphics::Gdi::{
        BeginPaint, EndPaint, SelectObject, DeleteObject,
        PAINTSTRUCT, CreateCompatibleDC, DeleteDC,
        CreateDIBSection, GetDC, ReleaseDC,
        CreateFontW, SetBkMode, SetTextColor,
        CreateBitmap, BITMAPINFO, BITMAPINFOHEADER, DIB_RGB_COLORS, BI_RGB,
        HDC,
    };
    use windows_sys::Win32::UI::Shell::{
        Shell_NotifyIconW, NOTIFYICONDATAW, NIM_ADD, NIM_DELETE, NIM_MODIFY,
        NIF_ICON, NIF_MESSAGE, NIF_TIP, NIF_INFO,
        NIIF_INFO, NIIF_WARNING, NIIF_ERROR,
    };

    const SM_CXSCREEN: i32 = 0;
    const SM_CYSCREEN: i32 = 1;

    const FW_NORMAL: i32 = 400;
    const DEFAULT_CHARSET: u32 = 1;
    const OUT_DEFAULT_PRECIS: u32 = 0;
    const CLIP_DEFAULT_PRECIS: u32 = 0;
    const CLEARTYPE_QUALITY: u32 = 5;
    const DEFAULT_PITCH: u32 = 0;
    const FF_DONTCARE: u32 = 0;
    const TRANSPARENT: u32 = 1;

    const DT_LEFT: u32 = 0x00000000;
    const DT_WORDBREAK: u32 = 0x00000010;
    const DT_CALCRECT: u32 = 0x00000400;
    const DT_NOPREFIX: u32 = 0x00000800;
    const DT_VCENTER: u32 = 0x00000004;
    const DT_SINGLELINE: u32 = 0x00000020;
    const FW_SEMIBOLD: i32 = 600;

    const GWL_EXSTYLE: i32 = -20;
    const CS_DBLCLKS: u32 = 0x0008;
    const SWP_FRAMECHANGED: u32 = 0x0020;
    const WM_MOUSEMOVE: u32 = 0x0200;
    const WM_LBUTTONDOWN: u32 = 0x0201;
    const WM_LBUTTONUP: u32 = 0x0202;
    const WM_LBUTTONDBLCLK: u32 = 0x0203;
    const WM_RBUTTONUP: u32 = 0x0205;
    const WM_SETCURSOR: u32 = 0x0020;
    const WM_CAPTURECHANGED: u32 = 0x0215;

    static IS_DRAGGING: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
    static DRAG_OFFSET_X: std::sync::atomic::AtomicI32 = std::sync::atomic::AtomicI32::new(0);
    static DRAG_OFFSET_Y: std::sync::atomic::AtomicI32 = std::sync::atomic::AtomicI32::new(0);
    static MEETING_DRAGGED_X: std::sync::atomic::AtomicI32 = std::sync::atomic::AtomicI32::new(-1);
    static MEETING_DRAGGED_Y: std::sync::atomic::AtomicI32 = std::sync::atomic::AtomicI32::new(-1);

    #[link(name = "user32")]
    extern "system" {
        fn DrawTextW(
            hdc: HDC,
            lpchtext: *const u16,
            cchtext: i32,
            lprect: *mut RECT,
            uformat: u32,
        ) -> i32;
        fn GetWindowLongW(hWnd: HWND, nIndex: i32) -> i32;
        fn SetWindowLongW(hWnd: HWND, nIndex: i32, dwNewLong: i32) -> i32;
        fn GetWindowRect(hWnd: HWND, lpRect: *mut RECT) -> i32;
        fn ReleaseCapture() -> i32;
        fn SetCapture(hWnd: HWND) -> HWND;
        fn GetCursorPos(lpPoint: *mut windows_sys::Win32::Foundation::POINT) -> i32;
        fn LoadCursorW(hInstance: *mut std::ffi::c_void, lpCursorName: *const u16) -> *mut std::ffi::c_void;
        fn SetCursor(hCursor: *mut std::ffi::c_void) -> *mut std::ffi::c_void;
    }

    pub struct SendHWND(pub HWND);
    unsafe impl Send for SendHWND {}
    unsafe impl Sync for SendHWND {}

    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum PreviewState {
        Hidden = 0,
        Listening = 1,
        Processing = 2,
        Meeting = 3,
    }

    impl From<u8> for PreviewState {
        fn from(val: u8) -> Self {
            match val {
                1 => PreviewState::Listening,
                2 => PreviewState::Processing,
                3 => PreviewState::Meeting,
                _ => PreviewState::Hidden,
            }
        }
    }

    pub static PREVIEW_HWND: std::sync::OnceLock<SendHWND> = std::sync::OnceLock::new();
    pub static PREVIEW_TEXT: std::sync::OnceLock<std::sync::Mutex<String>> = std::sync::OnceLock::new();
    pub static PREVIEW_STATE: std::sync::atomic::AtomicU8 = std::sync::atomic::AtomicU8::new(0);
    pub static TRAY_HWND: std::sync::OnceLock<SendHWND> = std::sync::OnceLock::new();

    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum NotificationLevel {
        Info,
        Warning,
        Error,
    }

    pub fn show_tray_notification(title: &str, message: &str, level: NotificationLevel) {
        if let Some(send_hwnd) = TRAY_HWND.get() {
            unsafe {
                let mut nid: NOTIFYICONDATAW = mem::zeroed();
                nid.cbSize = mem::size_of::<NOTIFYICONDATAW>() as u32;
                nid.hWnd = send_hwnd.0;
                nid.uID = 1;
                nid.uFlags = NIF_INFO;

                let wide_title = encode_wide(title);
                for (i, &c) in wide_title.iter().enumerate() {
                    if i < nid.szInfoTitle.len() {
                        nid.szInfoTitle[i] = c;
                    }
                }

                let wide_msg = encode_wide(message);
                for (i, &c) in wide_msg.iter().enumerate() {
                    if i < nid.szInfo.len() {
                        nid.szInfo[i] = c;
                    }
                }

                nid.dwInfoFlags = match level {
                    NotificationLevel::Info => NIIF_INFO,
                    NotificationLevel::Warning => NIIF_WARNING,
                    NotificationLevel::Error => NIIF_ERROR,
                };

                Shell_NotifyIconW(NIM_MODIFY, &nid);
            }
        }
    }

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

    pub fn get_preview_text() -> String {
        PREVIEW_TEXT.get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }

    pub fn set_preview_text(text: &str) {
        {
            let mut lock = PREVIEW_TEXT.get_or_init(|| std::sync::Mutex::new(String::new()))
                .lock()
                .unwrap_or_else(|e| e.into_inner());
            *lock = text.to_string();
        }
        let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
        if state != PreviewState::Hidden {
            update_preview_position();
        }
    }

    pub fn set_preview_state(state: PreviewState) {
        PREVIEW_STATE.store(state as u8, Ordering::SeqCst);
        match state {
            PreviewState::Hidden | PreviewState::Meeting => {
                hide_preview();
            }
            PreviewState::Listening | PreviewState::Processing => {
                show_preview();
                update_preview_position();
            }
        }
    }

    pub fn calculate_pill_position(config: &crate::config::AppConfig, width: i32, height: i32) -> (i32, i32) {
        if config.pill_position_mode == "custom" {
            (config.pill_custom_x, config.pill_custom_y)
        } else {
            unsafe {
                let screen_width = GetSystemMetrics(SM_CXSCREEN);
                let screen_height = GetSystemMetrics(SM_CYSCREEN);
                
                match config.pill_predefined_position.as_str() {
                    "Top Left" => (50, 50),
                    "Top Right" => (screen_width - width - 50, 50),
                    "Bottom Center" => ((screen_width - width) / 2, screen_height - height - 80),
                    "Bottom Left" => (50, screen_height - height - 80),
                    "Bottom Right" => (screen_width - width - 50, screen_height - height - 80),
                    _ => ((screen_width - width) / 2, 50), // Top Center
                }
            }
        }
    }

    // 4px outer margin on each side for the refined atmospheric outer halo
    pub const GLOW_MARGIN: i32 = 4;

    pub unsafe fn calculate_preview_bounds(text: &str) -> (i32, i32, i32, i32) {
        let screen_width = GetSystemMetrics(SM_CXSCREEN);
        let screen_height = GetSystemMetrics(SM_CYSCREEN);

        let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        let (pill_x, pill_y) = calculate_pill_position(&config, 62, 24);

        let display_text = if text.trim().is_empty() {
            let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
            if state == PreviewState::Processing { "Polishing..." } else { "Listening..." }
        } else {
            text
        };

        let wide_text = encode_wide(display_text);

        let hdc_screen = GetDC(std::ptr::null_mut());
        let font_name = encode_wide("Segoe UI");
        let hfont = CreateFontW(
            -13, 0, 0, 0,
            FW_NORMAL, 0, 0, 0,
            DEFAULT_CHARSET,
            OUT_DEFAULT_PRECIS,
            CLIP_DEFAULT_PRECIS,
            CLEARTYPE_QUALITY,
            DEFAULT_PITCH | FF_DONTCARE,
            font_name.as_ptr(),
        );

        let old_font = SelectObject(hdc_screen, hfont);

        let max_text_width = 340;
        let mut calc_rect = RECT {
            left: 0,
            top: 0,
            right: max_text_width,
            bottom: 0,
        };

        if wide_text.len() > 1 {
            DrawTextW(
                hdc_screen,
                wide_text.as_ptr(),
                (wide_text.len() - 1) as i32,
                &mut calc_rect,
                DT_CALCRECT | DT_WORDBREAK | DT_LEFT | DT_NOPREFIX,
            );
        }

        SelectObject(hdc_screen, old_font);
        DeleteObject(hfont);
        ReleaseDC(std::ptr::null_mut(), hdc_screen);

        let measured_w = calc_rect.right - calc_rect.left;
        let measured_h = calc_rect.bottom - calc_rect.top;

        let pad_x = 12;
        let pad_y = 6;
        let card_w = (measured_w + pad_x * 2).clamp(120, 380);
        let card_h = (measured_h + pad_y * 2).max(28);

        // Include GLOW_MARGIN on all 4 sides for the outer halo
        let total_w = card_w + GLOW_MARGIN * 2;
        let total_h = card_h + GLOW_MARGIN * 2;

        let mut preview_x = pill_x + (62 - total_w) / 2;
        preview_x = preview_x.clamp(4, screen_width - total_w - 4);

        let preview_y = if pill_y < screen_height / 2 {
            // Below the pill
            (pill_y + 24 + 4).min(screen_height - total_h - 4)
        } else {
            // Above the pill
            (pill_y - total_h - 4).max(4)
        };

        (preview_x, preview_y, total_w, total_h)
    }

    pub fn show_overlay() {
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            let hwnd = send_hwnd.0;
            unsafe {
                let ex_style = GetWindowLongW(hwnd, GWL_EXSTYLE);
                if (ex_style & (WS_EX_TRANSPARENT as i32)) == 0 {
                    SetWindowLongW(hwnd, GWL_EXSTYLE, ex_style | (WS_EX_TRANSPARENT as i32) | (WS_EX_TOPMOST as i32));
                }
                let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                let (x, y) = calculate_pill_position(&config, 62, 24);
                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    x - 4, y - 4, 70, 32,
                    SWP_NOACTIVATE | SWP_FRAMECHANGED | SWP_SHOWWINDOW,
                );
                BringWindowToTop(hwnd);
                update_layered_overlay(hwnd);
                ShowWindow(hwnd, SW_SHOWNA);
                SetTimer(hwnd, 1, 30, None);
            }
        }
        if let Some(app) = crate::TAURI_APP.get() {
            use tauri::Emitter;
            let _ = app.emit("dictation-recording-started", ());
        }
    }

    pub fn hide_overlay() {
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            unsafe {
                ShowWindow(send_hwnd.0, SW_HIDE);
            }
        }
        if let Some(app) = crate::TAURI_APP.get() {
            use tauri::Emitter;
            let _ = app.emit("dictation-recording-stopped", ());
        }
    }

    pub fn update_tray_tooltip(text: &str) {
        if let Some(send_hwnd) = TRAY_HWND.get() {
            unsafe {
                let mut nid: NOTIFYICONDATAW = mem::zeroed();
                nid.cbSize = mem::size_of::<NOTIFYICONDATAW>() as u32;
                nid.hWnd = send_hwnd.0;
                nid.uID = 1;
                nid.uFlags = NIF_TIP;
                let wide_tip = encode_wide(text);
                for (i, &c) in wide_tip.iter().enumerate() {
                    if i < nid.szTip.len() {
                        nid.szTip[i] = c;
                    }
                }
                Shell_NotifyIconW(NIM_MODIFY, &nid);
            }
        }
    }

    pub fn show_meeting_overlay() {
        set_preview_state(PreviewState::Meeting);
        MEETING_DRAGGED_X.store(-1, Ordering::Relaxed);
        MEETING_DRAGGED_Y.store(-1, Ordering::Relaxed);
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            let hwnd = send_hwnd.0;
            unsafe {
                // Remove WS_EX_TRANSPARENT so the overlay pill can be clicked & dragged by the user; ensure WS_EX_TOPMOST
                let ex_style = GetWindowLongW(hwnd, GWL_EXSTYLE);
                SetWindowLongW(hwnd, GWL_EXSTYLE, (ex_style & !(WS_EX_TRANSPARENT as i32)) | (WS_EX_TOPMOST as i32));

                let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                let (x, y) = calculate_pill_position(&config, 104, 24);

                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    x, y, 104, 24,
                    SWP_NOACTIVATE | SWP_FRAMECHANGED | SWP_SHOWWINDOW,
                );
                BringWindowToTop(hwnd);
                update_layered_overlay(hwnd);
                ShowWindow(hwnd, SW_SHOWNA);
                SetTimer(hwnd, 1, 30, None);
            }
        }
        update_tray_tooltip("SpeekTo.Me - 🔴 Scribing Meeting...");
        if let Some(app) = crate::TAURI_APP.get() {
            use tauri::Emitter;
            let _ = app.emit("meeting-recording-started", ());
        }
    }

    pub fn hide_meeting_overlay() {
        MEETING_DRAGGED_X.store(-1, Ordering::Relaxed);
        MEETING_DRAGGED_Y.store(-1, Ordering::Relaxed);
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            let hwnd = send_hwnd.0;
            unsafe {
                // Restore WS_EX_TRANSPARENT for normal click-through dictation; ensure WS_EX_TOPMOST
                let ex_style = GetWindowLongW(hwnd, GWL_EXSTYLE);
                SetWindowLongW(hwnd, GWL_EXSTYLE, ex_style | (WS_EX_TRANSPARENT as i32) | (WS_EX_TOPMOST as i32));

                let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                let (def_x, def_y) = calculate_pill_position(&config, 62, 24);

                // Reposition back to default position and 62x24 size, then hide
                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    def_x - 4, def_y - 4, 70, 32,
                    SWP_NOACTIVATE | SWP_FRAMECHANGED,
                );
                ShowWindow(hwnd, SW_HIDE);
            }
        }
        set_preview_state(PreviewState::Hidden);
        update_tray_tooltip("SpeekTo.Me (Hold Shortcut to transcribe)");
        if let Some(app) = crate::TAURI_APP.get() {
            use tauri::Emitter;
            let _ = app.emit("meeting-recording-stopped", ());
        }
    }

    pub fn show_preview() {
        if let Some(send_hwnd) = PREVIEW_HWND.get() {
            unsafe {
                ShowWindow(send_hwnd.0, SW_SHOWNA);
            }
        }
    }

    pub fn hide_preview() {
        let current_state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
        if current_state != PreviewState::Meeting {
            PREVIEW_STATE.store(PreviewState::Hidden as u8, Ordering::SeqCst);
        }
        if let Some(lock) = PREVIEW_TEXT.get() {
            if let Ok(mut t) = lock.lock() {
                t.clear();
            }
        }
        if let Some(send_hwnd) = PREVIEW_HWND.get() {
            unsafe {
                ShowWindow(send_hwnd.0, SW_HIDE);
            }
        }
    }

    pub fn update_overlay_position() {
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            let hwnd = send_hwnd.0;
            let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
            let pill_width = if state == PreviewState::Meeting { 104 } else { 62 };
            let (x, y) = if state == PreviewState::Meeting && MEETING_DRAGGED_X.load(Ordering::Relaxed) >= 0 {
                (MEETING_DRAGGED_X.load(Ordering::Relaxed), MEETING_DRAGGED_Y.load(Ordering::Relaxed))
            } else {
                let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                calculate_pill_position(&config, pill_width, 24)
            };
            let margin=if state==PreviewState::Meeting{0}else{4};
            unsafe {
                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    x-margin,
                    y-margin,
                    pill_width+margin*2,
                    24+margin*2,
                    SWP_NOACTIVATE,
                );
                BringWindowToTop(hwnd);
                update_layered_overlay(hwnd);
            }
        }
        update_preview_position();
    }

    pub fn update_preview_position() {
        if let Some(send_hwnd) = PREVIEW_HWND.get() {
            let hwnd = send_hwnd.0;
            unsafe {
                update_layered_preview(hwnd);
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
            WM_WINDOWPOSCHANGING => {
                let wp = lparam as *mut WINDOWPOS;
                if !wp.is_null() {
                    (*wp).hwndInsertAfter = HWND_TOPMOST;
                    (*wp).flags &= !SWP_NOZORDER;
                }
                0
            }
            WM_TIMER => {
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state != PreviewState::Hidden {
                    static TOPMOST_COUNTER: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
                    let count = TOPMOST_COUNTER.fetch_add(1, Ordering::Relaxed);
                    if count % 10 == 0 {
                        SetWindowPos(
                            hwnd,
                            HWND_TOPMOST,
                            0, 0, 0, 0,
                            SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE,
                        );
                        BringWindowToTop(hwnd);
                    }
                    update_layered_overlay(hwnd);
                }
                0
            }
            WM_SETCURSOR => {
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state == PreviewState::Meeting {
                    const IDC_SIZEALL: usize = 32646;
                    let cursor = LoadCursorW(std::ptr::null_mut(), IDC_SIZEALL as *const u16);
                    if !cursor.is_null() {
                        SetCursor(cursor);
                        return 1;
                    }
                }
                DefWindowProcW(hwnd, msg, wparam, lparam)
            }
            WM_LBUTTONDOWN => {
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state == PreviewState::Meeting {
                    SetCapture(hwnd);
                    let mut pt: windows_sys::Win32::Foundation::POINT = mem::zeroed();
                    GetCursorPos(&mut pt);
                    let mut rect: RECT = mem::zeroed();
                    GetWindowRect(hwnd, &mut rect);
                    DRAG_OFFSET_X.store(pt.x - rect.left, Ordering::Relaxed);
                    DRAG_OFFSET_Y.store(pt.y - rect.top, Ordering::Relaxed);
                    IS_DRAGGING.store(true, Ordering::Relaxed);
                    0
                } else {
                    DefWindowProcW(hwnd, msg, wparam, lparam)
                }
            }
            WM_MOUSEMOVE => {
                if IS_DRAGGING.load(Ordering::Relaxed) {
                    let mut pt: windows_sys::Win32::Foundation::POINT = mem::zeroed();
                    GetCursorPos(&mut pt);
                    let off_x = DRAG_OFFSET_X.load(Ordering::Relaxed);
                    let off_y = DRAG_OFFSET_Y.load(Ordering::Relaxed);
                    let new_x = pt.x - off_x;
                    let new_y = pt.y - off_y;
                    let screen_w = GetSystemMetrics(SM_CXSCREEN);
                    let screen_h = GetSystemMetrics(SM_CYSCREEN);
                    let clamped_x = new_x.clamp(2, screen_w - 106);
                    let clamped_y = new_y.clamp(2, screen_h - 26);
                    SetWindowPos(
                        hwnd,
                        HWND_TOPMOST,
                        clamped_x,
                        clamped_y,
                        0,
                        0,
                        SWP_NOSIZE | SWP_NOACTIVATE,
                    );
                    0
                } else {
                    DefWindowProcW(hwnd, msg, wparam, lparam)
                }
            }
            WM_LBUTTONUP => {
                if IS_DRAGGING.swap(false, Ordering::Relaxed) {
                    ReleaseCapture();
                    let mut rect: RECT = mem::zeroed();
                    GetWindowRect(hwnd, &mut rect);
                    let (new_x, new_y) = (rect.left, rect.top);
                    println!("[OVERLAY] Pill placed at ({}, {}) for this meeting session.", new_x, new_y);
                    MEETING_DRAGGED_X.store(new_x, Ordering::Relaxed);
                    MEETING_DRAGGED_Y.store(new_y, Ordering::Relaxed);
                    SetWindowPos(
                        hwnd,
                        HWND_TOPMOST,
                        new_x,
                        new_y,
                        0,
                        0,
                        SWP_NOSIZE | SWP_NOACTIVATE,
                    );
                    BringWindowToTop(hwnd);
                    0
                } else {
                    DefWindowProcW(hwnd, msg, wparam, lparam)
                }
            }
            WM_CAPTURECHANGED => {
                if IS_DRAGGING.swap(false, Ordering::Relaxed) {
                    let mut rect: RECT = mem::zeroed();
                    GetWindowRect(hwnd, &mut rect);
                    let (new_x, new_y) = (rect.left, rect.top);
                    MEETING_DRAGGED_X.store(new_x, Ordering::Relaxed);
                    MEETING_DRAGGED_Y.store(new_y, Ordering::Relaxed);
                }
                0
            }
            WM_LBUTTONDBLCLK | WM_RBUTTONUP => {
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state == PreviewState::Meeting {
                    IS_DRAGGING.store(false, Ordering::Relaxed);
                    ReleaseCapture();
                    if let Some(app) = crate::TAURI_APP.get() {
                        crate::show_or_create_settings_window(app);
                    }
                    0
                } else {
                    DefWindowProcW(hwnd, msg, wparam, lparam)
                }
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

    pub unsafe extern "system" fn wnd_proc_preview(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
        match msg {
            WM_CREATE => {
                SetTimer(hwnd, 1, 30, None);
                0
            }
            WM_TIMER => {
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state == PreviewState::Processing {
                    update_layered_preview(hwnd);
                }
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

            let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
            let margin=if state==PreviewState::Meeting{0.0}else{4.0};
            let r = (height as f32 - margin*2.0) / 2.0;
            let c1_x = margin+r;
            let c1_y = margin+r;
            let c2_x = width as f32 - margin-r;
            let c2_y = margin+r;

            let bg_r = (bg_color & 0xFF) as f32;
            let bg_g = ((bg_color >> 8) & 0xFF) as f32;
            let bg_b = ((bg_color >> 16) & 0xFF) as f32;

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

                    if d >= r + 0.5 {
                        pixel_slice[(y * width + x) as usize] = 0;
                        continue;
                    }

                    let edge_cov = (r + 0.5 - d).clamp(0.0, 1.0);
                    let alpha = (235.0 * edge_cov) as u32;

                    if alpha > 0 {
                        let pr = (bg_r as u32 * alpha) / 255;
                        let pg = (bg_g as u32 * alpha) / 255;
                        let pb = (bg_b as u32 * alpha) / 255;
                        pixel_slice[(y * width + x) as usize] = (alpha << 24) | (pr << 16) | (pg << 8) | pb;
                    }
                }
            }

            static SMOOTHED_VOLUME: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let raw_vol = if state == PreviewState::Meeting {
                let mic = crate::audio::MIC_VOLUME.load(Ordering::Relaxed);
                let lb = crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed);
                mic.max(lb)
            } else {
                crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed)
            };
            let mut smoothed = SMOOTHED_VOLUME.load(Ordering::Relaxed);
            smoothed = ((smoothed as f32 * 0.8) + (raw_vol as f32 * 0.2)) as u32;
            SMOOTHED_VOLUME.store(smoothed, Ordering::Relaxed);

            static FRAME_COUNT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let frame = FRAME_COUNT.fetch_add(1, Ordering::Relaxed);

            if state==PreviewState::Processing {
                let bounds=crate::native_overlay::Rect{x:4.0,y:4.0,width:(width-8)as f64,height:(height-8)as f64};
                let background=crate::native_overlay::color(bg_color);
                for y in 0..height {for x in 0..width {
                    pixel_slice[(y*width+x)as usize]=crate::native_overlay::processing_pixel(x as f64+0.5,y as f64+0.5,bounds,12.0,frame as u64,background);
                }}
            }
            let bar_r = (wave_color & 0xFF) as u32;
            let bar_g = ((wave_color >> 8) & 0xFF) as u32;
            let bar_b = ((wave_color >> 16) & 0xFF) as u32;

            let center_y = height as f32 / 2.0;
            let bar_width = 2.5;
            let gap = 2.0;
            let total_width = 5.0 * bar_width + 4.0 * gap;
            let start_x = if state == PreviewState::Meeting {
                14.0
            } else {
                (width as f32 - total_width) / 2.0
            };

            let is_processing = state == PreviewState::Processing;

            for i in 0..5 {
                let bar_height = crate::native_overlay::bar_height(
                    frame as u64, i, smoothed as f64, is_processing,
                ) as f32;

                let x_center = start_x + (i as f32) * (bar_width + gap) + bar_width / 2.0;
                let y_start = center_y - bar_height + 0.8;
                let y_end = center_y + bar_height - 0.8;

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

                        let bar_alpha = if d <= 0.8 {
                            255
                        } else if d >= 1.8 {
                            0
                        } else {
                            ((1.8 - d) * 255.0) as u32
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

            if state == PreviewState::Meeting {
                let hdc_mask = CreateCompatibleDC(hdc_screen);
                let mut mask_bits: *mut std::ffi::c_void = ptr::null_mut();
                let mask_dib = CreateDIBSection(
                    hdc_mask,
                    &bmi as *const _,
                    0,
                    &mut mask_bits,
                    ptr::null_mut(),
                    0,
                );

                if !mask_dib.is_null() && !mask_bits.is_null() {
                    let old_mask_bitmap = SelectObject(hdc_mask, mask_dib);
                    let mask_slice = std::slice::from_raw_parts_mut(mask_bits as *mut u32, pixel_count);
                    for p in mask_slice.iter_mut() {
                        *p = 0;
                    }

                    let font_name = encode_wide("Segoe UI");
                    let hfont = CreateFontW(
                        -11, 0, 0, 0,
                        FW_SEMIBOLD, 0, 0, 0,
                        DEFAULT_CHARSET,
                        OUT_DEFAULT_PRECIS,
                        CLIP_DEFAULT_PRECIS,
                        CLEARTYPE_QUALITY,
                        DEFAULT_PITCH | FF_DONTCARE,
                        font_name.as_ptr(),
                    );

                    let old_font = SelectObject(hdc_mask, hfont);
                    SetBkMode(hdc_mask, TRANSPARENT as i32);
                    SetTextColor(hdc_mask, 0x00FFFFFF);

                    let wide_display = encode_wide("Scribing");
                    let mut text_rect = RECT {
                        left: 41,
                        top: 0,
                        right: width - 8,
                        bottom: height,
                    };

                    DrawTextW(
                        hdc_mask,
                        wide_display.as_ptr(),
                        (wide_display.len() - 1) as i32,
                        &mut text_rect,
                        DT_SINGLELINE | DT_VCENTER | DT_NOPREFIX,
                    );

                    let luminance = 0.299 * bg_r + 0.587 * bg_g + 0.114 * bg_b;
                    let (text_r, text_g, text_b) = if luminance > 128.0 {
                        (24u32, 16u32, 34u32)
                    } else {
                        (245u32, 245u32, 250u32)
                    };

                    for idx in 0..pixel_count {
                        let mask_val = mask_slice[idx];
                        let glyph_alpha = (mask_val & 0xFF) as u32;
                        if glyph_alpha > 0 {
                            let dst = pixel_slice[idx];
                            let dst_a = dst >> 24;
                            if dst_a >= 100 {
                                let dst_r = ((dst >> 16 & 0xFF) * 255) / dst_a;
                                let dst_g = ((dst >> 8 & 0xFF) * 255) / dst_a;
                                let dst_b = (dst & 0xFF) * 255 / dst_a;

                                let src_a = (glyph_alpha * 245) / 255;
                                let out_a = src_a + (dst_a * (255 - src_a)) / 255;
                                if out_a > 0 {
                                    let out_r = ((text_r * src_a) + (dst_r * dst_a * (255 - src_a) / 255)) / out_a;
                                    let out_g = ((text_g * src_a) + (dst_g * dst_a * (255 - src_a) / 255)) / out_a;
                                    let out_b = ((text_b * src_a) + (dst_b * dst_a * (255 - src_a) / 255)) / out_a;

                                    let pr = ((out_r.min(255) * out_a) / 255).min(255);
                                    let pg = ((out_g.min(255) * out_a) / 255).min(255);
                                    let pb = ((out_b.min(255) * out_a) / 255).min(255);

                                    pixel_slice[idx] = (out_a << 24) | (pr << 16) | (pg << 8) | pb;
                                }
                            }
                        }
                    }

                    SelectObject(hdc_mask, old_font);
                    DeleteObject(hfont);
                    SelectObject(hdc_mask, old_mask_bitmap);
                    DeleteObject(mask_dib);
                }
                DeleteDC(hdc_mask);
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

    // Apple Intelligence Chromatic Aurora Gradient Palette
    #[inline(always)]
    fn get_apple_intelligence_color(u:f32)->(f32,f32,f32){
        let (r,g,b)=crate::native_overlay::processing_color(u as f64);((r*255.0)as f32,(g*255.0)as f32,(b*255.0)as f32)
    }

    pub unsafe fn update_layered_preview(hwnd: HWND) {
        let raw_text = get_preview_text();
        let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
        if state == PreviewState::Hidden {
            return;
        }

        let (x, y, width, height) = calculate_preview_bounds(&raw_text);
        if width <= 0 || height <= 0 {
            return;
        }

        SetWindowPos(
            hwnd,
            std::ptr::null_mut(),
            x,
            y,
            width,
            height,
            SWP_NOZORDER | SWP_NOACTIVATE,
        );

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

            let bg_r = (bg_color & 0xFF) as f32;
            let bg_g = ((bg_color >> 8) & 0xFF) as f32;
            let bg_b = ((bg_color >> 16) & 0xFF) as f32;

            let wave_r = (wave_color & 0xFF) as f32;
            let wave_g = ((wave_color >> 8) & 0xFF) as f32;
            let wave_b = ((wave_color >> 16) & 0xFF) as f32;

            // Determine contrasting text color based on background luminance
            let luminance = 0.299 * bg_r + 0.587 * bg_g + 0.114 * bg_b;
            let (text_r, text_g, text_b) = if luminance > 128.0 {
                // Light background: Dark slate text
                (24u32, 16u32, 34u32)
            } else {
                // Dark background: Crisp pale text
                (245u32, 245u32, 250u32)
            };

            static PREVIEW_FRAME_COUNT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
            let frame = PREVIEW_FRAME_COUNT.fetch_add(1, Ordering::Relaxed) as f32;

            let w_f = width as f32;
            let h_f = height as f32;
            let gm = GLOW_MARGIN as f32;
            let card_w = w_f - gm * 2.0;
            let card_h = h_f - gm * 2.0;
            let hx = card_w / 2.0;
            let hy = card_h / 2.0;
            let cx = w_f / 2.0;
            let cy = h_f / 2.0;
            let radius = 7.0f32.min(hy - 1.0);

            let is_processing = state == PreviewState::Processing;

            // Draw rounded card background & refined Apple Intelligence Chromatic Glow
            for py in 0..height {
                for px in 0..width {
                    let x_f = px as f32 + 0.5;
                    let y_f = py as f32 + 0.5;

                    // Signed Distance Function to Card Edge (dist = 0 is card boundary)
                    let qx = (x_f - cx).abs() - (hx - radius);
                    let qy = (y_f - cy).abs() - (hy - radius);

                    let dist = if qx > 0.0 && qy > 0.0 {
                        (qx * qx + qy * qy).sqrt() - radius
                    } else {
                        qx.max(qy) - radius
                    };

                    if is_processing {
                        // Angle around center + rotational flow
                        let angle = (y_f - cy).atan2(x_f - cx); // -PI to PI
                        let u = (angle / (2.0 * std::f32::consts::PI) + 1.0 + frame * 0.008).rem_euclid(1.0);
                        let (chroma_r, chroma_g, chroma_b) = get_apple_intelligence_color(u);

                        if dist > 3.5 {
                            // Outside outer glow halo
                            pixel_slice[(py * width + px) as usize] = 0;
                            continue;
                        } else if dist > 0.0 {
                            // Sleek outer atmospheric chromatic glow halo (3.5px diffusion)
                            let halo_t = 1.0 - (dist / 3.5);
                            let halo_alpha = ((halo_t * halo_t) * 150.0) as u32;
                            if halo_alpha > 0 {
                                let pr = ((chroma_r * halo_alpha as f32) / 255.0) as u32;
                                let pg = ((chroma_g * halo_alpha as f32) / 255.0) as u32;
                                let pb = ((chroma_b * halo_alpha as f32) / 255.0) as u32;
                                pixel_slice[(py * width + px) as usize] = (halo_alpha << 24) | (pr << 16) | (pg << 8) | pb;
                            } else {
                                pixel_slice[(py * width + px) as usize] = 0;
                            }
                            continue;
                        } else {
                            // Inside card (dist <= 0.0): Sleek, refined inner border stroke (2.8px depth)
                            let inner_glow_depth = 2.8f32;
                            let stroke_mix = if dist >= -1.2 {
                                0.85 // Crisp illuminated edge
                            } else if dist >= -inner_glow_depth {
                                0.85 * ((dist + inner_glow_depth) / (inner_glow_depth - 1.2))
                            } else {
                                0.0
                            };

                            let cr = bg_r * (1.0 - stroke_mix) + chroma_r * stroke_mix;
                            let cg = bg_g * (1.0 - stroke_mix) + chroma_g * stroke_mix;
                            let cb = bg_b * (1.0 - stroke_mix) + chroma_b * stroke_mix;

                            let edge_cov = (0.5 - dist).clamp(0.0, 1.0);
                            let alpha_u = ((242.0 * edge_cov) as u32).min(255);

                            let pr = (((cr * alpha_u as f32) / 255.0).min(255.0)) as u32;
                            let pg = (((cg * alpha_u as f32) / 255.0).min(255.0)) as u32;
                            let pb = (((cb * alpha_u as f32) / 255.0).min(255.0)) as u32;

                            pixel_slice[(py * width + px) as usize] = (alpha_u << 24) | (pr << 16) | (pg << 8) | pb;
                        }
                    } else {
                        // Listening state: Clean, minimal static theme border (1.2px)
                        if dist >= 0.5 {
                            pixel_slice[(py * width + px) as usize] = 0;
                            continue;
                        }
                        let edge_cov = (0.5 - dist).clamp(0.0, 1.0);
                        let in_border = ((dist + 1.2) / 1.2).clamp(0.0, 1.0);
                        let blend = in_border * 0.40;

                        let cr = bg_r * (1.0 - blend) + wave_r * blend;
                        let cg = bg_g * (1.0 - blend) + wave_g * blend;
                        let cb = bg_b * (1.0 - blend) + wave_b * blend;

                        let alpha_u = ((240.0 * edge_cov) as u32).min(255);
                        let pr = (((cr * alpha_u as f32) / 255.0).min(255.0)) as u32;
                        let pg = (((cg * alpha_u as f32) / 255.0).min(255.0)) as u32;
                        let pb = (((cb * alpha_u as f32) / 255.0).min(255.0)) as u32;

                        pixel_slice[(py * width + px) as usize] = (alpha_u << 24) | (pr << 16) | (pg << 8) | pb;
                    }
                }
            }

            // Draw text onto a mask DC and composite
            let hdc_mask = CreateCompatibleDC(hdc_screen);
            let mut mask_bits: *mut std::ffi::c_void = ptr::null_mut();
            let mask_dib = CreateDIBSection(
                hdc_mask,
                &bmi as *const _,
                0,
                &mut mask_bits,
                ptr::null_mut(),
                0,
            );

            if !mask_dib.is_null() && !mask_bits.is_null() {
                let old_mask_bitmap = SelectObject(hdc_mask, mask_dib);
                let mask_slice = std::slice::from_raw_parts_mut(mask_bits as *mut u32, pixel_count);
                for p in mask_slice.iter_mut() {
                    *p = 0;
                }

                let font_name = encode_wide("Segoe UI");
                let hfont = CreateFontW(
                    -13, 0, 0, 0,
                    FW_NORMAL, 0, 0, 0,
                    DEFAULT_CHARSET,
                    OUT_DEFAULT_PRECIS,
                    CLIP_DEFAULT_PRECIS,
                    CLEARTYPE_QUALITY,
                    DEFAULT_PITCH | FF_DONTCARE,
                    font_name.as_ptr(),
                );

                let old_font = SelectObject(hdc_mask, hfont);
                SetBkMode(hdc_mask, TRANSPARENT as i32);
                SetTextColor(hdc_mask, 0x00FFFFFF);

                let display_text = if raw_text.trim().is_empty() {
                    if is_processing { "Polishing..." } else { "Listening..." }
                } else {
                    &raw_text
                };

                let wide_display = encode_wide(display_text);
                let pad_x = 12;
                let pad_y = 6;
                let mut text_rect = RECT {
                    left: GLOW_MARGIN + pad_x,
                    top: GLOW_MARGIN + pad_y,
                    right: width - (GLOW_MARGIN + pad_x),
                    bottom: height - (GLOW_MARGIN + pad_y),
                };

                if wide_display.len() > 1 {
                    DrawTextW(
                        hdc_mask,
                        wide_display.as_ptr(),
                        (wide_display.len() - 1) as i32,
                        &mut text_rect,
                        DT_WORDBREAK | DT_LEFT | DT_NOPREFIX,
                    );
                }

                // Composite antialiased text onto pixel_slice (only inside the card)
                for idx in 0..pixel_count {
                    let mask_val = mask_slice[idx];
                    let glyph_alpha = (mask_val & 0xFF) as u32;
                    if glyph_alpha > 0 {
                        let dst = pixel_slice[idx];
                        let dst_a = dst >> 24;
                        // Only draw text if interior card opacity is solid
                        if dst_a >= 180 {
                            let dst_r = ((dst >> 16 & 0xFF) * 255) / dst_a;
                            let dst_g = ((dst >> 8 & 0xFF) * 255) / dst_a;
                            let dst_b = (dst & 0xFF) * 255 / dst_a;

                            let src_a = (glyph_alpha * 245) / 255;
                            let out_a = src_a + (dst_a * (255 - src_a)) / 255;
                            if out_a > 0 {
                                let out_r = ((text_r * src_a) + (dst_r * dst_a * (255 - src_a) / 255)) / out_a;
                                let out_g = ((text_g * src_a) + (dst_g * dst_a * (255 - src_a) / 255)) / out_a;
                                let out_b = ((text_b * src_a) + (dst_b * dst_a * (255 - src_a) / 255)) / out_a;

                                let pr = ((out_r.min(255) * out_a) / 255).min(255);
                                let pg = ((out_g.min(255) * out_a) / 255).min(255);
                                let pb = ((out_b.min(255) * out_a) / 255).min(255);

                                pixel_slice[idx] = (out_a << 24) | (pr << 16) | (pg << 8) | pb;
                            }
                        }
                    }
                }

                SelectObject(hdc_mask, old_font);
                DeleteObject(hfont);
                SelectObject(hdc_mask, old_mask_bitmap);
                DeleteObject(mask_dib);
            }
            DeleteDC(hdc_mask);

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
                    let is_meeting = crate::ACTIVE_MEETING.lock().unwrap_or_else(|e| e.into_inner()).is_some();
                    let menu = windows_sys::Win32::UI::WindowsAndMessaging::CreatePopupMenu();

                    if is_meeting {
                        let meeting_str = encode_wide("🔴 Open Active Meeting");
                        windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(menu, windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING, 3, meeting_str.as_ptr());
                    }
                    let language_options = crate::speaking_language_options();
                    let active_language = crate::speaking_language();
                    let language_menu = windows_sys::Win32::UI::WindowsAndMessaging::CreatePopupMenu();
                    for (index, (code, name)) in language_options.iter().enumerate() {
                        let label = encode_wide(name);
                        let flags = windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING
                            | if code == &active_language { windows_sys::Win32::UI::WindowsAndMessaging::MF_CHECKED } else { 0 };
                        windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(language_menu, flags, 1000 + index, label.as_ptr());
                    }
                    let language_label = encode_wide("Speaking language");
                    windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(menu,
                        windows_sys::Win32::UI::WindowsAndMessaging::MF_POPUP, language_menu as usize, language_label.as_ptr());
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
                    
                    if cmd >= 1000 && (cmd as usize - 1000) < language_options.len() {
                        let _ = crate::set_active_language(language_options[cmd as usize - 1000].0.clone());
                    } else if cmd == 1 || cmd == 3 {
                        if let Some(app) = crate::TAURI_APP.get() {
                            crate::show_or_create_settings_window(app);
                        }
                    } else if cmd == 2 {
                        if let Some(app) = crate::TAURI_APP.get() {
                            crate::lifecycle::quit(app);
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

    fn is_key_part_of_combo(vk_code: u32, req_modifiers: u32, req_vk: u32) -> bool {
        if req_vk != 0 && vk_code == req_vk {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_CTRL != 0) && (vk_code == 0x11 || vk_code == 0xA2 || vk_code == 0xA3) {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_SHIFT != 0) && (vk_code == 0x10 || vk_code == 0xA0 || vk_code == 0xA1) {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_ALT != 0) && (vk_code == 0x12 || vk_code == 0xA4 || vk_code == 0xA5) {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_WIN != 0) && (vk_code == 0x5B || vk_code == 0x5C) {
            return true;
        }
        false
    }

    fn is_modifier_key(vk_code: u32) -> bool {
        matches!(
            vk_code,
            0x10 | 0xA0 | 0xA1 | // Shift
            0x11 | 0xA2 | 0xA3 | // Ctrl
            0x12 | 0xA4 | 0xA5 | // Alt
            0x5B | 0x5C          // Win
        )
    }

    fn should_suppress_key(vk_code: u32) -> bool {
        // Windows key (0x5B = VK_LWIN, 0x5C = VK_RWIN) must ALWAYS be suppressed
        // when part of our hotkey so the Windows Start Menu NEVER triggers.
        if vk_code == 0x5B || vk_code == 0x5C {
            return true;
        }
        !is_modifier_key(vk_code)
    }

    // Latch the physical chord independently from recording state. The chord
    // remains down while processing or copying selected text, so repeats and
    // trailing modifier releases cannot count as another input.
    static PHYSICAL_CHORD: std::sync::OnceLock<Mutex<crate::gestures::ChordLatch>> = std::sync::OnceLock::new();
    static LAST_WIN_HOTKEY_ACTIVE: Mutex<Option<std::time::Instant>> = Mutex::new(None);

    pub unsafe extern "system" fn keyboard_hook_proc(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
        if code < 0 { return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam); }
        let kb = *(lparam as *const KBDLLHOOKSTRUCT);
        // Ignore injected input, but keep real releases even while the app is
        // copying a selection. SIMULATING_KEY used to swallow those releases.
        if (kb.flags & 0x12) != 0 {
            return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam);
        }
        let vk = kb.vkCode;
        let combo = crate::ACTIVE_HOTKEY_COMBO.load(Ordering::Relaxed);
        let modifiers = combo & 0xFFFF;
        let trigger = combo >> 16;
        let down = wparam as u32 == WM_KEYDOWN || wparam as u32 == WM_SYSKEYDOWN;
        let up = wparam as u32 == WM_KEYUP || wparam as u32 == WM_SYSKEYUP;
        let part = is_key_part_of_combo(vk, modifiers, trigger);
        let mut chord = PHYSICAL_CHORD.get_or_init(|| Mutex::new(crate::gestures::ChordLatch::default()))
            .lock().unwrap_or_else(|e| e.into_inner());
        if crate::shortcuts::selecting() || !crate::HOTKEY_ALLOWED.load(Ordering::SeqCst) {
            if down || up { let _ = chord.edge(vk, down, combo); }
            return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam);
        }
        let was_active = chord.active();
        let edge = if down || up { chord.edge(vk, down, combo) } else { None };
        let mut suppress = part && (was_active || chord.active()) && should_suppress_key(vk);
        if down && was_active && !part && crate::ACTIVE_HOTKEY_MODE.load(Ordering::Relaxed) == 0
            && crate::shortcuts::is_capturing() {
            crate::shortcuts::cancel();
        }
        if let Some(pressed) = edge {
            if pressed && !crate::ACTIVE_HAS_SESSION_TOKEN.load(Ordering::SeqCst) {
                show_tray_notification("SpeekTo.Me — Sign In Required",
                    "Please sign in in Settings to start voice dictation.", NotificationLevel::Warning);
                if let Some(app) = crate::TAURI_APP.get() { crate::show_or_create_settings_window(app); }
            } else {
                crate::shortcuts::on_edge(pressed);
            }
        }
        if (modifiers & crate::config::HOTKEY_WIN) != 0 {
            if chord.active() || was_active {
                *LAST_WIN_HOTKEY_ACTIVE.lock().unwrap_or_else(|e| e.into_inner()) = Some(std::time::Instant::now());
                crate::keyboard::suppress_win_key_start_menu();
            }
            if up && matches!(vk, 0x5B | 0x5C) {
                let recent = LAST_WIN_HOTKEY_ACTIVE.lock().unwrap_or_else(|e| e.into_inner())
                    .is_some_and(|at| at.elapsed() < Duration::from_millis(600));
                if recent {
                    crate::keyboard::suppress_win_key_start_menu();
                    suppress = true;
                }
            }
        }
        if suppress { 1 } else { CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam) }
    }

    static CURRENT_TRAY_HICON: std::sync::atomic::AtomicIsize = std::sync::atomic::AtomicIsize::new(0);

    pub unsafe fn create_themed_soundwave_icon(theme: &str) -> *mut std::ffi::c_void {
        let (r, g, b) = match theme {
            "lavender" => (179u8, 136u8, 255u8),   // #b388ff
            "mint" => (0u8, 230u8, 118u8),         // #00e676
            "classic_dark" => (0u8, 230u8, 118u8), // #00e676
            _ => (244u8, 63u8, 94u8),              // "forest" / default -> #f43f5e
        };

        let size = 32i32;
        let mut bgra = vec![0u8; (size * size * 4) as usize];

        struct Bar {
            cx: f32,
            half_h: f32,
            radius: f32,
        }
        let bars = [
            Bar { cx: 4.5, half_h: 5.0, radius: 1.8 },
            Bar { cx: 10.25, half_h: 10.0, radius: 1.8 },
            Bar { cx: 16.0, half_h: 14.2, radius: 1.8 },
            Bar { cx: 21.75, half_h: 10.0, radius: 1.8 },
            Bar { cx: 27.5, half_h: 5.0, radius: 1.8 },
        ];

        for y in 0..size {
            for x in 0..size {
                let px = x as f32 + 0.5;
                let py = y as f32 + 0.5;
                let cy = 15.5f32;

                let mut min_dist_to_pill = 999.0f32;
                for bar in &bars {
                    let dx = (px - bar.cx).abs();
                    let segment_half = (bar.half_h - bar.radius).max(0.0);
                    let dy = (py - cy).abs();
                    let clamped_dy = (dy - segment_half).max(0.0);
                    let dist = (dx * dx + clamped_dy * clamped_dy).sqrt();
                    if dist < min_dist_to_pill {
                        min_dist_to_pill = dist;
                    }
                }

                let radius = 1.8f32;
                let edge = min_dist_to_pill - radius;
                let alpha = if edge <= -0.5 {
                    1.0f32
                } else if edge >= 0.5 {
                    0.0f32
                } else {
                    0.5 - edge
                };

                if alpha > 0.0 {
                    let row = (size - 1 - y) as usize;
                    let col = x as usize;
                    let idx = (row * size as usize + col) * 4;
                    let a = (alpha * 255.0).clamp(0.0, 255.0) as u8;
                    bgra[idx + 0] = (b as f32 * alpha) as u8; // B
                    bgra[idx + 1] = (g as f32 * alpha) as u8; // G
                    bgra[idx + 2] = (r as f32 * alpha) as u8; // R
                    bgra[idx + 3] = a;                        // A
                }
            }
        }

        let h_dc = GetDC(std::ptr::null_mut());
        let mut bmi: BITMAPINFO = mem::zeroed();
        bmi.bmiHeader.biSize = mem::size_of::<BITMAPINFOHEADER>() as u32;
        bmi.bmiHeader.biWidth = size;
        bmi.bmiHeader.biHeight = size;
        bmi.bmiHeader.biPlanes = 1;
        bmi.bmiHeader.biBitCount = 32;
        bmi.bmiHeader.biCompression = BI_RGB;

        let mut bits_ptr = std::ptr::null_mut();
        let h_color = CreateDIBSection(
            h_dc,
            &bmi,
            DIB_RGB_COLORS,
            &mut bits_ptr,
            std::ptr::null_mut(),
            0,
        );

        if !bits_ptr.is_null() {
            std::ptr::copy_nonoverlapping(bgra.as_ptr(), bits_ptr as *mut u8, bgra.len());
        }

        let mask_bytes = [0u8; 128];
        let h_mask = CreateBitmap(size, size, 1, 1, mask_bytes.as_ptr() as *const _);

        let mut ii: ICONINFO = mem::zeroed();
        ii.fIcon = 1;
        ii.xHotspot = 0;
        ii.yHotspot = 0;
        ii.hbmMask = h_mask;
        ii.hbmColor = h_color;

        let h_icon = CreateIconIndirect(&ii);

        DeleteObject(h_color);
        DeleteObject(h_mask);
        ReleaseDC(std::ptr::null_mut(), h_dc);

        h_icon as *mut std::ffi::c_void
    }

    pub fn update_tray_icon_for_theme(theme: &str) {
        unsafe {
            if let Some(hwnd) = TRAY_HWND.get() {
                let h_icon = create_themed_soundwave_icon(theme);
                if !h_icon.is_null() {
                    let mut nid: NOTIFYICONDATAW = mem::zeroed();
                    nid.cbSize = mem::size_of::<NOTIFYICONDATAW>() as u32;
                    nid.hWnd = hwnd.0;
                    nid.uID = 1;
                    nid.uFlags = NIF_ICON;
                    nid.hIcon = h_icon;
                    let res = Shell_NotifyIconW(NIM_MODIFY, &nid);
                    println!("[WIN32] Updated tray icon for theme: {} (success: {})", theme, res != 0);

                    let old_icon = CURRENT_TRAY_HICON.swap(h_icon as isize, Ordering::SeqCst);
                    if old_icon != 0 {
                        DestroyIcon(old_icon as *mut std::ffi::c_void);
                    }
                }
            }
        }
    }

    pub fn set_window_icon_for_theme(hwnd: *mut std::ffi::c_void, theme: &str) {
        unsafe {
            if !hwnd.is_null() {
                let h_icon = create_themed_soundwave_icon(theme);
                if !h_icon.is_null() {
                    const WM_SETICON: u32 = 0x0080;
                    const ICON_SMALL: usize = 0;
                    const ICON_BIG: usize = 1;
                    SendMessageW(hwnd as HWND, WM_SETICON, ICON_BIG, h_icon as isize);
                    SendMessageW(hwnd as HWND, WM_SETICON, ICON_SMALL, h_icon as isize);
                    println!("[WIN32] Updated taskbar window icon for theme: {}", theme);
                }
            }
        }
    }

    pub unsafe fn run_win32_hook_and_tray_thread() {
        PlaySoundW(std::ptr::null(), std::ptr::null_mut(), 0);
        let hinstance = windows_sys::Win32::System::LibraryLoader::GetModuleHandleW(ptr::null());
        
        let overlay_class = encode_wide("SpeekTo.MeOverlayClass");
        let wnd_class_overlay = WNDCLASSW {
            style: CS_HREDRAW | CS_VREDRAW | CS_DBLCLKS,
            lpfnWndProc: Some(wnd_proc_overlay),
            cbClsExtra: 0,
            cbWndExtra: 0,
            hInstance: hinstance,
            hIcon: std::ptr::null_mut(),
            hCursor: std::ptr::null_mut(),
            hbrBackground: std::ptr::null_mut(),
            lpszMenuName: std::ptr::null(),
            lpszClassName: overlay_class.as_ptr(),
        };
        RegisterClassW(&wnd_class_overlay);
        
        let hwnd_overlay = CreateWindowExW(
            WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOACTIVATE,
            overlay_class.as_ptr(),
            ptr::null(),
            WS_POPUP,
            0, 0, 70, 32,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            hinstance,
            ptr::null_mut()
        );
        
        crate::OVERLAY_HWND.set(SendHWND(hwnd_overlay)).ok();
        update_overlay_position();

        let preview_class = encode_wide("SpeekTo.MePreviewClass");
        let wnd_class_preview = WNDCLASSW {
            style: CS_HREDRAW | CS_VREDRAW,
            lpfnWndProc: Some(wnd_proc_preview),
            cbClsExtra: 0,
            cbWndExtra: 0,
            hInstance: hinstance,
            hIcon: std::ptr::null_mut(),
            hCursor: std::ptr::null_mut(),
            hbrBackground: std::ptr::null_mut(),
            lpszMenuName: std::ptr::null(),
            lpszClassName: preview_class.as_ptr(),
        };
        RegisterClassW(&wnd_class_preview);

        let hwnd_preview = CreateWindowExW(
            WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOACTIVATE,
            preview_class.as_ptr(),
            ptr::null(),
            WS_POPUP,
            0, 0, 160, 36,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            hinstance,
            ptr::null_mut()
        );

        PREVIEW_HWND.set(SendHWND(hwnd_preview)).ok();
        
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
            lpszMenuName: std::ptr::null(),
            lpszClassName: tray_class.as_ptr(),
        };
        RegisterClassW(&wnd_class_tray);
        
        let hwnd_tray = CreateWindowExW(
            0,
            tray_class.as_ptr(),
            encode_wide("SpeekTo.Me Message Window").as_ptr(),
            0,
            0, 0, 0, 0,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            hinstance,
            ptr::null_mut()
        );
        
        if hwnd_tray == std::ptr::null_mut() {
            panic!("Failed to create SpeekTo.Me tray helper window!");
        }

        TRAY_HWND.set(SendHWND(hwnd_tray)).ok();
        
        let mut nid: NOTIFYICONDATAW = mem::zeroed();
        nid.cbSize = mem::size_of::<NOTIFYICONDATAW>() as u32;
        nid.hWnd = hwnd_tray;
        nid.uID = 1;
        nid.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
        nid.uCallbackMessage = WM_TRAYICON;
        let initial_theme = "forest";
        let h_icon = create_themed_soundwave_icon(initial_theme);
        if !h_icon.is_null() {
            CURRENT_TRAY_HICON.store(h_icon as isize, Ordering::SeqCst);
        }
        nid.hIcon = h_icon;
        
        let tip = encode_wide("SpeekTo.Me (Hold Shortcut to transcribe)");
        for (i, &c) in tip.iter().enumerate() {
            if i < nid.szTip.len() {
                nid.szTip[i] = c;
            }
        }
        let tray_ok = Shell_NotifyIconW(NIM_ADD, &nid);
        println!("[WIN32] Themed system tray icon registered (success: {}).", tray_ok != 0);
        
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
pub use win32_impl::{
    SendHWND, PreviewState, PREVIEW_HWND, PREVIEW_TEXT, PREVIEW_STATE, TRAY_HWND, NotificationLevel,
    calculate_pill_position, update_overlay_position, play_chime,
    show_overlay, hide_overlay, show_preview, hide_preview,
    show_meeting_overlay, hide_meeting_overlay, update_tray_tooltip,
    set_preview_text, get_preview_text, set_preview_state, update_preview_position,
    show_tray_notification, run_win32_hook_and_tray_thread, ensure_single_instance,
    update_tray_icon_for_theme, set_window_icon_for_theme
};

// ==========================================
// Non-Windows Implementation Stubs
// ==========================================
#[cfg(not(target_os = "windows"))]
mod win32_stub {
    static PREVIEW_TEXT: std::sync::OnceLock<std::sync::Mutex<String>> = std::sync::OnceLock::new();
    pub struct SendHWND(pub *mut std::ffi::c_void);
    unsafe impl Send for SendHWND {}
    unsafe impl Sync for SendHWND {}

    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum PreviewState {
        Hidden = 0,
        Listening = 1,
        Processing = 2,
        Meeting = 3,
    }

    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum NotificationLevel {
        Info,
        Warning,
        Error,
    }

    pub fn show_tray_notification(title: &str, message: &str, _level: NotificationLevel) {
        #[cfg(target_os = "linux")]
        crate::linux_desktop::notify(title, message);
        #[cfg(target_os = "macos")]
        crate::macos_desktop::notify(title, message);
    }

    pub fn calculate_pill_position(_config: &crate::config::AppConfig, _width: i32, _height: i32) -> (i32, i32) {
        (0, 0)
    }

    pub fn update_overlay_position() {
        #[cfg(target_os = "linux")]
        crate::linux_desktop::update_overlay_position();
        #[cfg(target_os = "macos")]
        crate::macos_desktop::update_position();
    }

    pub fn show_overlay() {
        #[cfg(target_os = "linux")]
        crate::linux_desktop::show_overlay();
        #[cfg(target_os = "macos")]
        crate::macos_desktop::show_overlay();
    }

    pub fn hide_overlay() {
        #[cfg(target_os = "macos")]
        {
            *PREVIEW_TEXT.get_or_init(|| std::sync::Mutex::new(String::new())).lock().unwrap_or_else(|e| e.into_inner()) = String::new();
            crate::macos_desktop::set_state(0);
        }
        #[cfg(target_os = "linux")]
        {
            *PREVIEW_TEXT.get_or_init(|| std::sync::Mutex::new(String::new()))
                .lock().unwrap_or_else(|e| e.into_inner()) = String::new();
            crate::linux_desktop::set_preview_text("");
            crate::linux_desktop::hide_overlay();
        }
    }

    pub fn show_meeting_overlay() {
        #[cfg(target_os = "macos")]
        set_preview_state(PreviewState::Meeting);
        #[cfg(target_os = "linux")]
        {
            set_preview_state(PreviewState::Meeting);
            crate::linux_desktop::set_preview_text("Recording meeting…");
            crate::linux_desktop::show_overlay();
        }
    }

    pub fn hide_meeting_overlay() {
        #[cfg(any(target_os = "linux", target_os = "macos"))]
        hide_overlay();
    }

    pub fn update_tray_tooltip(_text: &str) {}

    pub fn show_preview() { show_overlay(); }

    pub fn hide_preview() { hide_overlay(); }

    pub fn set_preview_text(text: &str) {
        *PREVIEW_TEXT.get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock().unwrap_or_else(|e| e.into_inner()) = text.to_string();
        #[cfg(target_os = "linux")]
        crate::linux_desktop::set_preview_text(text);
        #[cfg(target_os = "macos")]
        crate::macos_desktop::set_preview_text(text);
    }

    pub fn get_preview_text() -> String {
        PREVIEW_TEXT.get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock().unwrap_or_else(|e| e.into_inner()).clone()
    }

    pub fn set_preview_state(state: PreviewState) {
        #[cfg(target_os = "macos")]
        crate::macos_desktop::set_state(state as u8);
        #[cfg(target_os = "linux")]
        crate::linux_desktop::set_preview_state(state as u8);
        match state {
            PreviewState::Hidden => hide_overlay(),
            PreviewState::Listening | PreviewState::Processing | PreviewState::Meeting => show_overlay(),
        }
    }

    pub fn update_preview_position() { update_overlay_position(); }

    pub fn play_chime(_start: bool) {
        #[cfg(target_os = "macos")]
        crate::macos_desktop::chime(_start);
    }

    pub fn ensure_single_instance() {}

    pub fn update_tray_icon_for_theme(_theme: &str) {}

    pub fn set_window_icon_for_theme(_hwnd: *mut std::ffi::c_void, _theme: &str) {}

    pub unsafe fn run_win32_hook_and_tray_thread() {
        loop {
            std::thread::sleep(std::time::Duration::from_secs(3600));
        }
    }
}

#[cfg(not(target_os = "windows"))]
pub use win32_stub::{
    SendHWND, PreviewState, NotificationLevel, calculate_pill_position, update_overlay_position, play_chime,
    show_overlay, hide_overlay, show_preview, hide_preview,
    show_meeting_overlay, hide_meeting_overlay, update_tray_tooltip,
    set_preview_text, get_preview_text, set_preview_state, update_preview_position,
    show_tray_notification, run_win32_hook_and_tray_thread, ensure_single_instance,
    update_tray_icon_for_theme, set_window_icon_for_theme
};
