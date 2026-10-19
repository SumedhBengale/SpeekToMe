use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicU32, Ordering};
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use serde::{Serialize, Deserialize};
#[cfg(target_os = "linux")]
use libpulse_binding::{sample::{Format as PulseFormat, Spec as PulseSpec}, stream::Direction as PulseDirection};
#[cfg(target_os = "linux")]
use libpulse_simple_binding::Simple as PulseSimple;

pub use crate::audio_processing::{is_speech, resample_to_16k_mono, encode_opus_ogg};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct EnergySample {
    pub time: f32,
    pub mic: u32,
    pub lb: u32,
}

pub static CURRENT_VOLUME: AtomicU32 = AtomicU32::new(0);
const MAX_DICTATION_SAMPLES: usize = 12_000_000;

// Encode samples to WAV in memory
pub fn write_wav_to_memory(samples: &[i16], sample_rate: u32, channels: u16) -> Vec<u8> {
    let mut cursor = std::io::Cursor::new(Vec::new());
    let spec = hound::WavSpec {
        channels,
        sample_rate,
        bits_per_sample: 16,
        sample_format: hound::SampleFormat::Int,
    };
    let mut writer = hound::WavWriter::new(&mut cursor, spec).expect("Failed to create WAV writer");
    for &sample in samples {
        writer.write_sample(sample).expect("Failed to write sample");
    }
    writer.finalize().expect("Failed to finalize WAV file");
    cursor.into_inner()
}

