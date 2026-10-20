//! One account-bound, ephemeral disk-backed WAV. IPC carries a random ticket,
//! never audio/base64; protocol reads are capped independently of meeting length.
use std::{
    fs::{File, OpenOptions},
    io::{Read, Seek, SeekFrom},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU64, Ordering},
        Mutex,
    },
};
use tauri::http::{Request, Response};
static ACTIVE: Mutex<Option<Playback>> = Mutex::new(None);
static GENERATION: AtomicU64 = AtomicU64::new(0);
const CHUNK: u64 = 1024 * 1024;
struct Playback {
    ticket: String,
    token: String,
    directory: PathBuf,
    wav: PathBuf,
    stop: std::sync::Arc<std::sync::atomic::AtomicBool>,
    address: std::net::SocketAddr,
}
impl Drop for Playback {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
        let _ = std::net::TcpStream::connect_timeout(
            &self.address,
            std::time::Duration::from_millis(100),
        );
        let _ = std::fs::remove_dir_all(&self.directory);
    }
}
pub fn generation() -> u64 {
    GENERATION.load(Ordering::SeqCst)
}
pub fn clear() {
    GENERATION.fetch_add(1, Ordering::SeqCst);
    ACTIVE.lock().unwrap_or_else(|e| e.into_inner()).take();
}
fn root() -> PathBuf {
    crate::config::get_config_path().with_file_name("playback-cache")
}
pub fn cleanup_startup() {
    let _ = std::fs::remove_dir_all(root());
}
pub fn prepare(
    local: Option<PathBuf>,
    base: &str,
    meeting_id: i64,
    token: String,
    generation: u64,
) -> Result<String, String> {
    let directory = root().join(uuid::Uuid::new_v4().simple().to_string());
    std::fs::create_dir_all(&directory).map_err(|_| "Playback cache unavailable")?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(root(), std::fs::Permissions::from_mode(0o700))
            .map_err(|_| "Cannot protect playback cache")?;
        std::fs::set_permissions(&directory, std::fs::Permissions::from_mode(0o700))
            .map_err(|_| "Cannot protect playback cache")?;
    }
    let listener = std::net::TcpListener::bind("127.0.0.1:0")
        .map_err(|_| "Native playback endpoint unavailable")?;
    let address = listener
        .local_addr()
        .map_err(|_| "Native playback endpoint unavailable")?;
    let stop = std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false));
    let playback = Playback {
        stop: stop.clone(),
        address,
        ticket: uuid::Uuid::new_v4().simple().to_string(),
        token: token.clone(),
        wav: directory.join("audio.wav"),
        directory,
    };
    let source = match local {
        Some(path) => path,
        None => {
            let path = playback.directory.join("cloud.ogg");
            let response = reqwest::blocking::Client::builder()
                .timeout(std::time::Duration::from_secs(20 * 60))
                .build()
                .map_err(|_| "Playback download unavailable")?
                .get(format!("{}/meetings/{}/audio", base, meeting_id))
                .bearer_auth(&token)
                .send()
                .map_err(|_| "Cloud recording unavailable")?;
            if !response.status().is_success()
                || !response
                    .headers()
                    .get(reqwest::header::CONTENT_TYPE)
                    .and_then(|v| v.to_str().ok())
                    .is_some_and(|v| v.starts_with("audio/ogg"))
            {
                return Err("Cloud recording unavailable for this account".into());
            }
            let expected = response.content_length();
            let mut file = private_file(&path)?;
            let mut response = response.take(2_000_000_001);
            let mut size = 0u64;
            let mut buffer = [0u8; 65536];
            loop {
                if generation != self::generation() {
                    return Err("Playback cancelled".into());
                }
                let n = response
                    .read(&mut buffer)
                    .map_err(|_| "Playback download interrupted")?;
                if n == 0 {
                    break;
                }
                use std::io::Write;
                file.write_all(&buffer[..n])
                    .map_err(|_| "Playback cache is full")?;
                size += n as u64;
            }
            if size > 2_000_000_000 || expected.is_some_and(|n| n != size) {
                return Err("Playback download incomplete".into());
            }
            path
        }
    };
    decode(&source, &playback.wav, Some(generation))?;
    if generation != self::generation()
        || crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .session_token
            .as_deref()
            != Some(token.as_str())
    {
        return Err("Playback cancelled".into());
    }
    let ticket = playback.ticket.clone();
    let mut active = ACTIVE.lock().unwrap_or_else(|e| e.into_inner());
    if generation != self::generation() {
        return Err("Playback cancelled".into());
    }
    *active = Some(playback);
    drop(active);
    std::thread::spawn(move || serve(listener, stop));
    Ok(format!("http://{}/{}", address, ticket))
}
fn private_file(path: &Path) -> Result<File, String> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    options
        .open(path)
        .map_err(|_| "Cannot create playback cache".into())
}
fn decode(source: &Path, destination: &Path, generation: Option<u64>) -> Result<(), String> {
    let mut reader =
        ogg::reading::PacketReader::new(File::open(source).map_err(|_| "Recording unavailable")?);
    let mut decoder =
        opus::Decoder::new(16000, opus::Channels::Mono).map_err(|_| "Audio decoder unavailable")?;
    let mut writer = hound::WavWriter::new(
        std::io::BufWriter::new(private_file(destination)?),
        hound::WavSpec {
            channels: 1,
            sample_rate: 16000,
            bits_per_sample: 16,
            sample_format: hound::SampleFormat::Int,
        },
    )
    .map_err(|_| "Cannot create playback audio")?;
    let mut samples = [0i16; 5760];
    let mut count = 0u64;
    while let Some(packet) = reader
        .read_packet()
        .map_err(|_| "Recording contains damaged audio")?
    {
        if generation.is_some_and(|g| g != self::generation()) {
            return Err("Playback cancelled".into());
        }
        if packet.data.starts_with(b"OpusHead") || packet.data.starts_with(b"OpusTags") {
            continue;
        }
        let n = decoder
            .decode(&packet.data, &mut samples, false)
            .map_err(|_| "Recording contains damaged audio")?;
        count += n as u64;
        if count > 16000 * 60 * 60 * 4 + 16000 {
            return Err("Recording exceeds the four-hour limit".into());
        }
        for &sample in &samples[..n] {
            writer
                .write_sample(sample)
                .map_err(|_| "Playback cache is full")?;
        }
    }
    if count == 0 {
        return Err("Recording contains no playable audio".into());
    }
    writer
        .finalize()
        .map_err(|_| "Cannot finish playback cache".into())
}
fn range(header: Option<&str>, size: u64) -> Option<(u64, u64)> {
    if size == 0 {
        return None;
    }
    let (start, end) = if let Some(header) = header {
        let (start, end) = header.strip_prefix("bytes=")?.split_once('-')?;
        if start.is_empty() {
            let suffix = end.parse::<u64>().ok()?;
            if suffix == 0 {
                return None;
            }
            (size.saturating_sub(suffix), size - 1)
        } else {
            (
                start.parse().ok()?,
                if end.is_empty() {
                    size - 1
                } else {
                    end.parse::<u64>().ok()?.min(size - 1)
                },
            )
        }
    } else {
        (0, size - 1)
    };
    if start >= size || end < start {
        return None;
    }
    Some((start, end.min(start + CHUNK - 1)))
}
pub fn respond(request: Request<Vec<u8>>) -> Response<Vec<u8>> {
    fn error(status: u16) -> Response<Vec<u8>> {
        Response::builder()
            .status(status)
            .header("Access-Control-Allow-Origin", "*")
            .body(Vec::new())
            .unwrap()
    }
    if request.method() == "OPTIONS" {
        return Response::builder()
            .status(204)
            .header("Access-Control-Allow-Origin", "*")
            .header("Access-Control-Allow-Headers", "Range")
            .header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
            .body(Vec::new())
            .unwrap();
    }
    if request.method() != "GET" && request.method() != "HEAD" {
        return error(405);
    }
    let active = ACTIVE.lock().unwrap_or_else(|e| e.into_inner());
    let Some(audio) = active.as_ref() else {
        return error(404);
    };
    let token = crate::get_active_config()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .session_token
        .clone();
    read_audio(request, audio, token.as_deref())
}
fn read_audio(
    request: Request<Vec<u8>>,
    audio: &Playback,
    token: Option<&str>,
) -> Response<Vec<u8>> {
    fn error(status: u16) -> Response<Vec<u8>> {
        Response::builder()
            .status(status)
            .header("Access-Control-Allow-Origin", "*")
            .body(Vec::new())
            .unwrap()
    }
    if request.uri().path().trim_start_matches('/') != audio.ticket
        || token != Some(audio.token.as_str())
    {
        return error(403);
    }
    let Ok(mut file) = File::open(&audio.wav) else {
        return error(404);
    };
    let Ok(meta) = file.metadata() else {
        return error(404);
    };
    let size = meta.len();
    let Some((start, end)) = range(
        request.headers().get("Range").and_then(|h| h.to_str().ok()),
        size,
    ) else {
        return Response::builder()
            .status(416)
            .header("Content-Range", format!("bytes */{}", size))
            .body(Vec::new())
            .unwrap();
    };
    let head = request.method() == "HEAD";
    let mut bytes = Vec::new();
    if !head
        && (file.seek(SeekFrom::Start(start)).is_err()
            || file.take(end - start + 1).read_to_end(&mut bytes).is_err())
    {
        return error(500);
    }
    let mut result = Response::builder()
        .status(if head { 200 } else { 206 })
        .header("Content-Type", "audio/wav")
        .header("Cache-Control", "no-store")
        .header("Access-Control-Allow-Origin", "*")
        .header(
            "Access-Control-Expose-Headers",
            "Content-Range, Accept-Ranges, Content-Length",
        )
        .header("Accept-Ranges", "bytes")
        .header("Content-Length", if head { size } else { end - start + 1 });
    if !head {
        result = result.header("Content-Range", format!("bytes {}-{}/{}", start, end, size));
    }
    result.body(bytes).unwrap()
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn range_reads_are_bounded_for_four_hour_wav() {
        let size = 44 + 4 * 60 * 60 * 16000 * 2;
        assert_eq!(range(Some("bytes=0-"), size), Some((0, CHUNK - 1)));
        assert_eq!(
            range(Some("bytes=450000000-"), size),
            Some((450000000, 451048575))
        );
        assert_eq!(range(Some("bytes=-44"), size), Some((size - 44, size - 1)));
        for header in ["bytes=9-8", "bytes=-0", "bytes=0-1,4-5", "invalid"] {
            assert!(range(Some(header), size).is_none());
        }
    }
    #[test]
    fn disk_decoder_preserves_audio_without_whole_file_pcm() {
        let directory = std::env::temp_dir().join(uuid::Uuid::new_v4().to_string());
        std::fs::create_dir(&directory).unwrap();
        let source = directory.join("audio.ogg");
        let output = directory.join("audio.wav");
        std::fs::write(
            &source,
            crate::audio_processing::encode_opus_ogg(&vec![1000; 16000 * 3], 16000, 1).unwrap(),
        )
        .unwrap();
        decode(&source, &output, None).unwrap();
        let reader = hound::WavReader::open(&output).unwrap();
        assert_eq!(reader.spec().sample_rate, 16000);
        assert!(reader.duration() >= 16000 * 3);
        std::fs::remove_dir_all(directory).unwrap();
    }
}

