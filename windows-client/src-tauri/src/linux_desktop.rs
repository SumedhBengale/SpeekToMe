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
const MEETING: u8 = 3;
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
    label: gtk::pango::Layout,
    /// How far the user has dragged the meeting pill, in logical pixels.
    meeting_offset: (f64, f64),
    /// Where a meeting-pill drag grabbed the surface, and when the last move
    /// may still be in flight.
    drag: Option<((f64, f64), std::time::Instant)>,
    has_preview: bool,
    height: f64,
    timer: Option<gtk::glib::SourceId>,
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
    let label = surface.create_pango_layout(Some(crate::native_overlay::MEETING_LABEL));
    let mut label_font = gtk::pango::FontDescription::from_string("Sans Semi-Bold");
    label_font
        .set_absolute_size(crate::native_overlay::MEETING_LABEL_SIZE * gtk::pango::SCALE as f64);
    label.set_font_description(Some(&label_font));
    surface.add_events(
        gtk::gdk::EventMask::BUTTON_PRESS_MASK
            | gtk::gdk::EventMask::BUTTON_RELEASE_MASK
            | gtk::gdk::EventMask::POINTER_MOTION_MASK,
    );
    surface.connect_button_press_event(|_, event| {
        if event.button() == 1 && OVERLAY_STATE.load(Ordering::SeqCst) == MEETING {
            NATIVE_OVERLAY.with(|slot| {
                if let Some(overlay) = slot.borrow_mut().as_mut() {
                    overlay.drag = Some((event.position(), std::time::Instant::now()));
                }
            });
        }
        gtk::glib::Propagation::Stop
    });
    surface.connect_button_release_event(|_, _| {
        NATIVE_OVERLAY.with(|slot| {
            if let Some(overlay) = slot.borrow_mut().as_mut() {
                overlay.drag = None;
            }
        });
        gtk::glib::Propagation::Stop
    });
    surface.connect_motion_notify_event(|_, event| {
        drag_meeting_pill(event.position());
        gtk::glib::Propagation::Stop
    });
    let container = overlay.default_vbox()?;
    container.pack_start(&surface, true, true, 0);
    container.show();
    surface.show();
    NATIVE_OVERLAY.with(|slot| {
        *slot.borrow_mut() = Some(NativeOverlay {
            surface,
            layout,
            label,
            meeting_offset: (0.0, 0.0),
            drag: None,
            has_preview: false,
            height: 140.0,
            timer: None,
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

fn draw_overlay(cr: &gtk::cairo::Context) {
    cr.set_operator(gtk::cairo::Operator::Source);
    cr.set_source_rgba(0.0, 0.0, 0.0, 0.0);
    let _ = cr.paint();
    let background =
        crate::native_overlay::color(crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed));
    NATIVE_OVERLAY.with(|slot| {
        let mut slot = slot.borrow_mut();
        let Some(overlay) = slot.as_mut() else { return };
        let state = OVERLAY_STATE.load(Ordering::SeqCst);
        let level = if state == 3 {
            crate::audio::MIC_VOLUME
                .load(Ordering::Relaxed)
                .max(crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed))
        } else {
            crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed)
        };
        overlay.smoothed = crate::native_overlay::smooth_volume(overlay.smoothed, level as f64);

        let scale = overlay.surface.scale_factor().max(1) as f64;
        let (width, height) = (
            (crate::native_overlay::WIDTH * scale).ceil() as usize,
            (overlay.height * scale).ceil() as usize,
        );
        let meeting = state == MEETING;
        let pill = if meeting {
            crate::native_overlay::meeting_pill(overlay.pill_x, overlay.pill_y)
        } else {
            crate::native_overlay::Rect {
                x: overlay.pill_x,
                y: overlay.pill_y,
                width: crate::native_overlay::PILL_WIDTH,
                height: crate::native_overlay::PILL_HEIGHT,
            }
        };
        let logo = if meeting {
            crate::native_overlay::meeting_logo(pill)
        } else {
            (pill.x, pill.y)
        };
        let mut label = Vec::new();
        if meeting {
            let text_box = crate::native_overlay::meeting_label_box(pill);
            let (_, label_height) = overlay.label.pixel_size();
            let clip = [pill.x, pill.y, pill.width, pill.height];
            label = coverage(width, height, scale, clip, |cr| {
                cr.move_to(
                    text_box.x,
                    text_box.y + (text_box.height - label_height as f64) / 2.0,
                );
                pangocairo::functions::show_layout(cr, &overlay.label);
            });
        }
        let card = (overlay.has_preview && !meeting).then(|| {
            let (text_width, text_height) = overlay.layout.pixel_size();
            crate::native_overlay::preview_card_in_surface(
                pill.x,
                pill.y,
                text_width as f64,
                text_height as f64,
                overlay.height,
            )
        });
        let text = crate::native_overlay::text_color(background);
        let mut glyphs = Vec::new();
        let mut words = Vec::new();
        if let Some(card) = card {
            let origin = (card.x + 12.0, card.y + 8.0);
            let clip = [
                card.x + 8.0,
                card.y + 8.0,
                card.width - 16.0,
                card.height - 16.0,
            ];
            let layout = &overlay.layout;
            glyphs = coverage(width, height, scale, clip, |cr| {
                cr.move_to(origin.0, origin.1);
                pangocairo::functions::show_layout(cr, layout);
            });
            for (range, look) in valid_glows(layout, crate::win32::preview_glow()) {
                if let Some(word) = Word::new(layout, origin, range) {
                    let mask = coverage(width, height, scale, clip, |cr| {
                        word.glyphs(cr, layout);
                        let _ = cr.fill();
                    });
                    words.push((mask, look));
                }
            }
        }
        let seconds = crate::win32::animation_seconds();
        let mood = crate::speek::overlay_mood(state, crate::win32::preview_finished());
        let speek = crate::speek::overlay_frame(seconds, mood, overlay.smoothed);
        let mut pixels = vec![0u32; width * height];
        crate::overlay_paint::paint(
            &crate::overlay_paint::Scene {
                scale,
                seconds,
                background,
                wave: crate::native_overlay::color(
                    crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed),
                ),
                processing: state == 2,
                pill: Some(pill),
                speek: Some((logo.0, logo.1, speek)),
                label: meeting.then_some(label.as_slice()),
                card,
                text: card.map(|_| crate::overlay_paint::Text {
                    color: text,
                    glyphs: &glyphs,
                    words: &words,
                }),
            },
            &mut pixels,
            width,
            height,
        );
        let Ok(mut image) = gtk::cairo::ImageSurface::create(
            gtk::cairo::Format::ARgb32,
            width as i32,
            height as i32,
        ) else {
            return;
        };
        let stride = image.stride() as usize;
        if let Ok(mut data) = image.data() {
            for (row, line) in pixels.chunks(width).enumerate() {
                for (column, pixel) in line.iter().enumerate() {
                    let at = row * stride + column * 4;
                    data[at..at + 4].copy_from_slice(&pixel.to_ne_bytes());
                }
            }
        }
        image.set_device_scale(scale, scale);
        cr.set_operator(gtk::cairo::Operator::Over);
        let _ = cr.set_source_surface(&image, 0.0, 0.0);
        let _ = cr.paint();
    });
}