// Background recording thread routine (manages non-Send WASAPI stream)
pub fn recording_thread(rx: std::sync::mpsc::Receiver<bool>, session: crate::shortcuts::RecordingSession) {
    let language = crate::speaking_language();
    let _capture_life = session.keep_alive();
    // Introduce a short delay so the start chime has time to play before CPAL starts input recording.
    // This prevents audio driver profile/channel switching conflicts.
    std::thread::sleep(std::time::Duration::from_millis(150));
    if session.is_cancelled() { return; }

    let host = cpal::default_host();
    let device = match host.default_input_device() {
        Some(d) => d,
        None => {
            eprintln!("No input device found!");
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
            crate::win32::show_tray_notification(
                "SpeekTo.Me — Microphone Not Found",
                "No active microphone was detected. Please connect a microphone and check Windows Sound settings.",
                crate::win32::NotificationLevel::Error,
            );
            return;
        }
    };
    
    let config = match device.default_input_config() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("Failed to get default input config: {:?}", e);
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
            crate::win32::show_tray_notification(
                "SpeekTo.Me — Audio Device Error",
                "Failed to configure the default microphone. Please check your audio settings.",
                crate::win32::NotificationLevel::Error,
            );
            return;
        }
    };
    
    let sample_rate = config.sample_rate().0;
    let channels = config.channels();
    let sample_format = config.sample_format();
    
    let samples_buf = Arc::new(Mutex::new(Vec::new()));
    let samples_buf_clone = samples_buf.clone();
    
    let err_fn = |err| eprintln!("Audio stream error: {}", err);
    
    let stream_res = match sample_format {
        cpal::SampleFormat::F32 => {
            device.build_input_stream(
                &config.into(),
                move |data: &[f32], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            sum_squares += sample * sample;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        let vol = (rms * 1000.0) as u32;
                        CURRENT_VOLUME.store(vol, Ordering::Relaxed);
                    }
                    if let Ok(mut buf) = samples_buf_clone.lock() {
                        let remaining = MAX_DICTATION_SAMPLES.saturating_sub(buf.len());
                        for &sample in data.iter().take(remaining) {
                            let i16_val = (sample * i16::MAX as f32) as i16;
                            buf.push(i16_val);
                        }
                    }
                },
                err_fn,
                None
            )
        }
        cpal::SampleFormat::I16 => {
            device.build_input_stream(
                &config.into(),
                move |data: &[i16], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            let val = sample as f32 / i16::MAX as f32;
                            sum_squares += val * val;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        let vol = (rms * 1000.0) as u32;
                        CURRENT_VOLUME.store(vol, Ordering::Relaxed);
                    }
                    if let Ok(mut buf) = samples_buf_clone.lock() {
                        let remaining = MAX_DICTATION_SAMPLES.saturating_sub(buf.len());
                        buf.extend_from_slice(&data[..data.len().min(remaining)]);
                    }
                },
                err_fn,
                None
            )
        }
        cpal::SampleFormat::U16 => {
            device.build_input_stream(
                &config.into(),
                move |data: &[u16], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            let val = (sample as f32 - 32768.0) / 32768.0;
                            sum_squares += val * val;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        let vol = (rms * 1000.0) as u32;
                        CURRENT_VOLUME.store(vol, Ordering::Relaxed);
                    }
                    if let Ok(mut buf) = samples_buf_clone.lock() {
                        let remaining = MAX_DICTATION_SAMPLES.saturating_sub(buf.len());
                        for &sample in data.iter().take(remaining) {
                            let i16_val = (sample as i32 - 32768) as i16;
                            buf.push(i16_val);
                        }
                    }
                },
                err_fn,
                None
            )
        }
        _ => {
            eprintln!("Unsupported sample format");
            return;
        }
    };
      match stream_res {
        Ok(s) => {
            if let Err(e) = s.play() {
                eprintln!("[AUDIO ERROR] Failed to play stream: {:?}", e);
                crate::win32::hide_overlay();
                crate::win32::hide_preview();
                crate::win32::show_tray_notification(
                    "SpeekTo.Me — Audio Error",
                    "Failed to start recording audio from the microphone.",
                    crate::win32::NotificationLevel::Error,
                );
                return;
            }
            println!("[AUDIO] Recording started...");
            
            let Some(context) = session.resolve() else {
                drop(s);
                CURRENT_VOLUME.store(0, Ordering::Relaxed);
                return;
            };
            let is_cmd = context.is_command_mode;
            let (is_stream_mode, api_url, token, enhance, custom_prompt) = {
                let cfg = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                (
                    cfg.transcription_mode == "stream" && cfg.session_token.is_some(),
                    cfg.api_url.clone().unwrap_or_else(|| "https://api.speekto.me".to_string()),
                    cfg.session_token.clone().unwrap_or_default(),
                    if is_cmd { false } else { cfg.enhance },
                    cfg.custom_prompt.clone(),
                )
            };
            println!(
                "[AUDIO] Active transcription mode: {} (enhance: {}, is_command_mode: {})",
                if is_stream_mode { "STREAM" } else { "BATCH" },
                enhance,
                is_cmd
            );

            if is_stream_mode {
                let stream_handle = match crate::stream::start_streaming_session(
                    api_url.clone(),
                    token.clone(),
                    enhance,
                    custom_prompt.clone(),
                    language.clone(),
                    context.clone(),
                ) {
                    Ok(handle) => Some(handle),
                    Err(e) => {
                        eprintln!("[STREAM INIT ERROR] Failed to start live stream session: {}. Falling back to batch mode.", e);
                        None
                    }
                };

                if let Some(handle) = stream_handle {
                    let mut vad = crate::vad::SileroVadSession::new().ok();
                    let mut raw_processed_len = 0;
                    let mut stream_16k_buf = Vec::<i16>::new();
                    let mut pre_speech_ring = std::collections::VecDeque::<i16>::with_capacity(4000);
                    let mut active_phrase_samples = Vec::<i16>::new();
                    let mut is_in_speech = false;
                    let mut is_aborted = false;

                    let mut last_sound_time = std::time::Instant::now();
                    let mut silence_countdown_active = false;
                    let mut last_countdown_sec: Option<u64> = None;
                    let mut had_ever_spoken = false;

                    match handle.mode {
                        crate::stream::NegotiatedStreamMode::ContinuousStream => {
                            let frame_samples = 8960; // 560ms @ 16kHz (aligned with native real-time ASR acoustic geometry)
                            loop {
                                match rx.recv_timeout(std::time::Duration::from_millis(20)) {
                                    Ok(true) => {
                                        // Hotkey released normally
                                        break;
                                    }
                                    Ok(false) => {
                                        // Aborted
                                        is_aborted = true;
                                        break;
                                    }
                                    Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                                        let current_vol = crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed);
                                        let is_active_sound = current_vol >= 6;
                                        if is_active_sound {
                                            last_sound_time = std::time::Instant::now();
                                            had_ever_spoken = true;
                                            if silence_countdown_active {
                                                silence_countdown_active = false;
                                                last_countdown_sec = None;
                                                let is_cmd = context.is_command_mode;
                                                if is_cmd {
                                                    crate::win32::set_preview_text("✨ Command Mode (Listening...)");
                                                } else {
                                                    crate::win32::set_preview_text("");
                                                }
                                            }
                                        } else {
                                            let silence_dur = last_sound_time.elapsed();
                                            if silence_dur >= std::time::Duration::from_secs(15) {
                                                println!("[AUDIO STREAM] 15 seconds of silence detected. Auto-stopping recording...");
                                                context.capture_finished();
                                                if !had_ever_spoken {
                                                    is_aborted = true;
                                                }
                                                break;
                                            } else if silence_dur >= std::time::Duration::from_secs(10) {
                                                let remaining = 15 - silence_dur.as_secs();
                                                if last_countdown_sec != Some(remaining) && remaining >= 1 {
                                                    last_countdown_sec = Some(remaining);
                                                    silence_countdown_active = true;
                                                    crate::win32::set_preview_text(&format!("Silence detected — stopping in {}s...", remaining));
                                                }
                                            }
                                        }

                                        let new_raw_samples: Vec<i16> = {
                                            if let Ok(buf) = samples_buf.lock() {
                                                if buf.len() > raw_processed_len {
                                                    let chunk = buf[raw_processed_len..].to_vec();
                                                    raw_processed_len = buf.len();
                                                    chunk
                                                } else {
                                                    Vec::new()
                                                }
                                            } else {
                                                Vec::new()
                                            }
                                        };

                                        if !new_raw_samples.is_empty() {
                                            let new_16k = resample_to_16k_mono(&new_raw_samples, sample_rate, channels);
                                            stream_16k_buf.extend_from_slice(&new_16k);
                                        }

                                        while stream_16k_buf.len() >= frame_samples {
                                            let frame_i16: Vec<i16> = stream_16k_buf.drain(..frame_samples).collect();
                                            if let Ok(ogg_bytes) = encode_opus_ogg(&frame_i16, 16000, 1) {
                                                let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(ogg_bytes));
                                            }
                                        }
                                    }
                                    Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                                        is_aborted = true;
                                        break;
                                    }
                                }
                            }
                        }
                        crate::stream::NegotiatedStreamMode::ChunkedVad => {
                            loop {
                                match rx.recv_timeout(std::time::Duration::from_millis(32)) {
                                    Ok(true) => {
                                        // Hotkey released normally
                                        break;
                                    }
                                    Ok(false) => {
                                        // Aborted
                                        is_aborted = true;
                                        break;
                                    }
                                    Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                                        let current_vol = crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed);
                                        let is_active_sound = is_in_speech || current_vol >= 6;
                                        if is_active_sound {
                                            last_sound_time = std::time::Instant::now();
                                            had_ever_spoken = true;
                                            if silence_countdown_active {
                                                silence_countdown_active = false;
                                                last_countdown_sec = None;
                                                let is_cmd = context.is_command_mode;
                                                if is_cmd {
                                                    crate::win32::set_preview_text("✨ Command Mode (Listening...)");
                                                } else {
                                                    crate::win32::set_preview_text("");
                                                }
                                            }
                                        } else {
                                            let silence_dur = last_sound_time.elapsed();
                                            if silence_dur >= std::time::Duration::from_secs(15) {
                                                println!("[AUDIO CHUNKED] 15 seconds of silence detected. Auto-stopping recording...");
                                                context.capture_finished();
                                                if !had_ever_spoken {
                                                    is_aborted = true;
                                                }
                                                break;
                                            } else if silence_dur >= std::time::Duration::from_secs(10) {
                                                let remaining = 15 - silence_dur.as_secs();
                                                if last_countdown_sec != Some(remaining) && remaining >= 1 {
                                                    last_countdown_sec = Some(remaining);
                                                    silence_countdown_active = true;
                                                    crate::win32::set_preview_text(&format!("Silence detected — stopping in {}s...", remaining));
                                                }
                                            }
                                        }

                                        // Incrementally grab ONLY newly arrived raw samples
                                        let new_raw_samples: Vec<i16> = {
                                            if let Ok(buf) = samples_buf.lock() {
                                                if buf.len() > raw_processed_len {
                                                    let chunk = buf[raw_processed_len..].to_vec();
                                                    raw_processed_len = buf.len();
                                                    chunk
                                                } else {
                                                    Vec::new()
                                                }
                                            } else {
                                                Vec::new()
                                            }
                                        };

                                        if !new_raw_samples.is_empty() {
                                            let new_16k = resample_to_16k_mono(&new_raw_samples, sample_rate, channels);
                                            stream_16k_buf.extend_from_slice(&new_16k);
                                        }

                                        if let Some(ref mut vad_inst) = vad {
                                            while stream_16k_buf.len() >= 512 {
                                                let frame_i16: Vec<i16> = stream_16k_buf.drain(..512).collect();
                                                let frame_f32: Vec<f32> = frame_i16.iter().map(|&s| s as f32 / 32768.0).collect();
                                                let (_prob, speaking, pause_detected) = vad_inst.process_frame(&frame_f32);

                                                if speaking {
                                                    if !is_in_speech {
                                                        is_in_speech = true;
                                                        // Prepend pre-speech buffer so leading consonants are never clipped
                                                        active_phrase_samples.extend(pre_speech_ring.drain(..));
                                                    }
                                                    active_phrase_samples.extend_from_slice(&frame_i16);
                                                } else {
                                                    if is_in_speech {
                                                        // Keep appending trailing frames until pause is finalized
                                                        active_phrase_samples.extend_from_slice(&frame_i16);
                                                    } else {
                                                        for &s in &frame_i16 {
                                                            if pre_speech_ring.len() >= 4000 { // ~250ms rolling buffer
                                                                pre_speech_ring.pop_front();
                                                            }
                                                            pre_speech_ring.push_back(s);
                                                        }
                                                    }
                                                }

                                                let should_commit = pause_detected || active_phrase_samples.len() >= 80000; // max 5s chunk

                                                if should_commit && active_phrase_samples.len() >= 4800 { // >= 300ms of audio
                                                    is_in_speech = false;
                                                    let t_enc = std::time::Instant::now();
                                                    match encode_opus_ogg(&active_phrase_samples, 16000, 1) {
                                                        Ok(ogg_bytes) => {
                                                            let enc_dur = t_enc.elapsed();
                                                            let pcm_len = active_phrase_samples.len() * 2;
                                                            let ogg_len = ogg_bytes.len();
                                                            let ratio = if ogg_len > 0 { pcm_len as f64 / ogg_len as f64 } else { 1.0 };
                                                            let audio_sec = active_phrase_samples.len() as f64 / 16000.0;
                                                            println!("[CLIENT BENCHMARK] Opus Ogg encode: {:.2?} (Audio: {:.2}s | Raw PCM: {} B -> Opus Ogg: {} B | {:.1}x compression)",
                                                                enc_dur, audio_sec, pcm_len, ogg_len, ratio);
                                                            let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(ogg_bytes));
                                                        }
                                                        Err(e) => {
                                                            eprintln!("[STREAM ERROR] Failed to encode Opus Ogg: {}", e);
                                                        }
                                                    }

                                                    // Retain trailing 200ms (3200 samples) for acoustic continuity
                                                    let overlap_samples = 3200;
                                                    let retained = if active_phrase_samples.len() > overlap_samples {
                                                        active_phrase_samples[active_phrase_samples.len() - overlap_samples..].to_vec()
                                                    } else {
                                                        Vec::new()
                                                    };
                                                    active_phrase_samples = retained;
                                                    vad_inst.reset();
                                                }
                                            }
                                        } else {
                                            // Fallback if VAD failed to init: 2-second periodic chunking
                                            if stream_16k_buf.len() >= 32000 {
                                                let chunk: Vec<i16> = stream_16k_buf.drain(..32000).collect();
                                                let t_enc = std::time::Instant::now();
                                                if let Ok(ogg_bytes) = encode_opus_ogg(&chunk, 16000, 1) {
                                                    let enc_dur = t_enc.elapsed();
                                                    println!("[CLIENT BENCHMARK] Opus Ogg fallback encode: {:.2?}", enc_dur);
                                                    let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(ogg_bytes));
                                                }
                                            }
                                        }
                                    }
                                    Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                                        is_aborted = true;
                                        break;
                                    }
                                }
                            }
                        }
                    }

                    drop(s);
                    CURRENT_VOLUME.store(0, Ordering::Relaxed);
                    println!("Recording stopped.");

                    context.capture_finished();
                    let is_command_mode = context.is_command_mode;
                    let selected_text = context.selected_text.clone();

                    if !is_aborted {
                        crate::win32::set_preview_state(crate::win32::PreviewState::Processing);

                        // Drain any remaining tail audio samples sitting in buffer
                        let remaining_raw_samples: Vec<i16> = {
                            if let Ok(buf) = samples_buf.lock() {
                                if buf.len() > raw_processed_len {
                                    buf[raw_processed_len..].to_vec()
                                } else {
                                    Vec::new()
                                }
                            } else {
                                Vec::new()
                            }
                        };
                        if !remaining_raw_samples.is_empty() {
                            let rem_16k = resample_to_16k_mono(&remaining_raw_samples, sample_rate, channels);
                            stream_16k_buf.extend_from_slice(&rem_16k);
                        }

                        if handle.mode == crate::stream::NegotiatedStreamMode::ContinuousStream {
                            if stream_16k_buf.len() >= 480 { // >= 30ms
                                if let Ok(ogg_bytes) = encode_opus_ogg(&stream_16k_buf, 16000, 1) {
                                    let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(ogg_bytes));
                                }
                            }
                        } else {
                            active_phrase_samples.extend(stream_16k_buf.drain(..));

                            // Only send tail phrase if there is genuine audio beyond the retained 200ms silence buffer
                            let overlap_samples = 3200;
                            if active_phrase_samples.len() > overlap_samples + 1600 { // at least 100ms of new audio beyond overlap
                                let t_enc = std::time::Instant::now();
                                if let Ok(ogg_bytes) = encode_opus_ogg(&active_phrase_samples, 16000, 1) {
                                    let enc_dur = t_enc.elapsed();
                                    let pcm_len = active_phrase_samples.len() * 2;
                                    let ogg_len = ogg_bytes.len();
                                    let ratio = if ogg_len > 0 { pcm_len as f64 / ogg_len as f64 } else { 1.0 };
                                    let audio_sec = active_phrase_samples.len() as f64 / 16000.0;
                                    println!("[CLIENT BENCHMARK] Tail Opus Ogg encode: {:.2?} (Audio: {:.2}s | Raw PCM: {} B -> Opus Ogg: {} B | {:.1}x compression)",
                                        enc_dur, audio_sec, pcm_len, ogg_len, ratio);
                                    let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(ogg_bytes));
                                }
                            }
                            active_phrase_samples.clear();
                        }

                        let _ = handle.tx.send(crate::stream::StreamAudioMessage::EndOfStream);

                        let mut stream_error_msg: Option<String> = None;
                        let mut stream_timed_out = false;

                        let final_text_to_inject = match handle.rx_final.recv_timeout(std::time::Duration::from_secs(15)) {
                            Ok(Ok(final_result)) => {
                                println!("[STREAM SUCCESS] Live streaming completed. Final text: {}", final_result.text);
                                let trimmed = final_result.text.trim().to_string();
                                let interim = handle.interim_text();
                                let interim_trimmed = interim.trim().to_string();

                                let final_words = trimmed.split_whitespace().count();
                                let interim_words = interim_trimmed.split_whitespace().count();

                                // Truncation guard: if final result dropped more than 40% of interim words (when interim >= 6 words),
                                // keep the fuller interim transcript to avoid losing sentences.
                                if interim_words >= 6 && final_words < (interim_words * 6) / 10 {
                                    println!("[STREAM TRUNCATION GUARD] Final text ({} words) dropped substantial text vs interim ({} words). Keeping interim transcript to prevent loss.",
                                        final_words, interim_words);
                                    Some(interim_trimmed)
                                } else if !trimmed.is_empty() {
                                    Some(trimmed)
                                } else if !interim_trimmed.is_empty() {
                                    Some(interim_trimmed)
                                } else {
                                    None
                                }
                            }
                            Ok(Err(e)) => {
                                eprintln!("[STREAM ERROR] Stream returned error: {}. Falling back to interim preview text.", e);
                                stream_error_msg = Some(e);
                                let fallback = handle.interim_text();
                                let trimmed = fallback.trim().to_string();
                                if !trimmed.is_empty() { Some(trimmed) } else { None }
                            }
                            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                                eprintln!("[STREAM TIMEOUT] Final transcript timed out after 15s. Falling back to interim preview text.");
                                stream_timed_out = true;
                                let fallback = handle.interim_text();
                                let trimmed = fallback.trim().to_string();
                                if !trimmed.is_empty() { Some(trimmed) } else { None }
                            }
                            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                                eprintln!("[STREAM DISCONNECTED] Stream disconnected before final transcript. Falling back to interim preview text.");
                                let fallback = handle.interim_text();
                                let trimmed = fallback.trim().to_string();
                                if !trimmed.is_empty() { Some(trimmed) } else { None }
                            }
                        };


                        let (api_url, session_token) = {
                            let cfg = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                            (
                                cfg.api_url.clone().unwrap_or_else(|| "https://api.speekto.me".to_string()),
                                cfg.session_token.clone().unwrap_or_default(),
                            )
                        };

                        if let Some(normalized) = final_text_to_inject {
                            crate::win32::set_preview_text(&normalized);

                            if is_command_mode {
                                println!("\n=======================================================");
                                println!("✨ [COMMAND MODE COMPLETED — EXECUTING LLM TRANSFORMATION]");
                                println!("Target Selected Text:\n{}", selected_text.as_deref().unwrap_or("(none)"));
                                println!("-------------------------------------------------------");
                                println!("Spoken User Command: \"{}\"", normalized);
                                println!("=======================================================\n");

                                // Dynamic word limit check
                                let max_words = crate::get_transform_word_limit();
                                if let Some(ref target) = selected_text {
                                    let word_count = target.split_whitespace().count();
                                    if word_count > max_words {
                                        println!("[COMMAND MODE WARNING] Selected text exceeds {} words ({} words).", max_words, word_count);
                                        crate::win32::set_preview_text(&format!("⚠️ Text exceeds {} words", max_words));
                                        crate::win32::show_tray_notification(
                                            "SpeekTo.Me — Text Too Long",
                                            &format!("Text longer than {} words is not supported at the moment.", max_words),
                                            crate::win32::NotificationLevel::Warning,
                                        );
                                        std::thread::sleep(std::time::Duration::from_millis(1500));
                                        crate::win32::hide_overlay();
                                        crate::win32::hide_preview();
                                        return;
                                    }
                                }

                                let target_text = selected_text.unwrap_or_default();
                                let has_target = !target_text.trim().is_empty();

                                crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
                                if has_target {
                                    crate::win32::set_preview_text("✨ Transforming text...");
                                } else {
                                    crate::win32::set_preview_text("✨ Generating...");
                                }

                                let api_url_clone = api_url.clone();
                                let token_clone = session_token.clone();
                                let target_clone = target_text;
                                let cmd_clone = normalized.clone();

                                let transform_context = context.clone();
                                std::thread::spawn(move || {
                                    let _context = transform_context;
                                    let rt = match tokio::runtime::Builder::new_current_thread()
                                        .enable_all()
                                        .build()
                                    {
                                        Ok(r) => r,
                                        Err(e) => {
                                            eprintln!("[TRANSFORM ERROR] Failed to create runtime: {}", e);
                                            crate::win32::hide_overlay();
                                            crate::win32::hide_preview();
                                            return;
                                        }
                                    };

                                    let typing_mode = {
                                        let cfg = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                                        cfg.typing_mode
                                    };

                                    let mut is_first_chunk = true;
                                    let mut last_ui_update = std::time::Instant::now();

                                    match rt.block_on(crate::api::transform_text_stream(
                                        &api_url_clone,
                                        &token_clone,
                                        &target_clone,
                                        &cmd_clone,
                                        move |delta, accumulated| {
                                            if delta == "[RESET]" {
                                                println!("[STREAM] Received [RESET], resetting stream UI preview");
                                                crate::win32::set_preview_text("✨ Retrying with backup AI...");
                                                is_first_chunk = true;
                                                return;
                                            }

                                            if is_first_chunk {
                                                if has_target && typing_mode {
                                                    crate::keyboard::simulate_single_key(0x08);
                                                    std::thread::sleep(std::time::Duration::from_millis(20));
                                                }
                                                is_first_chunk = false;
                                            }

                                            if typing_mode {
                                                crate::keyboard::simulate_typing(delta);
                                            }

                                            if last_ui_update.elapsed() >= std::time::Duration::from_millis(80) {
                                                last_ui_update = std::time::Instant::now();
                                                let preview_display = if let Some((start_byte, _)) = accumulated.char_indices().rev().nth(31) {
                                                    let tail = &accumulated[start_byte..];
                                                    format!("✨ ...{}", tail.trim_start())
                                                } else {
                                                    format!("✨ {}", accumulated)
                                                };
                                                crate::win32::set_preview_text(&preview_display);
                                            }
                                        },
                                    )) {
                                        Ok(transformed) => {
                                            println!("\n=======================================================");
                                            println!("✨ [COMMAND MODE — LLM TRANSFORMATION COMPLETED]");
                                            println!("Transformed Output ({} characters):", transformed.len());
                                            println!("-------------------------------------------------------");
                                            println!("{}", transformed);
                                            println!("=======================================================\n");

                                            crate::win32::set_preview_text("✨ Done!");

                                            if !typing_mode {
                                                crate::keyboard::paste_transformed_text(&transformed);
                                            } else {
                                                // Permanently leave transformed text in clipboard as a fallback
                                                if let Ok(mut ctx) = arboard::Clipboard::new() {
                                                    let _ = ctx.set_text(crate::keyboard::normalize_newlines(&transformed));
                                                }
                                            }

                                            let (notify_title, notify_msg) = if has_target {
                                                (
                                                    "SpeekTo.Me — Transformed Text Ready",
                                                    "Transformed text replaced selection and is copied to clipboard.",
                                                )
                                            } else {
                                                (
                                                    "SpeekTo.Me — AI Response Ready",
                                                    "AI generated text inserted and is copied to clipboard.",
                                                )
                                            };

                                            crate::win32::show_tray_notification(
                                                notify_title,
                                                notify_msg,
                                                crate::win32::NotificationLevel::Info,
                                            );

                                            if let Some(app) = crate::TAURI_APP.get() {
                                                use tauri::Emitter;
                                                let _ = app.emit("command-transformed", transformed);
                                            }

                                            std::thread::sleep(std::time::Duration::from_millis(800));
                                            crate::win32::hide_overlay();
                                            crate::win32::hide_preview();
                                        }
                                        Err(err) => {
                                            eprintln!("[TRANSFORM ERROR] {}", err);
                                            crate::win32::set_preview_text("⚠️ Transformation failed");
                                            crate::win32::show_tray_notification(
                                                "SpeekTo.Me — Transformation Failed",
                                                &err,
                                                crate::win32::NotificationLevel::Warning,
                                            );
                                            std::thread::sleep(std::time::Duration::from_millis(1500));
                                            crate::win32::hide_overlay();
                                            crate::win32::hide_preview();
                                        }
                                    }
                                });
                            } else {
                                let typing_mode = {
                                    let cfg = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                                    cfg.typing_mode
                                };

                                println!("[STREAM INJECTION] Single-shot inject (typing_mode: {}): {}", typing_mode, normalized);
                                if typing_mode {
                                    crate::keyboard::simulate_typing(&normalized);
                                } else {
                                    crate::keyboard::paste_text(&normalized);
                                }

                                if let Some(app) = crate::TAURI_APP.get() {
                                    use tauri::Emitter;
                                    let _ = app.emit("dictation-transcribed", normalized.clone());
                                }
                            }
                        } else {
                            // If no text could be injected, notify the user if an error occurred
                            if let Some(err) = stream_error_msg {
                                let err_lower = err.to_lowercase();
                                if err_lower.contains("401") || err_lower.contains("unauthorized") || err_lower.contains("invalid token") || err_lower.contains("token expired") {
                                    crate::handle_session_expired(Some("Your session has expired. Please sign in again to continue dictating."));
                                } else if err_lower.contains("limit") || err_lower.contains("quota") || err_lower.contains("429") {
                                    crate::win32::show_tray_notification(
                                        "SpeekTo.Me — Usage Limit Reached",
                                        "You have reached your monthly transcription limit. Upgrade your plan in Settings.",
                                        crate::win32::NotificationLevel::Warning,
                                    );
                                    if let Some(app) = crate::TAURI_APP.get() {
                                        crate::show_or_create_settings_window(app);
                                    }
                                } else if err_lower.contains("network") || err_lower.contains("timeout") || err_lower.contains("connection refused") || err_lower.contains("websocket connection failed") {
                                    crate::win32::show_tray_notification(
                                        "SpeekTo.Me — Network Error",
                                        "Could not connect to SpeekTo.Me server. Please check your internet connection.",
                                        crate::win32::NotificationLevel::Error,
                                    );
                                } else {
                                    crate::win32::show_tray_notification(
                                        "SpeekTo.Me — Transcription Error",
                                        &format!("Live streaming error: {}", err),
                                        crate::win32::NotificationLevel::Error,
                                    );
                                }
                            } else if stream_timed_out {
                                crate::win32::show_tray_notification(
                                    "SpeekTo.Me — Server Timeout",
                                    "Transcription timed out waiting for server response.",
                                    crate::win32::NotificationLevel::Warning,
                                );
                            }
                        }
                    } else {
                        println!("Recording aborted and discarded.");
                    }
                    if !is_command_mode {
                        crate::win32::hide_overlay();
                        crate::win32::hide_preview();
                    }
                    return;
                }
            }

            // Fallback / standard batch mode: Wait for key release/abort signal with 15s silence detection
            let mut last_sound_time = std::time::Instant::now();
            let mut silence_countdown_active = false;
            let mut last_countdown_sec: Option<u64> = None;
            let mut had_ever_spoken = false;
            let mut batch_aborted = false;

            loop {
                match rx.recv_timeout(std::time::Duration::from_millis(50)) {
                    Ok(true) => {
                        // Hotkey released normally
                        break;
                    }
                    Ok(false) => {
                        batch_aborted = true;
                        break;
                    }
                    Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                        let current_vol = CURRENT_VOLUME.load(Ordering::Relaxed);
                        let is_active_sound = current_vol >= 6;
                        if is_active_sound {
                            last_sound_time = std::time::Instant::now();
                            had_ever_spoken = true;
                            if silence_countdown_active {
                                silence_countdown_active = false;
                                last_countdown_sec = None;
                                let is_cmd = context.is_command_mode;
                                if is_cmd {
                                    crate::win32::set_preview_text("✨ Command Mode (Listening...)");
                                } else {
                                    crate::win32::set_preview_text("");
                                }
                            }
                        } else {
                            let silence_dur = last_sound_time.elapsed();
                            if silence_dur >= std::time::Duration::from_secs(15) {
                                println!("[AUDIO BATCH] 15 seconds of silence detected. Auto-stopping recording...");
                                context.capture_finished();
                                if !had_ever_spoken {
                                    batch_aborted = true;
                                }
                                break;
                            } else if silence_dur >= std::time::Duration::from_secs(10) {
                                let remaining = 15 - silence_dur.as_secs();
                                if last_countdown_sec != Some(remaining) && remaining >= 1 {
                                    last_countdown_sec = Some(remaining);
                                    silence_countdown_active = true;
                                    crate::win32::set_preview_text(&format!("Silence detected — stopping in {}s...", remaining));
                                }
                            }
                        }
                    }
                    Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                        batch_aborted = true;
                        break;
                    }
                }
            }

            if !batch_aborted {
                // Drop stream to stop recording
                drop(s);
                CURRENT_VOLUME.store(0, Ordering::Relaxed);
                println!("Recording stopped.");
                crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
                
                let samples = samples_buf.lock().unwrap_or_else(|e| e.into_inner()).clone();
                if !samples.is_empty() {
                    // Calculate if there is active speech (VAD Filter)
                    let has_speech = is_speech(&samples, sample_rate, channels);
                    println!("Recording finished. Samples: {}, Speech detected: {}", samples.len(), has_speech);
                    
                    if has_speech {
                        context.capture_finished();
                        crate::api::process_and_paste(samples, sample_rate, channels, language, context);
                    } else {
                        println!("Silence or keyboard click detected. Transcription request skipped.");
                        crate::win32::hide_overlay();
                        crate::win32::hide_preview();
                    }
                } else {
                    crate::win32::hide_overlay();
                    crate::win32::hide_preview();
                }
            } else {
                drop(s);
                CURRENT_VOLUME.store(0, Ordering::Relaxed);
                println!("Recording aborted and discarded.");
                crate::win32::hide_overlay();
                crate::win32::hide_preview();
            }
        }
        Err(e) => {
            eprintln!("Failed to build input stream: {:?}", e);
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
            crate::win32::show_tray_notification(
                "SpeekTo.Me — Audio Error",
                "Failed to initialize the microphone capture stream.",
                crate::win32::NotificationLevel::Error,
            );
        }
    }
}



