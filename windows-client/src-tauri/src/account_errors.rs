//! Human-readable API failures shared by dictation, Command Mode and meetings.
use std::{
    collections::HashMap,
    sync::{Mutex, OnceLock},
    time::{Duration, Instant},
};
use tauri::Emitter;

pub fn message(status: u16, body: &str) -> String {
    let parsed = serde_json::from_str::<serde_json::Value>(body).ok();
    if let Some(text) = parsed
        .as_ref()
        .and_then(|v| v.get("error"))
        .and_then(|v| v.as_str())
    {
        return text.to_string();
    }
    let text = body.trim();
    if !text.is_empty() {
        return text.chars().take(500).collect();
    }
    match status {
        429 => "Your account has reached a rate limit. Please try again shortly.".into(),
        403 => "Your account cannot perform this action. Check your plan and remaining allowance."
            .into(),
        _ => format!(
            "The server could not complete this request (HTTP {}).",
            status
        ),
    }
}
pub fn is_limit(message: &str) -> bool {
    let text = message.to_lowercase();
    [
        "quota",
        "allowance",
        "rate limit",
        "too many",
        "already has",
        "three meetings",
        "output limit",
        "account limits",
        "meeting limit",
    ]
    .iter()
    .any(|term| text.contains(term))
}
pub fn notify(feature: &str, message: &str) {
    if !is_limit(message) {
        return;
    }
    static NOTICES: OnceLock<Mutex<HashMap<String, Instant>>> = OnceLock::new();
    let key = format!("{}:{}", feature, message);
    let mut notices = NOTICES
        .get_or_init(|| Mutex::new(HashMap::new()))
        .lock()
        .unwrap_or_else(|e| e.into_inner());
    notices.retain(|_, at| at.elapsed() < Duration::from_secs(300));
    if notices.contains_key(&key) {
        return;
    }
    notices.insert(key, Instant::now());
    drop(notices);
    crate::win32::show_tray_notification(
        &format!("SpeekTo.Me — {} Limit", feature),
        message,
        crate::win32::NotificationLevel::Warning,
    );
    if let Some(app) = crate::TAURI_APP.get() {
        let _ = app.emit(
            "account-limit-error",
            serde_json::json!({"feature":feature,"message":message}),
        );
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn structured_errors_preserve_the_actionable_message() {
        let text = message(
            429,
            r#"{"code":"command_output_quota_exhausted","error":"Your monthly Command Mode output-token allowance is exhausted. It resets on 18 Nov 2026.","retry_after_seconds":500}"#,
        );
        assert!(!text.contains("retry_after_seconds"));
        assert!(text.contains("18 Nov 2026"));
        assert!(is_limit(&text));
        assert!(is_limit(&message(429, "")));
        assert_eq!(message(400, "Invalid language"), "Invalid language");
        assert!(!is_limit("Network connection interrupted"));
    }
}
