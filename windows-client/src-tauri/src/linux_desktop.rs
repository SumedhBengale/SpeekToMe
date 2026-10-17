use tauri::{menu::{MenuBuilder, SubmenuBuilder, CheckMenuItem}, tray::TrayIconBuilder, Manager};
use gtk::prelude::*;
use libloading::Library;
use std::ffi::{c_char, CString};
use std::sync::atomic::{AtomicBool, AtomicU8, Ordering};
use std::sync::OnceLock;

static OVERLAY_STATE: AtomicU8 = AtomicU8::new(0);
static LAYER_SHELL_ACTIVE: AtomicBool = AtomicBool::new(false);

type LayerSupported = unsafe extern "C" fn() -> i32;
type LayerInit = unsafe extern "C" fn(*mut gtk::ffi::GtkWindow);
type LayerSetNamespace = unsafe extern "C" fn(*mut gtk::ffi::GtkWindow, *const c_char);
type LayerSetInt = unsafe extern "C" fn(*mut gtk::ffi::GtkWindow, i32);
type LayerSetAnchor = unsafe extern "C" fn(*mut gtk::ffi::GtkWindow, i32, i32);
type LayerSetMargin = unsafe extern "C" fn(*mut gtk::ffi::GtkWindow, i32, i32);

struct LayerShellApi {
    _library: Library,
    is_supported: LayerSupported,
    init_for_window: LayerInit,
    set_namespace: LayerSetNamespace,
    set_layer: LayerSetInt,
    set_keyboard_mode: LayerSetInt,
    set_exclusive_zone: LayerSetInt,
    set_anchor: LayerSetAnchor,
    set_margin: LayerSetMargin,
}

fn layer_shell_api() -> Option<&'static LayerShellApi> {
    static API: OnceLock<Option<LayerShellApi>> = OnceLock::new();
    API.get_or_init(|| unsafe {
        let library = Library::new("libgtk-layer-shell.so.0").ok()?;
        Some(LayerShellApi {
            is_supported: *library.get(b"gtk_layer_is_supported\0").ok()?,
            init_for_window: *library.get(b"gtk_layer_init_for_window\0").ok()?,
            set_namespace: *library.get(b"gtk_layer_set_namespace\0").ok()?,
            set_layer: *library.get(b"gtk_layer_set_layer\0").ok()?,
            set_keyboard_mode: *library.get(b"gtk_layer_set_keyboard_mode\0").ok()?,
            set_exclusive_zone: *library.get(b"gtk_layer_set_exclusive_zone\0").ok()?,
            set_anchor: *library.get(b"gtk_layer_set_anchor\0").ok()?,
            set_margin: *library.get(b"gtk_layer_set_margin\0").ok()?,
            _library: library,
        })
    })
    .as_ref()
}

fn speaking_menu(app: &tauri::AppHandle) -> tauri::Result<tauri::menu::Menu<tauri::Wry>> {
    let active = crate::speaking_language();
    let mut languages = SubmenuBuilder::new(app, "Speaking language");
    for (code, name) in crate::speaking_language_options() {
        let item = CheckMenuItem::with_id(app, format!("language:{}", code), name, true, code == active, None::<&str>)?;
        languages = languages.item(&item);
    }
    let languages = languages.build()?;
    MenuBuilder::new(app).item(&languages).separator()
        .text("settings", "Open Settings").text("quit", "Quit SpeekTo.Me").build()
}

pub fn refresh_language_menu(app: &tauri::AppHandle) {
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        if let Some(tray) = handle.tray_by_id("main-tray") {
            if let Ok(menu) = speaking_menu(&handle) { let _ = tray.set_menu(Some(menu)); }
        }
    });
}

