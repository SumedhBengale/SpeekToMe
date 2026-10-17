//! Resumable disk-backed meeting uploads. Server processing outlives this app.
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    fs::File,
    io::{Read, Seek, SeekFrom},
    path::Path,
    sync::Mutex,
    time::Duration,
};
static UPLOAD_LOCK: Mutex<()> = Mutex::new(());
pub fn replace_file(from: &Path, to: &Path) -> std::io::Result<()> {
    #[cfg(windows)]
    {
        use std::os::windows::ffi::OsStrExt;
        #[link(name = "kernel32")]
        extern "system" {
            fn MoveFileExW(from: *const u16, to: *const u16, flags: u32) -> i32;
        }
        let from: Vec<u16> = from.as_os_str().encode_wide().chain(Some(0)).collect();
        let to: Vec<u16> = to.as_os_str().encode_wide().chain(Some(0)).collect();
        if unsafe { MoveFileExW(from.as_ptr(), to.as_ptr(), 0x1 | 0x8) } == 0 {
            return Err(std::io::Error::last_os_error());
        }
        Ok(())
    }
    #[cfg(not(windows))]
    {
        std::fs::rename(from, to)
    }
}
fn manifest_file(path: &Path) -> Result<Value, String> {
    let mut file = File::open(path).map_err(|e| e.to_string())?;
    let size = file.metadata().map_err(|e| e.to_string())?.len();
    let mut hash = Sha256::new();
    let mut buffer = [0u8; 65536];
    loop {
        let n = file.read(&mut buffer).map_err(|e| e.to_string())?;
        if n == 0 {
            break;
        }
        hash.update(&buffer[..n]);
    }
    Ok(json!({"size":size,"sha256":format!("{:x}",hash.finalize())}))
}
fn response(response: reqwest::blocking::Response) -> Result<Value, String> {
    let status = response.status();
    let body = response.text().map_err(|e| e.to_string())?;
    if !status.is_success() {
        return Err(format!(
            "Meeting server returned {}: {}",
            status,
            body.chars().take(300).collect::<String>()
        ));
    }
    serde_json::from_str(&body).map_err(|e| e.to_string())
}
pub fn upload(filepath: &str) -> Result<String, String> {
    let _guard = UPLOAD_LOCK.lock().unwrap_or_else(|e| e.into_inner());
    let record = crate::load_meeting_index()
        .into_iter()
        .find(|r| r.filepath == filepath)
        .ok_or("Local meeting not found")?;
    if record.status == "recording" {
        return Err("Meeting audio is still recording".into());
    }
    let token = {
        crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .session_token
            .clone()
            .ok_or("Sign in to upload your meeting")?
    };
    let base = crate::api::get_api_url();
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(90))
        .build()
        .map_err(|e| e.to_string())?;
    let account = response(
        client
            .get(format!("{}/auth/me", base))
            .bearer_auth(&token)
            .send()
            .map_err(|e| e.to_string())?,
    )?;
    let user = account
        .get("email")
        .and_then(Value::as_str)
        .ok_or("Account identity unavailable")?;
    if let Some(owner) = &record.owner_email {
        if owner != user {
            return Err("Sign in to the account that recorded this meeting".into());
        }
    } else if let Some(fingerprint) = &record.token_fingerprint {
        if fingerprint != &format!("{:x}", Sha256::digest(token.as_bytes())) {
            return Err("Recording account could not be verified after session changed".into());
        }
    }
    let path = Path::new(filepath);
    let files = BTreeMap::from([
        ("mic", path.with_extension("mic.ogg")),
        ("system", path.with_extension("loopback.ogg")),
        ("mixed", path.to_path_buf()),
    ]);
    let result = (|| {
        let existing = client
            .get(format!("{}/v1/meetings/jobs/{}", base, record.id))
            .bearer_auth(&token)
            .send()
            .map_err(|e| e.to_string())?;
        if existing.status().is_success() {
            let state = response(existing)?;
            let status = state["status"].as_str().unwrap_or("queued");
            if status != "uploading" {
                let local_status = if status == "failed" {
                    "processing_failed"
                } else {
                    status
                };
                crate::update_meeting_status(
                    filepath,
                    local_status,
                    state["error"]
                        .as_str()
                        .filter(|s| !s.is_empty())
                        .map(String::from),
                );
                if status == "completed" {
                    if let Some(meeting) = state.get("meeting") {
                        return Ok(meeting.to_string());
                    }
                }
                return Ok(state.to_string());
            }
        } else if existing.status().as_u16() != 404 {
            return Err(format!("Cannot query meeting ({})", existing.status()));
        }
        let mut manifest = BTreeMap::new();
        for (channel, path) in &files {
            manifest.insert(*channel, manifest_file(path)?);
        }
        let mut state = response(
            client
                .post(format!("{}/v1/meetings/jobs", base))
                .bearer_auth(&token)
                .json(&json!({"id":record.id,"title":record.title,"files":manifest,"language":record.language}))
                .send()
                .map_err(|e| e.to_string())?,
        )?;
        if state["status"] == "uploading" {
            crate::update_meeting_status(filepath, "uploading", None);
            for (channel, path) in &files {
                let mut file = File::open(path).map_err(|e| e.to_string())?;
                let size = file.metadata().map_err(|e| e.to_string())?.len();
                let mut offset = state["offsets"][channel].as_u64().unwrap_or(0);
                if offset > size {
                    return Err("Server upload offset exceeds local recording".into());
                }
                file.seek(SeekFrom::Start(offset))
                    .map_err(|e| e.to_string())?;
                let mut buffer = vec![0u8; 4 * 1024 * 1024];
                while offset < size {
                    let n = file.read(&mut buffer).map_err(|e| e.to_string())?;
                    if n == 0 {
                        return Err("Recording changed during upload".into());
                    }
                    let response = client
                        .put(format!(
                            "{}/v1/meetings/jobs/{}/audio/{}",
                            base, record.id, channel
                        ))
                        .bearer_auth(&token)
                        .header("Upload-Offset", offset.to_string())
                        .body(buffer[..n].to_vec())
                        .send()
                        .map_err(|e| e.to_string())?;
                    if !response.status().is_success() {
                        return Err(format!("Upload interrupted ({})", response.status()));
                    }
                    let next = response
                        .headers()
                        .get("Upload-Offset")
                        .and_then(|v| v.to_str().ok())
                        .and_then(|v| v.parse::<u64>().ok())
                        .ok_or("Missing upload acknowledgement")?;
                    if next != offset + n as u64 {
                        return Err("Invalid upload acknowledgement".into());
                    }
                    offset = next;
                }
            }
            state = response(
                client
                    .post(format!("{}/v1/meetings/jobs/{}/commit", base, record.id))
                    .bearer_auth(&token)
                    .send()
                    .map_err(|e| e.to_string())?,
            )?;
        }
        let status = state["status"].as_str().unwrap_or("queued");
        crate::update_meeting_status(
            filepath,
            status,
            state["error"]
                .as_str()
                .filter(|s| !s.is_empty())
                .map(String::from),
        );
        if status == "completed" {
            if let Some(meeting) = state.get("meeting") {
                return Ok(meeting.to_string());
            }
        }
        Ok(state.to_string())
    })();
    if let Err(error) = &result {
        crate::update_meeting_status(filepath, "failed", Some(error.clone()));
    }
    result
}
pub fn start_recovery() {
    std::thread::spawn(|| {
        loop {
            std::thread::sleep(Duration::from_secs(30));
            let active = {
                crate::CURRENT_MEETING_PATH
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone()
            };
            for record in crate::load_meeting_index() {
                if record.status == "completed"
                    || record.status == "uploaded"
                    || record.status == "processing_failed"
                    || record.status == "cancelled"
                {
                    continue;
                }
                if record.status == "recording" {
                    if active
                        .as_ref()
                        .map(|p| p.to_string_lossy() == record.filepath)
                        .unwrap_or(false)
                    {
                        continue;
                    };
                    crate::update_meeting_status(&record.filepath, "recorded", None);
                }
                // All retries query server state via the idempotent manifest; an interrupted
                // upload resumes at its acknowledged channel offsets.
                let _ = upload(&record.filepath);
            }
        }
    });
}

