//! Delivers a finished transcript. Dictation is inserted at the cursor;
//! command mode sends the spoken instruction and the selected text to the
//! command model and inserts its answer. Streaming and batch transcription
//! share this path so both behave identically.
use crate::shortcuts::RecordingContext;
use crate::win32::{self, NotificationLevel, PreviewState};

fn typing_mode() -> bool {
    crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .typing_mode
}

fn emit(event: &str, text: String) {
    if let Some(app) = crate::TAURI_APP.get() {
        use tauri::Emitter;
        let _ = app.emit(event, text);
    }
}

pub fn hide_overlay() {
    win32::hide_overlay();
    win32::hide_preview();
}

/// Inserts dictated text by typing or pasting, per the user's setting.
pub fn insert_dictation(text: &str, context: &RecordingContext) {
    println!("[PRIVATE DATA] Result processed; content omitted from logs.");
    if typing_mode() {
        crate::keyboard::simulate_typing(text);
    } else {
        crate::keyboard::paste_text(text);
    }
    crate::api::record_delivered_final(context, text);
    emit("dictation-transcribed", text.to_string());
}

/// The live preview shows the last 32 characters of a streamed answer.
fn preview_tail(accumulated: &str) -> String {
    match accumulated.char_indices().rev().nth(31) {
        Some((start, _)) => format!("✨ ...{}", accumulated[start..].trim_start()),
        None => format!("✨ {}", accumulated),
    }
}

/// Runs a spoken command against the selected text (or on its own when
/// nothing is selected), streaming the answer into the focused application.
pub async fn run_command(
    command: String,
    context: RecordingContext,
    api_url: String,
    token: String,
) {
    println!("[COMMAND MODE] Executing transformation; private content omitted.");
    let target = context.selected_text.clone().unwrap_or_default();
    let max_words = crate::get_transform_word_limit();
    if target.split_whitespace().count() > max_words {
        win32::set_preview_text(&format!("⚠️ Text exceeds {} words", max_words));
        win32::show_tray_notification(
            "SpeekTo.Me — Text Too Long",
            &format!(
                "Text longer than {} words is not supported at the moment.",
                max_words
            ),
            NotificationLevel::Warning,
        );
        tokio::time::sleep(std::time::Duration::from_millis(1500)).await;
        hide_overlay();
        return;
    }
    let has_target = !target.trim().is_empty();
    win32::set_preview_state(PreviewState::Processing);
    win32::set_preview_text(if has_target {
        "✨ Transforming text..."
    } else {
        "✨ Generating..."
    });

    let typing = typing_mode();
    let mut first_chunk = true;
    let mut last_ui_update = std::time::Instant::now();
    let result = crate::api::transform_text_stream(
        &api_url,
        &token,
        &target,
        &command,
        move |delta, accumulated| {
            if delta == "[RESET]" {
                win32::set_preview_text("✨ Retrying with backup AI...");
                first_chunk = true;
                return;
            }
            if first_chunk {
                // Typing mode replaces the selection by deleting it first.
                if has_target && typing {
                    crate::keyboard::simulate_single_key(0x08);
                    std::thread::sleep(std::time::Duration::from_millis(20));
                }
                first_chunk = false;
            }
            if typing {
                crate::keyboard::simulate_typing(delta);
            }
            if last_ui_update.elapsed() >= std::time::Duration::from_millis(80) {
                last_ui_update = std::time::Instant::now();
                win32::set_preview_text(&preview_tail(accumulated));
            }
        },
    )
    .await;

    match result {
        Ok(transformed) => {
            println!(
                "[COMMAND MODE] Transformation completed ({} characters).",
                transformed.len()
            );
            win32::set_preview_text("✨ Done!");
            if typing {
                // Keep the full answer available to paste anywhere.
                crate::keyboard::copy_private(&crate::keyboard::normalize_newlines(&transformed));
            } else {
                crate::keyboard::paste_transformed_text(&transformed);
            }
            let (title, message) = if has_target {
                (
                    "SpeekTo.Me — Transformed Text Ready",
                    "Transformed text replaced selection and is copied to clipboard.",
                )
            } else {
                (
                    "SpeekTo.Me — AI Response Ready",
                    "AI generated text inserted and is copied to clipboard.",
                )
            };
            win32::show_tray_notification(title, message, NotificationLevel::Info);
            crate::api::record_delivered_final(&context, &transformed);
            emit("command-transformed", transformed);
            tokio::time::sleep(std::time::Duration::from_millis(800)).await;
        }
        Err(err) => {
            eprintln!("[TRANSFORM ERROR] {}", err);
            win32::set_preview_text("⚠️ Transformation failed");
            if crate::account_errors::is_limit(&err) {
                crate::account_errors::notify("Command Mode", &err);
            } else {
                win32::show_tray_notification(
                    "SpeekTo.Me — Transformation Failed",
                    &err,
                    NotificationLevel::Warning,
                );
            }
            tokio::time::sleep(std::time::Duration::from_millis(1500)).await;
        }
    }
    hide_overlay();
}

/// Runs a command on its own thread, so the capture session can end while
/// the model answers. The context keeps the recording lease until it is done.
pub fn spawn_command(command: String, context: RecordingContext, api_url: String, token: String) {
    std::thread::spawn(move || {
        match tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
        {
            Ok(runtime) => runtime.block_on(run_command(command, context, api_url, token)),
            Err(_) => hide_overlay(),
        }
    });
}

/// Explains a failed transcription in a notification suited to its cause.
pub fn report_failure(err: &str, prefix: &str) {
    let lower = err.to_lowercase();
    if ["401", "unauthorized", "invalid token", "token expired"]
        .iter()
        .any(|k| lower.contains(k))
    {
        crate::handle_session_expired(Some(
            "Your session has expired. Please sign in again to continue dictating.",
        ));
    } else if ["limit", "quota", "429"].iter().any(|k| lower.contains(k)) {
        win32::show_tray_notification(
            "SpeekTo.Me — Usage Limit Reached",
            "You have reached your monthly transcription limit. Upgrade your plan in Settings.",
            NotificationLevel::Warning,
        );
        if let Some(app) = crate::TAURI_APP.get() {
            crate::show_or_create_settings_window(app);
        }
    } else if [
        "network",
        "timeout",
        "connection refused",
        "dns",
        "websocket connection failed",
    ]
    .iter()
    .any(|k| lower.contains(k))
    {
        win32::show_tray_notification(
            "SpeekTo.Me — Network Error",
            "Could not connect to SpeekTo.Me server. Please check your internet connection.",
            NotificationLevel::Error,
        );
    } else {
        win32::show_tray_notification(
            "SpeekTo.Me — Transcription Error",
            &format!("{}: {}", prefix, err),
            NotificationLevel::Error,
        );
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn preview_shows_the_last_32_characters() {
        assert_eq!(super::preview_tail("short"), "✨ short");
        let long = "क्लाउड कंप्यूटिंग हमारे तकनीक तक पहुँचने और प्रबंधित करने के तरीके में";
        let preview = super::preview_tail(long);
        assert!(preview.starts_with("✨ ..."));
        assert!(preview.chars().count() <= 32 + 5);
    }
}
