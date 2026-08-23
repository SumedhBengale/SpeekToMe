use std::sync::mpsc::{Sender, Receiver, channel};
use futures_util::{SinkExt, StreamExt};
use serde::{Serialize, Deserialize};
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
    pub tx: Sender<StreamAudioMessage>,
    pub rx_final: Receiver<Result<StreamFinalResult, String>>,
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
    let (audio_tx, audio_rx) = channel::<StreamAudioMessage>();
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
            while let Ok(msg) = audio_rx.recv() {
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
            let mut final_res: Option<StreamFinalResult> = None;
            while let Some(msg_res) = read.next().await {
                match msg_res {
                    Ok(Message::Text(text)) => {
                        if let Ok(event) = serde_json::from_str::<StreamEventResponse>(&text) {
                            if event.event_type == "final" {
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