pub fn cache_account(token: &str, email: &str) {
    let path = crate::config::get_config_path().with_file_name("recording_account.json");
    let temporary = path.with_extension("tmp");
    let data =
        json!({"fingerprint":format!("{:x}",Sha256::digest(token.as_bytes())),"email":email})
            .to_string();
    if let Ok(mut file) = File::create(&temporary) {
        use std::io::Write;
        if file.write_all(data.as_bytes()).is_ok() && file.sync_all().is_ok() {
            drop(file);
            let _ = replace_file(&temporary, &path);
        }
    }
}
pub fn cached_account(token: &str) -> Option<String> {
    let path = crate::config::get_config_path().with_file_name("recording_account.json");
    let data: Value = serde_json::from_slice(&std::fs::read(path).ok()?).ok()?;
    if data["fingerprint"] == format!("{:x}", Sha256::digest(token.as_bytes())) {
        data["email"].as_str().map(String::from)
    } else {
        None
    }
}
pub fn retry(filepath: &str) -> Result<String, String> {
    let initial = upload(filepath)?;
    let state: Value = serde_json::from_str(&initial).map_err(|e| e.to_string())?;
    if state["status"] != "failed" {
        return Ok(initial);
    }
    let record = crate::load_meeting_index()
        .into_iter()
        .find(|r| r.filepath == filepath)
        .ok_or("Local meeting not found")?;
    let token = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .session_token
        .clone()
        .ok_or("Sign in to retry")?;
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(30))
        .build()
        .map_err(|e| e.to_string())?;
    let state = response(
        client
            .post(format!(
                "{}/v1/meetings/jobs/{}/retry",
                crate::api::get_api_url(),
                record.id
            ))
            .bearer_auth(token)
            .send()
            .map_err(|e| e.to_string())?,
    )?;
    crate::update_meeting_status(filepath, "queued", None);
    Ok(state.to_string())
}
