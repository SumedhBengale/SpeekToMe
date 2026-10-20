use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct UserInfo {
    #[serde(default)] pub id: i64,
    pub name: String,
    pub email: String,
    pub plan_tier: String,
    pub plan_active: bool,
    pub plan_activated_at: String,
    pub duration_seconds: f64,
    pub monthly_limit_seconds: f64,
    pub languages: Option<Vec<String>>,
    pub notification: Option<String>,
    pub transform_input_tokens: Option<usize>,
    #[serde(default)]
    pub command_usage: Option<serde_json::Value>,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct ClientVocabularyTerm {
    #[serde(rename = "canonical_term")]
    pub canonical: String,
    #[serde(rename = "normalized_term")]
    pub normalized: String,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct PromptValidationResult {
    pub safe: bool,
    pub explanation: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct TranscribeResult {
    pub text: String,
    #[serde(default)]
    pub log_id: Option<i64>,
    #[serde(default)]
    pub ack_token: Option<String>,
}

#[derive(Deserialize)]
struct TranscriptResponse {
    text: String,
    #[serde(default)]
    log_id: Option<i64>,
    #[serde(default)]
    ack_token: Option<String>,
}

#[derive(Deserialize)]
struct AuthResponse {
    token: String,
}

fn create_client() -> reqwest::Client {
    let mut builder = reqwest::Client::builder();
    builder = builder.timeout(std::time::Duration::from_secs(120));
    builder.build().unwrap_or_else(|_| reqwest::Client::new())
}

pub fn get_api_url() -> String {
    let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
    config.api_url.clone().unwrap_or_else(|| "https://api.speekto.me".to_string())
}

pub fn open_browser(url: &str) {
    #[cfg(target_os = "windows")]
    {
        use std::os::windows::ffi::OsStrExt;
        let url_wide: Vec<u16> = std::ffi::OsStr::new(url)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let open_wide: Vec<u16> = std::ffi::OsStr::new("open")
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        unsafe {
            windows_sys::Win32::UI::Shell::ShellExecuteW(
                std::ptr::null_mut(),
                open_wide.as_ptr(),
                url_wide.as_ptr(),
                std::ptr::null(),
                std::ptr::null(),
                1,
            );
        }
    }
    #[cfg(target_os = "linux")]
    {
        // Keep the URL as one argument and reject non-web schemes so this
        // helper cannot accidentally hand a command-like payload to a handler.
        if url.starts_with("https://") || url.starts_with("http://") {
            let _ = std::process::Command::new("xdg-open").arg(url).spawn();
        }
    }
    #[cfg(target_os = "macos")]
    {
        if url.starts_with("https://") || url.starts_with("http://") {
            let _ = std::process::Command::new("open").arg(url).spawn();
        }
    }
    #[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
    {
        let _ = url;
    }
}

pub fn listen_for_token() -> Option<String> {
    use std::io::{Read, Write};
    use std::net::TcpListener;
    let listener = TcpListener::bind("127.0.0.1:0").ok()?;
    let port = listener.local_addr().ok()?.port();
    
    let client_state: String = (0..16).map(|_| format!("{:02x}", rand::random::<u8>())).collect();
    let auth_url = format!("{}/auth/login?port={}&client_state={}", get_api_url(), port, client_state);
    open_browser(&auth_url);
    
    listener.set_nonblocking(false).ok();
    if let Ok((mut stream, _)) = listener.accept() {
        let mut buffer = [0; 2048];
        if let Ok(bytes_read) = stream.read(&mut buffer) {
            let request = String::from_utf8_lossy(&buffer[..bytes_read]);
            
            let mut captured_token = None;
            let mut captured_state = None;
            if let Some(path_start) = request.find("GET /callback?") {
                let query_slice = &request[path_start + 14..];
                if let Some(space_pos) = query_slice.find(' ') {
                    let query = &query_slice[..space_pos];
                    for pair in query.split('&') {
                        let mut parts = pair.split('=');
                        if let (Some(k), Some(v)) = (parts.next(), parts.next()) {
                            if k == "token" {
                                captured_token = Some(v.to_string());
                            } else if k == "client_state" {
                                captured_state = Some(v.to_string());
                            }
                        }
                    }
                }
            }

            if let (Some(token), Some(state)) = (captured_token, captured_state) {
                if state == client_state {
                    let body = "<html><body style='font-family:sans-serif;text-align:center;margin-top:50px;'><h2>Authentication Successful</h2><p>You can close this window now and return to SpeekTo.Me.</p></body></html>";
                    let response = format!(
                        "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nContent-Type: text/html; charset=utf-8\r\nConnection: close\r\n\r\n{}",
                        body.len(),
                        body
                    );
                    let _ = stream.write_all(response.as_bytes());
                    let _ = stream.flush();
                    return Some(token);
                }
            }
        }
    }
    None
}

pub fn process_and_paste(samples: Vec<i16>, sample_rate: u32, channels: u16, language: String, context: crate::shortcuts::RecordingContext) {
    let is_cmd = context.is_command_mode;
    let (token_opt, model, enhance, custom_prompt, transcription_mode, api_url) = {
        let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        (
            context.history_identity().1.map(str::to_owned),
            config.model.clone(),
            if is_cmd { false } else { config.enhance },
            config.custom_prompt.clone(),
            config.transcription_mode.clone(),
            context.history_identity().2.to_string(),
        )
    };

    let token = match token_opt {
        Some(t) => t,
        None => {
            println!("No session token available.");
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
            crate::win32::show_tray_notification(
                "SpeekTo.Me — Sign In Required",
                "Please sign in to your SpeekTo.Me account in Settings to start voice dictation.",
                crate::win32::NotificationLevel::Warning,
            );
            if let Some(app) = crate::TAURI_APP.get() {
                crate::show_or_create_settings_window(app);
            }
            return;
        }
    };

    let ogg_opus_bytes = match crate::audio::encode_opus_ogg(&samples, sample_rate, channels) {
        Ok(b) => b,
        Err(e) => {
            eprintln!("Failed to encode Opus audio: {:?}", e);
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
            crate::win32::show_tray_notification(
                "SpeekTo.Me — Audio Error",
                "Failed to encode microphone audio for transcription.",
                crate::win32::NotificationLevel::Error,
            );
            return;
        }
    };

    tauri::async_runtime::spawn(async move {
        let context = context; // Keep the session lease through the complete async operation.
        let transcript_result = if transcription_mode == "stream" {
            match stream_transcribe_audio(&api_url, &token, ogg_opus_bytes.clone(), enhance, &custom_prompt, &language).await {
                Ok(res) => Ok(res),
                Err(err) => {
                    println!("[STREAM WARNING] Streaming transcription failed ({}), falling back to batch API...", err);
                    transcribe_audio(&api_url, &token, ogg_opus_bytes, &model, enhance, &custom_prompt, &language).await
                }
            }
        } else {
            transcribe_audio(&api_url, &token, ogg_opus_bytes, &model, enhance, &custom_prompt, &language).await
        };

        match transcript_result {
            Ok(result) => {
                let text = result.text.trim();
                if !text.is_empty() {
                    crate::win32::set_preview_text(text);

                    let is_command_mode = context.is_command_mode;
                    let selected_text = context.selected_text.clone();

                    if is_command_mode {
                        println!("\n=======================================================");
                        println!("✨ [COMMAND MODE COMPLETED — EXECUTING LLM TRANSFORMATION]");
                        println!("[PRIVATE DATA] Result processed; content omitted from logs.");
                        println!("-------------------------------------------------------");
                        println!("[PRIVATE DATA] Result processed; content omitted from logs.");
                        println!("=======================================================\n");

                        // Check dynamic word limit
                        let max_words = crate::get_transform_word_limit();
                        if let Some(ref target) = selected_text {
                            let word_count = target.split_whitespace().count();
                            if word_count > max_words {
                                println!("[COMMAND MODE WARNING] Selected text exceeds {} words ({} words).", max_words, word_count);
                                crate::win32::set_preview_text(&format!("⚠️ Text exceeds {} words", max_words));
                                crate::win32::show_tray_notification(
                                    "SpeekTo.Me — Text Too Long",
                                    &format!("Text longer than {} words is not supported at the moment.", max_words),
                                    crate::win32::NotificationLevel::Warning,
                                );
                                return;
                            }
                        }

                        let target_text = selected_text.unwrap_or_default();
                        let has_target = !target_text.trim().is_empty();

                        crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
                        if has_target {
                            crate::win32::set_preview_text("✨ Transforming text...");
                        } else {
                            crate::win32::set_preview_text("✨ Generating...");
                        }

                        let typing_mode = {
                            let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                            config.typing_mode
                        };

                        let mut is_first_chunk = true;
                        let mut last_ui_update = std::time::Instant::now();

                        let res = transform_text_stream(&api_url, &token, &target_text, text, move |delta, accumulated| {
                            if delta == "[RESET]" {
                                println!("[STREAM] Received [RESET], resetting stream UI preview");
                                crate::win32::set_preview_text("✨ Retrying with backup AI...");
                                is_first_chunk = true;
                                return;
                            }

                            if is_first_chunk {
                                if has_target && typing_mode {
                                    crate::keyboard::simulate_single_key(0x08);
                                    std::thread::sleep(std::time::Duration::from_millis(20));
                                }
                                is_first_chunk = false;
                            }

                            if typing_mode {
                                crate::keyboard::simulate_typing(delta);
                            }

                            if last_ui_update.elapsed() >= std::time::Duration::from_millis(80) {
                                last_ui_update = std::time::Instant::now();
                                let preview_display = if let Some((start_byte, _)) = accumulated.char_indices().rev().nth(31) {
                                    let tail = &accumulated[start_byte..];
                                    format!("✨ ...{}", tail.trim_start())
                                } else {
                                    format!("✨ {}", accumulated)
                                };
                                crate::win32::set_preview_text(&preview_display);
                            }
                        }).await;

                        match res {
                            Ok(transformed) => {
                                println!("\n=======================================================");
                                println!("✨ [COMMAND MODE — LLM TRANSFORMATION COMPLETED]");
                                println!("Transformed Output ({} characters):", transformed.len());
                                println!("-------------------------------------------------------");
                                println!("[PRIVATE DATA] Result processed; content omitted from logs.");
                                println!("=======================================================\n");

                                crate::win32::set_preview_text("✨ Done!");

                                if !typing_mode {
                                    crate::keyboard::paste_transformed_text(&transformed);
                                } else {
                                    // Permanently retain full transformed text in clipboard as a fallback
                                    if let Ok(mut ctx) = arboard::Clipboard::new() {
                                        let _ = ctx.set_text(crate::keyboard::normalize_newlines(&transformed));
                                    }
                                }

                                let (notify_title, notify_msg) = if has_target {
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

                                crate::win32::show_tray_notification(
                                    notify_title,
                                    notify_msg,
                                    crate::win32::NotificationLevel::Info,
                                );

                                record_delivered_final(&context, &transformed);
                                if let Some(app) = crate::TAURI_APP.get() {
                                    use tauri::Emitter;
                                    let _ = app.emit("command-transformed", transformed);
                                }
                            }
                            Err(err) => {
                                eprintln!("[TRANSFORM ERROR] {}", err);
                                crate::win32::set_preview_text("⚠️ Transformation failed");
                                if crate::account_errors::is_limit(&err) {
                                    crate::account_errors::notify("Command Mode", &err);
                                } else {
                                    crate::win32::show_tray_notification(
                                        "SpeekTo.Me — Transformation Failed", &err,
                                        crate::win32::NotificationLevel::Warning,
                                    );
                                }
                            }
                        }
                    } else {
                        println!("[PRIVATE DATA] Result processed; content omitted from logs.");
                        let typing_mode = {
                            let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                            config.typing_mode
                        };

                        if typing_mode {
                            crate::keyboard::simulate_typing(text);
                        } else {
                            crate::keyboard::paste_text(text);
                        }

                        record_delivered_final(&context, text);
                        if let Some(app) = crate::TAURI_APP.get() {
                            use tauri::Emitter;
                            let _ = app.emit("dictation-transcribed", text.to_string());
                        }
                    }
                }
                crate::win32::hide_overlay();
                crate::win32::hide_preview();
            }
            Err(err) => {
                eprintln!("Transcription failed: {}", err);
                crate::win32::hide_overlay();
                crate::win32::hide_preview();

                let err_lower = err.to_lowercase();
                if err_lower.contains("401") || err_lower.contains("unauthorized") || err_lower.contains("invalid token") || err_lower.contains("token expired") {
                    crate::handle_session_expired(Some("Your session has expired. Please sign in again to continue dictating."));
                } else if err_lower.contains("limit") || err_lower.contains("quota") || err_lower.contains("429") {
                    crate::win32::show_tray_notification(
                        "SpeekTo.Me — Usage Limit Reached",
                        "You have reached your monthly transcription limit. Upgrade your plan in Settings.",
                        crate::win32::NotificationLevel::Warning,
                    );
                    if let Some(app) = crate::TAURI_APP.get() {
                        crate::show_or_create_settings_window(app);
                    }
                } else if err_lower.contains("network") || err_lower.contains("timeout") || err_lower.contains("connection refused") || err_lower.contains("dns") {
                    crate::win32::show_tray_notification(
                        "SpeekTo.Me — Network Error",
                        "Could not connect to SpeekTo.Me server. Please check your internet connection.",
                        crate::win32::NotificationLevel::Error,
                    );
                } else {
                    crate::win32::show_tray_notification(
                        "SpeekTo.Me — Transcription Error",
                        &format!("Transcription failed: {}", err),
                        crate::win32::NotificationLevel::Error,
                    );
                }
            }
        }
    });
}

pub async fn stream_transcribe_audio(
    api_url: &str,
    token: &str,
    ogg_opus_bytes: Vec<u8>,
    enhance: bool,
    custom_prompt: &str,
    language: &str,
) -> Result<TranscribeResult, String> {
    use futures_util::{SinkExt, StreamExt};
    use tokio_tungstenite::tungstenite::Message;
    use tokio_tungstenite::connect_async;

    let ws_url = crate::stream::get_stream_ws_url(api_url, token);
    let (ws_stream, _) = connect_async(&ws_url).await
        .map_err(|e| format!("WebSocket stream connection error: {}", e))?;

    let (mut write, mut read) = ws_stream.split();

    let start_payload = serde_json::json!({
        "type": "start",
        "format": "opus",
        "sample_rate": 16000,
        "channels": 1,
        "enhance": enhance,
        "prompt": custom_prompt,
        "language": language,
    });

    write.send(Message::Text(start_payload.to_string())).await
        .map_err(|e| format!("Failed to send stream handshake: {}", e))?;

    for chunk in ogg_opus_bytes.chunks(4096) {
        write.send(Message::Binary(chunk.to_vec())).await
            .map_err(|e| format!("Failed to send audio chunk: {}", e))?;
    }

    write.send(Message::Text(r#"{"type":"end_of_stream"}"#.to_string())).await
        .map_err(|e| format!("Failed to send end_of_stream: {}", e))?;

    while let Some(msg_res) = read.next().await {
        match msg_res {
            Ok(Message::Text(text)) => {
                if let Ok(event) = serde_json::from_str::<crate::stream::StreamEventResponse>(&text) {
                    if event.event_type == "final" {
                        crate::win32::set_preview_text(&event.text);
                        return Ok(TranscribeResult {
                            text: event.text,
                            log_id: event.log_id,
                            ack_token: event.ack_token,
                        });
                    } else if event.event_type == "interim" && !event.text.is_empty() {
                        crate::win32::set_preview_text(&event.text);
                    } else if event.event_type == "error" {
                        return Err(event.error.unwrap_or_else(|| "Server streaming transcription error".to_string()));
                    }
                }
            }
            Ok(Message::Close(_)) => break,
            Err(e) => return Err(format!("Stream read error: {}", e)),
            _ => {}
        }
    }

    Err("Stream closed before receiving final transcription".to_string())
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct ClientConfig {
    pub transform_input_tokens: Option<usize>,
    pub notification: Option<String>,
}

pub async fn fetch_client_config(api_url: &str) -> Result<ClientConfig, String> {
    let client = create_client();
    let url = format!("{}/v1/config", api_url.trim_end_matches('/'));
    let res = client.get(&url).send().await.map_err(|e| e.to_string())?;
    if res.status().is_success() {
        if let Ok(cfg) = res.json::<ClientConfig>().await {
            if let Some(limit) = cfg.transform_input_tokens {
                crate::set_transform_word_limit(limit);
                println!("[CONFIG SYNC] Dynamic transform input word limit set from server: {}", limit);
            }
            return Ok(cfg);
        }
    }
    Err("Failed to fetch client config".to_string())
}

pub async fn fetch_user_info(api_url: &str, token: &str) -> Result<UserInfo, String> {
    let client = create_client();
    let res = client.get(format!("{}/auth/me", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    if status.is_success() {
        let info = res.json::<UserInfo>().await.map_err(|e| format!("Failed to parse user info: {}", e))?;
 crate::meeting_upload::cache_account(token,&info.email,info.id);
        if let Some(limit) = info.transform_input_tokens {
            crate::set_transform_word_limit(limit);
            println!("[CONFIG SYNC] Dynamic transform input word limit updated from user info: {}", limit);
        }
        Ok(info)
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        let err_text = res.text().await.unwrap_or_default();
        Err(format!("Server error: {}", err_text))
    }
}

pub async fn login_with_credentials(api_url: &str, email: &str, password: &str) -> Result<(String, UserInfo), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);

    let res = client.post(format!("{}/auth/login", api_url))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&body_text) {
            if v.get("require_2fa").and_then(|b| b.as_bool()).unwrap_or(false) {
                return Err("This account is not allowed to login.".to_string());
            }
        }
        let auth_res: AuthResponse = serde_json::from_str(&body_text)
            .map_err(|e| format!("Failed to parse login response: {}", e))?;
        let user_info = fetch_user_info(api_url, &auth_res.token).await?;
        Ok((auth_res.token, user_info))
    } else {
        let body_trimmed = body_text.trim().to_string();
        if !body_trimmed.is_empty() {
            Err(body_trimmed)
        } else {
            Err(format!("Login failed with status: {}", status))
        }
    }
}

pub async fn signup_with_credentials(api_url: &str, email: &str, password: &str, name: &str) -> Result<String, String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);
    map.insert("name", name);

    let res = client.post(format!("{}/auth/signup", api_url))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        if let Ok(json_res) = serde_json::from_str::<serde_json::Value>(&body_text) {
            if let Some(msg) = json_res.get("message").and_then(|v| v.as_str()) {
                return Ok(msg.to_string());
            }
        }
        Ok("Account created successfully.".to_string())
    } else {
        let body_trimmed = body_text.trim().to_string();
        if !body_trimmed.is_empty() {
            Err(body_trimmed)
        } else {
            Err(format!("Signup failed with status: {}", status))
        }
    }
}

pub async fn fetch_vocabulary(api_url: &str, token: &str) -> Result<Vec<ClientVocabularyTerm>, String> {
    let client = create_client();
    let res = client.get(format!("{}/auth/vocabulary", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        serde_json::from_str::<Vec<ClientVocabularyTerm>>(&body_text)
            .map_err(|e| format!("Failed to parse vocabulary list: {}", e))
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        Err(format!("Failed to retrieve vocabulary: {}", body_text))
    }
}

pub async fn add_vocab_term(api_url: &str, token: &str, term: &str) -> Result<(), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    let res = client.post(format!("{}/auth/vocabulary", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        Ok(())
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        Err(format!("Failed to add vocabulary term: {}", body_text))
    }
}

pub async fn delete_vocab_term(api_url: &str, token: &str, term: &str) -> Result<(), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    let res = client.delete(format!("{}/auth/vocabulary", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        Ok(())
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        Err(format!("Failed to delete vocabulary term: {}", body_text))
    }
}

pub async fn validate_custom_prompt(api_url: &str, token: &str, prompt: &str) -> Result<PromptValidationResult, String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("prompt", prompt);

    let res = client.post(format!("{}/validate-prompt", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        serde_json::from_str::<PromptValidationResult>(&body_text)
            .map_err(|e| format!("Failed to parse validation result: {}", e))
    } else {
        Err(format!("Prompt validation failed: {}", body_text))
    }
}

pub async fn transcribe_audio(
    api_url: &str,
    token: &str,
    ogg_opus_bytes: Vec<u8>,
    model: &str,
    enhance: bool,
    custom_prompt: &str,
    language: &str,
) -> Result<TranscribeResult, String> {
    let client = create_client();
    let part = reqwest::multipart::Part::bytes(ogg_opus_bytes)
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();

    let form = reqwest::multipart::Form::new()
        .part("file", part)
        .text("language", language.to_string())
        .text("model", model.to_string())
        .text("enhance", enhance.to_string())
        .text("prompt", custom_prompt.to_string());

    let res = client.post(format!("{}/generate", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        let resp: TranscriptResponse = serde_json::from_str(&body_text)
            .map_err(|e| format!("Failed to parse response: {}", e))?;
        Ok(TranscribeResult {
            text: resp.text,
            log_id: resp.log_id,
            ack_token: resp.ack_token,
        })
    } else {
        let body_trimmed = body_text.trim().to_string();
        if status.as_u16() == 401 {
            Err("401 Unauthorized: Session token is invalid or expired.".to_string())
        } else if status.as_u16() == 429 {
            Err(crate::account_errors::message(status.as_u16(), &body_text))
        } else if !body_trimmed.is_empty() {
            Err(body_trimmed)
        } else {
            Err(format!("Server error with HTTP status {}", status))
        }
    }
}

#[derive(Serialize)]
struct TransformPayload<'a> {
    selected_text: &'a str,
    command: &'a str,
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    stream: bool,
}

#[derive(Deserialize)]
#[allow(dead_code)]
struct TransformResponse {
    text: String,
    #[serde(default)]
    model: Option<String>,
    #[serde(default)]
    provider: Option<String>,
}

#[derive(Deserialize, Debug)]
#[allow(dead_code)]
struct StreamChunk {
    #[serde(default)]
    delta: Option<String>,
    #[serde(default)]
    text: Option<String>,
    #[serde(default)]
    done: Option<bool>,
    #[serde(default)]
    error: Option<String>,
    #[serde(default)]
    reset: Option<bool>,
}

pub async fn transform_text_stream<F>(
    api_url: &str,
    token: &str,
    selected_text: &str,
    command: &str,
    mut on_chunk: F,
) -> Result<String, String>
where
    F: FnMut(&str, &str) + Send,
{
    use futures_util::StreamExt;

    let client = create_client();
    let url = format!("{}/v1/transform", api_url.trim_end_matches('/'));

    let payload = TransformPayload {
        selected_text,
        command,
        stream: true,
    };

    let res = client
        .post(&url)
        .header("Authorization", format!("Bearer {}", token))
        .header("Content-Type", "application/json")
        .header("Accept", "text/event-stream")
        .header("User-Agent", "SpeekToMe-WindowsClient/0.1.0")
        .json(&payload)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    if !status.is_success() {
        let err_text = res.text().await.unwrap_or_default();
        let message = crate::account_errors::message(status.as_u16(), &err_text);
        return Err(message);
    }

    let is_event_stream = res.headers()
        .get("content-type")
        .and_then(|h| h.to_str().ok())
        .map(|ct| ct.contains("text/event-stream"))
        .unwrap_or(false);

    if !is_event_stream {
        let body_text = res.text().await.unwrap_or_default();
        let resp: TransformResponse = serde_json::from_str(&body_text)
            .map_err(|e| format!("Failed to parse response: {}", e))?;
        on_chunk(&resp.text, &resp.text);
        return Ok(resp.text);
    }

    let mut stream = res.bytes_stream();
    let mut buffer = String::new();
    let mut accumulated = String::new();

    while let Some(chunk_res) = stream.next().await {
        let bytes = chunk_res.map_err(|e| format!("Stream error: {}", e))?;
        let text_chunk = String::from_utf8_lossy(&bytes);
        buffer.push_str(&text_chunk);

        while let Some(newline_pos) = buffer.find('\n') {
            let line = buffer[..newline_pos].trim().to_string();
            buffer = buffer[newline_pos + 1..].to_string();

            if line.starts_with("data: ") {
                let data = line.trim_start_matches("data: ").trim();
                if data == "[DONE]" {
                    break;
                }

                if let Ok(parsed) = serde_json::from_str::<StreamChunk>(data) {
                    if let Some(ref err_msg) = parsed.error {
                        return Err(err_msg.clone());
                    }

                    if parsed.reset.unwrap_or(false) {
                        println!("[STREAM RESET] Server signaled stream reset due to provider fallback.");
                        accumulated.clear();
                        on_chunk("[RESET]", "");
                        continue;
                    }

                    if let Some(ref delta) = parsed.delta {
                        if !delta.is_empty() {
                            accumulated.push_str(delta);
                            on_chunk(delta, &accumulated);
                        }
                    }

                    if parsed.done.unwrap_or(false) {
                        if let Some(ref full) = parsed.text {
                            if !full.is_empty() {
                                accumulated = full.clone();
                            }
                        }
                    } else if let Some(ref full) = parsed.text {
                        if !full.is_empty() && accumulated.is_empty() {
                            accumulated = full.clone();
                            on_chunk(&accumulated, &accumulated);
                        }
                    }
                }
            }
        }
    }

    if accumulated.is_empty() && !buffer.trim().is_empty() {
        let data = buffer.trim().trim_start_matches("data: ").trim();
        if let Ok(parsed) = serde_json::from_str::<StreamChunk>(data) {
            if let Some(ref delta) = parsed.delta {
                accumulated.push_str(delta);
                on_chunk(delta, &accumulated);
            }
        }
    }

    Ok(accumulated)
}

pub async fn transform_text(
    api_url: &str,
    token: &str,
    selected_text: &str,
    command: &str,
) -> Result<String, String> {
    transform_text_stream(api_url, token, selected_text, command, |_, _| {}).await
}

pub async fn send_transcribe_ack(
    api_url: &str,
    token: &str,
    log_id: i64,
    ack_token: &str,
    final_output: &str,
) -> Result<(), String> {
    let client = create_client();
    let payload = serde_json::json!({
        "log_id": log_id,
        "ack_token": ack_token,
        "final_output": final_output,
    });

    let res = client
        .post(format!("{}/v1/transcribe/ack", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&payload)
        .send()
        .await
        .map_err(|e| format!("Network error sending transcribe ack: {}", e))?;

    let status = res.status();
    if status.is_success() {
        Ok(())
    } else {
        let err_text = res.text().await.unwrap_or_default();
        Err(format!("Transcribe ack failed ({}): {}", status, err_text))
    }
}

pub async fn upload_meeting(
    api_url: &str,
    token: &str,
    title: &str,
    language: &str,
    mixed_bytes: Vec<u8>,
    mic_bytes: Option<Vec<u8>>,
    loopback_bytes: Option<Vec<u8>>,
    energy_json: Option<String>,
) -> Result<String, String> {
    let client = create_client();
    let mixed_part = reqwest::multipart::Part::bytes(mixed_bytes)
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();

    let mut form = reqwest::multipart::Form::new()
        .part("file", mixed_part)
        .text("title", title.to_string())
        .text("language", language.to_string());

    if let Some(mic) = mic_bytes {
        let mic_part = reqwest::multipart::Part::bytes(mic)
            .file_name("recording_mic.ogg")
            .mime_str("audio/ogg")
            .unwrap();
        form = form.part("file_mic", mic_part);
    }

    if let Some(loopback) = loopback_bytes {
        let lb_part = reqwest::multipart::Part::bytes(loopback)
            .file_name("recording_loopback.ogg")
            .mime_str("audio/ogg")
            .unwrap();
        form = form.part("file_loopback", lb_part);
    }

    if let Some(energy) = energy_json {
        form = form.text("energy_json", energy);
    }

    let res = client.post(format!("{}/meetings", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        Ok(body_text)
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        Err(format!("Upload failed with status {}: {}", status, body_text))
    }
}

pub async fn update_user_languages(api_url: &str, token: &str, languages: Vec<String>) -> Result<Vec<String>, String> {
    let client = create_client();
    let mut payload = std::collections::HashMap::new();
    payload.insert("languages", languages);

    let res = client.post(format!("{}/auth/languages", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&payload)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    if status.is_success() {
        #[derive(Deserialize)]
        struct Resp {
            languages: Vec<String>,
        }
        let parsed = res.json::<Resp>().await.map_err(|e| format!("Failed to parse response: {}", e))?;
        Ok(parsed.languages)
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        let err_text = res.text().await.unwrap_or_default();
        Err(format!("Server error: {}", err_text))
    }
}

// Persist only the actual delivered result. Bind retries to the recording's
// original account and backend; never use a subsequently switched session.
pub fn record_delivered_final(context: &crate::shortcuts::RecordingContext, text: &str) {
    if text.trim().is_empty() { return; }
    let (id, token, base) = context.history_identity();
    let Some(token) = token else { return; };
    let (id, token, base, text) = (id.to_string(),token.to_string(),base.to_string(),text.to_string());
    let mode = if context.is_command_mode { "command" } else { "dictation" };
    tauri::async_runtime::spawn(async move {
        let client=reqwest::Client::builder().timeout(std::time::Duration::from_secs(15)).build().unwrap_or_default();
        for attempt in 0..3 {
            if attempt>0 {tokio::time::sleep(std::time::Duration::from_secs(2*attempt)).await;}
            let result=client.post(format!("{}/v1/transcriptions/final",base)).bearer_auth(&token)
                .json(&serde_json::json!({"request_id":id,"text":text,"mode":mode})).send().await;
            match result {
                Ok(response) if response.status().is_success()=>return,
                Ok(response) if response.status().is_client_error() && response.status().as_u16()!=429=>break,
                _=>{}
            }
        }
        crate::win32::show_tray_notification("SpeekTo.Me — History not saved", "Your text was delivered, but its encrypted history could not be saved. Check your connection.",crate::win32::NotificationLevel::Warning);
    });
}
