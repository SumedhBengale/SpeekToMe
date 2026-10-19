//! Windows implementation of the shared launch-at-sign-in preference.
#[cfg(target_os = "windows")]
pub fn sync(enabled: bool) -> std::io::Result<()> {
    use winreg::{enums::HKEY_CURRENT_USER, RegKey};
    let (run, _) = RegKey::predef(HKEY_CURRENT_USER)
        .create_subkey(r"Software\Microsoft\Windows\CurrentVersion\Run")?;
    if enabled {
        let executable = std::env::current_exe()?;
        run.set_value("SpeekToMe", &launch_command(&executable.to_string_lossy()))
    } else {
        match run.delete_value("SpeekToMe") {
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
            result => result,
        }
    }
}

fn launch_command(executable: &str) -> String {
    format!("\"{executable}\"")
}

#[cfg(test)]
mod tests {
    #[test]
    fn startup_command_keeps_spaces_in_one_executable_path() {
        assert_eq!(
            super::launch_command(r"C:\Users\Test User\SpeekTo.Me\speektome.exe"),
            r#""C:\Users\Test User\SpeekTo.Me\speektome.exe""#
        );
    }
}
