//! Settings is disposable. Tray/capture state never belongs to its webview.
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use tauri::Manager;
static DIAGNOSTIC: AtomicBool = AtomicBool::new(false);
#[cfg(target_os = "linux")]
static GENERATION: AtomicUsize = AtomicUsize::new(0);
static CONFIG_READS: AtomicUsize = AtomicUsize::new(0);

pub fn note_config_read() {
    if DIAGNOSTIC.load(Ordering::Relaxed) {
        CONFIG_READS.fetch_add(1, Ordering::Relaxed);
    }
}

fn remember_geometry(window: &tauri::Window) {
    if !window.is_maximized().unwrap_or(false) && !window.is_minimized().unwrap_or(false) {
        if let Ok(position) = window.outer_position() {
            *crate::LAST_WINDOW_POSITION
                .get_or_init(|| std::sync::Mutex::new(None))
                .lock()
                .unwrap_or_else(|e| e.into_inner()) = Some(position);
        }
        if let Ok(size) = window.inner_size() {
            *crate::LAST_WINDOW_SIZE
                .get_or_init(|| std::sync::Mutex::new(None))
                .lock()
                .unwrap_or_else(|e| e.into_inner()) = Some(size);
        }
    }
}

pub fn dismiss(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        remember_geometry(&window.as_ref().window());
        #[cfg(target_os = "macos")]
        crate::macos_desktop::prepare_settings_close(&window);
        // Destroy also bypasses any future page beforeunload handler. No hidden
        // Settings page should keep JS, timers, media or a renderer alive.
        if let Err(error) = window.destroy() {
            eprintln!("[WINDOW] Could not destroy Settings: {error}");
        }
    }
}

pub fn watch(window: &tauri::WebviewWindow) {
    #[cfg(target_os = "macos")]
    crate::macos_desktop::track_settings(window);
    #[cfg(target_os = "linux")]
    {
        use gtk::prelude::*;
        let generation = GENERATION.fetch_add(1, Ordering::SeqCst) + 1;
        if let Ok(native) = window.gtk_window() {
            let app = window.app_handle().clone();
            native.connect_hide(move |_| {
                let app = app.clone();
                gtk::glib::idle_add_local_once(move || {
                    if GENERATION.load(Ordering::SeqCst) == generation {
                        dismiss(&app);
                    }
                });
            });
        }
    }
    #[cfg(not(any(target_os = "linux", target_os = "macos")))]
    let _ = window;
}

pub fn on_window_event(window: &tauri::Window, event: &tauri::WindowEvent) {
    if window.label() != "main" {
        return;
    }
    match event {
        tauri::WindowEvent::CloseRequested { api, .. } => {
            api.prevent_close();
            dismiss(window.app_handle());
        }
        tauri::WindowEvent::Resized(_) | tauri::WindowEvent::Focused(false) => {
            // Switching applications and minimizing both retain Settings.
            remember_geometry(window);
        }
        tauri::WindowEvent::Moved(_) => remember_geometry(window),
        tauri::WindowEvent::Destroyed => {
            crate::shortcuts::set_selecting(false);
            #[cfg(target_os = "linux")]
            crate::linux_desktop::release_idle_webkit(window.app_handle());
            #[cfg(target_os = "macos")]
            crate::macos_desktop::release_idle_browser(window.app_handle());
            eprintln!("[WINDOW] Settings destroyed; native tray/capture remain available.");
        }
        _ => {}
    }
}

