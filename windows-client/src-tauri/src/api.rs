use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct UserInfo {
    #[serde(default)]
    pub id: i64,
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
    #[serde(default)]
    warning: Option<String>,
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

fn build_client(timeout: std::time::Duration) -> reqwest::Client {
    reqwest::Client::builder()
        .connect_timeout(std::time::Duration::from_secs(10))
        .timeout(timeout)
        .build()
        .unwrap_or_else(|_| reqwest::Client::new())
}

/// Shared client for short API calls. Clones share one connection pool.
pub fn http() -> reqwest::Client {
    static CLIENT: std::sync::OnceLock<reqwest::Client> = std::sync::OnceLock::new();
    CLIENT
        .get_or_init(|| build_client(std::time::Duration::from_secs(30)))
        .clone()
}

/// Shared client for transcription and other calls that wait on models.
fn create_client() -> reqwest::Client {
    static CLIENT: std::sync::OnceLock<reqwest::Client> = std::sync::OnceLock::new();
    CLIENT
        .get_or_init(|| build_client(std::time::Duration::from_secs(120)))
        .clone()
}

#[derive(Serialize)]
pub struct ApiResponse {
    pub status: u16,
    pub body: String,
}

/// Whether the webview may make this request. Each entry is a method and a
/// path pattern where `*` matches one non-empty segment.
fn webview_request_allowed(method: &str, path: &str) -> bool {
    const ALLOWED: &[(&str, &str)] = &[
        ("GET", "/meetings"),
        ("GET", "/meetings/*"),
        ("DELETE", "/meetings/*"),
        ("PUT", "/meetings/*/speakers"),
        ("POST", "/v1/meetings/jobs/*/retry"),
        ("GET", "/config/promo"),
    ];
    let segments: Vec<&str> = path.split('/').collect();
    let safe = |segment: &str| {
        !segment.is_empty()
            && segment
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_')
    };
    ALLOWED.iter().any(|(allowed_method, pattern)| {
        let expected: Vec<&str> = pattern.split('/').collect();
        *allowed_method == method
            && expected.len() == segments.len()
            && expected.iter().zip(&segments).all(|(want, got)| {
                if *want == "*" {
                    safe(got)
                } else {
                    want == got
                }
            })
    })
}

/// Makes an API request on the webview's behalf, adding the session token
/// here so it never crosses into JavaScript.
#[tauri::command]
pub async fn api_request(
    method: String,
    path: String,
    body: Option<serde_json::Value>,
) -> Result<ApiResponse, String> {
    if !webview_request_allowed(&method, &path) {
        return Err("Request not permitted".into());
    }
    let token = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .session_token
        .clone();
    let url = format!("{}{}", get_api_url(), path);
    let mut request = match method.as_str() {
        "GET" => http().get(&url),
        "DELETE" => http().delete(&url),
        "PUT" => http().put(&url),
        _ => http().post(&url),
    };
    if path != "/config/promo" {
        request = request.bearer_auth(token.as_deref().ok_or("Sign in to continue")?);
    }
    if let Some(body) = body {
        request = request.json(&body);
    }
    let response = request
        .send()
        .await
        .map_err(|_| "Network unavailable. Check your connection.".to_string())?;
    let status = response.status().as_u16();
    let body = response.text().await.unwrap_or_default();
    // A response for an account the user has since left must not reach the UI.
    if token.is_some()
        && crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .session_token
            != token
    {
        return Err("Account changed".into());
    }
    Ok(ApiResponse { status, body })
}

pub fn get_api_url() -> String {
    let config = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner());
    config
        .api_url
        .clone()
        .unwrap_or_else(|| "https://api.speekto.me".to_string())
}

