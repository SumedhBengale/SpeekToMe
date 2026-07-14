use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::atomic::Ordering;

// Re-export shared core data types to maintain backward compatibility in src-tauri
pub use speektome_core::api::{UserInfo, ClientVocabularyTerm, PromptValidationResult};

pub fn get_api_url() -> String {
    let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
    config.api_url.clone().unwrap_or_else(|| "https://api.speekto.me".to_string())
}

// Windows-specific ShellExecuteW for opening default browser
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
                1, // SW_SHOWNORMAL
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

// Temporary loopback listener to capture OAuth token
pub fn listen_for_token() -> Option<String> {
    let listener = TcpListener::bind("127.0.0.1:0").ok()?;
    let port = listener.local_addr().ok()?.port();
    
    // Generate a random client_state
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

pub fn fetch_user_info(token: &str) -> Option<UserInfo> {
    tauri::async_runtime::block_on(
        speektome_core::api::fetch_user_info(&get_api_url(), token)
    ).ok()
}

pub fn login_with_credentials(email: &str, password: &str) -> Result<(String, UserInfo), String> {
    tauri::async_runtime::block_on(
        speektome_core::api::login_with_credentials(&get_api_url(), email, password)
    )
}

pub fn signup_with_credentials(email: &str, password: &str, name: &str) -> Result<String, String> {
    tauri::async_runtime::block_on(
        speektome_core::api::signup_with_credentials(&get_api_url(), email, password, name)
    )
}

pub fn fetch_vocabulary(token: &str) -> Result<Vec<ClientVocabularyTerm>, String> {
    tauri::async_runtime::block_on(
        speektome_core::api::fetch_vocabulary(&get_api_url(), token)
    )
}

pub fn add_vocab_term(token: &str, term: &str) -> Result<(), String> {
    tauri::async_runtime::block_on(
        speektome_core::api::add_vocab_term(&get_api_url(), token, term)
    )
}

pub fn delete_vocab_term(token: &str, term: &str) -> Result<(), String> {
    tauri::async_runtime::block_on(
        speektome_core::api::delete_vocab_term(&get_api_url(), token, term)
    )
}

pub fn validate_custom_prompt(token: &str, prompt: &str) -> Result<PromptValidationResult, String> {
    tauri::async_runtime::block_on(
        speektome_core::api::validate_custom_prompt(&get_api_url(), token, prompt)
    )
}

pub fn upload_meeting(token: &str, title: &str, filepath: &std::path::Path) -> Result<String, String> {
    let mut file = std::fs::File::open(filepath).map_err(|e| format!("Failed to open meeting audio file: {}", e))?;
    let mut mixed_bytes = Vec::new();
    file.read_to_end(&mut mixed_bytes).map_err(|e| format!("Failed to read meeting audio file: {}", e))?;
    drop(file);

    let mic_filepath = filepath.with_extension("mic.ogg");
    let loopback_filepath = filepath.with_extension("loopback.ogg");

    let mut mic_bytes = None;
    if let Ok(mut mic_file) = std::fs::File::open(&mic_filepath) {
        let mut bytes = Vec::new();
        if mic_file.read_to_end(&mut bytes).is_ok() {
            mic_bytes = Some(bytes);
        }
    }

    let mut loopback_bytes = None;
    if let Ok(mut lb_file) = std::fs::File::open(&loopback_filepath) {
        let mut bytes = Vec::new();
        if lb_file.read_to_end(&mut bytes).is_ok() {
            loopback_bytes = Some(bytes);
        }
    }

    let result = tauri::async_runtime::block_on(
        speektome_core::api::upload_meeting(&get_api_url(), token, title, mixed_bytes, mic_bytes, loopback_bytes)
    );

    if result.is_ok() {
        let _ = std::fs::remove_file(filepath);
        let _ = std::fs::remove_file(mic_filepath);
        let _ = std::fs::remove_file(loopback_filepath);
    }
    result
}

// Dictation workflow wrapper
pub fn process_and_paste(samples: Vec<i16>, sample_rate: u32, channels: u16) {
    let encode_start = std::time::Instant::now();
    let ogg_opus_bytes = match speektome_core::audio_processing::encode_opus_ogg(&samples, sample_rate, channels) {
        Ok(bytes) => bytes,
        Err(e) => {
            eprintln!("Failed to encode to Ogg/Opus: {:?}", e);
            return;
        }
    };
    let encode_duration = encode_start.elapsed();
    
    let (model, enhance, typing_mode, smart_spacing, custom_prompt, session_token) = {
        let config = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
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

    let raw_bytes_size = samples.len() * 2;
    let compressed_size = ogg_opus_bytes.len();
    let compression_ratio = (1.0 - (compressed_size as f64 / raw_bytes_size as f64)) * 100.0;
    
    let request_start = std::time::Instant::now();
    let result = tauri::async_runtime::block_on(
        speektome_core::api::transcribe_audio(&get_api_url(), &token, ogg_opus_bytes, &model, enhance, &custom_prompt)
    );

    match result {
        Ok(text) => {
            let request_duration = request_start.elapsed();
            let mut formatted_text = text.trim().to_string();
            
            println!("\n==================================================");
            println!("                SPEEKTOME CLIENT REST LOG");
            println!("==================================================");
            println!("Encode Time:       {:?}", encode_duration);
            println!("Original PCM Size: {} bytes", raw_bytes_size);
            println!("Opus Ogg Size:     {} bytes (Saved {:.1}%)", compressed_size, compression_ratio);
            println!("Request Latency:   {:?}", request_duration);
            println!("==================================================\n");

            if !formatted_text.is_empty() {
                if smart_spacing && crate::IS_CONSECUTIVE.load(Ordering::SeqCst) {
                    if !formatted_text.starts_with(' ') {
                        formatted_text = format!(" {}", formatted_text);
                    }
                }

                println!("Transcription received (typing: {}).", typing_mode);
                if typing_mode {
                    crate::keyboard::simulate_typing(&formatted_text);
                } else {
                    crate::keyboard::paste_text(&formatted_text);
                }
            } else {
                println!("Empty transcript returned.");
            }
        }
        Err(err_msg) => {
            // Native platform feedback beep on error
            #[cfg(target_os = "windows")]
            unsafe {
                windows_sys::Win32::System::Diagnostics::Debug::MessageBeep(0x00000010);
            }
            println!("\n==================================================");
            println!("          SPEEKTOME CLIENT REST LOG (ERROR)");
            println!("==================================================");
            println!("Encode Time:       {:?}", encode_duration);
            println!("Original PCM Size: {} bytes", raw_bytes_size);
            println!("Error Message:     \"{}\"", err_msg);
            println!("==================================================\n");
        }
    }
}