pub fn setup(app: &tauri::AppHandle) -> tauri::Result<()> {
    let menu = speaking_menu(app)?;
    let mut tray = TrayIconBuilder::with_id("main-tray")
        .menu(&menu)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "settings" => crate::show_or_create_settings_window(app),
            "quit" => app.exit(0),
            id => { if let Some(code) = id.strip_prefix("language:") {
                let _ = crate::set_active_language(code.to_string());
            } }
        });
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;
    let overlay = tauri::WebviewWindowBuilder::new(
        app,
        "overlay",
        tauri::WebviewUrl::App("overlay.html".into()),
    )
    .title("SpeekTo.Me status")
    // The transparent canvas has room for both the pill and its live transcript.
    .inner_size(420.0, 140.0)
    .decorations(false)
    .transparent(true)
    .skip_taskbar(true)
    .always_on_top(true)
    .focusable(false)
    .visible(false)
    .build()?;
    if let Ok(native) = overlay.gtk_window() {
        native.set_accept_focus(false);
        native.set_focus_on_map(false);
        if let Some(api) = layer_shell_api() {
            let ptr = native.as_ptr() as *mut gtk::ffi::GtkWindow;
            unsafe {
                if (api.is_supported)() != 0 {
                    (api.init_for_window)(ptr);
                    let namespace = CString::new("speektome-overlay").unwrap();
                    (api.set_namespace)(ptr, namespace.as_ptr());
                    (api.set_layer)(ptr, 3); // Overlay layer
                    (api.set_keyboard_mode)(ptr, 0); // No keyboard focus
                    (api.set_exclusive_zone)(ptr, 0);
                    LAYER_SHELL_ACTIVE.store(true, Ordering::SeqCst);
                    eprintln!("[LINUX OVERLAY] Wayland layer-shell positioning enabled");
                }
            }
        }
    }
    let config = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .clone();
    configure_overlay_position(&overlay, &config);
    Ok(())
}

pub fn show_overlay() {
    if let Some(app) = crate::TAURI_APP.get() {
        if let Some(window) = app.get_webview_window("overlay") {
            let app = app.clone();
            let state = match OVERLAY_STATE.load(Ordering::SeqCst) {
                2 => "processing",
                3 => "meeting",
                _ => "listening",
            };
            let config = crate::get_active_config()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .clone();
            let preview = crate::win32::get_preview_text();
            let preview_js = serde_json::to_string(&preview).unwrap_or_else(|_| "\"\"".into());
            let _ = app.run_on_main_thread(move || {
                let _ = window.eval(&format!("window.setPreviewState?.({state:?})"));
                let _ = window.eval(&format!("window.setStatus?.({preview_js})"));
                if let Ok(native) = window.gtk_window() {
                    native.set_accept_focus(false);
                    native.set_focus_on_map(false);
                }
                configure_overlay_position(&window, &config);
                let _ = window.show();
                let _ = window.set_ignore_cursor_events(true);
            });
        }
    }
}

pub fn hide_overlay() {
    if let Some(app) = crate::TAURI_APP.get() {
        if let Some(window) = app.get_webview_window("overlay") {
            let _ = window.hide();
        }
    }
}

pub fn set_preview_text(text: &str) {
    if let Some(app) = crate::TAURI_APP.get() {
        if let Some(window) = app.get_webview_window("overlay") {
            let encoded = serde_json::to_string(text).unwrap_or_default();
            let _ = window.eval(&format!("window.setStatus({encoded})"));
        }
    }
}

pub fn set_preview_state(state: u8) {
    OVERLAY_STATE.store(state, Ordering::SeqCst);
    if let Some(app) = crate::TAURI_APP.get() {
        if let Some(window) = app.get_webview_window("overlay") {
            let name = match state {
                0 => "hidden",
                2 => "processing",
                3 => "meeting",
                _ => "listening",
            };
            let _ = window.eval(&format!("window.setPreviewState?.({name:?})"));
        }
    }
}