/// No microphone, network, or user recording: exercise the actual WebKit/
/// WebView2 protocol with synthetic audio, including a seek and teardown.
pub fn run_diagnostic() {
    let app = tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![diagnostic_result])
        .setup(|app| {
            let handle = app.handle().clone();
            let _ = crate::TAURI_APP.set(handle.clone());
            let token = crate::get_active_config()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .session_token
                .clone()
                .ok_or("Playback diagnostic requires an existing sign-in")?;
            let source = std::env::temp_dir()
                .join(format!("speektome-synthetic-{}.ogg", uuid::Uuid::new_v4()));
            let samples: Vec<i16> = (0..16000 * 90)
                .map(|i| ((i as f64 * 0.08).sin() * 1000.0) as i16)
                .collect();
            std::fs::write(
                &source,
                crate::audio_processing::encode_opus_ogg(&samples, 16000, 1)
                    .map_err(|_| "Cannot encode diagnostic")?,
            )?;
            clear();
            let result = prepare(Some(source.clone()), "", 0, token, generation());
            let _ = std::fs::remove_file(source);
            let ticket = result?;
            tauri::WebviewWindowBuilder::new(
                app,
                "playback-check",
                tauri::WebviewUrl::App("playback-check.html".into()),
            )
            .title("SpeekTo.Me playback check")
            .inner_size(400.0, 180.0)
            .initialization_script(format!(
                "window.playbackTicket={};",
                serde_json::to_string(&ticket)?
            ))
            .build()?;
            std::thread::spawn(move || {
                std::thread::sleep(std::time::Duration::from_secs(12));
                let _ = std::fs::write("/tmp/speektome-playback-check-timeout", b"timeout");
                crate::lifecycle::quit(&handle);
            });
            Ok(())
        })
        .build(crate::desktop_context())
        .expect("Playback diagnostic setup failed");
    app.run(|_, _| {});
}
#[tauri::command]
fn diagnostic_result(app: tauri::AppHandle, passed: bool, duration: f64, seek: f64) {
    let report = serde_json::json!({"passed":passed,"duration":duration,"seek":seek,"read_buffer_bytes":65536});
    let path = std::env::temp_dir().join("speektome-playback-check.json");
    let _ = std::fs::write(path, report.to_string());
    clear();
    crate::lifecycle::quit(&app);
}

