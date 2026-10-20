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
static RETRY_AT: Mutex<BTreeMap<String, std::time::Instant>> = Mutex::new(BTreeMap::new());
fn defer_retry(filepath: &str, response: &reqwest::blocking::Response) {
    if !response.status().is_success() {
        if let Some(seconds) = response
            .headers()
            .get("Retry-After")
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.parse::<u64>().ok())
        {
            if let Some(at) = std::time::Instant::now().checked_add(Duration::from_secs(seconds)) {
                RETRY_AT
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .insert(filepath.to_string(), at);
            }
        }
    }
}
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
fn response(response: reqwest::blocking::Response, filepath: &str) -> Result<Value, String> {
    defer_retry(filepath, &response);
    let status = response.status();
    let body = response.text().map_err(|e| e.to_string())?;
    if !status.is_success() {
        let message = crate::account_errors::message(status.as_u16(), &body);
        crate::account_errors::notify("Meetings", &message);
        return Err(message);
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
        filepath,
    )?;
    let user_id=account.get("id").and_then(Value::as_i64).filter(|id|*id>0).ok_or("Account identity unavailable")?;
    if !crate::recording_identity::owns(&record,user_id,&token) {return Err("Sign in to the account that recorded this meeting. Older recordings require their original session.".into());}
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
            let state = response(existing, filepath)?;
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
            return response(existing, filepath).map(|_| String::new());
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
            filepath,
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
                        defer_retry(filepath, &response);
                        let status = response.status().as_u16();
                        let body = response.text().unwrap_or_default();
                        let message = crate::account_errors::message(status, &body);
                        crate::account_errors::notify("Meetings", &message);
                        return Err(message);
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
                filepath,
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
    if result.is_ok() {
        RETRY_AT
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .remove(filepath);
    }
    if let Err(error) = &result {
        crate::update_meeting_status(filepath, "failed", Some(error.clone()));
    }
    result
}
pub fn start_recovery() {
    std::thread::spawn(|| {
        loop {
            std::thread::sleep(Duration::from_secs(30));
            crate::meeting_session::release_pending();
            let active = {
                crate::CURRENT_MEETING_PATH
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone()
            };
            if crate::meeting_session::is_starting(){continue;}
            for record in crate::load_meeting_index() {
                if crate::meeting_session::is_starting(){break;}
                if RETRY_AT
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .get(&record.filepath)
                    .map(|at| *at > std::time::Instant::now())
                    .unwrap_or(false)
                {
                    continue;
                }

                if record.status == "completed"
                    || record.status == "uploaded"
                    || record.status == "processing_failed"
                    || record.status == "cancelled"
                    || record.status == "capture_failed"
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
                    if let Err(message)=crate::recording_recovery::repair_meeting(Path::new(&record.filepath)) {
                        crate::update_meeting_status(&record.filepath,"capture_failed",Some(message));
                        crate::account_errors::notify("Meetings", "An interrupted recording could not be recovered automatically. Local audio files have been retained.");
                        continue;
                    }
                    crate::update_meeting_status(&record.filepath, "recorded", None);
                    crate::account_errors::notify("Meetings", "An interrupted meeting was recovered. Upload and processing will resume for its recording account.");
                }
                // All retries query server state via the idempotent manifest; an interrupted
                // upload resumes at its acknowledged channel offsets.
                let _ = upload(&record.filepath);
            }
        }
    });
}

pub fn cache_account(token: &str, email: &str, user_id:i64) {
    let path = crate::config::get_config_path().with_file_name("recording_account.json");
    let temporary = path.with_extension("tmp");
    let data =
        json!({"fingerprint":format!("{:x}",Sha256::digest(token.as_bytes())),"email":email,"id":user_id})
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
        filepath,
    )?;
    crate::update_meeting_status(filepath, "queued", None);
    Ok(state.to_string())
}

pub fn cached_account_id(token:&str)->Option<i64>{
 let path=crate::config::get_config_path().with_file_name("recording_account.json");
 let data:Value=serde_json::from_slice(&std::fs::read(path).ok()?).ok()?;
 if data["fingerprint"]==format!("{:x}",Sha256::digest(token.as_bytes())){data["id"].as_i64().filter(|id|*id>0)}else{None}
}
