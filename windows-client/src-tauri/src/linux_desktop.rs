use gtk::prelude::*;
use libloading::Library;
use std::ffi::{c_char, CString};
use std::sync::atomic::{AtomicBool, AtomicU8, Ordering};
use std::sync::OnceLock;
use tauri::{
    menu::{CheckMenuItem, MenuBuilder, SubmenuBuilder},
    tray::TrayIconBuilder,
    Manager,
};

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
    // Leave room for the submenu indicator in compact Linux tray menus.
    let mut languages = SubmenuBuilder::new(app, "Language");
    for (code, name) in crate::speaking_language_options() {
        let item = CheckMenuItem::with_id(
            app,
            format!("language:{}", code),
            name,
            true,
            code == active,
            None::<&str>,
        )?;
        languages = languages.item(&item);
    }
    let languages = languages.build()?;
    MenuBuilder::new(app)
        .item(&languages)
        .separator()
        .text("settings", "Open Settings")
        .text("quit", "Quit SpeekTo.Me")
        .build()
}

pub fn refresh_language_menu(app: &tauri::AppHandle) {
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        if let Some(tray) = handle.tray_by_id("main-tray") {
            if let Ok(menu) = speaking_menu(&handle) {
                let _ = tray.set_menu(Some(menu));
            }
        }
    });
}

// GTK owns this tiny native status surface. No WebKit context is created for it.
struct NativeOverlay {
    surface: gtk::DrawingArea,
    layout: gtk::pango::Layout,
    has_preview: bool,
    height: f64,
    timer: Option<gtk::glib::SourceId>,
    frame: u64,
    smoothed: f64,
    pill_x: f64,
    pill_y: f64,
}
thread_local! {
    static NATIVE_OVERLAY: std::cell::RefCell<Option<NativeOverlay>> = const { std::cell::RefCell::new(None) };
}

pub fn setup(app: &tauri::AppHandle) -> tauri::Result<()> {
    let menu = speaking_menu(app)?;
    let mut tray = TrayIconBuilder::with_id("main-tray")
        .menu(&menu)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "settings" => crate::show_or_create_settings_window(app),
            "quit" => crate::lifecycle::quit(app),
            id => {
                if let Some(code) = id.strip_prefix("language:") {
                    let _ = crate::set_active_language(code.to_string());
                }
            }
        });
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;
    let overlay = tauri::WindowBuilder::new(app, "overlay")
        .title("SpeekTo.Me status")
        .inner_size(420.0, 140.0)
        .resizable(false)
        .decorations(false)
        .transparent(true)
        .skip_taskbar(true)
        .always_on_top(true)
        .focusable(false)
        .visible(false)
        .build()?;
    let native = overlay.gtk_window()?;
    native.set_accept_focus(false);
    native.set_focus_on_map(false);
    native.set_app_paintable(true);
    native.connect_draw(|_, cr| {
        cr.set_operator(gtk::cairo::Operator::Source);
        cr.set_source_rgba(0.0, 0.0, 0.0, 0.0);
        let _ = cr.paint();
        cr.set_operator(gtk::cairo::Operator::Over);
        gtk::glib::Propagation::Proceed
    });
    let surface = gtk::DrawingArea::new();
    surface.set_size_request(420, 140);
    surface.connect_draw(|_, cr| {
        draw_overlay(cr);
        gtk::glib::Propagation::Stop
    });
    let layout = surface.create_pango_layout(None);
    let mut font = gtk::pango::FontDescription::from_string("Sans");
    font.set_absolute_size(13.0 * gtk::pango::SCALE as f64);
    layout.set_font_description(Some(&font));
    layout.set_width(370 * gtk::pango::SCALE);
    layout.set_wrap(gtk::pango::WrapMode::WordChar);
    layout.set_height(-1);
    layout.set_ellipsize(gtk::pango::EllipsizeMode::None);
    let container = overlay.default_vbox()?;
    container.pack_start(&surface, true, true, 0);
    container.show();
    surface.show();
    NATIVE_OVERLAY.with(|slot| {
        *slot.borrow_mut() = Some(NativeOverlay {
            surface,
            layout,
            has_preview: false,
            height: 140.0,
            timer: None,
            frame: 0,
            smoothed: 0.0,
            pill_x: 179.0,
            pill_y: 0.0,
        });
    });
    if let Some(api) = layer_shell_api() {
        let ptr = native.as_ptr() as *mut gtk::ffi::GtkWindow;
        unsafe {
            if (api.is_supported)() != 0 {
                (api.init_for_window)(ptr);
                let namespace = CString::new("speektome-overlay").unwrap();
                (api.set_namespace)(ptr, namespace.as_ptr());
                (api.set_layer)(ptr, 3);
                (api.set_keyboard_mode)(ptr, 0);
                (api.set_exclusive_zone)(ptr, 0);
                LAYER_SHELL_ACTIVE.store(true, Ordering::SeqCst);
                eprintln!("[LINUX OVERLAY] Native GTK layer-shell status surface enabled");
            }
        }
    }
    let config = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .clone();
    configure_overlay_position(&overlay, &config);
    apply_overlay_text_and_theme();
    if std::env::args().any(|arg| arg == "--diagnose-native-overlay") {
        gtk::glib::timeout_add_local_once(std::time::Duration::from_millis(500), move || {
            NATIVE_OVERLAY.with(|slot| {
                if let Some(overlay) = slot.borrow().as_ref() {
                    let allocation = overlay.surface.allocation();
                    let report=serde_json::json!({"width":allocation.width(),"height":allocation.height(),"lines":overlay.layout.line_count(),"mapped":overlay.surface.is_mapped(),"processing":OVERLAY_STATE.load(Ordering::SeqCst)==2});
                    let _=std::fs::write(std::env::temp_dir().join("speektome-overlay-check.json"),report.to_string());
                    eprintln!(
                        "[NATIVE OVERLAY CHECK] mapped={} width={} height={} pill=({}, {})",
                        overlay.surface.is_mapped(),
                        allocation.width(),
                        allocation.height(),
                        overlay.pill_x,
                        overlay.pill_y
                    );
                }
            });
        });
    }
    Ok(())
}

