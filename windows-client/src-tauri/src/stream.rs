use std::sync::mpsc::{Receiver, channel};
use futures_util::{SinkExt, StreamExt};
use serde::{Serialize, Deserialize};
use tokio::sync::mpsc::{unbounded_channel, UnboundedSender};
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::connect_async;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NegotiatedStreamMode {
    ContinuousStream,
    ChunkedVad,
}

#[derive(Serialize, Deserialize, Debug)]
pub struct StreamEventResponse {
    #[serde(rename = "type")]
    pub event_type: String, // "session_ready" | "interim" | "final" | "error"
    #[serde(default)]
    pub mode: Option<String>, // "continuous_stream" | "chunked_vad"
    #[serde(default)]
    pub text: String,
    #[serde(default)]
    pub log_id: Option<i64>,
    #[serde(default)]
    pub ack_token: Option<String>,
    #[serde(default)]
    pub latency_ms: Option<i64>,
    #[serde(default)]
    pub provider: Option<String>,
    #[serde(default)]
    pub error: Option<String>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct StreamFinalResult {
    pub text: String,
    pub log_id: Option<i64>,
    pub ack_token: Option<String>,
}

pub enum StreamAudioMessage {
    AudioChunk(Vec<u8>),
    CommitChunk,
    EndOfStream,
}

pub struct StreamSessionHandle {
    pub tx: UnboundedSender<StreamAudioMessage>,
    pub rx_final: Receiver<Result<StreamFinalResult, String>>,
    pub mode: NegotiatedStreamMode,
    task: tauri::async_runtime::JoinHandle<()>,
    interim: std::sync::Arc<std::sync::Mutex<String>>,
}

impl StreamSessionHandle {
    pub fn interim_text(&self) -> String {
        self.interim.lock().unwrap_or_else(|e| e.into_inner()).clone()
    }
}

impl Drop for StreamSessionHandle {
    fn drop(&mut self) {
        // Stop late preview updates from an aborted or timed-out recording.
        self.task.abort();
    }
}

/// Computes the diff between what is currently on screen (`currently_typed`) and `incoming_text`,
/// and returns the number of backspaces needed followed by the new text suffix to type.
pub fn compute_stream_diff(currently_typed: &str, incoming_text: &str) -> (usize, String) {
    if currently_typed.is_empty() {
        return (0, incoming_text.to_string());
    }
    if incoming_text.is_empty() {
        return (0, String::new());
    }

    let cur_chars: Vec<char> = currently_typed.chars().collect();
    let inc_chars: Vec<char> = incoming_text.chars().collect();

    // Find Longest Common Prefix (LCP)
    let mut lcp_len = 0;
    while lcp_len < cur_chars.len() && lcp_len < inc_chars.len() && cur_chars[lcp_len] == inc_chars[lcp_len] {
        lcp_len += 1;
    }

    let backspaces = cur_chars.len() - lcp_len;
    let new_suffix: String = inc_chars[lcp_len..].iter().collect();

    (backspaces, new_suffix)
}

/// Converts raw 16kHz 16-bit mono PCM bytes to valid WAV file bytes
pub fn pcm_16k_to_wav(pcm_bytes: &[u8]) -> Vec<u8> {
    let mut header = Vec::with_capacity(44 + pcm_bytes.len());
    let data_len = pcm_bytes.len() as u32;
    let total_len = 36 + data_len;

    header.extend_from_slice(b"RIFF");
    header.extend_from_slice(&total_len.to_le_bytes());
    header.extend_from_slice(b"WAVE");
    header.extend_from_slice(b"fmt ");
    header.extend_from_slice(&16u32.to_le_bytes()); // Subchunk1Size
    header.extend_from_slice(&1u16.to_le_bytes());  // AudioFormat (PCM = 1)
    header.extend_from_slice(&1u16.to_le_bytes());  // Channels (1 = Mono)
    header.extend_from_slice(&16000u32.to_le_bytes()); // SampleRate (16000)
    header.extend_from_slice(&32000u32.to_le_bytes()); // ByteRate (16000 * 1 * 2)
    header.extend_from_slice(&2u16.to_le_bytes());  // BlockAlign (1 * 2)
    header.extend_from_slice(&16u16.to_le_bytes()); // BitsPerSample (16)
    header.extend_from_slice(b"data");
    header.extend_from_slice(&data_len.to_le_bytes());
    header.extend_from_slice(pcm_bytes);

    header
}

/// Converts HTTP/HTTPS API URL to WS/WSS URL
pub fn get_stream_ws_url(api_url: &str, token: &str) -> String {
    let base = api_url.trim_end_matches('/');
    let ws_base = if base.starts_with("https://") {
        base.replacen("https://", "wss://", 1)
    } else if base.starts_with("http://") {
        base.replacen("http://", "ws://", 1)
    } else {
        format!("ws://{}", base)
    };
    format!("{}/v1/stream?token={}", ws_base, token)
}

/// Starts an active WebSocket streaming dictation session.
pub fn start_streaming_session(
    api_url: String,
    token: String,
    enhance: bool,
    custom_prompt: String,
    language: String,
    context: crate::shortcuts::RecordingContext,
) -> Result<StreamSessionHandle, String> {
    let (audio_tx, mut audio_rx) = unbounded_channel::<StreamAudioMessage>();
    let (final_tx, final_rx) = channel::<Result<StreamFinalResult, String>>();
    let (mode_tx, mode_rx) = channel::<NegotiatedStreamMode>();

    let ws_url = get_stream_ws_url(&api_url, &token);

    let interim = std::sync::Arc::new(std::sync::Mutex::new(String::new()));
    let interim_reader = interim.clone();
    let task = tauri::async_runtime::spawn(async move {
        let _context = context;
        println!("[STREAM CLIENT] Connecting to transcription WebSocket (language: {})", language);
        let ws_stream = match connect_async(&ws_url).await {
            Ok((stream, _)) => {
                println!("[STREAM CLIENT] WebSocket connection established successfully!");
                stream
            }
            Err(e) => {
                eprintln!("[STREAM CLIENT ERROR] WebSocket connection failed: {}", e);
                let _ = final_tx.send(Err(format!("WebSocket connection failed: {}", e)));
                return;
            }
        };

        let (mut write, mut read) = ws_stream.split();

        // 1. Send start handshake frame with client capabilities
        let start_payload = serde_json::json!({
            "type": "start",
            "format": "ogg_opus",
            "sample_rate": 16000,
            "channels": 1,
            "enhance": enhance,
            "prompt": custom_prompt,
            "language": language,
            "capabilities": ["continuous_stream", "chunked_vad"]
        });

        if let Err(e) = write.send(Message::Text(start_payload.to_string())).await {
            eprintln!("[STREAM CLIENT ERROR] Failed to send start frame: {}", e);
            let _ = final_tx.send(Err(format!("Failed to send start frame: {}", e)));
            return;
        }

        let last_chunk_sent_ms = std::sync::Arc::new(std::sync::atomic::AtomicU64::new(0));
        let chunk_count = std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0));
        let session_start = std::time::Instant::now();

        let last_chunk_sent_writer = last_chunk_sent_ms.clone();
        let chunk_count_writer = chunk_count.clone();

        // 2. Stream audio frames from channel
        let write_task = async move {
            while let Some(msg) = audio_rx.recv().await {
                match msg {
                    StreamAudioMessage::AudioChunk(bytes) => {
                        let sent_ms = session_start.elapsed().as_millis() as u64;
                        last_chunk_sent_writer.store(sent_ms, std::sync::atomic::Ordering::Relaxed);
                        chunk_count_writer.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
                        if write.send(Message::Binary(bytes)).await.is_err() {
                            break;
                        }
                    }
                    StreamAudioMessage::CommitChunk => {
                        let _ = write.send(Message::Text(r#"{"type":"commit"}"#.to_string())).await;
                    }
                    StreamAudioMessage::EndOfStream => {
                        println!("[STREAM CLIENT] Sending end_of_stream message to server.");
                        let _ = write.send(Message::Text(r#"{"type":"end_of_stream"}"#.to_string())).await;
                        break;
                    }
                }
            }
        };

        let last_chunk_sent_reader = last_chunk_sent_ms.clone();
        let chunk_count_reader = chunk_count.clone();

        let read_task = async move {
            let mut final_res: Option<StreamFinalResult> = None;
            let mut mode_sent = false;

            while let Some(msg_res) = read.next().await {
                match msg_res {
                    Ok(Message::Text(text)) => {
                        if let Ok(event) = serde_json::from_str::<StreamEventResponse>(&text) {
                            if event.event_type == "session_ready" {
                                let negotiated = if event.mode.as_deref() == Some("continuous_stream") {
                                    NegotiatedStreamMode::ContinuousStream
                                } else {
                                    NegotiatedStreamMode::ChunkedVad
                                };
                                println!("[STREAM CLIENT] Server confirmed session mode: {:?} (provider: {:?})", negotiated, event.provider);
                                let _ = mode_tx.send(negotiated);
                                mode_sent = true;
                                continue;
                            }

                            if !mode_sent {
                                let _ = mode_tx.send(NegotiatedStreamMode::ChunkedVad);
                                mode_sent = true;
                            }

                            if event.event_type == "interim" {
                                if !event.text.is_empty() {
                                    let sent_ms = last_chunk_sent_reader.load(std::sync::atomic::Ordering::Relaxed);
                                    let now_ms = session_start.elapsed().as_millis() as u64;
                                    let rtt_ms = if sent_ms > 0 && now_ms >= sent_ms {
                                        now_ms - sent_ms
                                    } else {
                                        0
                                    };
                                    let current_chunk = chunk_count_reader.load(std::sync::atomic::Ordering::Relaxed);
                                    println!("[STREAM E2E LATENCY] Chunk #{} -> Interim Return: {} ms", current_chunk, rtt_ms);
                                    crate::win32::set_preview_text(&event.text);
                                    *interim_reader.lock().unwrap_or_else(|e| e.into_inner()) = event.text;
                                }
                            } else if event.event_type == "final" {
                                println!("[STREAM FINAL RECEIVED] Final result received");
                                crate::win32::set_preview_text(&event.text);

                                final_res = Some(StreamFinalResult {
                                    text: event.text,
                                    log_id: event.log_id,
                                    ack_token: event.ack_token,
                                });
                                break;
                            } else if event.event_type == "error" {
                                return Err(event.error.unwrap_or_else(|| "Unknown server streaming error".to_string()));
                            }
                        }
                    }
                    Ok(Message::Close(_)) => break,
                    Err(e) => return Err(format!("Stream read error: {}", e)),
                    _ => {}
                }
            }
            final_res.ok_or_else(|| "Stream closed without final transcript".to_string())
        };

        // Run write loop and read loop concurrently
        let (_, read_res) = futures_util::join!(write_task, read_task);
        let _ = final_tx.send(read_res);
    });

    let negotiated_mode = mode_rx.recv_timeout(std::time::Duration::from_millis(2500)).unwrap_or(NegotiatedStreamMode::ChunkedVad);
    println!("[STREAM CLIENT] Active negotiated stream mode: {:?}", negotiated_mode);

    Ok(StreamSessionHandle {
        tx: audio_tx,
        rx_final: final_rx,
        mode: negotiated_mode,
        task,
        interim,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_compute_stream_diff() {
        // 1. Initial text typing
        let (bs, to_type) = compute_stream_diff("", "Welcome");
        assert_eq!(bs, 0);
        assert_eq!(to_type, "Welcome");

        // 2. Normal forward append
        let (bs, to_type) = compute_stream_diff("Welcome", "Welcome to the live");
        assert_eq!(bs, 0);
        assert_eq!(to_type, " to the live");

        // 3. Trailing word revision
        let (bs, to_type) = compute_stream_diff("Welcome stand", "Welcome fast and completely");
        assert_eq!(bs, 5); // Deletes "stand" (5 chars)
        assert_eq!(to_type, "fast and completely");

        // 4. Identical text (no-op)
        let (bs, to_type) = compute_stream_diff("Welcome to the live", "Welcome to the live");
        assert_eq!(bs, 0);
        assert_eq!(to_type, "");

        // 5. Punctuation addition
        let (bs, to_type) = compute_stream_diff("Welcome to the live", "Welcome to the live.");
        assert_eq!(bs, 0);
        assert_eq!(to_type, ".");

        // 6. Final LLM capital polish
        let (bs, to_type) = compute_stream_diff("welcome to the live", "Welcome to the live.");
        assert_eq!(bs, 19);
        assert_eq!(to_type, "Welcome to the live.");
    }
}
