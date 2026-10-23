//! A server permit prevents simultaneous recordings across devices. The permit
//! lasts through a network outage; local audio always survives until upload.
use serde::{Deserialize, Serialize};
use std::{
    sync::{
        atomic::{AtomicBool, Ordering},
        Mutex, OnceLock,
    },
    time::Duration,
};
use tauri::Emitter;
#[derive(Clone, Deserialize, Serialize)]
pub struct Permit {
    id: String,
}
static ACTIVE: OnceLock<Mutex<Option<Permit>>> = OnceLock::new();
static STARTING: AtomicBool = AtomicBool::new(false);
pub struct Starting;
impl Drop for Starting {
    fn drop(&mut self) {
        STARTING.store(false, Ordering::SeqCst);
    }
}
pub fn is_starting() -> bool {
    STARTING.load(Ordering::SeqCst)
}
pub fn begin() -> Result<Starting, String> {
    if STARTING
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return Err("A meeting is already starting. Please wait.".into());
    }
    let guard = Starting;
    if active().lock().unwrap_or_else(|e| e.into_inner()).is_some() {
        return Err("A meeting is already recording. Stop it before starting another.".into());
    }
    Ok(guard)
}
fn active() -> &'static Mutex<Option<Permit>> {
    ACTIVE.get_or_init(|| Mutex::new(None))
}
fn path() -> std::path::PathBuf {
    crate::config::get_config_path().with_file_name("meeting-permit.json")
}
fn connection() -> Result<(String, String), String> {
    let token = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .session_token
        .clone()
        .ok_or("Sign in before starting a meeting.")?;
    Ok((crate::api::get_api_url(), token))
}
fn release(permit: Permit) {
    // Keep a small local release marker if offline; a later start retries it.
    std::thread::spawn(move || {
        let Ok((base, token)) = connection() else {
            return;
        };
        let Ok(client) = reqwest::blocking::Client::builder()
            .timeout(Duration::from_secs(8))
            .build()
        else {
            return;
        };
        if let Ok(response) = client
            .delete(format!(
                "{}/v1/meetings/session/{}",
                base.trim_end_matches('/'),
                permit.id
            ))
            .bearer_auth(token)
            .send()
        {
            if response.status().is_success() {
                if let Ok(saved) = std::fs::read(path()).and_then(|bytes| {
                    serde_json::from_slice::<Permit>(&bytes).map_err(std::io::Error::other)
                }) {
                    if saved.id == permit.id {
                        let _ = std::fs::remove_file(path());
                    }
                }
            }
        }
    });
}
pub async fn acquire() -> Result<Permit, String> {
    let (base, token) = connection()?;
    let marker = path();
    acquire_for(&base, &token, &marker).await
}
async fn acquire_for(base: &str, token: &str, marker: &std::path::Path) -> Result<Permit, String> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(10))
        .build()
        .map_err(|e| e.to_string())?;
    // Recover a stopped/crashed or ambiguously acknowledged start first.
    let no_active_permit = active().lock().unwrap_or_else(|e| e.into_inner()).is_none();
    if no_active_permit {
        if let Ok(bytes) = std::fs::read(marker) {
            if let Ok(saved) = serde_json::from_slice::<Permit>(&bytes) {
                let response=client.delete(format!("{}/v1/meetings/session/{}",base.trim_end_matches('/'),saved.id))
                    .bearer_auth(&token).send().await
                    .map_err(|_|"Connect to the internet to reconcile your previous meeting before starting another.".to_string())?;
                if !response.status().is_success() {
                    return Err(
                        "Your previous meeting could not be released. Please try again shortly."
                            .into(),
                    );
                }
                let _ = std::fs::remove_file(marker);
            }
        }
    }
    // Persist the owner before the request. A response lost during a connection
    // drop cannot strand a four-hour server slot whose ID the client never saw.
    let permit = Permit {
        id: uuid::Uuid::new_v4().simple().to_string(),
    };
    std::fs::write(
        marker,
        serde_json::to_vec(&permit).map_err(|e| e.to_string())?,
    )
    .map_err(|e| format!("Cannot save the recording permit: {}", e))?;
    let response=client.post(format!("{}/v1/meetings/session",base.trim_end_matches('/')))
        .bearer_auth(&token).json(&permit).send().await
        .map_err(|_|"Connect to the internet before starting a meeting. Once started, recording can continue offline.".to_string())?;
    if !response.status().is_success() {
        let status = response.status().as_u16();
        let body = response.text().await.unwrap_or_default();
        let message = crate::account_errors::message(status, &body);
        crate::account_errors::notify("Meetings", &message);
        return Err(message);
    }
    match response.json::<Permit>().await {
        Ok(confirmed) if confirmed.id == permit.id => Ok(permit),
        _ => {
            release(permit);
            Err("The server could not authorize this meeting. Please try again.".into())
        }
    }
}
pub fn abandon(permit: Permit) {
    release(permit)
}
pub fn install(permit: Permit) {
    let id = permit.id.clone();
    *active().lock().unwrap_or_else(|e| e.into_inner()) = Some(permit);
    std::thread::spawn(move || {
        // Wake frequently enough that a completed meeting leaves no lingering timer.
        let started = std::time::SystemTime::now();
        loop {
            if started.elapsed().unwrap_or(Duration::ZERO) >= Duration::from_secs(14400) {
                break;
            }
            std::thread::sleep(Duration::from_secs(1));
            if active()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .as_ref()
                .map(|p| p.id.as_str())
                != Some(id.as_str())
            {
                return;
            }
        }
        tauri::async_runtime::block_on(async {
            if active()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .as_ref()
                .map(|p| p.id.as_str())
                != Some(id.as_str())
            {
                return;
            }
            let message=match crate::stop_meeting(None).await {
                Ok(_) => "The four-hour meeting limit was reached. Your recording is saved locally and will upload when internet is available.".to_string(),
                Err(error) => format!("The four-hour meeting limit was reached. Recording stopped, but saving could not finish: {}. Local audio files have been retained.",error),
            };
            crate::account_errors::notify("Meetings", &message);
            if let Some(app) = crate::TAURI_APP.get() {
                let _ = app.emit("meeting-recording-stopped", message);
            }
        });
    });
}
pub fn finish() {
    if let Some(permit) = active().lock().unwrap_or_else(|e| e.into_inner()).take() {
        release(permit)
    }
}