pub static MIC_VOLUME: AtomicU32 = AtomicU32::new(0);
pub static LOOPBACK_VOLUME: AtomicU32 = AtomicU32::new(0);

pub struct SendStream(pub cpal::Stream);
unsafe impl Send for SendStream {}
unsafe impl Sync for SendStream {}

pub struct MeetingRecordingState {
    pub mic_stream: Option<SendStream>,
    pub loopback_stream: Option<SendStream>,
    pub shutdown_tx: Option<std::sync::mpsc::Sender<()>>,
 pub recording_thread: Option<std::thread::JoinHandle<Result<(),String>>>,
    #[cfg(target_os = "linux")]
    pub pulse_loopback_shutdown: Option<std::sync::Arc<std::sync::atomic::AtomicBool>>,
    #[cfg(target_os = "linux")]
    pub pulse_loopback_thread: Option<std::thread::JoinHandle<()>>,
}

pub static MEETING_STATE: std::sync::OnceLock<Mutex<MeetingRecordingState>> = std::sync::OnceLock::new();

pub fn get_meeting_state() -> &'static Mutex<MeetingRecordingState> {
    MEETING_STATE.get_or_init(|| {
        Mutex::new(MeetingRecordingState {
            mic_stream: None,
            loopback_stream: None,
            shutdown_tx: None,
            recording_thread: None,
            #[cfg(target_os = "linux")]
            pulse_loopback_shutdown: None,
            #[cfg(target_os = "linux")]
            pulse_loopback_thread: None,
        })
    })
}

