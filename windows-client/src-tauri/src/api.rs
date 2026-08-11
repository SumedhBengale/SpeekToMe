use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct UserInfo {
    pub name: String,
    pub email: String,
    pub plan_tier: String,
    pub plan_active: bool,
    pub plan_activated_at: String,
    pub duration_seconds: f64,
    pub monthly_limit_seconds: f64,
    pub languages: Option<Vec<String>>,
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

#[derive(Deserialize)]
struct TranscriptResponse {
    text: String,
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
    #[cfg(not(target_os = "windows"))]
    {
        let _ = std::process::Command::new("open")
            .arg(url)
            .spawn();
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

pub fn process_and_paste(samples: Vec<i16>, sample_rate: u32, channels: u16) {
    let (token_opt, model, enhance, custom_prompt) = {
        let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
        (
            config.session_token.clone(),
            config.model.clone(),
            config.enhance,
            config.custom_prompt.clone(),
        )
    };

    let token = match token_opt {
        Some(t) => t,
        None => {
            println!("No session token available.");
            return;
        }
    };

    let ogg_opus_bytes = match crate::audio::encode_opus_ogg(&samples, sample_rate, channels) {
        Ok(b) => b,
        Err(e) => {
            eprintln!("Failed to encode Opus audio: {:?}", e);
            return;
        }
    };

    tauri::async_runtime::spawn(async move {
        match transcribe_audio(&get_api_url(), &token, ogg_opus_bytes, &model, enhance, &custom_prompt).await {
            Ok(text) => {
                let trimmed = text.trim();
                if !trimmed.is_empty() {
                    println!("Transcribed Text: {}", trimmed);
                    if let Ok(mut clipboard) = arboard::Clipboard::new() {
                        let _ = clipboard.set_text(trimmed.to_string());
                        crate::keyboard::simulate_paste();
                    }
                }
            }
            Err(err) => {
                eprintln!("Transcription API failed: {}", err);
            }
        }
    });
}

pub async fn fetch_user_info(api_url: &str, token: &str) -> Result<UserInfo, String> {
    let client = create_client();
    let res = client.get(format!("{}/auth/me", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    if res.status().is_success() {
        res.json::<UserInfo>().await.map_err(|e| format!("Failed to parse user info: {}", e))
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
) -> Result<String, String> {
    let client = create_client();
    let part = reqwest::multipart::Part::bytes(ogg_opus_bytes)
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();

    let form = reqwest::multipart::Form::new()
        .part("file", part)
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
        Ok(resp.text)
    } else {
        Err(body_text)
    }
}

pub async fn upload_meeting(
    api_url: &str,
    token: &str,
    title: &str,
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
        .text("title", title.to_string());

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

    if res.status().is_success() {
        #[derive(Deserialize)]
        struct Resp {
            languages: Vec<String>,
        }
        let parsed = res.json::<Resp>().await.map_err(|e| format!("Failed to parse response: {}", e))?;
        Ok(parsed.languages)
    } else {
        let err_text = res.text().await.unwrap_or_default();
        Err(format!("Server error: {}", err_text))
    }
}