fn rounded_rect(cr: &gtk::cairo::Context, x: f64, y: f64, width: f64, height: f64, radius: f64) {
    let pi = std::f64::consts::PI;
    cr.new_sub_path();
    cr.arc(x + width - radius, y + radius, radius, -pi / 2.0, 0.0);
    cr.arc(
        x + width - radius,
        y + height - radius,
        radius,
        0.0,
        pi / 2.0,
    );
    cr.arc(x + radius, y + height - radius, radius, pi / 2.0, pi);
    cr.arc(x + radius, y + radius, radius, pi, 3.0 * pi / 2.0);
    cr.close_path();
}
fn draw_overlay(cr: &gtk::cairo::Context) {
    cr.set_operator(gtk::cairo::Operator::Source);
    cr.set_source_rgba(0.0, 0.0, 0.0, 0.0);
    let _ = cr.paint();
    cr.set_operator(gtk::cairo::Operator::Over);
    let background =
        crate::native_overlay::color(crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed));
    let wave = crate::native_overlay::color(crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed));
    NATIVE_OVERLAY.with(|slot| {
        let mut slot = slot.borrow_mut();
        let Some(overlay) = slot.as_mut() else { return };
        let (x, y) = (overlay.pill_x, overlay.pill_y);
        cr.set_source_rgba(background.0, background.1, background.2, 235.0 / 255.0);
        rounded_rect(cr, x + 0.5, y + 0.5, 61.0, 23.0, 11.5);
        let _ = cr.fill();
        if OVERLAY_STATE.load(Ordering::SeqCst) == 2 {
            processing_border(
                cr,
                crate::native_overlay::Rect {
                    x: x + 0.5,
                    y: y + 0.5,
                    width: 61.0,
                    height: 23.0,
                },
                11.5,
                overlay.frame,
            );
        }
        let logo = crate::native_overlay::color(crate::native_overlay::LOGO_COLOR);
        cr.set_source_rgb(logo.0, logo.1, logo.2);
        let level = crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed) as f64;
        overlay.smoothed = crate::native_overlay::smooth_volume(overlay.smoothed, level);
        let processing = OVERLAY_STATE.load(Ordering::SeqCst) == 2;
        for bar in crate::native_overlay::bars(x, y, overlay.frame, overlay.smoothed, processing) {
            rounded_rect(cr, bar.x, bar.y, bar.width, bar.height, bar.width / 2.0);
            let _ = cr.fill();
        }
        if overlay.has_preview {
            let (width, height) = overlay.layout.pixel_size();
            let card = crate::native_overlay::preview_card_in_surface(
                x,
                y,
                width as f64,
                height as f64,
                overlay.height,
            );
            rounded_rect(cr, card.x, card.y, card.width, card.height, 7.0);
            cr.set_source_rgba(background.0, background.1, background.2, 0.96);
            let _ = cr.fill_preserve();
            cr.set_source_rgba(wave.0, wave.1, wave.2, 0.64);
            cr.set_line_width(1.0);
            let _ = cr.stroke();
            if processing {
                processing_border(cr, card, 7.0, overlay.frame);
            }
            let _ = cr.save();
            cr.rectangle(
                card.x + 8.0,
                card.y + 8.0,
                card.width - 16.0,
                card.height - 16.0,
            );
            cr.clip();
            let text = crate::native_overlay::text_color(background);
            cr.set_source_rgb(text.0, text.1, text.2);
            cr.move_to(card.x + 12.0, card.y + 8.0);
            pangocairo::functions::show_layout(cr, &overlay.layout);
            let _ = cr.restore();
        }
        overlay.frame += 1;
    });
}