pub fn start_meeting_recording(
    output_ogg_path: std::path::PathBuf,
    stream_tx: Option<std::sync::mpsc::Sender<crate::meeting_stream::MeetingStreamAudioMessage>>,
) -> Result<(), Box<dyn std::error::Error>> {
    println!("[AUDIO MEETING LOG] Querying default input (microphone) device...");
    let host = cpal::default_host();
    let mic_device = host.default_input_device().ok_or("No input device found")?;
    let mic_name = mic_device.name().unwrap_or_else(|_| "Unknown Mic".to_string());
    let mic_config = mic_device.default_input_config()?;
    let mic_sample_format = mic_config.sample_format();
    let mic_rate = mic_config.sample_rate().0;
    let mic_channels = mic_config.channels();
    println!("[AUDIO MEETING LOG] Microphone device: '{}', rate: {}Hz, channels: {}, format: {:?}", mic_name, mic_rate, mic_channels, mic_sample_format);

    let mic_buffer = Arc::new(Mutex::new(Vec::new()));
    let mic_buffer_clone = mic_buffer.clone();

    let loopback_buffer = Arc::new(Mutex::new(Vec::new()));

    let mic_err_fn = |err| eprintln!("[AUDIO MEETING LOG ERROR] Microphone stream error: {}", err);
    let mic_stream = match mic_sample_format {
        cpal::SampleFormat::F32 => {
            mic_device.build_input_stream(
                &mic_config.clone().into(),
                move |data: &[f32], _| {
                    if let Ok(mut buf) = mic_buffer_clone.lock() {
                        for &sample in data {
                            let i16_val = (sample * i16::MAX as f32) as i16;
                            buf.push(i16_val);
                        }
                    }
                },
                mic_err_fn,
                None
            )?
        }
        cpal::SampleFormat::I16 => {
            mic_device.build_input_stream(
                &mic_config.clone().into(),
                move |data: &[i16], _| {
                    if let Ok(mut buf) = mic_buffer_clone.lock() {
                        buf.extend_from_slice(data);
                    }
                },
                mic_err_fn,
                None
            )?
        }
        cpal::SampleFormat::U16 => {
            mic_device.build_input_stream(
                &mic_config.clone().into(),
                move |data: &[u16], _| {
                    if let Ok(mut buf) = mic_buffer_clone.lock() {
                        for &sample in data {
                            let i16_val = (sample as i32 - 32768) as i16;
                            buf.push(i16_val);
                        }
                    }
                },
                mic_err_fn,
                None
            )?
        }
        _ => return Err("Unsupported microphone sample format".into()),
    };

    #[cfg(windows)]
    let mut loopback_rate = 48000;
    #[cfg(windows)]
    let mut loopback_channels = 2;
    #[cfg(windows)]
    let loopback_stream_opt = {
        println!("[AUDIO MEETING LOG] Querying default output (loopback) device...");
        let mut stream_opt = None;
        if let Some(loopback_device) = host.default_output_device() {
            let out_name = loopback_device.name().unwrap_or_else(|_| "Unknown Output".to_string());
            if let Ok(config) = loopback_device.default_output_config() {
                loopback_rate = config.sample_rate().0;
                loopback_channels = config.channels();
                let buffer = loopback_buffer.clone();
                let err = |e| eprintln!("[AUDIO MEETING LOG ERROR] Loopback stream error: {e}");
                stream_opt = match config.sample_format() {
                    cpal::SampleFormat::F32 => loopback_device.build_input_stream(&config.clone().into(), move |d: &[f32], _| {
                        let rms = if d.is_empty() { 0.0 } else { (d.iter().map(|x| x*x).sum::<f32>() / d.len() as f32).sqrt() };
                        LOOPBACK_VOLUME.store((rms * 2500.0).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
                        if let Ok(mut b) = buffer.lock() { b.extend(d.iter().map(|x| (x * i16::MAX as f32).clamp(i16::MIN as f32, i16::MAX as f32) as i16)); }
                    }, err, None).ok(),
                    cpal::SampleFormat::I16 => loopback_device.build_input_stream(&config.clone().into(), move |d: &[i16], _| {
                        let rms = if d.is_empty() { 0.0 } else { (d.iter().map(|x| (*x as f32 / i16::MAX as f32).powi(2)).sum::<f32>() / d.len() as f32).sqrt() };
                        LOOPBACK_VOLUME.store((rms * 2500.0).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
                        if let Ok(mut b) = buffer.lock() { b.extend_from_slice(d); }
                    }, err, None).ok(),
                    cpal::SampleFormat::U16 => loopback_device.build_input_stream(&config.clone().into(), move |d: &[u16], _| {
                        let rms = if d.is_empty() { 0.0 } else { (d.iter().map(|x| ((*x as f32 - 32768.0) / 32768.0).powi(2)).sum::<f32>() / d.len() as f32).sqrt() };
                        LOOPBACK_VOLUME.store((rms * 2500.0).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
                        if let Ok(mut b) = buffer.lock() { b.extend(d.iter().map(|x| (*x as i32 - 32768) as i16)); }
                    }, err, None).ok(),
                    other => { eprintln!("[AUDIO MEETING LOG WARNING] Unsupported loopback format: {other:?}"); None }
                };
                println!("[AUDIO MEETING LOG] Windows loopback device '{out_name}', {loopback_rate}Hz, {loopback_channels} channels");
            }
        }
        stream_opt
    };

    #[cfg(target_os = "linux")]
    let (loopback_rate, loopback_channels, pulse_loopback_shutdown, pulse_loopback_thread) = {
        // PipeWire provides this PulseAudio protocol source even when the session uses
        // PipeWire natively. The special name resolves to the monitor of the default sink.
        let rate = 48_000u32;
        let channels = 2u8;
        let spec = PulseSpec { format: PulseFormat::S16NE, channels, rate };
        if !spec.is_valid() { return Err("Invalid PulseAudio monitor capture format".into()); }
        let stream = PulseSimple::new(
            None,
            "SpeekTo.Me",
            PulseDirection::Record,
            Some("@DEFAULT_MONITOR@"),
            "Meeting output capture",
            &spec,
            None,
            None,
        ).map_err(|e| format!("Could not open the default output monitor through PulseAudio/PipeWire: {e}"))?;
        let shutdown = Arc::new(std::sync::atomic::AtomicBool::new(false));
        let thread_shutdown = shutdown.clone();
        let buffer = loopback_buffer.clone();
        let thread = std::thread::Builder::new().name("speektome-output-monitor".into()).spawn(move || {
            // Read 10 ms blocks so shutdown is observed promptly and the capture buffer
            // advances continuously, including during silence, preserving the 48 kHz clock.
            const FRAMES: usize = 480;
            let mut bytes = [0u8; FRAMES * 2 * 2];
            while !thread_shutdown.load(Ordering::Relaxed) {
                if let Err(e) = stream.read(&mut bytes) {
                    eprintln!("[AUDIO MEETING LOG ERROR] Output monitor capture stopped: {e}");
                    break;
                }
                if thread_shutdown.load(Ordering::Relaxed) { break; }
                let rms = bytes.chunks_exact(2).map(|b| {
                    let x = i16::from_ne_bytes([b[0], b[1]]) as f32 / i16::MAX as f32;
                    x * x
                }).sum::<f32>() / (FRAMES * channels as usize) as f32;
                LOOPBACK_VOLUME.store((rms.sqrt() * 2500.0).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
                if let Ok(mut out) = buffer.lock() {
                    out.extend(bytes.chunks_exact(2).map(|b| i16::from_ne_bytes([b[0], b[1]])));
                }
            }
        })?;
        println!("[AUDIO MEETING LOG] PulseAudio/PipeWire default sink monitor capture started at {rate}Hz, {channels} channels");
        (rate, channels as u16, Some(shutdown), Some(thread))
    };

    #[cfg(not(any(windows, target_os = "linux")))]
    let (loopback_rate, loopback_channels) = (48000u32, 2u16);
    #[cfg(not(any(windows, target_os = "linux")))]
    let loopback_stream_opt: Option<cpal::Stream> = None;
    let (shutdown_tx, shutdown_rx) = std::sync::mpsc::channel::<()>();

    println!("[AUDIO MEETING LOG] Starting mic stream playback...");
    mic_stream.play()?;
    println!("[AUDIO MEETING LOG] Mic stream playing!");

    #[cfg(windows)]
    if let Some(ref lb) = loopback_stream_opt {
        println!("[AUDIO MEETING LOG] Starting loopback stream playback...");
        if let Err(e) = lb.play() {
            eprintln!("[AUDIO MEETING LOG WARNING] Failed to start loopback stream playback: {}", e);
        } else {
            println!("[AUDIO MEETING LOG] Loopback stream playing!");
        }
    }

    let (recording_ready_tx, recording_ready_rx) = std::sync::mpsc::channel();
    let recording_thread=std::thread::spawn(move || {
        let mic_path = {
            let mut p = output_ogg_path.clone();
            p.set_extension("mic.ogg");
            p
        };
        let loopback_path = {
            let mut p = output_ogg_path.clone();
            p.set_extension("loopback.ogg");
            p
        };

        let mut encoder_mixed = match opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip) {
            Ok(enc) => enc,
            Err(e) => {
                eprintln!("Failed to create Opus mixed encoder: {:?}", e);
                return Err(format!("Audio recording initialization failed: {:?}", e));
            }
        };
        let mut encoder_mic = match opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip) {
            Ok(enc) => enc,
            Err(e) => {
                eprintln!("Failed to create Opus mic encoder: {:?}", e);
                return Err(format!("Audio recording initialization failed: {:?}", e));
            }
        };
        let mut encoder_loopback = match opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip) {
            Ok(enc) => enc,
            Err(e) => {
                eprintln!("Failed to create Opus loopback encoder: {:?}", e);
                return Err(format!("Audio recording initialization failed: {:?}", e));
            }
        };

        for enc in [&mut encoder_mixed, &mut encoder_mic, &mut encoder_loopback] {
            let _ = enc.set_bitrate(opus::Bitrate::Bits(24000));
            let _ = enc.set_complexity(1);
        }

        let file_mixed = match std::fs::File::create(&output_ogg_path) {
            Ok(f) => f,
            Err(e) => {
                eprintln!("Failed to create mixed Ogg output file: {:?}", e);
                return Err(format!("Audio recording initialization failed: {:?}", e));
            }
        };
        let file_mic = match std::fs::File::create(&mic_path) {
            Ok(f) => f,
            Err(e) => {
                eprintln!("Failed to create mic Ogg output file: {:?}", e);
                return Err(format!("Audio recording initialization failed: {:?}", e));
            }
        };
        let file_loopback = match std::fs::File::create(&loopback_path) {
            Ok(f) => f,
            Err(e) => {
                eprintln!("Failed to create loopback Ogg output file: {:?}", e);
                return Err(format!("Audio recording initialization failed: {:?}", e));
            }
        };

        let mut writer_mixed = ogg::writing::PacketWriter::new(file_mixed);
        let mut writer_mic = ogg::writing::PacketWriter::new(file_mic);
        let mut writer_loopback = ogg::writing::PacketWriter::new(file_loopback);

        let wav_path = output_ogg_path.with_extension("wav");
        let wav_spec = hound::WavSpec {
            channels: 1,
            sample_rate: 16000,
            bits_per_sample: 16,
            sample_format: hound::SampleFormat::Int,
        };
        let mut wav_writer = hound::WavWriter::create(&wav_path, wav_spec).ok();

        let serial_mixed = 987654321;
        let serial_mic = 123456789;
        let serial_lb = 555555555;

        let mut head = Vec::new();
        head.extend_from_slice(b"OpusHead");
        head.push(1);
        head.push(1);
        head.extend_from_slice(&312u16.to_le_bytes());
        head.extend_from_slice(&16000u32.to_le_bytes());
        head.extend_from_slice(&0i16.to_le_bytes());
        head.push(0);

        let mut tags = Vec::new();
        tags.extend_from_slice(b"OpusTags");
        let vendor = b"speektome-client-meeting";
        tags.extend_from_slice(&(vendor.len() as u32).to_le_bytes());
        tags.extend_from_slice(vendor);
        tags.extend_from_slice(&0u32.to_le_bytes());

        writer_mixed.write_packet(head.clone().into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::EndPage, 0).map_err(|e|format!("Audio recording write failed: {}",e))?;
        writer_mixed.write_packet(tags.clone().into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::EndPage, 0).map_err(|e|format!("Audio recording write failed: {}",e))?;

        writer_mic.write_packet(head.clone().into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::EndPage, 0).map_err(|e|format!("Audio recording write failed: {}",e))?;
        writer_mic.write_packet(tags.clone().into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::EndPage, 0).map_err(|e|format!("Audio recording write failed: {}",e))?;

        writer_loopback.write_packet(head.clone().into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::EndPage, 0).map_err(|e|format!("Audio recording write failed: {}",e))?;
        writer_loopback.write_packet(tags.clone().into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::EndPage, 0).map_err(|e|format!("Audio recording write failed: {}",e))?;

        let _ = recording_ready_tx.send(());
        let mut mic_stream_buffer: Vec<i16> = Vec::new();
        let mut loopback_stream_buffer: Vec<i16> = Vec::new();

        let mut mic_carry_encode: Vec<i16> = Vec::new();
        let mut loopback_carry_encode: Vec<i16> = Vec::new();

        let mut granule_pos_mixed: u64 = 0;
        let mut granule_pos_mic: u64 = 0;
        let mut granule_pos_lb: u64 = 0;

        let mut encoded_buf_mixed = [0u8; 512];
        let mut encoded_buf_mic = [0u8; 512];
        let mut encoded_buf_lb = [0u8; 512];

        let mut total_mic_samples = 0;
        let mut echo_canceller = crate::meeting_echo::MeetingEchoCanceller::new();
        let mut energy_log = Vec::new();
        let mut loop_count: u64 = 0;
        const CHUNK_560MS: usize = 8960; // 560ms @ 16kHz mono
        const CHUNK_60MS: usize = 960;   // 60ms @ 16kHz mono (Opus standard frame)

        loop {
            if shutdown_rx.recv_timeout(std::time::Duration::from_millis(50)).is_ok() {
                break;
            }
            loop_count += 1;
            if loop_count % 20 == 0 {
             writer_mixed.inner().sync_data().map_err(|e|e.to_string())?;
             writer_mic.inner().sync_data().map_err(|e|e.to_string())?;
             writer_loopback.inner().sync_data().map_err(|e|e.to_string())?;
            }

            let mic_raw = {
                let mut lock = mic_buffer.lock().unwrap();
                std::mem::take(&mut *lock)
            };
            let loopback_raw = {
                let mut lock = loopback_buffer.lock().unwrap();
                std::mem::take(&mut *lock)
            };

            let mic_16k = resample_to_16k_mono(&mic_raw, mic_rate, mic_channels);
            let loopback_16k = resample_to_16k_mono(&loopback_raw, loopback_rate, loopback_channels);

            total_mic_samples += mic_16k.len();
            mic_carry_encode.extend_from_slice(&mic_16k);
            loopback_carry_encode.extend_from_slice(&loopback_16k);

            // Let independently scheduled capture callbacks catch up. Only pad
            // a channel after 200 ms without matching audio (e.g. suspended WASAPI).
            const MAX_CHANNEL_LAG: usize = 3200;
            if mic_carry_encode.len() > loopback_carry_encode.len() + MAX_CHANNEL_LAG {
                loopback_carry_encode.resize(mic_carry_encode.len() - MAX_CHANNEL_LAG, 0);
            } else if loopback_carry_encode.len() > mic_carry_encode.len() + MAX_CHANNEL_LAG {
                mic_carry_encode.resize(loopback_carry_encode.len() - MAX_CHANNEL_LAG, 0);
            }

            // Encode and save time-synchronized aligned audio chunks
            let available = mic_carry_encode.len().min(loopback_carry_encode.len());
            let num_chunks = available / CHUNK_60MS;
            let encode_len = num_chunks * CHUNK_60MS;

            if encode_len > 0 {
                let mut mic_block = mic_carry_encode[..encode_len].to_vec();
                let lb_block = &loopback_carry_encode[..encode_len];
                echo_canceller.process(&mut mic_block, lb_block);
                let rms = (mic_block.iter().map(|&s| (s as f64 / 32768.0).powi(2)).sum::<f64>() / mic_block.len() as f64).sqrt();
                MIC_VOLUME.store((rms * 2500.0).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
                if let Some(ref tx) = stream_tx {
                    mic_stream_buffer.extend_from_slice(&mic_block);
                    loopback_stream_buffer.extend_from_slice(lb_block);
                    while mic_stream_buffer.len() >= CHUNK_560MS {
                        let chunk: Vec<i16> = mic_stream_buffer.drain(..CHUNK_560MS).collect();
                        if let Ok(bytes) = encode_opus_ogg(&chunk, 16000, 1) {
                            let _ = tx.send(crate::meeting_stream::MeetingStreamAudioMessage::ChannelChunk(0x01, bytes));
                        }
                    }
                    while loopback_stream_buffer.len() >= CHUNK_560MS {
                        let chunk: Vec<i16> = loopback_stream_buffer.drain(..CHUNK_560MS).collect();
                        if let Ok(bytes) = encode_opus_ogg(&chunk, 16000, 1) {
                            let _ = tx.send(crate::meeting_stream::MeetingStreamAudioMessage::ChannelChunk(0x02, bytes));
                        }
                    }
                }
                if loop_count % 5 == 0 {
                    energy_log.push(EnergySample { time: total_mic_samples as f32 / 16000.0,
                        mic: MIC_VOLUME.load(Ordering::Relaxed), lb: LOOPBACK_VOLUME.load(Ordering::Relaxed) });
                }

                // Write aligned samples directly to WAV file
                if let Some(ref mut w) = wav_writer {
                    for i in 0..encode_len {
                        let s = (mic_block[i] as i32 + lb_block[i] as i32).clamp(i16::MIN as i32, i16::MAX as i32) as i16;
                        let _ = w.write_sample(s);
                    }
                }

                // Encode Mic Opus packets
                for c_idx in 0..num_chunks {
                    let start = c_idx * CHUNK_60MS;
                    let end = start + CHUNK_60MS;
                    granule_pos_mic += 2880;
                    if let Ok(len) = encoder_mic.encode(&mic_block[start..end], &mut encoded_buf_mic) {
                        let packet = encoded_buf_mic[..len].to_vec();
                        writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, if granule_pos_mic % 14400 == 0 {ogg::writing::PacketWriteEndInfo::EndPage}else{ogg::writing::PacketWriteEndInfo::NormalPacket}, granule_pos_mic).map_err(|e|format!("Audio recording write failed: {}",e))?;
                    }
                }

                // Encode Loopback Opus packets
                for c_idx in 0..num_chunks {
                    let start = c_idx * CHUNK_60MS;
                    let end = start + CHUNK_60MS;
                    granule_pos_lb += 2880;
                    if let Ok(len) = encoder_loopback.encode(&lb_block[start..end], &mut encoded_buf_lb) {
                        let packet = encoded_buf_lb[..len].to_vec();
                        writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, if granule_pos_lb % 14400 == 0 {ogg::writing::PacketWriteEndInfo::EndPage}else{ogg::writing::PacketWriteEndInfo::NormalPacket}, granule_pos_lb).map_err(|e|format!("Audio recording write failed: {}",e))?;
                    }
                }

                // Encode Mixed Opus packets
                for c_idx in 0..num_chunks {
                    let start = c_idx * CHUNK_60MS;
                    let mut mixed_frame = [0i16; CHUNK_60MS];
                    for k in 0..CHUNK_60MS {
                        mixed_frame[k] = (mic_block[start + k] as i32 + lb_block[start + k] as i32).clamp(i16::MIN as i32, i16::MAX as i32) as i16;
                    }
                    granule_pos_mixed += 2880;
                    if let Ok(len) = encoder_mixed.encode(&mixed_frame, &mut encoded_buf_mixed) {
                        let packet = encoded_buf_mixed[..len].to_vec();
                        writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, if granule_pos_mixed % 14400 == 0 {ogg::writing::PacketWriteEndInfo::EndPage}else{ogg::writing::PacketWriteEndInfo::NormalPacket}, granule_pos_mixed).map_err(|e|format!("Audio recording write failed: {}",e))?;
                    }
                }

                mic_carry_encode = mic_carry_encode[encode_len..].to_vec();
                loopback_carry_encode = loopback_carry_encode[encode_len..].to_vec();
            }
        }

        // Final flush at end of recording
        let mic_raw = {
            let mut lock = mic_buffer.lock().unwrap();
            std::mem::take(&mut *lock)
        };
        let loopback_raw = {
            let mut lock = loopback_buffer.lock().unwrap();
            std::mem::take(&mut *lock)
        };
        let mic_16k = resample_to_16k_mono(&mic_raw, mic_rate, mic_channels);
        let loopback_16k = resample_to_16k_mono(&loopback_raw, loopback_rate, loopback_channels);

        mic_carry_encode.extend_from_slice(&mic_16k);
        loopback_carry_encode.extend_from_slice(&loopback_16k);

        // Align remaining lengths
        if loopback_carry_encode.len() < mic_carry_encode.len() {
            loopback_carry_encode.resize(mic_carry_encode.len(), 0);
        } else if mic_carry_encode.len() < loopback_carry_encode.len() {
            mic_carry_encode.resize(loopback_carry_encode.len(), 0);
        }

        echo_canceller.process(&mut mic_carry_encode, &loopback_carry_encode);
        if let Some(ref tx) = stream_tx {
            mic_stream_buffer.extend_from_slice(&mic_carry_encode);
            loopback_stream_buffer.extend_from_slice(&loopback_carry_encode);
            if !mic_stream_buffer.is_empty() {
                if let Ok(bytes) = encode_opus_ogg(&mic_stream_buffer, 16000, 1) {
                    let _ = tx.send(crate::meeting_stream::MeetingStreamAudioMessage::ChannelChunk(0x01, bytes));
                }
            }
            if !loopback_stream_buffer.is_empty() {
                if let Ok(bytes) = encode_opus_ogg(&loopback_stream_buffer, 16000, 1) {
                    let _ = tx.send(crate::meeting_stream::MeetingStreamAudioMessage::ChannelChunk(0x02, bytes));
                }
            }
            let _ = tx.send(crate::meeting_stream::MeetingStreamAudioMessage::EndOfMeeting);
        }

        let flush_len = mic_carry_encode.len();
        if flush_len > 0 {
            if let Some(ref mut w) = wav_writer {
                for i in 0..flush_len {
                    let s = (mic_carry_encode[i] as i32 + loopback_carry_encode[i] as i32).clamp(i16::MIN as i32, i16::MAX as i32) as i16;
                    let _ = w.write_sample(s);
                }
            }

            // Flush Opus chunks
            let num_flush_chunks = flush_len / CHUNK_60MS;
            for c_idx in 0..num_flush_chunks {
                let start = c_idx * CHUNK_60MS;
                let end = start + CHUNK_60MS;

                granule_pos_mic += 2880;
                if let Ok(len) = encoder_mic.encode(&mic_carry_encode[start..end], &mut encoded_buf_mic) {
                    let packet = encoded_buf_mic[..len].to_vec();
                    writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, if granule_pos_mic % 14400 == 0 {ogg::writing::PacketWriteEndInfo::EndPage}else{ogg::writing::PacketWriteEndInfo::NormalPacket}, granule_pos_mic).map_err(|e|format!("Audio recording write failed: {}",e))?;
                }

                granule_pos_lb += 2880;
                if let Ok(len) = encoder_loopback.encode(&loopback_carry_encode[start..end], &mut encoded_buf_lb) {
                    let packet = encoded_buf_lb[..len].to_vec();
                    writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, if granule_pos_lb % 14400 == 0 {ogg::writing::PacketWriteEndInfo::EndPage}else{ogg::writing::PacketWriteEndInfo::NormalPacket}, granule_pos_lb).map_err(|e|format!("Audio recording write failed: {}",e))?;
                }

                let mut mixed_frame = [0i16; CHUNK_60MS];
                for k in 0..CHUNK_60MS {
                    mixed_frame[k] = (mic_carry_encode[start + k] as i32 + loopback_carry_encode[start + k] as i32).clamp(i16::MIN as i32, i16::MAX as i32) as i16;
                }
                granule_pos_mixed += 2880;
                if let Ok(len) = encoder_mixed.encode(&mixed_frame, &mut encoded_buf_mixed) {
                    let packet = encoded_buf_mixed[..len].to_vec();
                    writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, if granule_pos_mixed % 14400 == 0 {ogg::writing::PacketWriteEndInfo::EndPage}else{ogg::writing::PacketWriteEndInfo::NormalPacket}, granule_pos_mixed).map_err(|e|format!("Audio recording write failed: {}",e))?;
                }
            }

            // Handle trailing partial frame if any
            let rem_start = num_flush_chunks * CHUNK_60MS;
            if rem_start < flush_len {
                let mut mic_final = mic_carry_encode[rem_start..].to_vec();
                let mut lb_final = loopback_carry_encode[rem_start..].to_vec();
                mic_final.resize(CHUNK_60MS, 0);
                lb_final.resize(CHUNK_60MS, 0);

                granule_pos_mic += 2880;
                if let Ok(len) = encoder_mic.encode(&mic_final, &mut encoded_buf_mic) {
                    let packet = encoded_buf_mic[..len].to_vec();
                    writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos_mic).map_err(|e|format!("Audio recording write failed: {}",e))?;
                }

                granule_pos_lb += 2880;
                if let Ok(len) = encoder_loopback.encode(&lb_final, &mut encoded_buf_lb) {
                    let packet = encoded_buf_lb[..len].to_vec();
                    writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos_lb).map_err(|e|format!("Audio recording write failed: {}",e))?;
                }

                let mut mixed_frame = [0i16; CHUNK_60MS];
                for k in 0..CHUNK_60MS {
                    mixed_frame[k] = (mic_final[k] as i32 + lb_final[k] as i32).clamp(i16::MIN as i32, i16::MAX as i32) as i16;
                }
                granule_pos_mixed += 2880;
                if let Ok(len) = encoder_mixed.encode(&mixed_frame, &mut encoded_buf_mixed) {
                    let packet = encoded_buf_mixed[..len].to_vec();
                    writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos_mixed).map_err(|e|format!("Audio recording write failed: {}",e))?;
                }
            }
        }

        if let Some(w) = wav_writer {
            let _ = w.finalize();
        }

        writer_mixed.inner().sync_all().map_err(|e|e.to_string())?;
        writer_mic.inner().sync_all().map_err(|e|e.to_string())?;
        writer_loopback.inner().sync_all().map_err(|e|e.to_string())?;
        let energy_path = output_ogg_path.with_extension("energy.json");
        if let Ok(json_bytes) = serde_json::to_vec(&energy_log) {
            let _ = std::fs::write(energy_path, json_bytes);
        }
        Ok(())
    });

    let mut state = get_meeting_state().lock().unwrap_or_else(|e| e.into_inner());
    state.mic_stream = Some(SendStream(mic_stream));
    #[cfg(windows)]
    { state.loopback_stream = loopback_stream_opt.map(SendStream); }
    #[cfg(not(windows))]
    { state.loopback_stream = None; }
    #[cfg(target_os = "linux")]
    {
        state.pulse_loopback_shutdown = pulse_loopback_shutdown;
        state.pulse_loopback_thread = pulse_loopback_thread;
    }
    state.shutdown_tx = Some(shutdown_tx);
    state.recording_thread = Some(recording_thread);
    drop(state);
    if recording_ready_rx.recv_timeout(std::time::Duration::from_secs(5)).is_err() {
        let error = stop_meeting_recording().err().unwrap_or_else(|| "Recording storage initialization timed out".to_string());
        return Err(error.into());
    }

    Ok(())
}

