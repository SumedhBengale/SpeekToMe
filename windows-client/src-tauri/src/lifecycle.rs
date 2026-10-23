//! Closing Settings keeps native capture alive; an explicit Quit never does.
use std::sync::atomic::{AtomicBool, Ordering};

static QUITTING: AtomicBool = AtomicBool::new(false);

pub fn is_quitting() -> bool {
    QUITTING.load(Ordering::SeqCst)
}

pub fn keep_tray_alive(exit_code: Option<i32>) -> bool {
    exit_code.is_none() && !is_quitting()
}

pub fn quit(app: &tauri::AppHandle) {
    if QUITTING.swap(true, Ordering::SeqCst) {
        return;
    }
    // Keep the event loop alive while the disk writer finishes. No network wait
    // is required: the durable manifest resumes upload on the next launch.
    std::thread::spawn(|| {
        std::thread::sleep(std::time::Duration::from_secs(15));
        force_exit(); // A blocked audio driver cannot prevent eventual shutdown.
    });
    let app = app.clone();
    std::thread::spawn(move || {
        crate::shortcuts::cancel();
        if crate::audio::MEETING_STATE.get().is_some() {
            let result = crate::audio::stop_meeting_recording();
            if let Some(path) = crate::CURRENT_MEETING_PATH
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .take()
            {
                match result {
                    Ok(()) => {
                        crate::update_meeting_status(&path.to_string_lossy(), "recorded", None)
                    }
                    Err(_) => crate::update_meeting_status(
                        &path.to_string_lossy(),
                        "recording",
                        Some("Interrupted recording will be recovered on restart".into()),
                    ),
                }
            }
        }
        crate::playback::clear();
        app.exit(0);
    });
}

pub fn force_exit() -> ! {
    #[cfg(target_os = "linux")]
    {
        terminate_webkit_children();
        unsafe extern "C" {
            fn _exit(code: i32) -> !;
        }
        unsafe { _exit(0) }
    }
    #[cfg(target_os = "windows")]
    unsafe {
        windows_sys::Win32::System::Threading::ExitProcess(0);
    }
    #[cfg(not(any(target_os = "linux", target_os = "windows")))]
    std::process::exit(0)
}

#[cfg(target_os = "linux")]
pub fn terminate_webkit_children() -> usize {
    // Never signal a browser owned by another application. Only direct children
    // with WebKit's actual helper executable names qualify.
    let parent = std::process::id().to_string();
    let Ok(entries) = std::fs::read_dir("/proc") else {
        return 0;
    };
    let mut stopped = 0;
    for entry in entries.flatten() {
        let Ok(pid) = entry.file_name().to_string_lossy().parse::<i32>() else {
            continue;
        };
        let path = entry.path();
        let Ok(status) = std::fs::read_to_string(path.join("status")) else {
            continue;
        };
        if !status.lines().any(|line| {
            line.strip_prefix("PPid:")
                .is_some_and(|value| value.trim() == parent)
        }) {
            continue;
        }
        let Ok(exe) = std::fs::read_link(path.join("exe")) else {
            continue;
        };
        if !matches!(
            exe.file_name().and_then(|name| name.to_str()),
            Some("WebKitWebProcess" | "WebKitNetworkProcess" | "WebKitGPUProcess")
        ) && !(is_quitting()
            && exe.file_name().and_then(|name| name.to_str()) == Some("speektome-keywatch"))
        {
            continue;
        }
        unsafe extern "C" {
            fn kill(pid: i32, signal: i32) -> i32;
        }
        // The app runs unprivileged; the kernel also enforces signal ownership.
        if unsafe { kill(pid, 15) } == 0 {
            stopped += 1;
        }
    }
    stopped
}

#[cfg(all(target_os = "linux", target_env = "gnu"))]
pub fn trim_idle_heap() {
    unsafe extern "C" {
        fn malloc_trim(pad: usize) -> i32;
    }
    unsafe {
        malloc_trim(0);
    }
}
#[cfg(all(target_os = "linux", not(target_env = "gnu")))]
pub fn trim_idle_heap() {}

#[cfg(test)]
mod tests {
    #[test]
    fn only_window_closure_keeps_the_tray_running() {
        assert!(super::keep_tray_alive(None));
        assert!(!super::keep_tray_alive(Some(0)));
        assert!(!super::keep_tray_alive(Some(1)));
    }
}