fn apply_overlay_text_and_theme() {
    let raw = crate::win32::get_preview_text();
    let text = crate::native_overlay::preview_text(&raw, OVERLAY_STATE.load(Ordering::SeqCst) == 2);
    NATIVE_OVERLAY.with(|slot| {
        if let Some(overlay) = slot.borrow_mut().as_mut() {
            // Pango text, not markup, handles all scripts without browser UI.
            overlay.layout.set_text(&text);
            overlay.has_preview = !text.trim().is_empty();
            overlay.surface.queue_draw();
        }
    });
    if let Some(window) = crate::TAURI_APP
        .get()
        .and_then(|app| app.get_window("overlay"))
    {
        let config = crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone();
        configure_overlay_position(&window, &config);
    }
}

pub fn show_overlay() {
    if crate::lifecycle::is_quitting() {
        return;
    }
    if let Some(app) = crate::TAURI_APP.get() {
        let handle = app.clone();
        let _ = app.run_on_main_thread(move || {
            if let Some(window) = handle.get_window("overlay") {
                apply_overlay_text_and_theme();
                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone();
                configure_overlay_position(&window, &config);
                let _ = window.show();
                let _ = window.set_ignore_cursor_events(true);
                NATIVE_OVERLAY.with(|slot| {
                    if let Some(overlay) = slot.borrow_mut().as_mut() {
                        if overlay.timer.is_none() {
                            overlay.timer = Some(gtk::glib::timeout_add_local(
                                std::time::Duration::from_millis(33),
                                || {
                                    NATIVE_OVERLAY.with(|slot| {
                                        if let Some(overlay) = slot.borrow().as_ref() {
                                            overlay.surface.queue_draw();
                                        }
                                    });
                                    gtk::glib::ControlFlow::Continue
                                },
                            ));
                        }
                    }
                });
            }
        });
    }
}
pub fn hide_overlay() {
    if let Some(app) = crate::TAURI_APP.get() {
        let handle = app.clone();
        let _ = app.run_on_main_thread(move || {
            if let Some(window) = handle.get_window("overlay") {
                let _ = window.hide();
            }
            NATIVE_OVERLAY.with(|slot| {
                if let Some(overlay) = slot.borrow_mut().as_mut() {
                    if let Some(timer) = overlay.timer.take() {
                        timer.remove();
                    }
                    overlay.has_preview = false;
                    overlay.layout.set_text("");
                }
            });
            if let Some(window) = handle.get_window("overlay") {
                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone();
                configure_overlay_position(&window, &config);
            }
        });
    }
}
pub fn set_preview_text(_text: &str) {
    if let Some(app) = crate::TAURI_APP.get() {
        let _ = app.run_on_main_thread(apply_overlay_text_and_theme);
    }
}
pub fn set_preview_state(state: u8) {
    OVERLAY_STATE.store(state, Ordering::SeqCst);
    set_preview_text("");
}
pub fn sync_overlay_theme() {
    if let Some(app) = crate::TAURI_APP.get() {
        let _ = app.run_on_main_thread(apply_overlay_text_and_theme);
    }
}
pub fn update_overlay_position() {
    if let Some(app) = crate::TAURI_APP.get() {
        let handle = app.clone();
        let _ = app.run_on_main_thread(move || {
            if let Some(window) = handle.get_window("overlay") {
                let config = crate::get_active_config()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone();
                configure_overlay_position(&window, &config);
            }
        });
    }
}

