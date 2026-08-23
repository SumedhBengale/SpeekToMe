use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicU32, Ordering};
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use serde::{Serialize, Deserialize};

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
pub fn recording_thread(rx: std::sync::mpsc::Receiver<bool>) {
    // Introduce a short delay so the start chime has time to play before CPAL starts input recording.
    // This prevents audio driver profile/channel switching conflicts.
    std::thread::sleep(std::time::Duration::from_millis(150));

    let host = cpal::default_host();
    let device = match host.default_input_device() {
        Some(d) => d,
        None => {
            eprintln!("No input device found!");
            return;
        }
    };
    
    let config = match device.default_input_config() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("Failed to get default input config: {:?}", e);
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
                return;
            }
            println!("[AUDIO] Recording started...");
            
            let (is_stream_mode, api_url, token, enhance, custom_prompt) = {
                let cfg = crate::get_active_config().lock().unwrap_or_else(|e| e.into_inner());
                (
                    cfg.transcription_mode == "stream" && cfg.session_token.is_some(),
                    cfg.api_url.clone().unwrap_or_else(|| "https://api.speekto.me".to_string()),
                    cfg.session_token.clone().unwrap_or_default(),
                    cfg.enhance,
                    cfg.custom_prompt.clone(),
                )
            };
            println!("[AUDIO] Active transcription mode: {}", if is_stream_mode { "STREAM" } else { "BATCH" });

            if is_stream_mode {
                let stream_handle = match crate::stream::start_streaming_session(
                    api_url.clone(),
                    token.clone(),
                    enhance,
                    custom_prompt.clone(),
                ) {
                    Ok(handle) => Some(handle),
                    Err(e) => {
                        eprintln!("[STREAM INIT ERROR] Failed to start live stream session: {}. Falling back to batch mode.", e);
                        None
                    }
                };

                if let Some(handle) = stream_handle {
                    let mut last_sent_16k_len = 0;
                    let mut is_aborted = false;

                    loop {
                        match rx.recv_timeout(std::time::Duration::from_millis(150)) {
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
                                // Periodic tick: extract new samples from contiguous resampled audio
                                let current_samples = samples_buf.lock().unwrap_or_else(|e| e.into_inner()).clone();
                                let all_16k = resample_to_16k_mono(&current_samples, sample_rate, channels);
                                if all_16k.len() > last_sent_16k_len + 1600 { // at least 100ms of new 16k audio
                                    let new_16k_slice = &all_16k[last_sent_16k_len..];
                                    let mut pcm_bytes = Vec::with_capacity(new_16k_slice.len() * 2);
                                    for sample in new_16k_slice {
                                        pcm_bytes.extend_from_slice(&sample.to_le_bytes());
                                    }
                                    let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(pcm_bytes));
                                    last_sent_16k_len = all_16k.len();
                                }
                            }
                            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                                is_aborted = true;
                                break;
                            }
                        }
                    }

                    drop(s);
                    CURRENT_VOLUME.store(0, Ordering::Relaxed);
                    println!("Recording stopped.");

                    if !is_aborted {
                        let final_samples = samples_buf.lock().unwrap_or_else(|e| e.into_inner()).clone();
                        let all_16k = resample_to_16k_mono(&final_samples, sample_rate, channels);
                        if all_16k.len() > last_sent_16k_len {
                            let new_16k_slice = &all_16k[last_sent_16k_len..];
                            let mut pcm_bytes = Vec::with_capacity(new_16k_slice.len() * 2);
                            for sample in new_16k_slice {
                                pcm_bytes.extend_from_slice(&sample.to_le_bytes());
                            }
                            let _ = handle.tx.send(crate::stream::StreamAudioMessage::AudioChunk(pcm_bytes));
                        }
                        let _ = handle.tx.send(crate::stream::StreamAudioMessage::EndOfStream);

                        if let Ok(Ok(final_result)) = handle.rx_final.recv_timeout(std::time::Duration::from_secs(5)) {
                            println!("[STREAM SUCCESS] Live streaming completed. Final text: {}", final_result.text);
                            if let (Some(log_id), Some(ack_token)) = (final_result.log_id, final_result.ack_token) {
                                let ack_api = api_url.clone();
                                let ack_auth = token.clone();
                                let ack_txt = final_result.text.clone();
                                tauri::async_runtime::spawn(async move {
                                    let _ = crate::api::send_transcribe_ack(&ack_api, &ack_auth, log_id, &ack_token, &ack_txt).await;
                                });
                            }
                        }
                    } else {
                        println!("Recording aborted and discarded.");
                    }
                    return;
                }
            }

            // Fallback / standard batch mode: Wait for key release/abort signal
            if let Ok(true) = rx.recv() {
                // Drop stream to stop recording
                drop(s);
                CURRENT_VOLUME.store(0, Ordering::Relaxed);
                println!("Recording stopped.");
                
                let samples = samples_buf.lock().unwrap_or_else(|e| e.into_inner()).clone();
                if !samples.is_empty() {
                    // Calculate if there is active speech (VAD Filter)
                    let has_speech = is_speech(&samples, sample_rate, channels);
                    println!("Recording finished. Samples: {}, Speech detected: {}", samples.len(), has_speech);
                    
                    if has_speech {
                        crate::api::process_and_paste(samples, sample_rate, channels);
                    } else {
                        println!("Silence or keyboard click detected. Transcription request skipped.");
                    }
                }
            } else {
                drop(s);
                CURRENT_VOLUME.store(0, Ordering::Relaxed);
                println!("Recording aborted and discarded.");
            }
        }
        Err(e) => {
            eprintln!("Failed to build input stream: {:?}", e);
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
}