/// Actual production Settings and teardown handlers, without capture/uploads.
/// Bounded cycles exercise close, Hide, custom minimize and native OS minimize.
pub fn run_diagnostic() {
    DIAGNOSTIC.store(true, Ordering::Relaxed);
    let _ = rustls::crypto::ring::default_provider().install_default();
    let app = tauri::Builder::default()
        .invoke_handler(crate::settings_invoke_handler())
        .on_window_event(on_window_event)
        .setup(|app| {
            let handle = app.handle().clone();
            let _ = crate::TAURI_APP.set(handle.clone());
            #[cfg(target_os = "macos")]
            crate::macos_desktop::setup(&handle)?;
            #[cfg(target_os = "linux")]
            crate::linux_desktop::setup(&handle)?;
            #[cfg(target_os = "linux")]
            let wayland = {
                use gtk::prelude::*;
                gtk::gdk::Display::default().is_some_and(|display| display.type_().name().contains("Wayland"))
            };
            #[cfg(not(target_os = "linux"))]
            let wayland = false;
            std::thread::spawn(move || {
                let mut samples = Vec::new();
                let actions = vec![
                    "close",
                    "hide",
                    "minimize",
                    "native-minimize",
                    "rapid-reopen",
                    "close",
                ];
                #[cfg(target_os = "macos")]
                let actions = {
                    let mut actions = actions;
                    actions.insert(4, "app-hide");
                    actions
                };
                // Wayland does not reliably expose compositor minimization.
                // The app minimize command is still exercised on every platform.
                let actions = actions.into_iter().filter(|action| !wayland || *action != "native-minimize");
                for action in actions {
                    let show = handle.clone();
                    let _ = handle
                        .run_on_main_thread(move || crate::show_or_create_settings_window(&show));
                    std::thread::sleep(std::time::Duration::from_millis(1800));
                    samples.push(snapshot(&handle, action, "open"));
                    let close = handle.clone();
                    let _ = handle.run_on_main_thread(move || {
                        if let Some(window) = close.get_webview_window("main") {
                            match action {
                                "close" | "rapid-reopen" => {
                                    let _ = window.close();
                                }
                                "hide" => crate::hide_window(window),
                                "minimize" => crate::minimize_window(window),
                                #[cfg(target_os = "macos")]
                                "app-hide" => {
                                    let _ = close.hide();
                                }
                                _ => {
                                    let _ = window.minimize();
                                }
                            }
                        }
                    });
                    if matches!(action, "minimize" | "native-minimize") {
                        std::thread::sleep(std::time::Duration::from_millis(900));
                        samples.push(snapshot(&handle, action, "minimized-retained"));
                        let close = handle.clone();
                        let _ = handle.run_on_main_thread(move || dismiss(&close));
                    }
                    if action == "rapid-reopen" {
                        std::thread::sleep(std::time::Duration::from_millis(100));
                        let show = handle.clone();
                        let _ = handle.run_on_main_thread(move || {
                            crate::show_or_create_settings_window(&show)
                        });
                        std::thread::sleep(std::time::Duration::from_millis(900));
                        samples.push(snapshot(&handle, action, "reopened"));
                        let close = handle.clone();
                        let _ = handle.run_on_main_thread(move || dismiss(&close));
                    }
                    std::thread::sleep(std::time::Duration::from_millis(1800));
                    samples.push(snapshot(&handle, action, "tray"));
                }
                std::thread::sleep(std::time::Duration::from_secs(6));
                samples.push(snapshot(&handle, "idle", "tray-idle"));
                let report = serde_json::json!({"pid": std::process::id(), "samples": samples, "native_minimize_state_available": !wayland});
                let path = crate::config::get_config_path()
                    .with_file_name("settings-lifecycle-diagnostic.json");
                let _ = std::fs::write(&path, serde_json::to_vec_pretty(&report).unwrap());
                eprintln!("[SETTINGS LIFECYCLE DIAGNOSTIC] {}", path.display());
                crate::lifecycle::quit(&handle);
            });
            Ok(())
        })
        .build(crate::desktop_context())
        .expect("Settings lifecycle diagnostic");
    app.run(|_, event| {
        if let tauri::RunEvent::ExitRequested { code, api, .. } = event {
            if crate::lifecycle::keep_tray_alive(code) {
                api.prevent_exit();
            }
        }
    });
}

fn snapshot(app: &tauri::AppHandle, action: &str, phase: &str) -> serde_json::Value {
    eprintln!("[SETTINGS MEMORY] action={action} phase={phase}");
    let mut value = serde_json::json!({"action": action, "phase": phase,
        "webviews": app.webview_windows().len(), "host_alive": !crate::lifecycle::is_quitting(),
        "configuration_reads": CONFIG_READS.load(Ordering::Relaxed),
        "authenticated": crate::ACTIVE_HAS_SESSION_TOKEN.load(std::sync::atomic::Ordering::SeqCst)});
    #[cfg(target_os = "macos")]
    {
        value["native"] = crate::macos_desktop::settings_snapshot();
    }
    #[cfg(not(target_os = "macos"))]
    let _ = &mut value;
    value
}
