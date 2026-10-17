//! Linux desktop integration helpers.

use std::path::PathBuf;

pub const APP_ID: &str = "me.speekto.SpeekToMe";

fn launch_executable() -> std::io::Result<PathBuf> {
    // AppImage mounts are temporary; its APPIMAGE value points to the durable
    // file the user launched, which is also what desktop launchers need.
    if let Some(path) = std::env::var_os("APPIMAGE") {
        return Ok(PathBuf::from(path));
    }
    std::env::current_exe()
}

/// The GlobalShortcuts portal requires a desktop entry for host applications.
/// Native packages install one already; standalone binaries and AppImages get
/// a per-user entry so they can register an identity with the portal.
pub fn ensure_portal_desktop_entry() -> std::io::Result<()> {
    let file = format!("{APP_ID}.desktop");
    let data_home = std::env::var_os("XDG_DATA_HOME")
        .filter(|path| !path.is_empty())
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".local/share")))
        .ok_or_else(|| {
            std::io::Error::new(std::io::ErrorKind::NotFound, "No XDG data directory")
        })?;
    let user_entry = data_home.join("applications").join(&file);
    if user_entry.exists() {
        return Ok(());
    }
    let system_dirs =
        std::env::var_os("XDG_DATA_DIRS").unwrap_or_else(|| "/usr/local/share:/usr/share".into());
    // AppImage prepends its private mount to XDG_DATA_DIRS. The portal runs
    // outside that environment and cannot see the mounted desktop entry.
    if std::env::var_os("APPIMAGE").is_none()
        && std::env::split_paths(&system_dirs)
            .any(|dir| dir.join("applications").join(&file).exists())
    {
        return Ok(());
    }
    let executable = launch_executable()?;
    std::fs::create_dir_all(user_entry.parent().unwrap())?;
    let contents = format!(
        "[Desktop Entry]\nType=Application\nName=SpeekTo.Me\nComment=Voice typing\nExec={}\nTerminal=false\nCategories=Utility;\n",
        desktop_exec_arg(&executable.to_string_lossy())
    );
    let temporary = user_entry.with_extension("desktop.tmp");
    std::fs::write(&temporary, contents)?;
    std::fs::rename(temporary, user_entry)
}

#[cfg(target_os = "linux")]
static INSTANCE_LISTENER: std::sync::OnceLock<std::os::unix::net::UnixListener> =
    std::sync::OnceLock::new();

/// Bind a per-user local socket and keep it open for the lifetime of the
/// process. Returns true for the process that acquired the instance slot.
/// A stale socket left after a crash is removed only when it cannot be reached.
#[cfg(target_os = "linux")]
pub fn ensure_single_instance() -> bool {
    use std::os::unix::net::{UnixListener, UnixStream};

    if INSTANCE_LISTENER.get().is_some() {
        return true;
    }

    unsafe extern "C" {
        fn getuid() -> u32;
    }
    let uid = unsafe { getuid() };
    let runtime_dir = std::env::var_os("XDG_RUNTIME_DIR")
        .filter(|path| !path.is_empty())
        .map(PathBuf::from)
        .filter(|path| path.is_dir());
    let socket_path = runtime_dir
        .unwrap_or_else(std::env::temp_dir)
        .join(format!("speektome-{uid}.sock"));

    if let Some(parent) = socket_path.parent() {
        if std::fs::create_dir_all(parent).is_err() {
            return false;
        }
    }

    let listener = match UnixListener::bind(&socket_path) {
        Ok(listener) => listener,
        Err(_) => {
            // A successful connect means another process owns the socket.
            if UnixStream::connect(&socket_path).is_ok() {
                return false;
            }
            if std::fs::remove_file(&socket_path).is_err() {
                return false;
            }
            match UnixListener::bind(&socket_path) {
                Ok(listener) => listener,
                Err(_) => return false,
            }
        }
    };

    // If another thread won the race, dropping this listener releases its
    // socket path only if we still own that path; OnceLock gives us no winner
    // indication, so avoid concurrent calls from startup code.
    INSTANCE_LISTENER.set(listener).is_ok()
}

#[cfg(not(target_os = "linux"))]
pub fn ensure_single_instance() -> bool {
    true
}

fn config_home() -> Option<PathBuf> {
    std::env::var_os("XDG_CONFIG_HOME")
        .filter(|path| !path.is_empty())
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".config")))
}

/// Keep the XDG autostart entry in sync with the user's saved preference.
/// Errors are deliberately non-fatal: a missing desktop session must not
/// prevent saving the rest of the application settings.
pub fn sync_autostart(enabled: bool) {
    let Some(config_dir) = config_home() else {
        return;
    };
    let autostart_dir = config_dir.join("autostart");
    let entry = autostart_dir.join("speektome.desktop");

    if !enabled {
        let _ = std::fs::remove_file(entry);
        return;
    }

    let Ok(executable) = launch_executable() else {
        return;
    };
    if std::fs::create_dir_all(&autostart_dir).is_err() {
        return;
    }

    let contents = format!(
        "[Desktop Entry]\nType=Application\nName=SpeekTo.Me\nComment=Voice typing\nExec={}\nTerminal=false\nX-GNOME-Autostart-enabled=true\n",
        desktop_exec_arg(&executable.to_string_lossy())
    );
    let temporary = autostart_dir.join("speektome.desktop.tmp");
    if std::fs::write(&temporary, contents).is_ok() {
        let _ = std::fs::rename(temporary, entry);
    }
}

// Desktop Entry Exec arguments use a small escaping grammar, not shell syntax.
// Quote the executable as one argument and escape reserved characters.
fn desktop_exec_arg(value: &str) -> String {
    let mut escaped = String::from("\"");
    for ch in value.chars() {
        if matches!(ch, '\\' | '"' | '`' | '$') {
            escaped.push('\\');
        }
        if ch == '%' {
            escaped.push('%'); // %% represents a literal percent in Exec.
        }
        escaped.push(ch);
    }
    escaped.push('"');
    escaped
}