/// Glyph coverage drawn by `draw` in logical pixels, inside `clip`, as one
/// value per device pixel.
fn coverage(
    width: usize,
    height: usize,
    scale: f64,
    clip: [f64; 4],
    draw: impl FnOnce(&gtk::cairo::Context),
) -> Vec<f32> {
    let mut values = vec![0f32; width * height];
    let Ok(mut mask) =
        gtk::cairo::ImageSurface::create(gtk::cairo::Format::A8, width as i32, height as i32)
    else {
        return values;
    };
    mask.set_device_scale(scale, scale);
    if let Ok(cr) = gtk::cairo::Context::new(&mask) {
        cr.rectangle(clip[0], clip[1], clip[2], clip[3]);
        cr.clip();
        cr.set_source_rgba(0.0, 0.0, 0.0, 1.0);
        draw(&cr);
    }
    let stride = mask.stride() as usize;
    if let Ok(data) = mask.data() {
        for (i, value) in values.iter_mut().enumerate() {
            *value = data[(i / width) * stride + i % width] as f32 / 255.0;
        }
    }
    values
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
                apply_input_region(&window);
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
    if let Some(app) = crate::TAURI_APP.get() {
        let handle = app.clone();
        let _ = app.run_on_main_thread(move || {
            if state != MEETING {
                // The next meeting starts where the settings place the pill.
                NATIVE_OVERLAY.with(|slot| {
                    if let Some(overlay) = slot.borrow_mut().as_mut() {
                        overlay.meeting_offset = (0.0, 0.0);
                        overlay.drag = None;
                    }
                });
            }
            apply_overlay_text_and_theme();
            if let Some(window) = handle.get_window("overlay") {
                apply_input_region(&window);
            }
        });
    }
}