// HTTP is understood by each platform's native media stack. No settings data,
// filesystem paths, API bearer token, or arbitrary file routes are exposed.
fn serve(listener: std::net::TcpListener, stop: std::sync::Arc<std::sync::atomic::AtomicBool>) {
    let connections = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    for connection in listener.incoming() {
        if stop.load(Ordering::SeqCst) {
            break;
        }
        let Ok(socket) = connection else { break };
        if connections.load(Ordering::SeqCst) >= 4 {
            continue;
        }
        connections.fetch_add(1, Ordering::SeqCst);
        let stop = stop.clone();
        let connections = connections.clone();
        std::thread::spawn(move || {
            serve_connection(socket, &stop);
            connections.fetch_sub(1, Ordering::SeqCst);
        });
    }
}
fn serve_connection(mut socket: std::net::TcpStream, stop: &std::sync::atomic::AtomicBool) {
    use std::io::{BufRead, Write};
    let _ = socket.set_read_timeout(Some(std::time::Duration::from_millis(500)));
    let _ = socket.set_write_timeout(Some(std::time::Duration::from_millis(100)));
    let Ok(copy) = socket.try_clone() else { return };
    let mut reader = std::io::BufReader::new(copy.take(8192));
    let mut line = String::new();
    if reader.read_line(&mut line).is_err() {
        return;
    }
    let words: Vec<_> = line.split_whitespace().collect();
    if words.len() != 3
        || !matches!(words[0], "GET" | "HEAD" | "OPTIONS")
        || !words[1].starts_with('/')
        || words[1].starts_with("//")
    {
        return;
    }
    let mut request = Request::builder().method(words[0]).uri(words[1]);
    let mut valid = false;
    let header_start = std::time::Instant::now();
    for _ in 0..60 {
        if header_start.elapsed() > std::time::Duration::from_millis(500) {
            return;
        }
        let mut header = String::new();
        if reader.read_line(&mut header).is_err() || header.is_empty() {
            break;
        }
        if header == "\r\n" {
            valid = true;
            break;
        }
        if let Some((name, value)) = header.split_once(':') {
            if name.eq_ignore_ascii_case("Range") {
                request = request.header("Range", value.trim());
            }
        }
    }
    if !valid || stop.load(Ordering::SeqCst) {
        return;
    }
    let Ok(request) = request.body(Vec::new()) else {
        return;
    };
    if request.method() == "GET" && stream_audio(&mut socket, &request, stop) {
        return;
    }
    let response = respond(request);
    let mut headers = format!(
        "HTTP/1.1 {} {}\r\nConnection: close\r\n",
        response.status().as_u16(),
        response.status().canonical_reason().unwrap_or("Response")
    );
    for (name, value) in response.headers() {
        if let Ok(value) = value.to_str() {
            headers.push_str(&format!("{}: {}\r\n", name, value));
        }
    }
    headers.push_str("\r\n");
    let _ = socket
        .write_all(headers.as_bytes())
        .and_then(|_| socket.write_all(response.body()));
}

