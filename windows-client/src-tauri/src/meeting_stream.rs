use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use std::sync::mpsc::{channel, Receiver, Sender};
use tauri::Emitter;
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::Message;

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct ClientMeetingTurn {
    pub id: String,
    pub channel: String,      // "mic" | "system"
    pub speaker_id: String,   // "speaker_local", "speaker_1", etc.
    pub speaker_name: String, // Display name
    pub start_time: f64,
    pub end_time: f64,
    pub text: String,
    pub is_final: bool,
}

#[derive(Deserialize, Debug)]
pub struct MeetingStreamServerEvent {
    #[serde(rename = "type")]
    pub event_type: String,
    pub meeting_id: Option<i64>,
    pub title: Option<String>,
    pub turn: Option<ClientMeetingTurn>,
    pub meeting: Option<serde_json::Value>,
    pub error: Option<String>,
}

pub enum MeetingStreamAudioMessage {
    ChannelChunk(u8, Vec<u8>), // channel_tag (0x01 = Mic, 0x02 = Loopback), opus_bytes
    EndOfMeeting,
}

pub struct MeetingStreamHandle {
    pub tx: Sender<MeetingStreamAudioMessage>,
    pub rx_final: Receiver<Result<String, String>>,
}

pub fn get_meeting_stream_ws_url(api_url: &str, token: &str) -> String {
    let base = if api_url.starts_with("https://") {
        api_url.replacen("https://", "wss://", 1)
    } else if api_url.starts_with("http://") {
        api_url.replacen("http://", "ws://", 1)
    } else if api_url.starts_with("wss://") || api_url.starts_with("ws://") {
        api_url.to_string()
    } else {
        format!("wss://{}", api_url)
    };
    format!("{}/v1/meetings/live?token={}", base, token)
}

