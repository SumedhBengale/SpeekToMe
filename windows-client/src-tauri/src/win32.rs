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

    use windows_sys::Win32::Foundation::{HWND, LPARAM, LRESULT, RECT, WPARAM};
    use windows_sys::Win32::Graphics::Gdi::{
        BeginPaint, CreateBitmap, CreateCompatibleDC, CreateDIBSection, CreateFontW, DeleteDC,
        DeleteObject, EndPaint, GetDC, ReleaseDC, SelectObject, SetBkMode, SetTextColor,
        BITMAPINFO, BITMAPINFOHEADER, BI_RGB, DIB_RGB_COLORS, HDC, PAINTSTRUCT,
    };
    use windows_sys::Win32::UI::Shell::{
        Shell_NotifyIconW, NIF_ICON, NIF_INFO, NIF_MESSAGE, NIF_TIP, NIIF_ERROR, NIIF_INFO,
        NIIF_WARNING, NIM_ADD, NIM_DELETE, NIM_MODIFY, NOTIFYICONDATAW,
    };
    use windows_sys::Win32::UI::WindowsAndMessaging::{
        BringWindowToTop, CallNextHookEx, CreateIconIndirect, CreateWindowExW, DefWindowProcW,
        DestroyIcon, DispatchMessageW, GetMessageW, GetSystemMetrics, IsWindowVisible, KillTimer,
        PostMessageW, PostQuitMessage, RegisterClassW, SendMessageW, SetTimer, SetWindowPos,
        SetWindowsHookExW, ShowWindow, TranslateMessage, UnhookWindowsHookEx, UpdateLayeredWindow,
        CS_HREDRAW, CS_VREDRAW, HWND_TOPMOST, ICONINFO, KBDLLHOOKSTRUCT, MSG, SWP_NOACTIVATE,
        SWP_NOMOVE, SWP_NOSIZE, SWP_NOZORDER, SWP_SHOWWINDOW, SW_HIDE, SW_SHOWNA, WH_KEYBOARD_LL,
        WINDOWPOS, WM_APP, WM_DESTROY, WM_KEYDOWN, WM_KEYUP, WM_PAINT, WM_SYSKEYDOWN, WM_SYSKEYUP,
        WM_TIMER, WM_WINDOWPOSCHANGING, WNDCLASSW, WS_EX_LAYERED, WS_EX_NOACTIVATE,
        WS_EX_TOOLWINDOW, WS_EX_TOPMOST, WS_EX_TRANSPARENT, WS_POPUP,
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
        fn LoadCursorW(
            hInstance: *mut std::ffi::c_void,
            lpCursorName: *const u16,
        ) -> *mut std::ffi::c_void;
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
    pub static PREVIEW_TEXT: std::sync::OnceLock<std::sync::Mutex<String>> =
        std::sync::OnceLock::new();
    pub static PREVIEW_STATE: std::sync::atomic::AtomicU8 = std::sync::atomic::AtomicU8::new(0);
    pub static TRAY_HWND: std::sync::OnceLock<SendHWND> = std::sync::OnceLock::new();
    static TEXT_GLOW: std::sync::OnceLock<Mutex<crate::native_overlay::TextGlow>> =
        std::sync::OnceLock::new();
    static ANIMATION_START: std::sync::OnceLock<std::time::Instant> = std::sync::OnceLock::new();
    // Set while a finished dictation's preview stays up for its polish.
    static PREVIEW_HOLD: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

    // Posted by show_* from any thread; timers belong to the window's own thread.
    const WM_APP_ANIMATE: u32 = WM_APP + 1;

    fn animation_seconds() -> f64 {
        ANIMATION_START
            .get_or_init(std::time::Instant::now)
            .elapsed()
            .as_secs_f64()
    }

    fn text_glow() -> std::sync::MutexGuard<'static, crate::native_overlay::TextGlow> {
        TEXT_GLOW
            .get_or_init(Default::default)
            .lock()
            .unwrap_or_else(|e| e.into_inner())
    }

    fn start_animation(hwnd: HWND) {
        unsafe {
            PostMessageW(hwnd, WM_APP_ANIMATE, 0, 0);
        }
    }

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
        PREVIEW_TEXT
            .get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }

    pub fn set_preview_text(text: &str) {
        set_preview_text_with_replacements(text, &[], &[]);
    }

    /// `replacements` are byte ranges of words a vocabulary decision put in
    /// `text`, and `deciding` those it is still deciding; both glow instead of
    /// popping into place.
    pub fn set_preview_text_with_replacements(
        text: &str,
        replacements: &[std::ops::Range<usize>],
        deciding: &[std::ops::Range<usize>],
    ) {
        store_preview_text(text);
        let animate = text_glow().update(text, replacements, deciding, animation_seconds());
        refresh_preview(animate);
    }

    /// Shows a dictation's final text; words it changed from the live
    /// preview polish in.
    pub fn set_preview_final_text(text: &str) {
        store_preview_text(text);
        let animate = text_glow().finish(text, animation_seconds());
        refresh_preview(animate);
    }

    fn store_preview_text(text: &str) {
        *PREVIEW_TEXT
            .get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock()
            .unwrap_or_else(|e| e.into_inner()) = text.to_string();
    }

    fn refresh_preview(animate: bool) {
        let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
        if state != PreviewState::Hidden {
            update_preview_position();
            if animate {
                if let Some(send_hwnd) = PREVIEW_HWND.get() {
                    start_animation(send_hwnd.0);
                }
            }
        }
    }

    /// Hides the preview once the final text's polish has played, so the
    /// changed words are seen after the text is inserted.
    pub fn hide_preview_after_polish() {
        let wait = text_glow().remaining(animation_seconds());
        let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
        if wait <= 0.0 || state != PreviewState::Processing {
            hide_preview();
            return;
        }
        // The work is done: stop the processing ring while the words settle.
        PREVIEW_STATE.store(PreviewState::Listening as u8, Ordering::SeqCst);
        PREVIEW_HOLD.store(true, Ordering::SeqCst);
        std::thread::spawn(move || {
            std::thread::sleep(std::time::Duration::from_secs_f64(wait));
            if PREVIEW_HOLD.swap(false, Ordering::SeqCst) {
                hide_preview();
            }
        });
    }

    pub fn set_preview_state(state: PreviewState) {
        // A new dictation replaces a finished one still on screen.
        if PREVIEW_HOLD.swap(false, Ordering::SeqCst) {
            store_preview_text("");
            text_glow().reset();
        }
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

    pub fn calculate_pill_position(
        config: &crate::config::AppConfig,
        width: i32,
        height: i32,
    ) -> (i32, i32) {
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

    // 4px outer margin on each side for the processing glow ring around the
    // preview card and the pill.
    pub const GLOW_MARGIN: i32 = 4;
    pub const PILL_GLOW_MARGIN: i32 = 4;

    pub unsafe fn calculate_preview_bounds(text: &str) -> (i32, i32, i32, i32) {
        let screen_width = GetSystemMetrics(SM_CXSCREEN);
        let screen_height = GetSystemMetrics(SM_CYSCREEN);

        let config = crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        let (pill_x, pill_y) = calculate_pill_position(&config, 62, 24);

        let display_text = if text.trim().is_empty() {
            let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
            if state == PreviewState::Processing {
                "Polishing..."
            } else {
                "Listening..."
            }
        } else {
            text
        };

        let wide_text = encode_wide(display_text);

        let hdc_screen = GetDC(std::ptr::null_mut());
        let font_name = encode_wide("Segoe UI");
        let hfont = CreateFontW(
            -13,
            0,
            0,
            0,
            FW_NORMAL,
            0,
            0,
            0,
            DEFAULT_CHARSET,
            OUT_DEFAULT_PRECIS,
            CLIP_DEFAULT_PRECIS,
            CLEARTYPE_QUALITY,
            DEFAULT_PITCH | FF_DONTCARE,
            font_name.as_ptr(),
        );

        let old_font = SelectObject(hdc_screen, hfont);

        // The shared card layout: 370px of text, 12px/8px padding.
        let max_text_width = 370;
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
        let pad_y = 8;
        let card_w = (measured_w + pad_x * 2).clamp(160, 394);
        let card_h = (measured_h + pad_y * 2).max(28);

        // Include GLOW_MARGIN on all 4 sides for the outer halo
        let total_w = card_w + GLOW_MARGIN * 2;
        let total_h = card_h + GLOW_MARGIN * 2;

        let mut preview_x = pill_x + (62 - total_w) / 2;
        preview_x = preview_x.clamp(4, screen_width - total_w - 4);

        // The card itself sits 8px from the pill; its halo may overlap the pill's.
        let preview_y = if pill_y < screen_height / 2 {
            // Below the pill
            (pill_y + 24 + 8 - GLOW_MARGIN).min(screen_height - total_h - 4)
        } else {
            // Above the pill
            (pill_y - 8 - total_h + GLOW_MARGIN).max(4)
        };

        (preview_x, preview_y, total_w, total_h)
    }

    pub fn show_overlay() {
        if let Some(send_hwnd) = crate::OVERLAY_HWND.get() {
            let hwnd = send_hwnd.0;
            unsafe {
                let ex_style = GetWindowLongW(hwnd, GWL_EXSTYLE);
                if (ex_style & (WS_EX_TRANSPARENT as i32)) == 0 {
                    SetWindowLongW(
                        hwnd,
                        GWL_EXSTYLE,
                        ex_style | (WS_EX_TRANSPARENT as i32) | (WS_EX_TOPMOST as i32),
                    );
                }
                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner());
                let (x, y) = calculate_pill_position(&config, 62, 24);
                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    x - PILL_GLOW_MARGIN,
                    y - PILL_GLOW_MARGIN,
                    62 + PILL_GLOW_MARGIN * 2,
                    24 + PILL_GLOW_MARGIN * 2,
                    SWP_NOACTIVATE | SWP_FRAMECHANGED | SWP_SHOWWINDOW,
                );
                BringWindowToTop(hwnd);
                update_layered_overlay(hwnd);
                ShowWindow(hwnd, SW_SHOWNA);
                start_animation(hwnd);
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
                SetWindowLongW(
                    hwnd,
                    GWL_EXSTYLE,
                    (ex_style & !(WS_EX_TRANSPARENT as i32)) | (WS_EX_TOPMOST as i32),
                );

                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner());
                let (x, y) = calculate_pill_position(&config, 104, 24);

                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    x,
                    y,
                    104,
                    24,
                    SWP_NOACTIVATE | SWP_FRAMECHANGED | SWP_SHOWWINDOW,
                );
                BringWindowToTop(hwnd);
                update_layered_overlay(hwnd);
                ShowWindow(hwnd, SW_SHOWNA);
                start_animation(hwnd);
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
                SetWindowLongW(
                    hwnd,
                    GWL_EXSTYLE,
                    ex_style | (WS_EX_TRANSPARENT as i32) | (WS_EX_TOPMOST as i32),
                );

                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner());
                let (def_x, def_y) = calculate_pill_position(&config, 62, 24);

                // Reposition back to default position and 62x24 size, then hide
                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    def_x - PILL_GLOW_MARGIN,
                    def_y - PILL_GLOW_MARGIN,
                    62 + PILL_GLOW_MARGIN * 2,
                    24 + PILL_GLOW_MARGIN * 2,
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
            start_animation(send_hwnd.0);
        }
    }

    pub fn hide_preview() {
        PREVIEW_HOLD.store(false, Ordering::SeqCst);
        let current_state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
        if current_state != PreviewState::Meeting {
            PREVIEW_STATE.store(PreviewState::Hidden as u8, Ordering::SeqCst);
        }
        if let Some(lock) = PREVIEW_TEXT.get() {
            if let Ok(mut t) = lock.lock() {
                t.clear();
            }
        }
        text_glow().reset();
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
            let pill_width = if state == PreviewState::Meeting {
                104
            } else {
                62
            };
            let (x, y) = if state == PreviewState::Meeting
                && MEETING_DRAGGED_X.load(Ordering::Relaxed) >= 0
            {
                (
                    MEETING_DRAGGED_X.load(Ordering::Relaxed),
                    MEETING_DRAGGED_Y.load(Ordering::Relaxed),
                )
            } else {
                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner());
                calculate_pill_position(&config, pill_width, 24)
            };
            let margin = if state == PreviewState::Meeting {
                0
            } else {
                PILL_GLOW_MARGIN
            };
            unsafe {
                SetWindowPos(
                    hwnd,
                    HWND_TOPMOST,
                    x - margin,
                    y - margin,
                    pill_width + margin * 2,
                    24 + margin * 2,
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
            PlaySoundW(
                bytes.as_ptr(),
                std::ptr::null_mut(),
                SND_ASYNC | SND_MEMORY | SND_NODEFAULT,
            );
        }
    }

    pub unsafe extern "system" fn wnd_proc_overlay(
        hwnd: HWND,
        msg: u32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        match msg {
            WM_APP_ANIMATE => {
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
                if IsWindowVisible(hwnd) == 0 {
                    KillTimer(hwnd, 1);
                    return 0;
                }
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state != PreviewState::Hidden {
                    static TOPMOST_COUNTER: std::sync::atomic::AtomicU32 =
                        std::sync::atomic::AtomicU32::new(0);
                    let count = TOPMOST_COUNTER.fetch_add(1, Ordering::Relaxed);
                    if count % 10 == 0 {
                        SetWindowPos(
                            hwnd,
                            HWND_TOPMOST,
                            0,
                            0,
                            0,
                            0,
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
                    println!(
                        "[OVERLAY] Pill placed at ({}, {}) for this meeting session.",
                        new_x, new_y
                    );
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

    pub unsafe extern "system" fn wnd_proc_preview(
        hwnd: HWND,
        msg: u32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        match msg {
            WM_APP_ANIMATE => {
                SetTimer(hwnd, 1, 30, None);
                0
            }
            WM_TIMER => {
                let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
                if state == PreviewState::Hidden || IsWindowVisible(hwnd) == 0 {
                    KillTimer(hwnd, 1);
                    return 0;
                }
                update_layered_preview(hwnd);
                // A listening preview only changes when new text arrives, so
                // stop once no word is polishing; a new replacement restarts it.
                if state == PreviewState::Listening
                    && text_glow().sample(animation_seconds()).is_empty()
                {
                    KillTimer(hwnd, 1);
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
        bmi.bmiHeader.biSize =
            mem::size_of::<windows_sys::Win32::Graphics::Gdi::BITMAPINFOHEADER>() as u32;
        bmi.bmiHeader.biWidth = width;
        bmi.bmiHeader.biHeight = -height;
        bmi.bmiHeader.biPlanes = 1;
        bmi.bmiHeader.biBitCount = 32;
        bmi.bmiHeader.biCompression = 0;

        let mut bits: *mut std::ffi::c_void = ptr::null_mut();
        let dib = CreateDIBSection(hdc_mem, &bmi as *const _, 0, &mut bits, ptr::null_mut(), 0);

        if !dib.is_null() && !bits.is_null() {
            let old_bitmap = SelectObject(hdc_mem, dib);
            let pixel_count = (width * height) as usize;
            let pixel_slice = std::slice::from_raw_parts_mut(bits as *mut u32, pixel_count);
            for pixel in pixel_slice.iter_mut() {
                *pixel = 0;
            }

            let bg_color = crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed);
            let bg_r = (bg_color & 0xFF) as f32;
            let bg_g = ((bg_color >> 8) & 0xFF) as f32;
            let bg_b = ((bg_color >> 16) & 0xFF) as f32;

            let state: PreviewState = PREVIEW_STATE.load(Ordering::Relaxed).into();
            let margin = if state == PreviewState::Meeting {
                0.0
            } else {
                PILL_GLOW_MARGIN as f64
            };

            static SMOOTHED_VOLUME: std::sync::atomic::AtomicU64 =
                std::sync::atomic::AtomicU64::new(0);
            let raw_vol = if state == PreviewState::Meeting {
                let mic = crate::audio::MIC_VOLUME.load(Ordering::Relaxed);
                let lb = crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed);
                mic.max(lb)
            } else {
                crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed)
            };
            let smoothed = crate::native_overlay::smooth_volume(
                f64::from_bits(SMOOTHED_VOLUME.load(Ordering::Relaxed)),
                raw_vol as f64,
            );
            SMOOTHED_VOLUME.store(smoothed.to_bits(), Ordering::Relaxed);

            // Keep the meeting indicator beside its timer; center normal dictation.
            let logo_x = if state == PreviewState::Meeting {
                -7.0
            } else {
                (width as f64 - crate::native_overlay::PILL_WIDTH) / 2.0
            };
            let logo_y = (height as f64 - crate::native_overlay::PILL_HEIGHT) / 2.0;
            crate::overlay_paint::paint(
                &crate::overlay_paint::Scene {
                    scale: 1.0,
                    seconds: animation_seconds(),
                    background: crate::native_overlay::color(bg_color),
                    wave: crate::native_overlay::color(
                        crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed),
                    ),
                    processing: state == PreviewState::Processing,
                    pill: Some(crate::native_overlay::Rect {
                        x: margin,
                        y: margin,
                        width: width as f64 - margin * 2.0,
                        height: height as f64 - margin * 2.0,
                    }),
                    logo: Some((logo_x, logo_y)),
                    volume: smoothed,
                    card: None,
                    text: None,
                },
                pixel_slice,
                width as usize,
                height as usize,
            );

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
                    let mask_slice =
                        std::slice::from_raw_parts_mut(mask_bits as *mut u32, pixel_count);
                    for p in mask_slice.iter_mut() {
                        *p = 0;
                    }

                    let font_name = encode_wide("Segoe UI");
                    let hfont = CreateFontW(
                        -11,
                        0,
                        0,
                        0,
                        FW_SEMIBOLD,
                        0,
                        0,
                        0,
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
                                    let out_r = ((text_r * src_a)
                                        + (dst_r * dst_a * (255 - src_a) / 255))
                                        / out_a;
                                    let out_g = ((text_g * src_a)
                                        + (dst_g * dst_a * (255 - src_a) / 255))
                                        / out_a;
                                    let out_b = ((text_b * src_a)
                                        + (dst_b * dst_a * (255 - src_a) / 255))
                                        / out_a;

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

            let psize = windows_sys::Win32::Foundation::SIZE {
                cx: width,
                cy: height,
            };
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
        bmi.bmiHeader.biSize =
            mem::size_of::<windows_sys::Win32::Graphics::Gdi::BITMAPINFOHEADER>() as u32;
        bmi.bmiHeader.biWidth = width;
        bmi.bmiHeader.biHeight = -height;
        bmi.bmiHeader.biPlanes = 1;
        bmi.bmiHeader.biBitCount = 32;
        bmi.bmiHeader.biCompression = 0;

        let mut bits: *mut std::ffi::c_void = ptr::null_mut();
        let dib = CreateDIBSection(hdc_mem, &bmi as *const _, 0, &mut bits, ptr::null_mut(), 0);

        if !dib.is_null() && !bits.is_null() {
            let old_bitmap = SelectObject(hdc_mem, dib);
            let pixel_count = (width * height) as usize;
            let pixel_slice = std::slice::from_raw_parts_mut(bits as *mut u32, pixel_count);
            for pixel in pixel_slice.iter_mut() {
                *pixel = 0;
            }

            let bg_color = crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed);
            let background = crate::native_overlay::color(bg_color);
            let text_color = crate::native_overlay::text_color(background);
            let seconds = animation_seconds();
            let is_processing = state == PreviewState::Processing;

            let display_text = if raw_text.trim().is_empty() {
                if is_processing {
                    "Polishing..."
                } else {
                    "Listening..."
                }
            } else {
                raw_text.as_str()
            };
            let glows = if !raw_text.trim().is_empty() {
                text_glow().sample(seconds)
            } else {
                Vec::new()
            };

            let font_name = encode_wide("Segoe UI");
            let hfont = CreateFontW(
                -13,
                0,
                0,
                0,
                FW_NORMAL,
                0,
                0,
                0,
                DEFAULT_CHARSET,
                OUT_DEFAULT_PRECIS,
                CLIP_DEFAULT_PRECIS,
                CLEARTYPE_QUALITY,
                DEFAULT_PITCH | FF_DONTCARE,
                font_name.as_ptr(),
            );
            let pad_x = 12;
            let pad_y = 8;
            let text_left = GLOW_MARGIN + pad_x;
            let text_right = width - (GLOW_MARGIN + pad_x);
            let render_mask = |text: &str| -> Vec<f32> {
                let mut coverage = vec![0f32; pixel_count];
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
                    let mask_slice =
                        std::slice::from_raw_parts_mut(mask_bits as *mut u32, pixel_count);
                    mask_slice.fill(0);
                    let old_font = SelectObject(hdc_mask, hfont);
                    SetBkMode(hdc_mask, TRANSPARENT as i32);
                    SetTextColor(hdc_mask, 0x00FFFFFF);
                    let wide = encode_wide(text);
                    let mut text_rect = RECT {
                        left: text_left,
                        top: GLOW_MARGIN + pad_y,
                        right: text_right,
                        bottom: height - (GLOW_MARGIN + pad_y),
                    };
                    if wide.len() > 1 {
                        DrawTextW(
                            hdc_mask,
                            wide.as_ptr(),
                            (wide.len() - 1) as i32,
                            &mut text_rect,
                            DT_WORDBREAK | DT_LEFT | DT_NOPREFIX,
                        );
                    }
                    for (value, mask) in coverage.iter_mut().zip(mask_slice.iter()) {
                        *value = (mask & 0xFF) as f32 / 255.0;
                    }
                    SelectObject(hdc_mask, old_font);
                    SelectObject(hdc_mask, old_mask_bitmap);
                    DeleteObject(mask_dib);
                }
                DeleteDC(hdc_mask);
                coverage
            };

            let glyphs = render_mask(display_text);
            // A changing word's pixels are what text[..end] adds over
            // text[..start]; greedy word wrap lays both prefixes out exactly as
            // the full text.
            let words: Vec<(Vec<f32>, crate::native_overlay::Polish)> = glows
                .iter()
                .filter(|(range, _)| {
                    range.end <= display_text.len()
                        && display_text.is_char_boundary(range.start)
                        && display_text.is_char_boundary(range.end)
                })
                .map(|(range, look)| {
                    let before = render_mask(&display_text[..range.start]);
                    let word = render_mask(&display_text[..range.end])
                        .iter()
                        .zip(before)
                        .map(|(after, before)| (after - before).max(0.0))
                        .collect();
                    (word, *look)
                })
                .collect();
            DeleteObject(hfont);

            let gm = GLOW_MARGIN as f64;
            crate::overlay_paint::paint(
                &crate::overlay_paint::Scene {
                    scale: 1.0,
                    seconds,
                    background,
                    wave: crate::native_overlay::color(
                        crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed),
                    ),
                    processing: is_processing,
                    pill: None,
                    logo: None,
                    volume: 0.0,
                    card: Some(crate::native_overlay::Rect {
                        x: gm,
                        y: gm,
                        width: width as f64 - gm * 2.0,
                        height: height as f64 - gm * 2.0,
                    }),
                    text: Some(crate::overlay_paint::Text {
                        color: text_color,
                        glyphs: &glyphs,
                        words: &words,
                    }),
                },
                pixel_slice,
                width as usize,
                height as usize,
            );

            let psize = windows_sys::Win32::Foundation::SIZE {
                cx: width,
                cy: height,
            };
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

    pub unsafe extern "system" fn wnd_proc_tray(
        hwnd: HWND,
        msg: u32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        match msg {
            WM_TRAYICON => {
                let event = lparam as u32;
                if event == windows_sys::Win32::UI::WindowsAndMessaging::WM_LBUTTONDBLCLK {
                    if let Some(app) = crate::TAURI_APP.get() {
                        crate::show_or_create_settings_window(app);
                    }
                } else if event == windows_sys::Win32::UI::WindowsAndMessaging::WM_RBUTTONUP {
                    let is_meeting = crate::ACTIVE_MEETING
                        .lock()
                        .unwrap_or_else(|e| e.into_inner())
                        .is_some();
                    let menu = windows_sys::Win32::UI::WindowsAndMessaging::CreatePopupMenu();

                    if is_meeting {
                        let meeting_str = encode_wide("🔴 Open Active Meeting");
                        windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(
                            menu,
                            windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING,
                            3,
                            meeting_str.as_ptr(),
                        );
                    }
                    let language_options = crate::speaking_language_options();
                    let active_language = crate::speaking_language();
                    let language_menu =
                        windows_sys::Win32::UI::WindowsAndMessaging::CreatePopupMenu();
                    for (index, (code, name)) in language_options.iter().enumerate() {
                        let label = encode_wide(name);
                        let flags = windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING
                            | if code == &active_language {
                                windows_sys::Win32::UI::WindowsAndMessaging::MF_CHECKED
                            } else {
                                0
                            };
                        windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(
                            language_menu,
                            flags,
                            1000 + index,
                            label.as_ptr(),
                        );
                    }
                    let language_label = encode_wide("Speaking language");
                    windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(
                        menu,
                        windows_sys::Win32::UI::WindowsAndMessaging::MF_POPUP,
                        language_menu as usize,
                        language_label.as_ptr(),
                    );
                    let settings_str = encode_wide("Settings");
                    let exit_str = encode_wide("Exit");
                    windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(
                        menu,
                        windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING,
                        1,
                        settings_str.as_ptr(),
                    );
                    windows_sys::Win32::UI::WindowsAndMessaging::AppendMenuW(
                        menu,
                        windows_sys::Win32::UI::WindowsAndMessaging::MF_STRING,
                        2,
                        exit_str.as_ptr(),
                    );

                    let mut pt = mem::zeroed();
                    windows_sys::Win32::UI::WindowsAndMessaging::GetCursorPos(&mut pt);
                    windows_sys::Win32::UI::WindowsAndMessaging::SetForegroundWindow(hwnd);

                    let cmd = windows_sys::Win32::UI::WindowsAndMessaging::TrackPopupMenu(
                        menu,
                        windows_sys::Win32::UI::WindowsAndMessaging::TPM_RETURNCMD
                            | windows_sys::Win32::UI::WindowsAndMessaging::TPM_RIGHTBUTTON,
                        pt.x,
                        pt.y,
                        0,
                        hwnd,
                        ptr::null(),
                    );
                    windows_sys::Win32::UI::WindowsAndMessaging::DestroyMenu(menu);

                    if cmd >= 1000 && (cmd as usize - 1000) < language_options.len() {
                        let _ = crate::set_active_language(
                            language_options[cmd as usize - 1000].0.clone(),
                        );
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
        if (req_modifiers & crate::config::HOTKEY_CTRL != 0)
            && (vk_code == 0x11 || vk_code == 0xA2 || vk_code == 0xA3)
        {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_SHIFT != 0)
            && (vk_code == 0x10 || vk_code == 0xA0 || vk_code == 0xA1)
        {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_ALT != 0)
            && (vk_code == 0x12 || vk_code == 0xA4 || vk_code == 0xA5)
        {
            return true;
        }
        if (req_modifiers & crate::config::HOTKEY_WIN != 0) && (vk_code == 0x5B || vk_code == 0x5C)
        {
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
            0x5B | 0x5C // Win
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
    static PHYSICAL_CHORD: std::sync::OnceLock<Mutex<crate::gestures::ChordLatch>> =
        std::sync::OnceLock::new();
    static LAST_WIN_HOTKEY_ACTIVE: Mutex<Option<std::time::Instant>> = Mutex::new(None);

    pub unsafe extern "system" fn keyboard_hook_proc(
        code: i32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        if code < 0 {
            return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam);
        }
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
        let mut chord = PHYSICAL_CHORD
            .get_or_init(|| Mutex::new(crate::gestures::ChordLatch::default()))
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        if crate::shortcuts::selecting() || !crate::HOTKEY_ALLOWED.load(Ordering::SeqCst) {
            if down || up {
                let _ = chord.edge(vk, down, combo);
            }
            return CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam);
        }
        let was_active = chord.active();
        let edge = if down || up {
            chord.edge(vk, down, combo)
        } else {
            None
        };
        let mut suppress = part && (was_active || chord.active()) && should_suppress_key(vk);
        if down
            && was_active
            && !part
            && crate::ACTIVE_HOTKEY_MODE.load(Ordering::Relaxed) == 0
            && crate::shortcuts::is_capturing()
        {
            crate::shortcuts::cancel();
        }
        if let Some(pressed) = edge {
            if pressed && !crate::ACTIVE_HAS_SESSION_TOKEN.load(Ordering::SeqCst) {
                show_tray_notification(
                    "SpeekTo.Me — Sign In Required",
                    "Please sign in in Settings to start voice dictation.",
                    NotificationLevel::Warning,
                );
                if let Some(app) = crate::TAURI_APP.get() {
                    crate::show_or_create_settings_window(app);
                }
            } else {
                crate::shortcuts::on_edge(pressed);
            }
        }
        if (modifiers & crate::config::HOTKEY_WIN) != 0 {
            if chord.active() || was_active {
                *LAST_WIN_HOTKEY_ACTIVE
                    .lock()
                    .unwrap_or_else(|e| e.into_inner()) = Some(std::time::Instant::now());
                crate::keyboard::suppress_win_key_start_menu();
            }
            if up && matches!(vk, 0x5B | 0x5C) {
                let recent = LAST_WIN_HOTKEY_ACTIVE
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .is_some_and(|at| at.elapsed() < Duration::from_millis(600));
                if recent {
                    crate::keyboard::suppress_win_key_start_menu();
                    suppress = true;
                }
            }
        }
        if suppress {
            1
        } else {
            CallNextHookEx(std::ptr::null_mut(), code, wparam, lparam)
        }
    }

    static CURRENT_TRAY_HICON: std::sync::atomic::AtomicIsize =
        std::sync::atomic::AtomicIsize::new(0);

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
            Bar {
                cx: 4.5,
                half_h: 5.0,
                radius: 1.8,
            },
            Bar {
                cx: 10.25,
                half_h: 10.0,
                radius: 1.8,
            },
            Bar {
                cx: 16.0,
                half_h: 14.2,
                radius: 1.8,
            },
            Bar {
                cx: 21.75,
                half_h: 10.0,
                radius: 1.8,
            },
            Bar {
                cx: 27.5,
                half_h: 5.0,
                radius: 1.8,
            },
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
                    bgra[idx + 3] = a; // A
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
                    println!(
                        "[WIN32] Updated tray icon for theme: {} (success: {})",
                        theme,
                        res != 0
                    );

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
            0,
            0,
            62 + PILL_GLOW_MARGIN * 2,
            24 + PILL_GLOW_MARGIN * 2,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            hinstance,
            ptr::null_mut(),
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
            0,
            0,
            160,
            36,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            hinstance,
            ptr::null_mut(),
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
            0,
            0,
            0,
            0,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            hinstance,
            ptr::null_mut(),
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
        println!(
            "[WIN32] Themed system tray icon registered (success: {}).",
            tray_ok != 0
        );

        let hook_id = SetWindowsHookExW(WH_KEYBOARD_LL, Some(keyboard_hook_proc), hinstance, 0);

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

        static SINGLE_INSTANCE_MUTEX: std::sync::atomic::AtomicIsize =
            std::sync::atomic::AtomicIsize::new(0);
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
    calculate_pill_position, ensure_single_instance, get_preview_text, hide_meeting_overlay,
    hide_overlay, hide_preview, hide_preview_after_polish, play_chime,
    run_win32_hook_and_tray_thread, set_preview_final_text, set_preview_state, set_preview_text,
    set_preview_text_with_replacements, set_window_icon_for_theme, show_meeting_overlay,
    show_overlay, show_preview, show_tray_notification, update_overlay_position,
    update_preview_position, update_tray_icon_for_theme, update_tray_tooltip, NotificationLevel,
    PreviewState, SendHWND, PREVIEW_HWND, PREVIEW_STATE, PREVIEW_TEXT, TRAY_HWND,
};

// ==========================================
// Non-Windows Implementation Stubs
// ==========================================
#[cfg(not(target_os = "windows"))]
mod win32_stub {
    use std::sync::atomic::{AtomicBool, Ordering};

    static PREVIEW_TEXT: std::sync::OnceLock<std::sync::Mutex<String>> = std::sync::OnceLock::new();
    static TEXT_GLOW: std::sync::OnceLock<std::sync::Mutex<crate::native_overlay::TextGlow>> =
        std::sync::OnceLock::new();
    static ANIMATION_START: std::sync::OnceLock<std::time::Instant> = std::sync::OnceLock::new();
    // Set while a finished dictation's preview stays up for its polish.
    static PREVIEW_HOLD: AtomicBool = AtomicBool::new(false);

    fn text_glow() -> std::sync::MutexGuard<'static, crate::native_overlay::TextGlow> {
        TEXT_GLOW
            .get_or_init(Default::default)
            .lock()
            .unwrap_or_else(|e| e.into_inner())
    }

    /// The overlay animation clock; every effect is timed by it.
    pub fn animation_seconds() -> f64 {
        ANIMATION_START
            .get_or_init(std::time::Instant::now)
            .elapsed()
            .as_secs_f64()
    }

    /// The preview's changing words and how each looks right now.
    pub fn preview_glow() -> Vec<(std::ops::Range<usize>, crate::native_overlay::Polish)> {
        text_glow().sample(animation_seconds())
    }
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

    pub fn calculate_pill_position(
        _config: &crate::config::AppConfig,
        _width: i32,
        _height: i32,
    ) -> (i32, i32) {
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
        // The hold hides the whole surface once the polish has played.
        if PREVIEW_HOLD.load(Ordering::SeqCst) {
            return;
        }
        text_glow().reset();
        #[cfg(target_os = "macos")]
        {
            *PREVIEW_TEXT
                .get_or_init(|| std::sync::Mutex::new(String::new()))
                .lock()
                .unwrap_or_else(|e| e.into_inner()) = String::new();
            crate::macos_desktop::set_state(0);
        }
        #[cfg(target_os = "linux")]
        {
            *PREVIEW_TEXT
                .get_or_init(|| std::sync::Mutex::new(String::new()))
                .lock()
                .unwrap_or_else(|e| e.into_inner()) = String::new();
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

    pub fn show_preview() {
        show_overlay();
    }

    pub fn hide_preview() {
        hide_overlay();
    }

    /// `replacements` are byte ranges of words a vocabulary decision put in
    /// `text`, and `deciding` those it is still deciding; both glow instead of
    /// popping into place.
    pub fn set_preview_text_with_replacements(
        text: &str,
        replacements: &[std::ops::Range<usize>],
        deciding: &[std::ops::Range<usize>],
    ) {
        text_glow().update(text, replacements, deciding, animation_seconds());
        show_text(text);
    }

    /// Shows a dictation's final text; words it changed from the live
    /// preview polish in.
    pub fn set_preview_final_text(text: &str) {
        text_glow().finish(text, animation_seconds());
        show_text(text);
    }

    /// Hides the overlay once the final text's polish has played, so the
    /// changed words are seen after the text is inserted.
    pub fn hide_preview_after_polish() {
        let wait = text_glow().remaining(animation_seconds());
        if wait <= 0.0 {
            hide_preview();
            return;
        }
        PREVIEW_HOLD.store(true, Ordering::SeqCst);
        // The work is done: stop the processing ring while the words settle.
        #[cfg(target_os = "linux")]
        crate::linux_desktop::set_preview_state(PreviewState::Listening as u8);
        #[cfg(target_os = "macos")]
        crate::macos_desktop::set_state(PreviewState::Listening as u8);
        std::thread::spawn(move || {
            std::thread::sleep(std::time::Duration::from_secs_f64(wait));
            if PREVIEW_HOLD.swap(false, Ordering::SeqCst) {
                hide_preview();
            }
        });
    }

    pub fn set_preview_text(text: &str) {
        set_preview_text_with_replacements(text, &[], &[]);
    }

    fn show_text(text: &str) {
        *PREVIEW_TEXT
            .get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock()
            .unwrap_or_else(|e| e.into_inner()) = text.to_string();
        #[cfg(target_os = "linux")]
        crate::linux_desktop::set_preview_text(text);
        #[cfg(target_os = "macos")]
        crate::macos_desktop::set_preview_text(text);
    }

    pub fn get_preview_text() -> String {
        PREVIEW_TEXT
            .get_or_init(|| std::sync::Mutex::new(String::new()))
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }

    pub fn set_preview_state(state: PreviewState) {
        // A new dictation replaces a finished one still on screen.
        if PREVIEW_HOLD.swap(false, Ordering::SeqCst) {
            set_preview_text("");
        }
        #[cfg(target_os = "macos")]
        crate::macos_desktop::set_state(state as u8);
        #[cfg(target_os = "linux")]
        crate::linux_desktop::set_preview_state(state as u8);
        match state {
            PreviewState::Hidden => hide_overlay(),
            PreviewState::Listening | PreviewState::Processing | PreviewState::Meeting => {
                show_overlay()
            }
        }
    }

    pub fn update_preview_position() {
        update_overlay_position();
    }

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
    animation_seconds, calculate_pill_position, ensure_single_instance, get_preview_text,
    hide_meeting_overlay, hide_overlay, hide_preview, hide_preview_after_polish, play_chime,
    preview_glow, run_win32_hook_and_tray_thread, set_preview_final_text, set_preview_state,
    set_preview_text, set_preview_text_with_replacements, set_window_icon_for_theme,
    show_meeting_overlay, show_overlay, show_preview, show_tray_notification,
    update_overlay_position, update_preview_position, update_tray_icon_for_theme,
    update_tray_tooltip, NotificationLevel, PreviewState, SendHWND,
};