fn stream_audio(
    socket: &mut std::net::TcpStream,
    request: &Request<Vec<u8>>,
    stop: &std::sync::atomic::AtomicBool,
) -> bool {
    use std::io::Write;
    let selected = {
        let active = ACTIVE.lock().unwrap_or_else(|e| e.into_inner());
        let Some(audio) = active.as_ref() else {
            return false;
        };
        if request.uri().path().trim_start_matches('/') != audio.ticket
            || crate::get_active_config()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .session_token
                .as_deref()
                != Some(audio.token.as_str())
        {
            return false;
        }
        let Ok(file) = File::open(&audio.wav) else {
            return false;
        };
        (file, audio.token.clone())
    };
    let (mut file, token) = selected;
    let Ok(metadata) = file.metadata() else {
        return false;
    };
    let size = metadata.len();
    let header = request
        .headers()
        .get("Range")
        .and_then(|value| value.to_str().ok());
    let Some((start, end)) = full_range(header, size) else {
        return false;
    };
    if file.seek(SeekFrom::Start(start)).is_err() {
        return false;
    }
    let mut headers=format!("HTTP/1.1 {}\r\nContent-Type: audio/wav\r\nCache-Control: no-store\r\nAccess-Control-Allow-Origin: *\r\nAccept-Ranges: bytes\r\nContent-Length: {}\r\nConnection: close\r\n",if header.is_some(){"206 Partial Content"}else{"200 OK"},end-start+1);
    if header.is_some() {
        headers.push_str(&format!(
            "Content-Range: bytes {}-{}/{}\r\n",
            start, end, size
        ));
    }
    headers.push_str("\r\n");
    if socket.write_all(headers.as_bytes()).is_err() {
        return true;
    }
    let mut buffer = [0u8; 65536];
    let mut remaining = end - start + 1;
    while remaining > 0 && !stop.load(Ordering::SeqCst) {
        if crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .session_token
            .as_deref()
            != Some(token.as_str())
        {
            break;
        }
        let count = remaining.min(buffer.len() as u64) as usize;
        let Ok(n) = file.read(&mut buffer[..count]) else {
            break;
        };
        if n == 0 {
            break;
        }
        let mut written = 0;
        while written < n && !stop.load(Ordering::SeqCst) {
            if crate::get_active_config()
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .session_token
                .as_deref()
                != Some(token.as_str())
            {
                return true;
            }
            match socket.write(&buffer[written..n]) {
                Ok(0) => return true,
                Ok(count) => written += count,
                Err(error)
                    if matches!(
                        error.kind(),
                        std::io::ErrorKind::WouldBlock
                            | std::io::ErrorKind::TimedOut
                            | std::io::ErrorKind::Interrupted
                    ) =>
                {
                    continue
                }
                Err(_) => return true,
            }
        }
        remaining -= n as u64;
    }
    true
}
fn full_range(header: Option<&str>, size: u64) -> Option<(u64, u64)> {
    if size == 0 {
        return None;
    }
    if let Some(value) = header {
        let (start, end) = value.strip_prefix("bytes=")?.split_once('-')?;
        let (start, end) = if start.is_empty() {
            let count = end.parse::<u64>().ok()?;
            if count == 0 {
                return None;
            }
            (size.saturating_sub(count), size - 1)
        } else {
            (
                start.parse::<u64>().ok()?,
                if end.is_empty() {
                    size - 1
                } else {
                    end.parse::<u64>().ok()?.min(size - 1)
                },
            )
        };
        if start >= size || end < start {
            return None;
        }
        Some((start, end))
    } else {
        Some((0, size - 1))
    }
}
