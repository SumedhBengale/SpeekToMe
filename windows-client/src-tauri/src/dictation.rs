//! Push-to-talk dictation capture. Audio is either streamed to the server
//! while the user speaks or transcribed in one request when they stop.
use crate::audio::{encode_opus_ogg, is_speech, CURRENT_VOLUME};
use crate::capture::{self, CaptureSink};
use crate::shortcuts::{RecordingContext, RecordingSession};
use crate::stream::{NegotiatedStreamMode, StreamAudioMessage, StreamSessionHandle};
use crate::win32::{self, NotificationLevel, PreviewState};
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, RecvTimeoutError};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

// A five-second callback queue is drained while capture is running. Batch
// dictation retains only normalized 16 kHz mono, with a visible 30-minute stop.
const MAX_DICTATION_SECONDS: u64 = 30 * 60;
/// Microphone level (0–1000) that counts as sound for silence detection.
const SOUND_LEVEL: u32 = 6;

pub(crate) fn take_capture(buffer: &Mutex<Vec<i16>>) -> Vec<i16> {
    std::mem::take(&mut *buffer.lock().unwrap_or_else(|e| e.into_inner()))
}

fn capture_notice(message: &str) {
    win32::show_tray_notification("SpeekTo.Me — Recording", message, NotificationLevel::Error);
}

fn fail(title: &str, message: &str) {
    crate::delivery::hide_overlay();
    win32::show_tray_notification(title, message, NotificationLevel::Error);
}

/// A running microphone capture for one dictation.
struct DictationCapture {
    stream: cpal::Stream,
    samples: Arc<Mutex<Vec<i16>>>,
    failed: Arc<AtomicBool>,
    overflow: Arc<AtomicBool>,
    started: Instant,
    sample_rate: u32,
    channels: u16,
}

impl DictationCapture {
    /// Opens and starts the default microphone, telling the user if it cannot.
    fn open() -> Option<Self> {
        let host = cpal::default_host();
        let Some(device) = host.default_input_device() else {
            eprintln!("No input device found!");
            fail(
                "SpeekTo.Me — Microphone Not Found",
                "No active microphone was detected. Please connect a microphone and check your system audio settings.",
            );
            return None;
        };
        let config = match device.default_input_config() {
            Ok(config) => config,
            Err(e) => {
                eprintln!("Failed to get default input config: {:?}", e);
                fail(
                    "SpeekTo.Me — Audio Device Error",
                    "Failed to configure the default microphone. Please check your audio settings.",
                );
                return None;
            }
        };
        let sample_rate = config.sample_rate().0;
        let channels = config.channels();
        let samples = Arc::new(Mutex::new(Vec::new()));
        let failed = Arc::new(AtomicBool::new(false));
        let overflow = Arc::new(AtomicBool::new(false));
        let sink = CaptureSink {
            buffer: samples.clone(),
            limit: sample_rate as usize * channels as usize * 5,
            overflow: overflow.clone(),
            keep_partial: true,
            level: Some((&CURRENT_VOLUME, 1000.0)),
        };
        let stream = match capture::input_stream(&device, &config, sink, failed.clone()) {
            Ok(stream) => stream,
            Err(capture::CaptureError::UnsupportedFormat(_)) => {
                capture_notice("This microphone uses an unsupported audio format. Choose another input device.");
                crate::delivery::hide_overlay();
                return None;
            }
            Err(e) => {
                eprintln!("Failed to build input stream: {:?}", e);
                fail(
                    "SpeekTo.Me — Audio Error",
                    "Failed to initialize the microphone capture stream.",
                );
                return None;
            }
        };
        if let Err(e) = stream.play() {
            eprintln!("[AUDIO ERROR] Failed to play stream: {:?}", e);
            fail(
                "SpeekTo.Me — Audio Error",
                "Failed to start recording audio from the microphone.",
            );
            return None;
        }
        Some(Self {
            stream,
            samples,
            failed,
            overflow,
            started: Instant::now(),
            sample_rate,
            channels,
        })
    }

    fn take(&self) -> Vec<i16> {
        take_capture(&self.samples)
    }

