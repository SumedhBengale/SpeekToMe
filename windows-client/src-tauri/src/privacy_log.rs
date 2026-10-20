//! Runtime values (including provider errors) must never reach diagnostic output.
macro_rules! println {
    () => { std::eprintln!() };
    ($template:literal $(, $argument:expr)* $(,)?) => {{
        let _ = format_args!($template $(, $argument)*);
        std::eprintln!("{}", $template);
    }};
    ($($argument:tt)*) => {{ let _ = format_args!($($argument)*); std::eprintln!("[diagnostic event]"); }};
}
macro_rules! eprintln {
    ($($argument:tt)*) => { println!($($argument)*) };
}
#[cfg(test)]
mod tests {
    #[test]
    fn emit_private_values() {
        if std::env::var_os("SPEEKTOME_LOG_TEST_CHILD").is_none() {
            return;
        }
        let private = "privacy-sentinel-transcript-command";
        println!(
            "[PRIVACY TEST] output={} provider_error={:?}",
            private, private
        );
        eprintln!("{}", private);
    }
    #[test]
    fn runtime_content_is_never_formatted() {
        let output = std::process::Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "privacy_log::tests::emit_private_values",
                "--nocapture",
            ])
            .env("SPEEKTOME_LOG_TEST_CHILD", "1")
            .output()
            .unwrap();
        assert!(output.status.success());
        let stderr = String::from_utf8_lossy(&output.stderr);
        assert!(stderr.contains("[PRIVACY TEST]"));
        assert!(!stderr.contains("privacy-sentinel-transcript-command"));
    }
}
