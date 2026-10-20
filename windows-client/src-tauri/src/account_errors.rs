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

// Defensive handling for old servers that accidentally return model control
// output inside a successful text response. Never paste the reserved object.
pub fn validate_command_result(text: &str) -> Result<(), String> {
    if text.trim().is_empty() {
        return Err("The AI returned an empty result. Your text was not replaced.".into());
    }
    let t = text
        .trim()
        .strip_prefix("```json")
        .and_then(|s| s.strip_suffix("```"))
        .unwrap_or(text)
        .trim();
    if let Ok(v) = serde_json::from_str::<serde_json::Value>(t) {
        if v.get("speektome_error").is_some() {
            return Err("The AI could not complete this request. Your text was not replaced. Try a clearer instruction.".into());
        }
    }
    Ok(())
}
pub fn notify_warning(message: Option<&str>) {
    if let Some(text) = message.filter(|s| !s.is_empty()) {
        crate::win32::show_tray_notification(
            "SpeekTo.Me — Enhancement Unavailable",
            text,
            crate::win32::NotificationLevel::Warning,
        );
    }
}

#[cfg(test)]
mod model_error_tests {
    use super::*;
    #[test]
    fn control_output_never_becomes_replacement_text() {
        assert!(validate_command_result(r#"{"speektome_error":{"code":"refused"}}"#).is_err());
        assert!(validate_command_result(
            "```json\n{\"speektome_error\":{\"code\":\"missing_input\"}}\n```"
        )
        .is_err());
        assert!(validate_command_result("").is_err());
        assert!(validate_command_result("मुझे खेद है, मैं ऐसा नहीं कर सकता।").is_ok());
        assert!(validate_command_result("I’m sorry, but I can’t comply with that.").is_ok());
        assert_eq!(
            message(
                422,
                r#"{"error":"More information needed","code":"missing_input","stage":"command"}"#
            ),
            "More information needed"
        );
    }
}