    /// Reports a capture problem or the length limit, telling the user why.
    fn should_stop(&self) -> bool {
        if self.failed.swap(false, Ordering::Relaxed) {
            capture_notice("Microphone capture stopped unexpectedly. The captured portion will be processed. Check your input device before recording again.");
            true
        } else if self.overflow.swap(false, Ordering::Relaxed) {
            capture_notice("Audio capture could not keep up. Recording stopped; the captured portion will be processed.");
            true
        } else if self.started.elapsed().as_secs() >= MAX_DICTATION_SECONDS {
            win32::show_tray_notification(
                "SpeekTo.Me — Recording saved",
                "The 30-minute dictation limit was reached. The captured audio will now be transcribed.",
                NotificationLevel::Warning,
            );
            true
        } else {
            false
        }
    }

    /// Stops the microphone, keeping any samples already queued.
    fn stop(self) -> Arc<Mutex<Vec<i16>>> {
        drop(self.stream);
        CURRENT_VOLUME.store(0, Ordering::Relaxed);
        self.samples
    }
}

/// Stops a recording after 15 seconds of silence, counting down from 10.
struct SilenceWatch {
    last_sound: Instant,
    countdown_active: bool,
    last_countdown: Option<u64>,
    had_spoken: bool,
}

impl SilenceWatch {
    fn new() -> Self {
        Self {
            last_sound: Instant::now(),
            countdown_active: false,
            last_countdown: None,
            had_spoken: false,
        }
    }

    /// Returns `Some(aborted)` when the recording should stop; it is aborted
    /// when nothing was ever said.
    fn observe(&mut self, sound: bool, context: &RecordingContext, label: &str) -> Option<bool> {
        if sound {
            self.last_sound = Instant::now();
            self.had_spoken = true;
            if self.countdown_active {
                self.countdown_active = false;
                self.last_countdown = None;
                win32::set_preview_text(if context.is_command_mode {
                    "✨ Command Mode (Listening...)"
                } else {
                    ""
                });
            }
            return None;
        }
        let silence = self.last_sound.elapsed();
        if silence >= Duration::from_secs(15) {
            println!(
                "{} 15 seconds of silence detected. Auto-stopping recording...",
                label
            );
            context.capture_finished();
            return Some(!self.had_spoken);
        }
        if silence >= Duration::from_secs(10) {
            let remaining = 15 - silence.as_secs();
            if self.last_countdown != Some(remaining) && remaining >= 1 {
                self.last_countdown = Some(remaining);
                self.countdown_active = true;
                win32::set_preview_text(&format!(
                    "Silence detected — stopping in {}s...",
                    remaining
                ));
            }
        }
        None
    }
}

fn sound_detected() -> bool {
    CURRENT_VOLUME.load(Ordering::Relaxed) >= SOUND_LEVEL
}

/// Background recording thread routine (manages the non-Send capture stream).
pub fn recording_thread(rx: Receiver<bool>, session: RecordingSession) {
    let language = crate::speaking_language();
    let _capture_life = session.keep_alive();
    // Introduce a short delay so the start chime has time to play before CPAL starts input recording.
    // This prevents audio driver profile/channel switching conflicts.
    std::thread::sleep(Duration::from_millis(150));
    if session.is_cancelled() {
        return;
    }
    let Some(capture) = DictationCapture::open() else {
        return;
    };
    println!("[AUDIO] Recording started...");

    let Some(context) = session.resolve() else {
        capture.stop();
        return;
    };
    let (is_stream_mode, api_url, token, custom_prompt) = {
        let cfg = crate::get_active_config()
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        (
            cfg.transcription_mode == "stream" && context.history_identity().1.is_some(),
            context.history_identity().2.to_string(),
            context.history_identity().1.unwrap_or_default().to_string(),
            cfg.custom_prompt.clone(),
        )
    };
    println!(
        "[AUDIO] Active transcription mode: {} (is_command_mode: {})",
        if is_stream_mode { "STREAM" } else { "BATCH" },
        context.is_command_mode
    );

    if is_stream_mode {
        match crate::stream::start_streaming_session(api_url, token, custom_prompt, language.clone(), context.clone()) {
            Ok(handle) => return stream_dictation(capture, handle, &rx, context),
            Err(e) => eprintln!("[STREAM INIT ERROR] Failed to start live stream session: {}. Falling back to batch mode.", e),
        }
    }
    batch_dictation(capture, &rx, context, language);
}

