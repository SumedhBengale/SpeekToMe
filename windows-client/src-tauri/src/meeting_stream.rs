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
    pub turn: Option<ClientMeetingTurn>,
    pub error: Option<String>,
}

pub enum MeetingStreamAudioMessage {
    ChannelChunk(u8, Vec<u8>), // channel_tag (0x01 = Mic, 0x02 = Loopback), opus_bytes
    EndOfMeeting,
}

/// A live transcript preview for a meeting being recorded. The recording
/// itself is saved locally and uploaded afterwards, where the final
/// transcript and speaker identities are produced.
pub struct MeetingStreamHandle {
    pub tx: Sender<MeetingStreamAudioMessage>,
    /// Signalled when the server has flushed the last live turns.
    pub done: Receiver<()>,
}

fn emit<S: serde::Serialize + Clone>(event: &str, payload: S) {
    if let Some(app) = crate::TAURI_APP.get() {
        let _ = app.emit(event, payload);
    }
}

pub fn start_meeting_streaming_session(
    api_url: String,
    token: String,
    title: String,
    turns_cache: Option<std::sync::Arc<std::sync::Mutex<Vec<ClientMeetingTurn>>>>,
) -> Result<MeetingStreamHandle, String> {
    let language = crate::speaking_language();
    let request = crate::stream::authorized_ws_request(&api_url, "/v1/meetings/live", &token)?;
    let (tx_audio, rx_audio) = channel::<MeetingStreamAudioMessage>();
    let (tx_done, done) = channel::<()>();

    std::thread::spawn(move || {
        let Ok(rt) = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
        else {
            emit("meeting-error", "Live transcription is unavailable");
            return;
        };
        rt.block_on(async move {
            let ws_stream = match connect_async(request).await {
                Ok((stream, _)) => stream,
                Err(_) => {
                    eprintln!("[MEETING STREAM] Live transcription connection failed; recording continues");
                    emit("meeting-error", "Live transcription is unavailable");
                    return;
                }
            };
            let (mut write, mut read) = ws_stream.split();
            let start_payload = serde_json::json!({
                "type": "start",
                "title": title,
                "language": language,
                "sample_rate": 16000,
                "channels": 1
            });
            if write.send(Message::Text(start_payload.to_string())).await.is_err() {
                emit("meeting-error", "Live transcription is unavailable");
                return;
            }

            // Bridge audio from the capture thread onto the socket.
            let (tx_ws_out, mut rx_ws_out) = tokio::sync::mpsc::unbounded_channel::<Message>();
            std::thread::spawn(move || {
                while let Ok(msg) = rx_audio.recv() {
                    let frame = match msg {
                        MeetingStreamAudioMessage::ChannelChunk(channel_tag, opus_bytes) => {
                            let mut frame = Vec::with_capacity(1 + opus_bytes.len());
                            frame.push(channel_tag);
                            frame.extend_from_slice(&opus_bytes);
                            Message::Binary(frame)
                        }
                        MeetingStreamAudioMessage::EndOfMeeting => {
                            let _ = tx_ws_out.send(Message::Text(serde_json::json!({ "type": "end_of_meeting" }).to_string()));
                            break;
                        }
                    };
                    if tx_ws_out.send(frame).is_err() {
                        break;
                    }
                }
            });

            loop {
                tokio::select! {
                    Some(out_msg) = rx_ws_out.recv() => {
                        if write.send(out_msg).await.is_err() {
                            break;
                        }
                    }
                    incoming = read.next() => {
                        let Some(Ok(Message::Text(text))) = incoming else {
                            if matches!(incoming, Some(Ok(_))) { continue; }
                            break;
                        };
                        let Ok(event) = serde_json::from_str::<MeetingStreamServerEvent>(&text) else { continue };
                        match event.event_type.as_str() {
                            "session_ready" => emit("meeting-session-ready", ()),
                            "turn" => {
                                if let Some(turn) = event.turn {
                                    if let Some(ref cache) = turns_cache {
                                        let mut cache = cache.lock().unwrap_or_else(|e| e.into_inner());
                                        match cache.iter_mut().find(|t| t.id == turn.id) {
                                            Some(existing) => *existing = turn.clone(),
                                            None => cache.push(turn.clone()),
                                        }
                                    }
                                    emit("meeting-turn-update", turn);
                                }
                            }
                            "live_complete" => break,
                            "error" => {
                                // Transcript text and server details stay out of logs.
                                eprintln!("[MEETING STREAM] Live transcription stopped; recording continues");
                                emit("meeting-error", event.error.unwrap_or_else(|| "Live transcription paused".into()));
                                break;
                            }
                            _ => {}
                        }
                    }
                }
            }
            let _ = tx_done.send(());
        });
    });

    Ok(MeetingStreamHandle { tx: tx_audio, done })
}