/// Only the meeting pill takes the mouse, so it can be dragged out of the
/// way; the rest of the overlay, and the pill while dictating, let clicks
/// through to the windows below.
fn apply_input_region(window: &tauri::Window) {
    let Ok(native) = window.gtk_window() else {
        return;
    };
    let pill = NATIVE_OVERLAY.with(|slot| {
        slot.borrow()
            .as_ref()
            .map(|o| crate::native_overlay::meeting_pill(o.pill_x, o.pill_y))
    });
    let region = match pill {
        Some(pill) if OVERLAY_STATE.load(Ordering::SeqCst) == MEETING => {
            gtk::cairo::Region::create_rectangle(&gtk::cairo::RectangleInt::new(
                pill.x.floor() as i32,
                pill.y.floor() as i32,
                pill.width.ceil() as i32,
                pill.height.ceil() as i32,
            ))
        }
        _ => gtk::cairo::Region::create(),
    };
    native.input_shape_combine_region(Some(&region));
    if let Some(gdk_window) = native.window() {
        let cursor = (OVERLAY_STATE.load(Ordering::SeqCst) == MEETING)
            .then(|| gtk::gdk::Cursor::from_name(&gdk_window.display(), "grab"))
            .flatten();
        gdk_window.set_cursor(cursor.as_ref());
    }
}

/// Moves the meeting pill with the pointer. The surface moves under the
/// pointer, so positions are relative to where the grab started; events
/// that arrive before the last move lands are skipped so the pill does not
/// overshoot.
fn drag_meeting_pill(position: (f64, f64)) {
    let moved = NATIVE_OVERLAY.with(|slot| {
        let mut slot = slot.borrow_mut();
        let overlay = slot.as_mut()?;
        let (grab, ready) = overlay.drag?;
        let now = std::time::Instant::now();
        if now < ready {
            return None;
        }
        let delta = (position.0 - grab.0, position.1 - grab.1);
        if delta.0.abs() < 0.5 && delta.1.abs() < 0.5 {
            return None;
        }
        overlay.meeting_offset.0 += delta.0;
        overlay.meeting_offset.1 += delta.1;
        overlay.drag = Some((grab, now + std::time::Duration::from_millis(25)));
        Some(())
    });
    if moved.is_none() {
        return;
    }
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
    let (mut x, mut y) = (design.x, design.y);
    let dragged = OVERLAY_STATE.load(Ordering::SeqCst) == MEETING
        && NATIVE_OVERLAY.with(|slot| {
            let mut slot = slot.borrow_mut();
            let Some(overlay) = slot.as_mut() else {
                return false;
            };
            if overlay.meeting_offset == (0.0, 0.0) {
                return false;
            }
            // Keep the whole meeting pill on the monitor.
            let pill = crate::native_overlay::meeting_pill(pill_x, pill_y);
            let left =
                (x + overlay.meeting_offset.0).clamp(-pill.x, monitor_width - pill.x - pill.width);
            let top = (y + overlay.meeting_offset.1)
                .clamp(-pill.y, monitor_height - pill.y - pill.height);
            overlay.meeting_offset = (left - x, top - y);
            (x, y) = (left, top);
            true
        });
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
                if position.starts_with("Bottom") && !custom && !dragged {
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

/// The glows whose ranges are whole characters of the layout's text.
fn valid_glows(
    layout: &gtk::pango::Layout,
    glows: Vec<(std::ops::Range<usize>, crate::native_overlay::Polish)>,
) -> Vec<(std::ops::Range<usize>, crate::native_overlay::Polish)> {
    let text = layout.text();
    glows
        .into_iter()
        .filter(|(range, _)| {
            range.start < range.end
                && range.end <= text.len()
                && text.is_char_boundary(range.start)
                && text.is_char_boundary(range.end)
        })
        .collect()
}

/// A changing word's character boxes in surface pixels.
struct Word {
    origin: (f64, f64),
    boxes: Vec<[f64; 4]>,
}

impl Word {
    fn new(
        layout: &gtk::pango::Layout,
        origin: (f64, f64),
        range: std::ops::Range<usize>,
    ) -> Option<Self> {
        let text = layout.text();
        let scale = gtk::pango::SCALE as f64;
        let boxes: Vec<[f64; 4]> = text[range.clone()]
            .char_indices()
            .map(|(offset, _)| {
                let r = layout.index_to_pos((range.start + offset) as i32);
                // Right-to-left characters report a negative width.
                let (x, width) = if r.width() < 0 {
                    (r.x() + r.width(), -r.width())
                } else {
                    (r.x(), r.width())
                };
                [
                    origin.0 + x as f64 / scale,
                    origin.1 + r.y() as f64 / scale,
                    width as f64 / scale,
                    r.height() as f64 / scale,
                ]
            })
            .collect();
        boxes
            .iter()
            .any(|b| b[2] > 0.0 && b[3] > 0.0)
            .then_some(Self { origin, boxes })
    }

    /// Clips to the word and sets the path to its glyphs.
    fn glyphs(&self, cr: &gtk::cairo::Context, layout: &gtk::pango::Layout) {
        for b in &self.boxes {
            cr.rectangle(b[0], b[1], b[2], b[3]);
        }
        cr.clip();
        cr.move_to(self.origin.0, self.origin.1);
        pangocairo::functions::layout_path(cr, layout);
    }
}