/// Waits for the key release (`Some(false)`), an abort (`Some(true)`), or a
/// timeout (`None`, keep recording).
fn poll_key(rx: &Receiver<bool>, timeout_ms: u64) -> Option<bool> {
    match rx.recv_timeout(Duration::from_millis(timeout_ms)) {
        Ok(released) => Some(!released),
        Err(RecvTimeoutError::Timeout) => None,
        Err(RecvTimeoutError::Disconnected) => Some(true),
    }
}

// ---------------------------------------------------------------------------
// Streaming dictation
// ---------------------------------------------------------------------------

/// Audio state while streaming: 16 kHz samples not yet sent, and for chunked
/// mode the phrase being collected between voice-activity pauses.
struct StreamFeed {
    resampler: crate::audio_processing::CaptureResampler,
    vad: Option<crate::vad::SileroVadSession>,
    pending: Vec<i16>,
    pre_speech: std::collections::VecDeque<i16>,
    phrase: Vec<i16>,
    in_speech: bool,
}

/// 560 ms at 16 kHz, matching the real-time ASR's acoustic frame.
const CONTINUOUS_FRAME: usize = 8960;
/// Pre-speech ring (~250 ms) so leading consonants are never clipped.
const PRE_SPEECH_SAMPLES: usize = 4000;
/// Trailing 200 ms kept between chunks for acoustic continuity.
const CHUNK_OVERLAP: usize = 3200;

fn send_chunk(handle: &StreamSessionHandle, samples: &[i16], label: &str) {
    let started = Instant::now();
    match encode_opus_ogg(samples, 16000, 1) {
        Ok(ogg) => {
            let pcm = samples.len() * 2;
            let ratio = if ogg.is_empty() {
                1.0
            } else {
                pcm as f64 / ogg.len() as f64
            };
            println!(
                "[CLIENT BENCHMARK] {} encode: {:.2?} (Audio: {:.2}s | Raw PCM: {} B -> Opus Ogg: {} B | {:.1}x compression)",
                label,
                started.elapsed(),
                samples.len() as f64 / 16000.0,
                pcm,
                ogg.len(),
                ratio
            );
            let _ = handle.tx.send(StreamAudioMessage::AudioChunk(ogg));
        }
        Err(e) => eprintln!("[STREAM ERROR] Failed to encode Opus Ogg: {}", e),
    }
}

impl StreamFeed {
    fn new(capture: &DictationCapture) -> Self {
        Self {
            resampler: crate::audio_processing::CaptureResampler::new(
                capture.sample_rate,
                capture.channels,
            ),
            vad: crate::vad::SileroVadSession::new().ok(),
            pending: Vec::new(),
            pre_speech: std::collections::VecDeque::with_capacity(PRE_SPEECH_SAMPLES),
            phrase: Vec::new(),
            in_speech: false,
        }
    }

    fn pull(&mut self, capture: &DictationCapture) {
        let raw = capture.take();
        if !raw.is_empty() {
            let converted = self.resampler.push(&raw);
            self.pending.extend_from_slice(&converted);
        }
    }

    /// Continuous mode sends fixed 560 ms frames as they fill.
    fn send_frames(&mut self, handle: &StreamSessionHandle) {
        while self.pending.len() >= CONTINUOUS_FRAME {
            let frame: Vec<i16> = self.pending.drain(..CONTINUOUS_FRAME).collect();
            if let Ok(ogg) = encode_opus_ogg(&frame, 16000, 1) {
                let _ = handle.tx.send(StreamAudioMessage::AudioChunk(ogg));
            }
        }
    }

    /// Chunked mode sends whole phrases at voice-activity pauses (at most 5 s),
    /// or 2-second chunks if the voice detector could not start.
    fn send_phrases(&mut self, handle: &StreamSessionHandle) {
        let Some(vad) = self.vad.as_mut() else {
            if self.pending.len() >= 32000 {
                let chunk: Vec<i16> = self.pending.drain(..32000).collect();
                send_chunk(handle, &chunk, "Opus Ogg fallback");
            }
            return;
        };
        while self.pending.len() >= 512 {
            let frame: Vec<i16> = self.pending.drain(..512).collect();
            let frame_f32: Vec<f32> = frame.iter().map(|&s| s as f32 / 32768.0).collect();
            let (_probability, speaking, pause) = vad.process_frame(&frame_f32);
            if speaking {
                if !self.in_speech {
                    self.in_speech = true;
                    self.phrase.extend(self.pre_speech.drain(..));
                }
                self.phrase.extend_from_slice(&frame);
            } else if self.in_speech {
                // Keep appending trailing frames until the pause is final.
                self.phrase.extend_from_slice(&frame);
            } else {
                for &sample in &frame {
                    if self.pre_speech.len() >= PRE_SPEECH_SAMPLES {
                        self.pre_speech.pop_front();
                    }
                    self.pre_speech.push_back(sample);
                }
            }
            let commit = pause || self.phrase.len() >= 80000;
            if commit && self.phrase.len() >= 4800 {
                self.in_speech = false;
                send_chunk(handle, &self.phrase, "Opus Ogg");
                let keep = self.phrase.len().saturating_sub(CHUNK_OVERLAP);
                self.phrase.drain(..keep);
                vad.reset();
            }
        }
    }