pub static MEETING_STATE: std::sync::OnceLock<Mutex<MeetingRecordingState>> = std::sync::OnceLock::new();

pub fn get_meeting_state() -> &'static Mutex<MeetingRecordingState> {
    MEETING_STATE.get_or_init(|| {
        Mutex::new(MeetingRecordingState {
            mic_stream: None,
            loopback_stream: None,
            shutdown_tx: None,
        })
    })
}

pub fn start_meeting_recording(output_ogg_path: std::path::PathBuf) -> Result<(), Box<dyn std::error::Error>> {
    let host = cpal::default_host();
    let mic_device = host.default_input_device().ok_or("No input device found")?;
    let mic_config = mic_device.default_input_config()?;
    let mic_sample_format = mic_config.sample_format();
    let mic_rate = mic_config.sample_rate().0;
    let mic_channels = mic_config.channels();

    let loopback_device = host.default_output_device().ok_or("No output device found")?;
    let loopback_config = match loopback_device.default_input_config() {
        Ok(cfg) => cfg,
        Err(_) => loopback_device.default_output_config()?,
    };
    let loopback_sample_format = loopback_config.sample_format();
    let loopback_rate = loopback_config.sample_rate().0;
    let loopback_channels = loopback_config.channels();

    let mic_buffer = Arc::new(Mutex::new(Vec::new()));
    let mic_buffer_clone = mic_buffer.clone();

    let loopback_buffer = Arc::new(Mutex::new(Vec::new()));
    let loopback_buffer_clone = loopback_buffer.clone();

    let mic_err_fn = |err| eprintln!("Microphone stream error: {}", err);
    let mic_stream = match mic_sample_format {
        cpal::SampleFormat::F32 => {
            mic_device.build_input_stream(
                &mic_config.clone().into(),
                move |data: &[f32], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            sum_squares += sample * sample;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        MIC_VOLUME.store((rms * 1000.0) as u32, Ordering::Relaxed);
                    }
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
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            let val = sample as f32 / i16::MAX as f32;
                            sum_squares += val * val;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        MIC_VOLUME.store((rms * 1000.0) as u32, Ordering::Relaxed);
                    }
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
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            let val = (sample as f32 - 32768.0) / 32768.0;
                            sum_squares += val * val;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        MIC_VOLUME.store((rms * 1000.0) as u32, Ordering::Relaxed);
                    }
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
        _ => return Err("Unsupported mic sample format".into()),
    };

    let loopback_err_fn = |err| eprintln!("Loopback stream error: {}", err);
    let loopback_stream = match loopback_sample_format {
        cpal::SampleFormat::F32 => {
            loopback_device.build_input_stream(
                &loopback_config.clone().into(),
                move |data: &[f32], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            sum_squares += sample * sample;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        LOOPBACK_VOLUME.store((rms * 1000.0) as u32, Ordering::Relaxed);
                    }
                    if let Ok(mut buf) = loopback_buffer_clone.lock() {
                        for &sample in data {
                            let i16_val = (sample * i16::MAX as f32) as i16;
                            buf.push(i16_val);
                        }
                    }
                },
                loopback_err_fn,
                None
            )?
        }
        cpal::SampleFormat::I16 => {
            loopback_device.build_input_stream(
                &loopback_config.clone().into(),
                move |data: &[i16], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            let val = sample as f32 / i16::MAX as f32;
                            sum_squares += val * val;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        LOOPBACK_VOLUME.store((rms * 1000.0) as u32, Ordering::Relaxed);
                    }
                    if let Ok(mut buf) = loopback_buffer_clone.lock() {
                        buf.extend_from_slice(data);
                    }
                },
                loopback_err_fn,
                None
            )?
        }
        cpal::SampleFormat::U16 => {
            loopback_device.build_input_stream(
                &loopback_config.clone().into(),
                move |data: &[u16], _| {
                    if !data.is_empty() {
                        let mut sum_squares = 0.0;
                        for &sample in data {
                            let val = (sample as f32 - 32768.0) / 32768.0;
                            sum_squares += val * val;
                        }
                        let rms = (sum_squares / data.len() as f32).sqrt();
                        LOOPBACK_VOLUME.store((rms * 1000.0) as u32, Ordering::Relaxed);
                    }
                    if let Ok(mut buf) = loopback_buffer_clone.lock() {
                        for &sample in data {
                            let i16_val = (sample as i32 - 32768) as i16;
                            buf.push(i16_val);
                        }
                    }
                },
                loopback_err_fn,
                None
            )?
        }
        _ => return Err("Unsupported loopback sample format".into()),
    };

    let (shutdown_tx, shutdown_rx) = std::sync::mpsc::channel::<()>();

    mic_stream.play()?;
    loopback_stream.play()?;

    std::thread::spawn(move || {
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
                return;
            }
        };
        let mut encoder_mic = match opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip) {
            Ok(enc) => enc,
            Err(e) => {
                eprintln!("Failed to create Opus mic encoder: {:?}", e);
                return;
            }
        };
        let mut encoder_loopback = match opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip) {
            Ok(enc) => enc,
            Err(e) => {
                eprintln!("Failed to create Opus loopback encoder: {:?}", e);
                return;
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
                return;
            }
        };
        let file_mic = match std::fs::File::create(&mic_path) {
            Ok(f) => f,
            Err(e) => {
                eprintln!("Failed to create mic Ogg output file: {:?}", e);
                return;
            }
        };
        let file_loopback = match std::fs::File::create(&loopback_path) {
            Ok(f) => f,
            Err(e) => {
                eprintln!("Failed to create loopback Ogg output file: {:?}", e);
                return;
            }
        };

        let mut writer_mixed = ogg::writing::PacketWriter::new(file_mixed);
        let mut writer_mic = ogg::writing::PacketWriter::new(file_mic);
        let mut writer_loopback = ogg::writing::PacketWriter::new(file_loopback);

        let serial_mixed = 987654321;
        let serial_mic = 987654322;
        let serial_lb = 987654323;

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

        // Write headers and tags to all three writers
        for (w, serial) in [
            (&mut writer_mixed, serial_mixed),
            (&mut writer_mic, serial_mic),
            (&mut writer_loopback, serial_lb)
        ] {
            if let Err(e) = w.write_packet(head.clone().into_boxed_slice(), serial, ogg::writing::PacketWriteEndInfo::EndPage, 0) {
                eprintln!("Failed to write Ogg header for serial {}: {:?}", serial, e);
                return;
            }
            if let Err(e) = w.write_packet(tags.clone().into_boxed_slice(), serial, ogg::writing::PacketWriteEndInfo::EndPage, 0) {
                eprintln!("Failed to write Ogg tags for serial {}: {:?}", serial, e);
                return;
            }
        }

        let mut mic_carry_encode: Vec<i16> = Vec::new();
        let mut loopback_carry_encode: Vec<i16> = Vec::new();
        let mut mixed_carry: Vec<i16> = Vec::new();
        
        let mut granule_pos_mic: u64 = 0;
        let mut granule_pos_lb: u64 = 0;
        let mut granule_pos_mixed: u64 = 0;

        let mut total_mic_samples: usize = 0;
        let mut total_lb_samples: usize = 0;

        let mut encoded_buf_mixed = [0u8; 512];
        let mut encoded_buf_mic = [0u8; 512];
        let mut encoded_buf_lb = [0u8; 512];

        let mut energy_log: Vec<EnergySample> = Vec::new();
        let mut loop_count: usize = 0;

        loop {
            if shutdown_rx.recv_timeout(std::time::Duration::from_millis(100)).is_ok() {
                break;
            }

            loop_count += 1;
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

            if loop_count % 5 == 0 {
                let time_sec = total_mic_samples as f32 / 16000.0;
                let mic_vol = MIC_VOLUME.load(Ordering::Relaxed);
                let lb_vol = LOOPBACK_VOLUME.load(Ordering::Relaxed);
                energy_log.push(EnergySample {
                    time: time_sec,
                    mic: mic_vol,
                    lb: lb_vol,
                });
            }

            if !loopback_16k.is_empty() {
                let target_lb_pos = total_mic_samples.saturating_sub(loopback_16k.len());
                if total_lb_samples < target_lb_pos {
                    let missing = target_lb_pos - total_lb_samples;
                    loopback_carry_encode.resize(loopback_carry_encode.len() + missing, 0);
                    total_lb_samples = target_lb_pos;
                }
                loopback_carry_encode.extend_from_slice(&loopback_16k);
                total_lb_samples += loopback_16k.len();
            }

            // Mix audio for current 100ms frame
            let mut mixed = Vec::with_capacity(mic_16k.len());
            for i in 0..mic_16k.len() {
                let m_val = mic_16k[i] as i32;
                let lb_val = if i < loopback_16k.len() {
                    loopback_16k[i] as i32
                } else {
                    0
                };
                mixed.push((m_val + lb_val).clamp(i16::MIN as i32, i16::MAX as i32) as i16);
            }
            mixed_carry.extend_from_slice(&mixed);

            let chunk_size = 960;
            
            // Encode available mic chunks
            let mic_chunks_count = mic_carry_encode.len() / chunk_size;
            for c_idx in 0..mic_chunks_count {
                let start = c_idx * chunk_size;
                let end = start + chunk_size;
                granule_pos_mic += 2880;
                if let Ok(len) = encoder_mic.encode(&mic_carry_encode[start..end], &mut encoded_buf_mic) {
                    let packet = encoded_buf_mic[..len].to_vec();
                    let _ = writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos_mic);
                }
            }
            mic_carry_encode = mic_carry_encode[mic_chunks_count * chunk_size..].to_vec();

            // Encode available loopback chunks
            let lb_chunks_count = loopback_carry_encode.len() / chunk_size;
            for c_idx in 0..lb_chunks_count {
                let start = c_idx * chunk_size;
                let end = start + chunk_size;
                granule_pos_lb += 2880;
                if let Ok(len) = encoder_loopback.encode(&loopback_carry_encode[start..end], &mut encoded_buf_lb) {
                    let packet = encoded_buf_lb[..len].to_vec();
                    let _ = writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos_lb);
                }
            }
            loopback_carry_encode = loopback_carry_encode[lb_chunks_count * chunk_size..].to_vec();

            // Encode available mixed chunks
            let mixed_chunks_count = mixed_carry.len() / chunk_size;
            for c_idx in 0..mixed_chunks_count {
                let start = c_idx * chunk_size;
                let end = start + chunk_size;
                granule_pos_mixed += 2880;
                if let Ok(len) = encoder_mixed.encode(&mixed_carry[start..end], &mut encoded_buf_mixed) {
                    let packet = encoded_buf_mixed[..len].to_vec();
                    let _ = writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos_mixed);
                }
            }
            mixed_carry = mixed_carry[mixed_chunks_count * chunk_size..].to_vec();
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

        total_mic_samples += mic_16k.len();
        mic_carry_encode.extend_from_slice(&mic_16k);

        if !loopback_16k.is_empty() {
            let target_lb_pos = total_mic_samples.saturating_sub(loopback_16k.len());
            if total_lb_samples < target_lb_pos {
                let missing = target_lb_pos - total_lb_samples;
                loopback_carry_encode.resize(loopback_carry_encode.len() + missing, 0);
            }
            loopback_carry_encode.extend_from_slice(&loopback_16k);
        }

        // Align loopback length to match mic total length at shutdown
        if loopback_carry_encode.len() < mic_carry_encode.len() {
            loopback_carry_encode.resize(mic_carry_encode.len(), 0);
        }

        let mut mixed = Vec::with_capacity(mic_16k.len());
        for i in 0..mic_16k.len() {
            let m_val = mic_16k[i] as i32;
            let lb_val = if i < loopback_16k.len() {
                loopback_16k[i] as i32
            } else {
                0
            };
            mixed.push((m_val + lb_val).clamp(i16::MIN as i32, i16::MAX as i32) as i16);
        }
        mixed_carry.extend_from_slice(&mixed);

        let chunk_size = 960;

        // Flush mic
        let mic_chunks_count = mic_carry_encode.len() / chunk_size;
        for c_idx in 0..mic_chunks_count {
            let start = c_idx * chunk_size;
            let end = start + chunk_size;
            granule_pos_mic += 2880;
            if let Ok(len) = encoder_mic.encode(&mic_carry_encode[start..end], &mut encoded_buf_mic) {
                let packet = encoded_buf_mic[..len].to_vec();
                let _ = writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos_mic);
            }
        }
        let mic_rem = &mic_carry_encode[mic_chunks_count * chunk_size..];
        if !mic_rem.is_empty() {
            let mut final_chunk = mic_rem.to_vec();
            final_chunk.resize(chunk_size, 0);
            granule_pos_mic += 2880;
            if let Ok(len) = encoder_mic.encode(&final_chunk, &mut encoded_buf_mic) {
                let packet = encoded_buf_mic[..len].to_vec();
                let _ = writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos_mic);
            }
        }

        // Flush loopback
        let lb_chunks_count = loopback_carry_encode.len() / chunk_size;
        for c_idx in 0..lb_chunks_count {
            let start = c_idx * chunk_size;
            let end = start + chunk_size;
            granule_pos_lb += 2880;
            if let Ok(len) = encoder_loopback.encode(&loopback_carry_encode[start..end], &mut encoded_buf_lb) {
                let packet = encoded_buf_lb[..len].to_vec();
                let _ = writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos_lb);
            }
        }
        let lb_rem = &loopback_carry_encode[lb_chunks_count * chunk_size..];
        if !lb_rem.is_empty() {
            let mut final_chunk = lb_rem.to_vec();
            final_chunk.resize(chunk_size, 0);
            granule_pos_lb += 2880;
            if let Ok(len) = encoder_loopback.encode(&final_chunk, &mut encoded_buf_lb) {
                let packet = encoded_buf_lb[..len].to_vec();
                let _ = writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos_lb);
            }
        }

        // Flush mixed
        let mixed_chunks_count = mixed_carry.len() / chunk_size;
        for c_idx in 0..mixed_chunks_count {
            let start = c_idx * chunk_size;
            let end = start + chunk_size;
            granule_pos_mixed += 2880;
            if let Ok(len) = encoder_mixed.encode(&mixed_carry[start..end], &mut encoded_buf_mixed) {
                let packet = encoded_buf_mixed[..len].to_vec();
                let _ = writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos_mixed);
            }
        }
        let mixed_rem = &mixed_carry[mixed_chunks_count * chunk_size..];
        if !mixed_rem.is_empty() {
            let mut final_chunk = mixed_rem.to_vec();
            final_chunk.resize(chunk_size, 0);
            granule_pos_mixed += 2880;
            if let Ok(len) = encoder_mixed.encode(&final_chunk, &mut encoded_buf_mixed) {
                let packet = encoded_buf_mixed[..len].to_vec();
                let _ = writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos_mixed);
            }
        }

        let energy_path = output_ogg_path.with_extension("energy.json");
        if let Ok(json_bytes) = serde_json::to_vec(&energy_log) {
            let _ = std::fs::write(energy_path, json_bytes);
        }
    });

    let mut state = get_meeting_state().lock().unwrap_or_else(|e| e.into_inner());
    state.mic_stream = Some(SendStream(mic_stream));
    state.loopback_stream = Some(SendStream(loopback_stream));
    state.shutdown_tx = Some(shutdown_tx);

    Ok(())
}

pub fn stop_meeting_recording() {
    let mut state = get_meeting_state().lock().unwrap_or_else(|e| e.into_inner());

    if let Some(tx) = state.shutdown_tx.take() {
        let _ = tx.send(());
    }

    state.mic_stream = None;
    state.loopback_stream = None;

    MIC_VOLUME.store(0, Ordering::Relaxed);
    LOOPBACK_VOLUME.store(0, Ordering::Relaxed);
}