pub fn start_meeting_streaming_session(
    api_url: String,
    token: String,
    title: String,
) -> Result<MeetingStreamHandle, String> {
    let ws_url = get_meeting_stream_ws_url(&api_url, &token);
    println!("[MEETING STREAM] Connecting to live meeting WebSocket: {}", ws_url);

    let (tx_audio, rx_audio) = channel::<MeetingStreamAudioMessage>();
    let (tx_final, rx_final) = channel::<Result<String, String>>();

    std::thread::spawn(move || {
        let rt = match tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
        {
            Ok(r) => r,
            Err(e) => {
                let err_msg = format!("Failed to build tokio runtime: {}", e);
                eprintln!("[MEETING STREAM ERROR] {}", err_msg);
                let _ = tx_final.send(Err(err_msg));
                return;
            }
        };

        rt.block_on(async move {
            let ws_stream = match connect_async(&ws_url).await {
                Ok((stream, _)) => stream,
                Err(e) => {
                    let err_msg = format!("WebSocket connection failed: {}", e);
                    eprintln!("[MEETING STREAM ERROR] {}", err_msg);
                    let _ = tx_final.send(Err(err_msg));
                    return;
                }
            };

            let (mut write, mut read) = ws_stream.split();

            // 1. Send start handshake payload
            let start_payload = serde_json::json!({
                "type": "start",
                "title": title,
                "sample_rate": 16000,
                "channels": 1
            });

            if let Err(e) = write.send(Message::Text(start_payload.to_string())).await {
                let err_msg = format!("Failed to send start handshake: {}", e);
                eprintln!("[MEETING STREAM ERROR] {}", err_msg);
                let _ = tx_final.send(Err(err_msg));
                return;
            }

            println!("[MEETING STREAM] Handshake sent successfully. Listening for live turns...");

            let mut final_meeting_json: Option<String> = None;
            let mut stream_error: Option<String> = None;

            // Channel bridge task: Send audio packets from client thread to WebSocket
            let (tx_ws_out, mut rx_ws_out) = tokio::sync::mpsc::unbounded_channel::<Message>();

            std::thread::spawn(move || {
                while let Ok(msg) = rx_audio.recv() {
                    match msg {
                        MeetingStreamAudioMessage::ChannelChunk(channel_tag, opus_bytes) => {
                            let mut frame = Vec::with_capacity(1 + opus_bytes.len());
                            frame.push(channel_tag);
                            frame.extend_from_slice(&opus_bytes);
                            if tx_ws_out.send(Message::Binary(frame)).is_err() {
                                break;
                            }
                        }
                        MeetingStreamAudioMessage::EndOfMeeting => {
                            let end_msg = serde_json::json!({ "type": "end_of_meeting" });
                            let _ = tx_ws_out.send(Message::Text(end_msg.to_string()));
                            break;
                        }
                    }
                }
            });

            // Read / Write Concurrent loop
            'select_loop: loop {
                tokio::select! {
                    Some(out_msg) = rx_ws_out.recv() => {
                        if let Err(e) = write.send(out_msg).await {
                            eprintln!("[MEETING STREAM ERROR] Failed to send WebSocket message: {}", e);
                            break 'select_loop;
                        }
                    }
                    in_msg_opt = read.next() => {
                        match in_msg_opt {
                            Some(Ok(Message::Text(text))) => {
                                println!("[CLIENT RAW FROM SERVER] {}", text);
                                if let Ok(event) = serde_json::from_str::<MeetingStreamServerEvent>(&text) {
                                    match event.event_type.as_str() {
                                        "session_ready" => {
                                            println!("[MEETING STREAM] Session ready on server.");
                                            if let Some(app) = crate::TAURI_APP.get() {
                                                let _ = app.emit("meeting-session-ready", ());
                                            }
                                        }
                                        "turn" => {
                                            if let Some(ref turn) = event.turn {
                                                println!("[CLIENT PARSED TURN] ID='{}' ch={} spk='{}' [{:.2}s - {:.2}s] (is_final={}) text=\"{}\"",
                                                    turn.id, turn.channel, turn.speaker_name, turn.start_time, turn.end_time, turn.is_final, turn.text);
                                                if let Some(app) = crate::TAURI_APP.get() {
                                                    let _ = app.emit("meeting-turn-update", turn);
                                                }
                                            }
                                        }
                                        "completed" => {
                                            println!("[MEETING STREAM] Meeting finalized by server.");
                                            if let Some(m_val) = event.meeting {
                                                let json_str = m_val.to_string();
                                                println!("[CLIENT COMPLETED PAYLOAD RAW] {}", json_str);
                                                final_meeting_json = Some(json_str.clone());
                                                if let Some(app) = crate::TAURI_APP.get() {
                                                    let _ = app.emit("meeting-completed", json_str);
                                                }
                                            }
                                            break 'select_loop;
                                        }
                                        "error" => {
                                            let err_str = event.error.unwrap_or_else(|| "Unknown meeting server error".to_string());
                                            eprintln!("[MEETING STREAM SERVER ERROR] {}", err_str);
                                            stream_error = Some(err_str.clone());
                                            if let Some(app) = crate::TAURI_APP.get() {
                                                let _ = app.emit("meeting-error", err_str);
                                            }
                                            break 'select_loop;
                                        }
                                        _ => {}
                                    }
                                }
                            }
                            Some(Ok(Message::Close(_))) => {
                                println!("[MEETING STREAM] WebSocket closed by server.");
                                break 'select_loop;
                            }
                            Some(Err(e)) => {
                                eprintln!("[MEETING STREAM ERROR] Read error: {}", e);
                                stream_error = Some(format!("WebSocket read error: {}", e));
                                break 'select_loop;
                            }
                            None => {
                                break 'select_loop;
                            }
                            _ => {}
                        }
                    }
                }
            }

            if let Some(res) = final_meeting_json {
                let _ = tx_final.send(Ok(res));
            } else if let Some(err) = stream_error {
                let _ = tx_final.send(Err(err));
            } else {
                let _ = tx_final.send(Err("Meeting stream closed before receiving completion result".to_string()));
            }
        });
    });

    Ok(MeetingStreamHandle {
        tx: tx_audio,
        rx_final,
    })
}