    /// Sends whatever remains after the key is released.
    fn flush(&mut self, samples: &Mutex<Vec<i16>>, handle: &StreamSessionHandle) {
        let raw = take_capture(samples);
        if !raw.is_empty() {
            let mut tail = self.resampler.push(&raw);
            tail.extend(self.resampler.finish());
            self.pending.extend_from_slice(&tail);
        }
        if handle.mode == NegotiatedStreamMode::ContinuousStream {
            if self.pending.len() >= 480 {
                if let Ok(ogg) = encode_opus_ogg(&self.pending, 16000, 1) {
                    let _ = handle.tx.send(StreamAudioMessage::AudioChunk(ogg));
                }
            }
        } else {
            self.phrase.extend(self.pending.drain(..));
            // Only send a tail with at least 100 ms of new audio beyond the overlap.
            if self.phrase.len() > CHUNK_OVERLAP + 1600 {
                send_chunk(handle, &self.phrase, "Tail Opus Ogg");
            }
            self.phrase.clear();
        }
    }
}

/// Records while streaming and returns whether the dictation was aborted.
fn stream_capture(
    capture: &DictationCapture,
    feed: &mut StreamFeed,
    handle: &StreamSessionHandle,
    rx: &Receiver<bool>,
    context: &RecordingContext,
) -> bool {
    let continuous = handle.mode == NegotiatedStreamMode::ContinuousStream;
    let (poll_ms, label) = if continuous {
        (20, "[AUDIO STREAM]")
    } else {
        (32, "[AUDIO CHUNKED]")
    };
    let mut silence = SilenceWatch::new();
    loop {
        if capture.should_stop() {
            context.capture_finished();
            return false;
        }
        if let Some(aborted) = poll_key(rx, poll_ms) {
            return aborted;
        }
        let sound = (!continuous && feed.in_speech) || sound_detected();
        if let Some(aborted) = silence.observe(sound, context, label) {
            return aborted;
        }
        feed.pull(capture);
        if continuous {
            feed.send_frames(handle);
        } else {
            feed.send_phrases(handle);
        }
    }
}

/// The final transcript: the server's result, or the live preview if the
/// result is missing or lost a substantial part of what was previewed.
fn final_stream_text(handle: &StreamSessionHandle) -> (Option<String>, Option<String>, bool) {
    let interim = || Some(handle.interim_text().trim().to_string()).filter(|text| !text.is_empty());
    match handle.rx_final.recv_timeout(Duration::from_secs(15)) {
        Ok(Ok(result)) => {
            println!("[PRIVATE DATA] Result processed; content omitted from logs.");
            let text = result.text.trim().to_string();
            let preview = handle.interim_text().trim().to_string();
            let (final_words, preview_words) = (
                text.split_whitespace().count(),
                preview.split_whitespace().count(),
            );
            // Truncation guard: keep the fuller preview when the final result
            // dropped more than 40% of a preview of at least six words.
            if preview_words >= 6 && final_words < (preview_words * 6) / 10 {
                println!("[STREAM TRUNCATION GUARD] Final text ({} words) dropped substantial text vs interim ({} words). Keeping interim transcript to prevent loss.", final_words, preview_words);
                (Some(preview), None, false)
            } else if !text.is_empty() {
                (Some(text), None, false)
            } else {
                (interim(), None, false)
            }
        }
        Ok(Err(e)) => {
            eprintln!(
                "[STREAM ERROR] Stream returned error: {}. Falling back to interim preview text.",
                e
            );
            (interim(), Some(e), false)
        }
        Err(RecvTimeoutError::Timeout) => {
            eprintln!("[STREAM TIMEOUT] Final transcript timed out after 15s. Falling back to interim preview text.");
            (interim(), None, true)
        }
        Err(RecvTimeoutError::Disconnected) => {
            eprintln!("[STREAM DISCONNECTED] Stream disconnected before final transcript. Falling back to interim preview text.");
            (interim(), None, false)
        }
    }
}

