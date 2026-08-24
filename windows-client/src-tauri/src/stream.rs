use std::sync::mpsc::{Receiver, channel};
use futures_util::{SinkExt, StreamExt};
use serde::{Serialize, Deserialize};
use tokio::sync::mpsc::{unbounded_channel, UnboundedSender};
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::connect_async;

#[derive(Serialize, Deserialize, Debug)]
pub struct StreamEventResponse {
    #[serde(rename = "type")]
    pub event_type: String, // "interim" | "final" | "error"
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
) -> Result<StreamSessionHandle, String> {
    let (audio_tx, mut audio_rx) = unbounded_channel::<StreamAudioMessage>();
    let (final_tx, final_rx) = channel::<Result<StreamFinalResult, String>>();

    let ws_url = get_stream_ws_url(&api_url, &token);

    tauri::async_runtime::spawn(async move {
        println!("[STREAM CLIENT] Connecting to WebSocket: {}", ws_url);
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

        // 1. Send start handshake frame
        let start_payload = serde_json::json!({
            "type": "start",
            "format": "ogg_opus",
            "sample_rate": 16000,
            "channels": 1,
            "enhance": enhance,
            "prompt": custom_prompt
        });

        if let Err(e) = write.send(Message::Text(start_payload.to_string())).await {
            eprintln!("[STREAM CLIENT ERROR] Failed to send start frame: {}", e);
            let _ = final_tx.send(Err(format!("Failed to send start frame: {}", e)));
            return;
        }

        // Create debug recordings folder for testing
        let timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0);
        let debug_dir = std::env::current_dir()
            .unwrap_or_else(|_| std::path::PathBuf::from("."))
            .join("debug_stream_recordings")
            .join(format!("session_{}", timestamp));
        let _ = std::fs::create_dir_all(&debug_dir);
        println!("[STREAM DEBUG RECORDINGS] Saving live chunk recordings to: {}", debug_dir.display());

        let mut chunk_index: usize = 0;

        // 2. Stream audio frames from channel
        let write_task = async move {
            while let Some(msg) = audio_rx.recv().await {
                match msg {
                    StreamAudioMessage::AudioChunk(bytes) => {
                        chunk_index += 1;
                        let chunk_path = debug_dir.join(format!("chunk_{:03}.ogg", chunk_index));
                        let _ = std::fs::write(&chunk_path, &bytes);

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

        let read_task = async move {
            let mut currently_typed = String::new();
            let mut final_res: Option<StreamFinalResult> = None;
            while let Some(msg_res) = read.next().await {
                match msg_res {
                    Ok(Message::Text(text)) => {
                        if let Ok(event) = serde_json::from_str::<StreamEventResponse>(&text) {
                            if event.event_type == "interim" {
                                if !event.text.is_empty() {
                                    let normalized_text = crate::vocab::get_vocab_manager().process_text(&event.text);
                                    let (backspaces, new_suffix) = compute_stream_diff(&currently_typed, &normalized_text);
                                    if backspaces > 0 || !new_suffix.is_empty() {
                                        currently_typed = normalized_text.clone();
                                        let type_text = new_suffix.clone();
                                        println!("[STREAM INTERIM] Backspaces: {}, Typing: {:?}", backspaces, type_text);
                                        tauri::async_runtime::spawn_blocking(move || {
                                            if backspaces > 0 {
                                                crate::keyboard::simulate_backspaces(backspaces);
                                            }
                                            if !type_text.is_empty() {
                                                crate::keyboard::simulate_typing(&type_text);
                                            }
                                        });
                                    }
                                }
                            } else if event.event_type == "final" {
                                if !event.text.is_empty() {
                                    let normalized_text = crate::vocab::get_vocab_manager().process_text(&event.text);
                                    let (backspaces, new_suffix) = compute_stream_diff(&currently_typed, &normalized_text);
                                    if backspaces > 0 || !new_suffix.is_empty() {
                                        let type_text = new_suffix.clone();
                                        println!("[STREAM FINAL] Backspaces: {}, Final typing: {:?}", backspaces, type_text);
                                        tauri::async_runtime::spawn_blocking(move || {
                                            if backspaces > 0 {
                                                crate::keyboard::simulate_backspaces(backspaces);
                                            }
                                            if !type_text.is_empty() {
                                                crate::keyboard::simulate_typing(&type_text);
                                            }
                                        });
                                    }
                                }

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

    Ok(StreamSessionHandle {
        tx: audio_tx,
        rx_final: final_rx,
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