pub fn release_pending() {
    let Ok(_starting) = begin() else {
        return;
    };
    if let Ok(bytes) = std::fs::read(path()) {
        if let Ok(permit) = serde_json::from_slice::<Permit>(&bytes) {
            release(permit)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        io::{BufRead, Read, Write},
        net::TcpListener,
        sync::Arc,
    };
    #[test]
    fn duplicate_local_starts_are_rejected_and_guard_recovers() {
        let guard = begin().unwrap();
        assert!(begin().is_err());
        drop(guard);
        assert!(begin().is_ok());
    }
    #[test]
    fn lost_start_response_preserves_owner_and_next_start_reconciles_it() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let owner = Arc::new(Mutex::new(None::<String>));
        let server_owner = owner.clone();
        let server = std::thread::spawn(move || {
            for step in 0..3 {
                let (mut socket, _) = listener.accept().unwrap();
                socket
                    .set_read_timeout(Some(Duration::from_secs(3)))
                    .unwrap();
                let mut reader = std::io::BufReader::new(socket.try_clone().unwrap());
                let mut request = String::new();
                reader.read_line(&mut request).unwrap();
                let mut length = 0;
                loop {
                    let mut line = String::new();
                    reader.read_line(&mut line).unwrap();
                    if line == "\r\n" {
                        break;
                    }
                    if let Some((key, value)) = line.split_once(':') {
                        if key.eq_ignore_ascii_case("content-length") {
                            length = value.trim().parse::<usize>().unwrap();
                        }
                    }
                }
                let mut body = vec![0; length];
                reader.read_exact(&mut body).unwrap();
                if step == 1 {
                    let saved = server_owner.lock().unwrap().take().unwrap();
                    assert!(request.contains(&saved));
                    assert!(request.starts_with("DELETE "));
                    socket
                        .write_all(b"HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
                        .unwrap();
                } else {
                    assert!(request.starts_with("POST "));
                    let permit: Permit = serde_json::from_slice(&body).unwrap();
                    *server_owner.lock().unwrap() = Some(permit.id.clone());
                    if step == 0 {
                        continue;
                    } // Accept, then lose the HTTP acknowledgement.
                    let body = serde_json::to_string(&permit).unwrap();
                    write!(socket,"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",body.len(),body).unwrap();
                }
            }
        });
        let directory =
            std::env::temp_dir().join(format!("speektome-permit-test-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir(&directory).unwrap();
        let marker = directory.join("permit.json");
        let runtime = tokio::runtime::Runtime::new().unwrap();
        assert!(runtime
            .block_on(acquire_for(&base, "test-token", &marker))
            .is_err());
        let saved: Permit = serde_json::from_slice(&std::fs::read(&marker).unwrap()).unwrap();
        assert_eq!(owner.lock().unwrap().as_deref(), Some(saved.id.as_str()));
        let resumed = runtime
            .block_on(acquire_for(&base, "test-token", &marker))
            .unwrap();
        assert_ne!(saved.id, resumed.id);
        assert_eq!(owner.lock().unwrap().as_deref(), Some(resumed.id.as_str()));
        server.join().unwrap();
        std::fs::remove_dir_all(directory).unwrap();
    }
}
