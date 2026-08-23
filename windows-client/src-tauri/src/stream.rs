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

fn normalize_word(w: &str) -> String {
    w.chars()
        .filter(|c| c.is_alphanumeric())
        .collect::<String>()
        .to_lowercase()
}

pub fn compute_text_delta(previous: &str, incoming: &str) -> String {
    let prev_trimmed = previous.trim();
    let inc_trimmed = incoming.trim();
    if prev_trimmed.is_empty() {
        return incoming.to_string();
    }
    if inc_trimmed.len() <= prev_trimmed.len() && !inc_trimmed.starts_with(prev_trimmed) {
        return String::new();
    }

    if inc_trimmed.starts_with(prev_trimmed) {
        let prev_char_count = prev_trimmed.chars().count();
        let delta: String = inc_trimmed.chars().skip(prev_char_count).collect();
        return delta;
    }

    let prev_words: Vec<&str> = prev_trimmed.split_whitespace().collect();
    let inc_words: Vec<&str> = inc_trimmed.split_whitespace().collect();

    if inc_words.is_empty() {
        return String::new();
    }

    let norm_prev: Vec<String> = prev_words.iter().map(|w| normalize_word(w)).collect();
    let norm_inc: Vec<String> = inc_words.iter().map(|w| normalize_word(w)).collect();

    let mut best_inc_idx = None;

    // 1. Try 3-word suffix match
    if norm_prev.len() >= 3 {
        let target = (&norm_prev[norm_prev.len() - 3], &norm_prev[norm_prev.len() - 2], &norm_prev[norm_prev.len() - 1]);
        for i in 0..norm_inc.len().saturating_sub(2) {
            if (&norm_inc[i], &norm_inc[i + 1], &norm_inc[i + 2]) == target {
                best_inc_idx = Some(i + 3);
                break;
            }
        }
    }

    // 2. Try 2-word suffix match
    if best_inc_idx.is_none() && norm_prev.len() >= 2 {
        let target = (&norm_prev[norm_prev.len() - 2], &norm_prev[norm_prev.len() - 1]);
        for i in 0..norm_inc.len().saturating_sub(1) {
            if (&norm_inc[i], &norm_inc[i + 1]) == target {
                best_inc_idx = Some(i + 2);
                break;
            }
        }
    }

    // 3. Try 1-word suffix match closest to expected position
    if best_inc_idx.is_none() {
        if let Some(last_prev_word) = norm_prev.last() {
            let expected_idx = norm_prev.len().saturating_sub(1) as isize;
            let mut min_distance = isize::MAX;
            for (i, iw) in norm_inc.iter().enumerate() {
                if iw == last_prev_word {
                    let distance = (i as isize - expected_idx).abs();
                    if distance < min_distance {
                        min_distance = distance;
                        best_inc_idx = Some(i + 1);
                    }
                }
            }
        }
    }

    if best_inc_idx.is_none() {
        let mut match_count = 0;
        for (pw, iw) in norm_prev.iter().zip(norm_inc.iter()) {
            if pw == iw || pw.contains(iw) || iw.contains(pw) {
                match_count += 1;
            } else {
                break;
            }
        }
        if match_count > 0 && match_count < inc_words.len() {
            best_inc_idx = Some(match_count);
        }
    }

    if let Some(idx) = best_inc_idx {
        if idx < inc_words.len() {
            let mut res = String::new();
            for word in &inc_words[idx..] {
                res.push(' ');
                res.push_str(word);
            }
            return res;
        }
    }

    if inc_words.len() > prev_words.len() {
        let skip = prev_words.len();
        let mut res = String::new();
        for word in &inc_words[skip..] {
            res.push(' ');
            res.push_str(word);
        }
        return res;
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
            "format": "pcm",
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
                        println!("[STREAM CLIENT] Sending end_of_stream message to server.");
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

        // Contraction and punctuation variations
        let prev = "Hello, I am trying to.";
        let inc = "Hello, I'm trying to speak to this speak to my application amadible. I do not think so.";
        let delta = compute_text_delta(prev, inc);
        assert_eq!(delta, " speak to this speak to my application amadible. I do not think so.");
    }
}
