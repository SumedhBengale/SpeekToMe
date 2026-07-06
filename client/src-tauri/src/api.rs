use std::io::{Read, Write};
use std::net::TcpListener;
use serde::Deserialize;
use std::sync::atomic::Ordering;

#[derive(Deserialize)]
struct TranscriptResponse {
    text: String,
}

// Helper to open the default system browser on Windows using standard shell commands
pub fn open_browser(url: &str) {
    let _ = std::process::Command::new("cmd")
        .args(["/C", "start", url])
        .spawn();
}

#[derive(serde::Serialize, serde::Deserialize, Clone, Debug)]
pub struct UserInfo {
    pub name: String,
    pub email: String,
    pub plan_tier: String,
    pub plan_active: bool,
    pub plan_activated_at: String,
    pub duration_seconds: f64,
}

// REST Client user profile retrieval helper
pub fn fetch_user_info(token: &str) -> Option<UserInfo> {
    let client = reqwest::blocking::Client::new();
    match client.get("http://127.0.0.1:8000/auth/me")
        .header("Authorization", format!("Bearer {}", token))
        .send() 
    {
        Ok(res) => {
            if res.status().is_success() {
                if let Ok(info) = res.json::<UserInfo>() {
                    return Some(info);
                }
            }
        }
        Err(_) => {}
    }
    None
}

// Temporary loopback listener to capture Google OAuth token
fn url_decode(s: &str) -> String {
    let mut res = String::new();
    let mut chars = s.chars();
    while let Some(c) = chars.next() {
        if c == '%' {
            let hex: String = chars.by_ref().take(2).collect();
            if let Ok(val) = u8::from_str_radix(&hex, 16) {
                res.push(val as char);
            }
        } else if c == '+' {
            res.push(' ');
        } else {
            res.push(c);
        }
    }
    res
}

