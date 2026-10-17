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
    // Do not wait for network requests, a suspended audio device, or GTK to
    // cooperate. Normal Tauri cleanup gets a short head start; OS teardown is
    // the fallback and releases all native handles/threads and capture devices.
    std::thread::spawn(|| {
        std::thread::sleep(std::time::Duration::from_millis(250));
        force_exit();
    });
    // Best-effort audio finalization runs off the UI thread and cannot extend
    // the hard deadline. Existing local recording recovery handles interruptions.
    std::thread::spawn(|| {
        if let Some(state) = crate::STATE.get() {
            if let Ok(mut state) = state.try_lock() {
                if let Some(stop) = state.stop_tx.take() {
                    let _ = stop.send(true);
                }
            }
        }
        if crate::audio::MEETING_STATE.get().is_some() {
            let _ = crate::audio::stop_meeting_recording();
        }
    });
    app.exit(0);
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