pub fn open_browser(url: &str) {
    // Only web pages: a file path or other scheme could launch a program.
    if !(url.starts_with("https://") || url.starts_with("http://")) {
        return;
    }
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

static LOGIN_ATTEMPT: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
pub fn begin_login() -> u64 {
    LOGIN_ATTEMPT.fetch_add(1, std::sync::atomic::Ordering::SeqCst) + 1
}
pub fn cancel_login() {
    LOGIN_ATTEMPT.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
}
pub fn login_is_current(attempt: u64) -> bool {
    LOGIN_ATTEMPT.load(std::sync::atomic::Ordering::SeqCst) == attempt
}
pub fn listen_for_token(attempt: u64) -> Option<String> {
    use std::io::{Read, Write};
    use std::net::TcpListener;
    if !login_is_current(attempt) {
        return None;
    }
    let listener = TcpListener::bind("127.0.0.1:0").ok()?;
    let port = listener.local_addr().ok()?.port();

    let client_state: String = (0..16)
        .map(|_| format!("{:02x}", rand::random::<u8>()))
        .collect();
    let auth_url = format!(
        "{}/auth/login?port={}&client_state={}&exchange=1",
        get_api_url(),
        port,
        client_state
    );
    open_browser(&auth_url);

    listener.set_nonblocking(true).ok()?;
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(180);
    let (mut stream, _) = loop {
        if !login_is_current(attempt) || std::time::Instant::now() >= deadline {
            return None;
        }
        match listener.accept() {
            Ok(connection) => break connection,
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                std::thread::sleep(std::time::Duration::from_millis(50))
            }
            Err(_) => return None,
        }
    };
    stream
        .set_read_timeout(Some(std::time::Duration::from_secs(5)))
        .ok()?;
    stream
        .set_write_timeout(Some(std::time::Duration::from_secs(5)))
        .ok()?;
    {
        let mut buffer = [0; 2048];
        if let Ok(bytes_read) = stream.read(&mut buffer) {
            let request = String::from_utf8_lossy(&buffer[..bytes_read]);

            let mut captured_token = None;
            let mut exchange_code = None;
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
                            } else if k == "code" {
                                exchange_code = Some(v.to_string());
                            } else if k == "client_state" {
                                captured_state = Some(v.to_string());
                            }
                        }
                    }
                }
            }

            if let Some(code) = exchange_code {
                if captured_state.as_deref() == Some(client_state.as_str()) {
                    if let Ok(response) = reqwest::blocking::Client::builder()
                        .timeout(std::time::Duration::from_secs(15))
                        .build()
                        .ok()?
                        .post(format!("{}/auth/exchange", get_api_url()))
                        .json(&serde_json::json!({"code":code}))
                        .send()
                    {
                        if response.status().is_success() {
                            if let Ok(body) = response.json::<serde_json::Value>() {
                                captured_token = body
                                    .get("token")
                                    .and_then(|v| v.as_str())
                                    .map(str::to_owned);
                            }
                        }
                    }
                }
            }
            if let (Some(token), Some(state)) = (captured_token, captured_state) {
                if state == client_state && login_is_current(attempt) {
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
pub fn process_and_paste(
    samples: Vec<i16>,
    sample_rate: u32,
    channels: u16,
    language: String,
    context: crate::shortcuts::RecordingContext,
) {
    let (token_opt, custom_prompt, transcription_mode, api_url) = {
        let config = crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        (
            context.history_identity().1.map(str::to_owned),
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
            match stream_transcribe_audio(
                &api_url,
                &token,
                ogg_opus_bytes.clone(),
                &custom_prompt,
                &language,
            )
            .await
            {
                Ok(res) => Ok(res),
                Err(err) => {
                    println!("[STREAM WARNING] Streaming transcription failed ({}), falling back to batch API...", err);
                    transcribe_audio(&api_url, &token, ogg_opus_bytes, &custom_prompt, &language)
                        .await
                }
            }
        } else {
            transcribe_audio(&api_url, &token, ogg_opus_bytes, &custom_prompt, &language).await
        };

        match transcript_result {
            Ok(result) => {
                let text = result.text.trim().to_string();
                if !text.is_empty() {
                    crate::win32::set_preview_final_text(&text);
                    if context.is_command_mode {
                        crate::delivery::run_command(text, context, api_url, token).await;
                        return;
                    }
                    crate::delivery::insert_dictation(&text, &context);
                }
                crate::delivery::hide_overlay();
            }
            Err(err) => {
                eprintln!("Transcription failed: {}", err);
                crate::delivery::hide_overlay();
                crate::delivery::report_failure(&err, "Transcription failed");
            }
        }
    });
}

pub async fn stream_transcribe_audio(
    api_url: &str,
    token: &str,
    ogg_opus_bytes: Vec<u8>,
    custom_prompt: &str,
    language: &str,
) -> Result<TranscribeResult, String> {
    use futures_util::{SinkExt, StreamExt};
    use tokio_tungstenite::connect_async;
    use tokio_tungstenite::tungstenite::Message;

    let request = crate::stream::authorized_ws_request(api_url, "/v1/stream", token)?;
    let (ws_stream, _) = connect_async(request)
        .await
        .map_err(|e| format!("WebSocket stream connection error: {}", e))?;

    let (mut write, mut read) = ws_stream.split();

    let start_payload = serde_json::json!({
        "type": "start",
        "format": "opus",
        "sample_rate": 16000,
        "channels": 1,
        "prompt": custom_prompt,
        "language": language,
    });

    write
        .send(Message::Text(start_payload.to_string()))
        .await
        .map_err(|e| format!("Failed to send stream handshake: {}", e))?;

    for chunk in ogg_opus_bytes.chunks(4096) {
        write
            .send(Message::Binary(chunk.to_vec()))
            .await
            .map_err(|e| format!("Failed to send audio chunk: {}", e))?;
    }

    write
        .send(Message::Text(r#"{"type":"end_of_stream"}"#.to_string()))
        .await
        .map_err(|e| format!("Failed to send end_of_stream: {}", e))?;

    while let Some(msg_res) = read.next().await {
        match msg_res {
            Ok(Message::Text(text)) => {
                if let Ok(event) = serde_json::from_str::<crate::stream::StreamEventResponse>(&text)
                {
                    if event.event_type == "final" {
                        crate::account_errors::notify_warning(event.warning.as_deref());
                        crate::win32::set_preview_final_text(&event.text);
                        return Ok(TranscribeResult {
                            text: event.text,
                            log_id: event.log_id,
                            ack_token: event.ack_token,
                        });
                    } else if event.event_type == "interim" && !event.text.is_empty() {
                        event.show_interim();
                    } else if event.event_type == "error" {
                        return Err(event.error.unwrap_or_else(|| {
                            "Server streaming transcription error".to_string()
                        }));
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
                println!(
                    "[CONFIG SYNC] Dynamic transform input word limit set from server: {}",
                    limit
                );
            }
            return Ok(cfg);
        }
    }
    Err("Failed to fetch client config".to_string())
}

pub async fn fetch_user_info(api_url: &str, token: &str) -> Result<UserInfo, String> {
    let client = create_client();
    let res = client
        .get(format!("{}/auth/me", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    if status.is_success() {
        let info = res
            .json::<UserInfo>()
            .await
            .map_err(|e| format!("Failed to parse user info: {}", e))?;
        crate::meeting_upload::cache_account(token, &info.email, info.id);
        if let Some(limit) = info.transform_input_tokens {
            crate::set_transform_word_limit(limit);
            println!(
                "[CONFIG SYNC] Dynamic transform input word limit updated from user info: {}",
                limit
            );
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

pub async fn login_with_credentials(
    api_url: &str,
    email: &str,
    password: &str,
) -> Result<(String, UserInfo), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);

    let res = client
        .post(format!("{}/auth/login", api_url))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&body_text) {
            if v.get("require_2fa")
                .and_then(|b| b.as_bool())
                .unwrap_or(false)
            {
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

pub async fn signup_with_credentials(
    api_url: &str,
    email: &str,
    password: &str,
    name: &str,
) -> Result<String, String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);
    map.insert("name", name);

    let res = client
        .post(format!("{}/auth/signup", api_url))
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

pub async fn fetch_vocabulary(
    api_url: &str,
    token: &str,
) -> Result<Vec<ClientVocabularyTerm>, String> {
    let client = create_client();
    let res = client
        .get(format!("{}/auth/vocabulary", api_url))
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

    let res = client
        .post(format!("{}/auth/vocabulary", api_url))
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
        Err(crate::account_errors::message(status.as_u16(), &body_text))
    }
}

pub async fn delete_vocab_term(api_url: &str, token: &str, term: &str) -> Result<(), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    let res = client
        .delete(format!("{}/auth/vocabulary", api_url))
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

/// Sends one snippets request and returns the server's JSON reply. Snippet
/// text can hold personal details, so it is never logged.
pub async fn snippets_request(
    api_url: &str,
    token: &str,
    method: reqwest::Method,
    path: &str,
    body: Option<serde_json::Value>,
) -> Result<serde_json::Value, String> {
    let mut request = create_client()
        .request(method, format!("{}{}", api_url, path))
        .header("Authorization", format!("Bearer {}", token));
    if let Some(body) = body {
        request = request.json(&body);
    }
    let res = request
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;
    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        serde_json::from_str(&body_text).map_err(|_| "Unexpected reply from the server".to_string())
    } else if status.as_u16() == 401 {
        crate::handle_session_expired(Some("Your session has expired. Please sign in again."));
        Err("Session expired. Please sign in again.".to_string())
    } else {
        Err(crate::account_errors::message(status.as_u16(), &body_text))
    }
}

pub async fn validate_custom_prompt(
    api_url: &str,
    token: &str,
    prompt: &str,
) -> Result<PromptValidationResult, String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("prompt", prompt);

    let res = client
        .post(format!("{}/validate-prompt", api_url))
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
        .text("prompt", custom_prompt.to_string());

    let res = client
        .post(format!("{}/generate", api_url))
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
        crate::account_errors::notify_warning(resp.warning.as_deref());
        Ok(TranscribeResult {
            text: resp.text,
            log_id: resp.log_id,
            ack_token: resp.ack_token,
        })
    } else {
        let body_trimmed = body_text.trim().to_string();
        if status.as_u16() == 401 {
            Err("401 Unauthorized: Session token is invalid or expired.".to_string())
        } else if !body_trimmed.is_empty() {
            Err(crate::account_errors::message(status.as_u16(), &body_text))
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

    let is_event_stream = res
        .headers()
        .get("content-type")
        .and_then(|h| h.to_str().ok())
        .map(|ct| ct.contains("text/event-stream"))
        .unwrap_or(false);

    if !is_event_stream {
        let body_text = res.text().await.unwrap_or_default();
        let resp: TransformResponse = serde_json::from_str(&body_text)
            .map_err(|e| format!("Failed to parse response: {}", e))?;
        crate::account_errors::validate_command_result(&resp.text)?;
        on_chunk(&resp.text, &resp.text);
        return Ok(resp.text);
    }

    let mut stream = res.bytes_stream();
    let mut buffer: Vec<u8> = Vec::new();
    let mut accumulated = String::new();
    let mut completed = false;

    while let Some(chunk_res) = stream.next().await {
        let bytes = chunk_res.map_err(|e| format!("Stream error: {}", e))?;
        buffer.extend_from_slice(&bytes);

        while let Some(newline_pos) = buffer.iter().position(|&b| b == b'\n') {
            let frame: Vec<u8> = buffer.drain(..=newline_pos).collect();
            let line = std::str::from_utf8(&frame)
                .map_err(|_| "Invalid text encoding in Command Mode response".to_string())?
                .trim();

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
                        println!(
                            "[STREAM RESET] Server signaled stream reset due to provider fallback."
                        );
                        accumulated.clear();
                        on_chunk("[RESET]", "");
                        continue;
                    }

                    if let Some(ref delta) = parsed.delta {
                        if !delta.is_empty() {
                            accumulated.push_str(delta);
                            // Provider deltas remain buffered until done=true.
                        }
                    }

                    if parsed.done.unwrap_or(false) {
                        completed = true;
                        if let Some(ref full) = parsed.text {
                            if !full.is_empty() {
                                accumulated = full.clone();
                            }
                        }
                    } else if let Some(ref full) = parsed.text {
                        if !full.is_empty() && accumulated.is_empty() {
                            accumulated = full.clone();
                            // Hold the completed text for validation.
                        }
                    }
                }
            }
        }
    }

    if !completed {
        return Err(
            "Command Mode ended before the result was complete. Your text was not replaced.".into(),
        );
    }
    crate::account_errors::validate_command_result(&accumulated)?;
    on_chunk(&accumulated, &accumulated);

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

pub async fn update_user_languages(
    api_url: &str,
    token: &str,
    languages: Vec<String>,
) -> Result<Vec<String>, String> {
    let client = create_client();
    let mut payload = std::collections::HashMap::new();
    payload.insert("languages", languages);

    let res = client
        .post(format!("{}/auth/languages", api_url))
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
        let parsed = res
            .json::<Resp>()
            .await
            .map_err(|e| format!("Failed to parse response: {}", e))?;
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
    if text.trim().is_empty() {
        return;
    }
    let (id, token, base) = context.history_identity();
    let Some(token) = token else {
        return;
    };
    let (id, token, base, text) = (
        id.to_string(),
        token.to_string(),
        base.to_string(),
        text.to_string(),
    );
    let mode = if context.is_command_mode {
        "command"
    } else {
        "dictation"
    };
    tauri::async_runtime::spawn(async move {
        let client = http();
        for attempt in 0..3 {
            if attempt > 0 {
                tokio::time::sleep(std::time::Duration::from_secs(2 * attempt)).await;
            }
            let result = client
                .post(format!("{}/v1/transcriptions/final", base))
                .bearer_auth(&token)
                .json(&serde_json::json!({"request_id":id,"text":text,"mode":mode}))
                .send()
                .await;
            match result {
                Ok(response) if response.status().is_success() => return,
                Ok(response)
                    if response.status().is_client_error() && response.status().as_u16() != 429 =>
                {
                    break
                }
                _ => {}
            }
        }
        crate::win32::show_tray_notification("SpeekTo.Me — History not saved", "Your text was delivered, but its encrypted history could not be saved. Check your connection.",crate::win32::NotificationLevel::Warning);
    });
}

#[cfg(test)]
mod command_stream_tests {
    use super::*;
    use std::io::{Read, Write};
    fn response(body: String) -> (String, std::thread::JoinHandle<()>) {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let worker = std::thread::spawn(move || {
            let (mut socket, _) = listener.accept().unwrap();
            let mut request = [0u8; 8192];
            let _ = socket.read(&mut request);
            write!(socket,"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",body.len()).unwrap();
            // Write bytewise to exercise multibyte characters across network reads.
            for byte in body.as_bytes() {
                socket.write_all(&[*byte]).unwrap();
            }
        });
        (url, worker)
    }
    #[tokio::test]
    async fn only_validated_complete_output_reaches_typing_callback() {
        for body in [
            "data: {\"delta\":\"partial text\"}\n\ndata: {\"error\":\"missing information\",\"done\":true}\n\n".to_string(),
            "data: {\"delta\":\"partial text\"}\n\n".to_string(),
            format!("data: {}\n\n",serde_json::json!({"text":"{\"speektome_error\":{\"code\":\"refused\"}}","done":true})),
        ] {
            let (url,worker)=response(body); let mut delivered=Vec::new();
            let result=transform_text_stream(&url,"test","original selection","translate",|delta,_|delivered.push(delta.to_owned())).await;
            worker.join().unwrap();
            assert!(result.is_err()); assert!(delivered.is_empty());
        }
        let expected = "यह बिल्कुल सही था।";
        let body = format!(
            "data: {}\n\ndata: {}\n\ndata: [DONE]\n\n",
            serde_json::json!({"delta":expected}),
            serde_json::json!({"text":expected,"done":true})
        );
        let (url, worker) = response(body);
        let mut delivered = Vec::new();
        let result =
            transform_text_stream(&url, "test", "selected text", "translate", |delta, _| {
                delivered.push(delta.to_owned())
            })
            .await
            .unwrap();
        worker.join().unwrap();
        assert_eq!(result, expected);
        assert_eq!(delivered, vec![expected]);
    }
}

#[cfg(test)]
mod login_attempt_tests {
    #[test]
    fn cancellation_and_retry_invalidate_previous_login_attempts() {
        let first = super::begin_login();
        assert!(super::login_is_current(first));
        super::cancel_login();
        assert!(!super::login_is_current(first));
        let second = super::begin_login();
        assert!(super::login_is_current(second));
        assert!(!super::login_is_current(first));
    }
}

#[cfg(test)]
mod webview_request_tests {
    use super::webview_request_allowed;

    #[test]
    fn only_listed_meeting_routes_are_allowed() {
        assert!(webview_request_allowed("GET", "/meetings"));
        assert!(webview_request_allowed("DELETE", "/meetings/42"));
        assert!(webview_request_allowed("PUT", "/meetings/42/speakers"));
        assert!(webview_request_allowed(
            "POST",
            "/v1/meetings/jobs/abc-123_x/retry"
        ));
        assert!(!webview_request_allowed("POST", "/meetings"));
        assert!(!webview_request_allowed("GET", "/admin/users"));
        assert!(!webview_request_allowed("DELETE", "/auth/account"));
        assert!(!webview_request_allowed("GET", "/meetings/../admin"));
        assert!(!webview_request_allowed("GET", "/meetings/42?x=1"));
        assert!(!webview_request_allowed("GET", "/meetings//"));
    }
}