pub fn stop_meeting_recording() -> Result<(),String> {
    let mut state = get_meeting_state().lock().unwrap_or_else(|e| e.into_inner());

    if let Some(tx) = state.shutdown_tx.take() {
        let _ = tx.send(());
    }

    state.mic_stream = None;
    state.loopback_stream = None;
    #[cfg(target_os = "linux")]
    {
        if let Some(stop) = state.pulse_loopback_shutdown.take() {
            stop.store(true, Ordering::Relaxed);
        }
        if let Some(worker) = state.pulse_loopback_thread.take() {
            // pa_simple_read may block if the monitor is suspended. The stop
            // flag is checked after it wakes; do not freeze the UI on join.
            drop(worker);
        }
    }

    MIC_VOLUME.store(0, Ordering::Relaxed);
    LOOPBACK_VOLUME.store(0, Ordering::Relaxed);
 let writer=state.recording_thread.take();drop(state);
 if let Some(writer)=writer {writer.join().map_err(|_|"Meeting audio finalization failed".to_string())??;}
 Ok(())
}

pub fn decode_ogg_to_wav(ogg_path: &std::path::Path) -> Result<Vec<u8>, String> {
    let file = std::fs::File::open(ogg_path).map_err(|e| format!("Failed to open ogg file: {}", e))?;
    let mut reader = ogg::reading::PacketReader::new(file);
    let mut decoder = opus::Decoder::new(16000, opus::Channels::Mono).map_err(|e| format!("Failed to init opus decoder: {:?}", e))?;
    
    let mut pcm_samples: Vec<i16> = Vec::new();
    let mut out_buf = [0i16; 5760]; // 120ms max

    while let Ok(Some(packet)) = reader.read_packet() {
        if packet.data.starts_with(b"OpusHead") || packet.data.starts_with(b"OpusTags") {
            continue;
        }
        if let Ok(samples_decoded) = decoder.decode(&packet.data, &mut out_buf, false) {
            if samples_decoded > 0 {
                pcm_samples.extend_from_slice(&out_buf[..samples_decoded]);
            }
        }
    }

    if pcm_samples.is_empty() {
        return Err("No audio samples decoded from ogg file".to_string());
    }

    let mut wav_cursor = std::io::Cursor::new(Vec::new());
    let spec = hound::WavSpec {
        channels: 1,
        sample_rate: 16000,
        bits_per_sample: 16,
        sample_format: hound::SampleFormat::Int,
    };
    let mut writer = hound::WavWriter::new(&mut wav_cursor, spec).map_err(|e| format!("WavWriter error: {}", e))?;
    for sample in pcm_samples {
        writer.write_sample(sample).map_err(|e| format!("Wav write sample error: {}", e))?;
    }
    writer.finalize().map_err(|e| format!("Wav finalize error: {}", e))?;

    Ok(wav_cursor.into_inner())
}
