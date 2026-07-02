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

// REST Client user profile retrieval helper
pub fn fetch_user_info(token: &str) -> Option<(String, String)> {
    let client = reqwest::blocking::Client::new();
    match client.get("http://127.0.0.1:8000/auth/me")
        .header("Authorization", format!("Bearer {}", token))
        .send() 
    {
        Ok(res) => {
            if res.status().is_success() {
                #[derive(Deserialize)]
                struct MeResponse {
                    email: String,
                    name: String,
                }
                if let Ok(me) = res.json::<MeResponse>() {
                    return Some((me.name, me.email));
                }
            }
        }
        Err(_) => {}
    }
    None
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
            // Search for "token=" query parameter in GET request
            if let Some(token_start) = request.find("token=") {
                let token_slice = &request[token_start + 6..];
                if let Some(space_pos) = token_slice.find(' ') {
                    let token = token_slice[..space_pos].trim().to_string();
                    
                    // Respond with simple success HTML page
                    let body = "<html><body style='font-family:sans-serif;text-align:center;margin-top:50px;'><h2>Authentication Successful</h2><p>You can close this window now and return to Speekey.</p></body></html>";
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

// Thread routine: send audio to server, receive transcript, paste text
pub fn process_and_paste(samples: Vec<i16>, sample_rate: u32, channels: u16) {
    let wav_bytes = crate::audio::write_wav_to_memory(&samples, sample_rate, channels);
    
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
    let part = reqwest::blocking::multipart::Part::bytes(wav_bytes)
        .file_name("recording.wav")
        .mime_str("audio/wav")
        .unwrap();
    let form = reqwest::blocking::multipart::Form::new()
        .part("file", part)
        .text("model", model)
        .text("enhance", enhance.to_string())
        .text("prompt", custom_prompt);
    
    match client.post("http://127.0.0.1:8000/generate")
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send() 
    {
        Ok(response) => {
            if response.status().is_success() {
                if let Ok(res_json) = response.json::<TranscriptResponse>() {
                    let mut text = res_json.text.trim().to_string();
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
                let status = response.status();
                unsafe {
                    windows_sys::Win32::System::Diagnostics::Debug::MessageBeep(0x00000010); // MB_ICONHAND (error chime)
                }
                if let Ok(err_text) = response.text() {
                    if let Ok(err_json) = serde_json::from_str::<serde_json::Value>(&err_text) {
                        if let Some(err_msg) = err_json.get("error").and_then(|v| v.as_str()) {
                            eprintln!("Server error ({}): {}", status, err_msg);
                        } else {
                            eprintln!("Server responded with error status ({}): {}", status, err_text);
                        }
                    } else {
                        eprintln!("Server responded with error status ({}): {}", status, err_text);
                    }
                } else {
                    eprintln!("Server responded with error status: {:?}", status);
                }
            }
        }
        Err(e) => {
            eprintln!("Failed to connect to Speekey server: {:?}", e);
        }
    }
}