pub fn sync_overlay_theme() {
    let background = crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed) & 0x00ff_ffff;
    let wave = crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed) & 0x00ff_ffff;
    if let Some(app) = crate::TAURI_APP.get() {
        if let Some(window) = app.get_webview_window("overlay") {
            let _ = window.eval(&format!(
                "window.setTheme?.('#{background:06X}', '#{wave:06X}')"
            ));
        }
    }
}

pub fn update_overlay_position() {
    if let Some(app) = crate::TAURI_APP.get() {
        if let Some(window) = app.get_webview_window("overlay") {
            let config = crate::get_active_config()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .clone();
            let app = app.clone();
            let _ = app.run_on_main_thread(move || configure_overlay_position(&window, &config));
        }
    }
}

fn configure_overlay_position(window: &tauri::WebviewWindow, config: &crate::config::AppConfig) {
    let monitor = window
        .current_monitor()
        .ok()
        .flatten()
        .or_else(|| window.primary_monitor().ok().flatten());
    let scale = window.scale_factor().unwrap_or(1.0).max(1.0);
    let (monitor_width, monitor_height, origin_x, origin_y) = monitor
        .as_ref()
        .map(|monitor| {
            let size = monitor.size();
            let origin = monitor.position();
            (
                size.width as f64 / scale,
                size.height as f64 / scale,
                origin.x,
                origin.y,
            )
        })
        .unwrap_or((1920.0, 1080.0, 0, 0));

    let custom = config.pill_position_mode == "custom";
    let position = config.pill_predefined_position.as_str();
    let pill_x = if custom {
        0.0
    } else if position.ends_with("Right") {
        358.0
    } else if position.ends_with("Left") {
        0.0
    } else {
        179.0
    };
    let pill_y = if custom {
        0.0
    } else if position.starts_with("Bottom") {
        116.0
    } else {
        0.0
    };
    let (x, y) = if custom {
        (config.pill_custom_x as f64, config.pill_custom_y as f64)
    } else {
        let x = if position.ends_with("Left") {
            50.0
        } else if position.ends_with("Right") {
            monitor_width - 420.0 - 50.0
        } else {
            (monitor_width - 420.0) / 2.0
        };
        let y = if position.starts_with("Bottom") {
            monitor_height - 140.0 - 80.0
        } else {
            50.0
        };
        (x, y)
    };

    let _ = window.eval(&format!("window.setOverlayLayout?.({pill_x}, {pill_y})"));
    if LAYER_SHELL_ACTIVE.load(Ordering::SeqCst) {
        if let (Some(api), Ok(native)) = (layer_shell_api(), window.gtk_window()) {
            let ptr = native.as_ptr() as *mut gtk::ffi::GtkWindow;
            unsafe {
                for edge in 0..4 {
                    (api.set_anchor)(ptr, edge, 0);
                    (api.set_margin)(ptr, edge, 0);
                }
                (api.set_anchor)(ptr, 0, 1); // left
                (api.set_margin)(ptr, 0, x.round() as i32);
                if position.starts_with("Bottom") && !custom {
                    (api.set_anchor)(ptr, 3, 1); // bottom
                    (api.set_margin)(ptr, 3, 80);
                } else {
                    (api.set_anchor)(ptr, 2, 1); // top
                    (api.set_margin)(ptr, 2, y.round() as i32);
                }
            }
        }
    } else {
        let physical_x = origin_x + (x * scale).round() as i32;
        let physical_y = origin_y + (y * scale).round() as i32;
        let _ = window.set_position(tauri::PhysicalPosition::new(physical_x, physical_y));
    }
}

pub fn notify(title: &str, message: &str) {
    // The native notification helper blocks on D-Bus. Keep it off Tokio's
    // runtime so a notification from async work cannot start a nested runtime.
    let title = title.to_owned();
    let message = message.to_owned();
    std::thread::spawn(move || {
        let _ = notify_rust::Notification::new()
            .summary(&title)
            .body(&message)
            .appname("SpeekTo.Me")
            .show();
    });
}