pub fn release_idle_webkit(app: &tauri::AppHandle) {
    let app = app.clone();
    // Window destruction is asynchronous. A newly opened Settings window wins
    // this race and keeps its browser processes. The native overlay is not a webview.
    gtk::glib::timeout_add_local_once(std::time::Duration::from_millis(350), move || {
        if app.webview_windows().is_empty() {
            let stopped = crate::lifecycle::terminate_webkit_children();
            crate::lifecycle::trim_idle_heap();
            eprintln!("[WINDOW] Settings destroyed; released {stopped} idle WebKit helpers");
        }
    });
}

fn configure_overlay_position(window: &tauri::Window, config: &crate::config::AppConfig) {
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
    let text_height = NATIVE_OVERLAY.with(|slot| {
        slot.borrow()
            .as_ref()
            .filter(|o| o.has_preview)
            .map(|o| o.layout.pixel_size().1 as f64)
            .unwrap_or(0.0)
    });
    let position_code = if custom {
        4
    } else {
        crate::native_overlay::position_code(position)
    };
    let height = crate::native_overlay::surface_height_at(
        text_height,
        monitor_height,
        position_code,
        config.pill_custom_y as f64,
    );
    let design = crate::native_overlay::placement_with_height(
        monitor_width,
        monitor_height,
        if custom {
            4
        } else {
            crate::native_overlay::position_code(position)
        },
        config.pill_custom_x as f64,
        config.pill_custom_y as f64,
        height,
    );
    let (pill_x, pill_y) = (design.pill_x, design.pill_y);
    let (x, y) = (design.x, design.y);
    let changed =
        NATIVE_OVERLAY.with(|slot| slot.borrow().as_ref().is_some_and(|o| o.height != height));
    NATIVE_OVERLAY.with(|slot| {
        if let Some(overlay) = slot.borrow_mut().as_mut() {
            if overlay.height != height {
                overlay.height = height;
                overlay.surface.set_size_request(420, height.ceil() as i32);
            }
            overlay.pill_x = pill_x;
            overlay.pill_y = pill_y;
            overlay.surface.queue_draw();
        }
    });
    if changed {
        let _ = window.set_size(tauri::LogicalSize::new(420.0, height));
    }
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

fn processing_border(
    cr: &gtk::cairo::Context,
    rect: crate::native_overlay::Rect,
    radius: f64,
    frame: u64,
) {
    for (width, alpha) in [
        (8.0, 0.025),
        (6.0, 0.045),
        (4.0, 0.08),
        (2.6, 0.16),
        (1.2, 0.95),
    ] {
        let gradient = gtk::cairo::LinearGradient::new(
            rect.x,
            rect.y,
            rect.x + rect.width,
            rect.y + rect.height,
        );
        for i in 0..=8 {
            let offset = i as f64 / 8.0;
            let (r, g, b) = crate::native_overlay::processing_color(offset + frame as f64 * 0.008);
            gradient.add_color_stop_rgba(offset, r, g, b, alpha);
        }
        rounded_rect(cr, rect.x, rect.y, rect.width, rect.height, radius);
        let _ = cr.set_source(&gradient);
        cr.set_line_width(width);
        let _ = cr.stroke();
    }
}