// Temporary loopback listener to capture Google OAuth token
pub fn listen_for_token() -> Option<String> {
    let listener = TcpListener::bind("127.0.0.1:0").ok()?;
    let port = listener.local_addr().ok()?.port();
    
    let auth_url = format!("http://127.0.0.1:8000/auth/login?port={}", port);
    open_browser(&auth_url);
    
    // Accept single connection
    listener.set_nonblocking(false).ok();
    if let Ok((mut stream, _)) = listener.accept() {
        let mut buffer = [0; 2048];
        if let Ok(bytes_read) = stream.read(&mut buffer) {
            let request = String::from_utf8_lossy(&buffer[..bytes_read]);
            
            let mut captured_token = None;
            let mut payment_url = None;

            if let Some(path_start) = request.find("GET /callback?") {
                let query_slice = &request[path_start + 14..];
                if let Some(space_pos) = query_slice.find(' ') {
                    let query = &query_slice[..space_pos];
                    for pair in query.split('&') {
                        let mut parts = pair.split('=');
                        if let (Some(k), Some(v)) = (parts.next(), parts.next()) {
                            if k == "token" {
                                captured_token = Some(v.to_string());
                            } else if k == "payment_url" {
                                payment_url = Some(url_decode(v));
                            }
                        }
                    }
                }
            }

            if let Some(token) = captured_token {
                // Respond with script redirect if a payment URL is provided, otherwise normal success HTML
                let body = if let Some(ref pay_url) = payment_url {
                    format!(
                        "<html><body style='font-family:sans-serif;text-align:center;margin-top:50px;'><h2>Authentication Successful</h2><p>Redirecting you to subscription payment...</p><script>window.location.href = '{}';</script></body></html>",
                        pay_url
                    )
                } else {
                    "<html><body style='font-family:sans-serif;text-align:center;margin-top:50px;'><h2>Authentication Successful</h2><p>You can close this window now and return to Speekey.</p></body></html>".to_string()
                };

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
    None
}

// Thread routine: send audio to server, receive transcript, paste text
pub fn process_and_paste(samples: Vec<i16>, sample_rate: u32, channels: u16) {
    let encode_start = std::time::Instant::now();
    let ogg_opus_bytes = match crate::audio::encode_opus_ogg(&samples, sample_rate, channels) {
        Ok(bytes) => bytes,
        Err(e) => {
            eprintln!("Failed to encode to Ogg/Opus: {:?}", e);
            return;
        }
    };
    let encode_duration = encode_start.elapsed();
    
    // Read model, enhance choice, typing mode, smart spacing, custom prompt, and session token from config
    let (model, enhance, typing_mode, smart_spacing, custom_prompt, session_token) = {
        let config = crate::get_active_config().lock().unwrap();
        (
            config.model.clone(),
            config.enhance,
            config.typing_mode,
            config.smart_spacing,
            config.custom_prompt.clone(),
            config.session_token.clone()
        )
    };
    
    let token = match session_token {
        Some(t) => t,
        None => {
            eprintln!("No active session token. Please log in first.");
            return;
        }
    };
    
    let client = reqwest::blocking::Client::new();
    let part = reqwest::blocking::multipart::Part::bytes(ogg_opus_bytes.clone())
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();
    let form = reqwest::blocking::multipart::Form::new()
        .part("file", part)
        .text("model", model)
        .text("enhance", enhance.to_string())
        .text("prompt", custom_prompt);
    
    let raw_bytes_size = samples.len() * 2;
    let compressed_size = ogg_opus_bytes.len();
    let compression_ratio = (1.0 - (compressed_size as f64 / raw_bytes_size as f64)) * 100.0;
    
    let request_start = std::time::Instant::now();
    match client.post("http://127.0.0.1:8000/generate")
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send() 
    {
        Ok(response) => {
            let request_duration = request_start.elapsed();
            let status = response.status();
            
            if status.is_success() {
                if let Ok(res_json) = response.json::<TranscriptResponse>() {
                    let mut text = res_json.text.trim().to_string();
                    
                    println!("\n==================================================");
                    println!("                SPEEKEY CLIENT REST LOG");
                    println!("==================================================");
                    println!("Encode Time:       {:?}", encode_duration);
                    println!("Original PCM Size: {} bytes", raw_bytes_size);
                    println!("Opus Ogg Size:     {} bytes (Saved {:.1}%)", compressed_size, compression_ratio);
                    println!("HTTP Status:       {}", status);
                    println!("Request Latency:   {:?}", request_duration);
                    println!("Transcript:        \"{}\"", text);
                    println!("==================================================\n");

                    if !text.is_empty() {
                        // Apply Smart Spacing if enabled and consecutive dictation flag is set
                        if smart_spacing && crate::IS_CONSECUTIVE.load(Ordering::SeqCst) {
                            if !text.starts_with(' ') {
                                text = format!(" {}", text);
                            }
                        }

                        println!("Transcription output (typing: {}): {}", typing_mode, text);
                        if typing_mode {
                            crate::keyboard::simulate_typing(&text);
                        } else {
                            crate::keyboard::paste_text(&text);
                        }
                    } else {
                        println!("Empty transcript returned.");
                    }
                } else {
                    eprintln!("Failed to parse response JSON.");
                }
            } else {
                unsafe {
                    windows_sys::Win32::System::Diagnostics::Debug::MessageBeep(0x00000010); // MB_ICONHAND (error chime)
                }
                let err_text = response.text().unwrap_or_default();
                let err_msg = if let Ok(err_json) = serde_json::from_str::<serde_json::Value>(&err_text) {
                    err_json.get("error").and_then(|v| v.as_str()).unwrap_or(&err_text).to_string()
                } else {
                    err_text.clone()
                };

                println!("\n==================================================");
                println!("          SPEEKEY CLIENT REST LOG (ERROR)");
                println!("==================================================");
                println!("Encode Time:       {:?}", encode_duration);
                println!("Original PCM Size: {} bytes", raw_bytes_size);
                println!("Opus Ogg Size:     {} bytes (Saved {:.1}%)", compressed_size, compression_ratio);
                println!("HTTP Status:       {}", status);
                println!("Request Latency:   {:?}", request_duration);
                println!("Error Message:     \"{}\"", err_msg);
                println!("==================================================\n");
            }
        }
        Err(e) => {
            eprintln!("Failed to connect to Speekey server: {:?}", e);
        }
    }
}

#[derive(Deserialize)]
struct AuthResponse {
    token: String,
}

pub fn login_with_credentials(email: &str, password: &str) -> Result<(String, UserInfo), String> {
    let client = reqwest::blocking::Client::new();
    
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);
    
    match client.post("http://127.0.0.1:8000/auth/login")
        .json(&map)
        .send()
    {
        Ok(res) => {
            let status = res.status();
            let body_text = res.text().unwrap_or_default();
            if status.is_success() {
                if let Ok(auth_res) = serde_json::from_str::<AuthResponse>(&body_text) {
                    if let Some(user_info) = fetch_user_info(&auth_res.token) {
                        return Ok((auth_res.token, user_info));
                    }
                    return Err("Failed to retrieve user profile after login".to_string());
                }
                return Err("Failed to parse login response".to_string());
            }
            let body_trimmed = body_text.trim().to_string();
            if !body_trimmed.is_empty() {
                return Err(body_trimmed);
            }
            Err(format!("Login failed with status: {}", status))
        }
        Err(e) => Err(format!("Failed to connect to server: {}", e)),
    }
}

pub fn signup_with_credentials(email: &str, password: &str, name: &str) -> Result<String, String> {
    let client = reqwest::blocking::Client::new();
    
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);
    map.insert("name", name);
    
    match client.post("http://127.0.0.1:8000/auth/signup")
        .json(&map)
        .send()
    {
        Ok(res) => {
            let status = res.status();
            let body_text = res.text().unwrap_or_default();
            if status.is_success() {
                if let Ok(json_res) = serde_json::from_str::<serde_json::Value>(&body_text) {
                    if let Some(msg) = json_res.get("message").and_then(|v| v.as_str()) {
                        return Ok(msg.to_string());
                    }
                }
                return Ok("Account created successfully. Please check your email to verify your address.".to_string());
            }
            let body_trimmed = body_text.trim().to_string();
            if !body_trimmed.is_empty() {
                return Err(body_trimmed);
            }
            Err(format!("Signup failed with status: {}", status))
        }
        Err(e) => Err(format!("Failed to connect to server: {}", e)),
    }
}

#[derive(Deserialize, serde::Serialize, Clone, Debug)]
pub struct ClientVocabularyTerm {
    #[serde(rename = "canonical_term")]
    pub canonical: String,
    #[serde(rename = "normalized_term")]
    pub normalized: String,
}

pub fn fetch_vocabulary(token: &str) -> Result<Vec<ClientVocabularyTerm>, String> {
    let client = reqwest::blocking::Client::new();
    match client.get("http://127.0.0.1:8000/auth/vocabulary")
        .header("Authorization", format!("Bearer {}", token))
        .send()
    {
        Ok(res) => {
            let status = res.status();
            let body_text = res.text().unwrap_or_default();
            if status.is_success() {
                if let Ok(terms) = serde_json::from_str::<Vec<ClientVocabularyTerm>>(&body_text) {
                    return Ok(terms);
                }
                return Err("Failed to parse vocabulary list".to_string());
            }
            Err(format!("Failed to retrieve vocabulary: {}", body_text))
        }
        Err(e) => Err(format!("Failed to connect to server: {}", e)),
    }
}

pub fn add_vocab_term(token: &str, term: &str) -> Result<(), String> {
    let client = reqwest::blocking::Client::new();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    match client.post("http://127.0.0.1:8000/auth/vocabulary")
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
    {
        Ok(res) => {
            let status = res.status();
            let body_text = res.text().unwrap_or_default();
            if status.is_success() {
                return Ok(());
            }
            Err(format!("Failed to add vocabulary term: {}", body_text))
        }
        Err(e) => Err(format!("Failed to connect to server: {}", e)),
    }
}

pub fn delete_vocab_term(token: &str, term: &str) -> Result<(), String> {
    let client = reqwest::blocking::Client::new();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    match client.delete("http://127.0.0.1:8000/auth/vocabulary")
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
    {
        Ok(res) => {
            let status = res.status();
            let body_text = res.text().unwrap_or_default();
            if status.is_success() {
                return Ok(());
            }
            Err(format!("Failed to delete vocabulary term: {}", body_text))
        }
        Err(e) => Err(format!("Failed to connect to server: {}", e)),
    }
}

pub fn upload_meeting(token: &str, title: &str, filepath: &str) -> Result<String, String> {
    let mut file = std::fs::File::open(filepath).map_err(|e| format!("Failed to open meeting audio file: {}", e))?;
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes).map_err(|e| format!("Failed to read meeting audio file: {}", e))?;

    let path = std::path::Path::new(filepath);
    let mic_filepath = path.with_extension("mic.ogg");
    let loopback_filepath = path.with_extension("loopback.ogg");

    let client = reqwest::blocking::Client::new();
    let part = reqwest::blocking::multipart::Part::bytes(bytes)
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();

    let mut form = reqwest::blocking::multipart::Form::new()
        .part("file", part)
        .text("title", title.to_string());

    if let Ok(mut mic_file) = std::fs::File::open(&mic_filepath) {
        let mut mic_bytes = Vec::new();
        if mic_file.read_to_end(&mut mic_bytes).is_ok() {
            let mic_part = reqwest::blocking::multipart::Part::bytes(mic_bytes)
                .file_name("recording_mic.ogg")
                .mime_str("audio/ogg")
                .unwrap();
            form = form.part("file_mic", mic_part);
        }
    }

    if let Ok(mut lb_file) = std::fs::File::open(&loopback_filepath) {
        let mut lb_bytes = Vec::new();
        if lb_file.read_to_end(&mut lb_bytes).is_ok() {
            let lb_part = reqwest::blocking::multipart::Part::bytes(lb_bytes)
                .file_name("recording_loopback.ogg")
                .mime_str("audio/ogg")
                .unwrap();
            form = form.part("file_loopback", lb_part);
        }
    }

    match client.post("http://127.0.0.1:8000/meetings")
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send()
    {
        Ok(res) => {
            let status = res.status();
            let body = res.text().unwrap_or_default();
            
            // Clean up temporary files since they are uploaded successfully
            let _ = std::fs::remove_file(mic_filepath);
            let _ = std::fs::remove_file(loopback_filepath);
            
            if status.is_success() {
                Ok(body)
            } else {
                Err(format!("Upload failed with status {}: {}", status, body))
            }
        }
        Err(e) => Err(format!("Failed to connect to server: {}", e)),
    }
}

