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
    EndOfStream,
}

pub struct StreamSessionHandle {
    pub tx: UnboundedSender<StreamAudioMessage>,
    pub rx_final: Receiver<Result<StreamFinalResult, String>>,
}

pub fn compute_text_delta(previous: &str, incoming: &str) -> String {
    let prev_trimmed = previous.trim();
    let inc_trimmed = incoming.trim();
    if prev_trimmed.is_empty() {
        return incoming.to_string();
    }
    if inc_trimmed.len() <= prev_trimmed.len() {
        return String::new();
    }

    if inc_trimmed.starts_with(prev_trimmed) {
        let prev_char_count = prev_trimmed.chars().count();
        let delta: String = inc_trimmed.chars().skip(prev_char_count).collect();
        return delta;
    }

    // Word-based suffix extraction
    let prev_words: Vec<&str> = prev_trimmed.split_whitespace().collect();
    let inc_words: Vec<&str> = inc_trimmed.split_whitespace().collect();

    if inc_words.len() > prev_words.len() {
        let mut match_count = 0;
        for (pw, iw) in prev_words.iter().zip(inc_words.iter()) {
            if pw.eq_ignore_ascii_case(iw) {
                match_count += 1;
            } else {
                break;
            }
        }
        if match_count == prev_words.len() {
            let mut res = String::new();
            for word in &inc_words[match_count..] {
                res.push(' ');
                res.push_str(word);
            }
            return res;
        }
    }

    String::new()
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
        let ws_stream = match connect_async(&ws_url).await {
            Ok((stream, _)) => stream,
            Err(e) => {
                let _ = final_tx.send(Err(format!("WebSocket connection failed: {}", e)));
                return;
            }
        };

        let (mut write, mut read) = ws_stream.split();

        // 1. Send start handshake frame
        let start_payload = serde_json::json!({
            "type": "start",
            "format": "opus",
            "sample_rate": 16000,
            "channels": 1,
            "enhance": enhance,
            "prompt": custom_prompt
        });

        if let Err(e) = write.send(Message::Text(start_payload.to_string())).await {
            let _ = final_tx.send(Err(format!("Failed to send start frame: {}", e)));
            return;
        }

        // 2. Stream audio frames from channel
        let write_task = async move {
            while let Some(msg) = audio_rx.recv().await {
                match msg {
                    StreamAudioMessage::AudioChunk(bytes) => {
                        if write.send(Message::Binary(bytes)).await.is_err() {
                            break;
                        }
                    }
                    StreamAudioMessage::EndOfStream => {
                        let _ = write.send(Message::Text(r#"{"type":"end_of_stream"}"#.to_string())).await;
                        break;
                    }
                }
            }
        };

        let read_task = async move {
            let mut emitted_text = String::new();
            let mut final_res: Option<StreamFinalResult> = None;
            while let Some(msg_res) = read.next().await {
                match msg_res {
                    Ok(Message::Text(text)) => {
                        if let Ok(event) = serde_json::from_str::<StreamEventResponse>(&text) {
                            if event.event_type == "interim" {
                                let delta = compute_text_delta(&emitted_text, &event.text);
                                if !delta.is_empty() {
                                    let normalized_delta = crate::vocab::get_vocab_manager().process_text(&delta);
                                    println!("[STREAM INTERIM] Emitting live text delta: {:?}", normalized_delta);
                                    let type_text = normalized_delta.clone();
                                    tauri::async_runtime::spawn_blocking(move || {
                                        crate::keyboard::simulate_typing(&type_text);
                                    });
                                    emitted_text.push_str(&delta);
                                }
                            } else if event.event_type == "final" {
                                let delta = compute_text_delta(&emitted_text, &event.text);
                                if !delta.is_empty() {
                                    let normalized_delta = crate::vocab::get_vocab_manager().process_text(&delta);
                                    println!("[STREAM FINAL] Emitting final remaining delta: {:?}", normalized_delta);
                                    let type_text = normalized_delta.clone();
                                    tauri::async_runtime::spawn_blocking(move || {
                                        crate::keyboard::simulate_typing(&type_text);
                                    });
                                    emitted_text.push_str(&delta);
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
    fn test_compute_text_delta() {
        assert_eq!(compute_text_delta("", "Hello"), "Hello");
        assert_eq!(compute_text_delta("Hello", "Hello world"), " world");
        assert_eq!(compute_text_delta("Hello world", "Hello world this is a test"), " this is a test");
        assert_eq!(compute_text_delta("Hello world", "Hello world"), "");
        assert_eq!(compute_text_delta("Hello world", "Hello"), "");
    }
}