fn stream_dictation(
    capture: DictationCapture,
    handle: StreamSessionHandle,
    rx: &Receiver<bool>,
    context: RecordingContext,
) {
    let mut feed = StreamFeed::new(&capture);
    let aborted = stream_capture(&capture, &mut feed, &handle, rx, &context);
    let samples = capture.stop();
    println!("Recording stopped.");
    context.capture_finished();
    let is_command_mode = context.is_command_mode;

    if aborted {
        println!("Recording aborted and discarded.");
    } else {
        win32::set_preview_state(PreviewState::Processing);
        feed.flush(&samples, &handle);
        let _ = handle.tx.send(StreamAudioMessage::EndOfStream);
        let (text, error, timed_out) = final_stream_text(&handle);
        let (api_url, token) = {
            let cfg = crate::get_active_config()
                .lock()
                .unwrap_or_else(|e| e.into_inner());
            (
                cfg.api_url
                    .clone()
                    .unwrap_or_else(|| "https://api.speekto.me".to_string()),
                cfg.session_token.clone().unwrap_or_default(),
            )
        };
        match text {
            Some(text) => {
                win32::set_preview_text(&text);
                if is_command_mode {
                    crate::delivery::spawn_command(text, context.clone(), api_url, token);
                } else {
                    crate::delivery::insert_dictation(&text, &context);
                }
            }
            None => {
                if let Some(error) = error {
                    crate::delivery::report_failure(&error, "Live streaming error");
                } else if timed_out {
                    win32::show_tray_notification(
                        "SpeekTo.Me — Server Timeout",
                        "Transcription timed out waiting for server response.",
                        NotificationLevel::Warning,
                    );
                }
            }
        }
    }
    // Command mode hides the overlay when its answer has been delivered.
    if !is_command_mode {
        crate::delivery::hide_overlay();
    }
}

// ---------------------------------------------------------------------------
// Batch dictation
// ---------------------------------------------------------------------------

fn batch_dictation(
    capture: DictationCapture,
    rx: &Receiver<bool>,
    context: RecordingContext,
    language: String,
) {
    let mut silence = SilenceWatch::new();
    let mut samples = Vec::new();
    let mut resampler =
        crate::audio_processing::CaptureResampler::new(capture.sample_rate, capture.channels);
    let aborted = loop {
        samples.extend(resampler.push(&capture.take()));
        if capture.should_stop() {
            context.capture_finished();
            break false;
        }
        if let Some(aborted) = poll_key(rx, 50) {
            break aborted;
        }
        if let Some(aborted) = silence.observe(sound_detected(), &context, "[AUDIO BATCH]") {
            break aborted;
        }
    };
    let queued = capture.stop();
    if aborted {
        println!("Recording aborted and discarded.");
        crate::delivery::hide_overlay();
        return;
    }
    println!("Recording stopped.");
    win32::set_preview_state(PreviewState::Processing);
    samples.extend(resampler.push(&take_capture(&queued)));
    samples.extend(resampler.finish());
    if samples.is_empty() {
        crate::delivery::hide_overlay();
        return;
    }
    let has_speech = is_speech(&samples, 16000, 1);
    println!(
        "Recording finished. Samples: {}, Speech detected: {}",
        samples.len(),
        has_speech
    );
    if has_speech {
        context.capture_finished();
        crate::api::process_and_paste(samples, 16000, 1, language, context);
    } else {
        println!("Silence or keyboard click detected. Transcription request skipped.");
        crate::delivery::hide_overlay();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn key_poll_maps_release_abort_and_disconnect() {
        let (tx, rx) = std::sync::mpsc::channel();
        assert_eq!(poll_key(&rx, 1), None);
        tx.send(true).unwrap();
        assert_eq!(poll_key(&rx, 1), Some(false));
        tx.send(false).unwrap();
        assert_eq!(poll_key(&rx, 1), Some(true));
        drop(tx);
        assert_eq!(poll_key(&rx, 1), Some(true));
    }
}
