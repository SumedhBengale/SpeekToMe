use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicU32, Ordering};
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};

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

// Frame-based Voice Activity Detection (VAD)
// Checks if the recorded audio contains actual speech (including quiet whispers)
// while filtering out quick mechanical keyboard clicks and short noise spikes.
pub fn is_speech(samples: &[i16], sample_rate: u32, channels: u16) -> bool {
    if samples.is_empty() {
        return false;
    }
    
    // 1. Minimum duration: require at least 300ms of recording to consider it speech
    let total_duration_ms = (samples.len() as f32 / (sample_rate as f32 * channels as f32)) * 1000.0;
    if total_duration_ms < 300.0 {
        return false;
    }
    
    // Remove DC offset (mean) to make the signal immune to microphone static level
    let sum_samples: f64 = samples.iter().map(|&s| s as f64).sum();
    let mean = sum_samples / samples.len() as f64;
    
    // 2. Define frame size of 50ms
    let frame_size = (sample_rate as f32 * channels as f32 * 0.05) as usize;
    if frame_size == 0 {
        return false;
    }
    
    let mut speech_frames = 0;
    let mut i = 0;
    
    while i < samples.len() {
        let end = std::cmp::min(i + frame_size, samples.len());
        let frame = &samples[i..end];
        
        // Calculate RMS of this frame (subtracting mean)
        let mut sum_squares = 0.0;
        for &sample in frame {
            let val = (sample as f64 - mean) / i16::MAX as f64;
            sum_squares += val * val;
        }
        let rms = (sum_squares / frame.len() as f64).sqrt() as f32;
        
        // 0.005 is very sensitive (captures whispers), yet immune to silence noise floor
        if rms >= 0.005 {
            speech_frames += 1;
        }
        
        i += frame_size;
    }
    
    // 3. We require at least 3 active frames (150ms of active sound) to qualify as speech.
    // A keyboard mechanical click usually only spans 1 frame (50ms).
    speech_frames >= 3
}

// Background recording thread routine (manages non-Send WASAPI stream)
pub fn recording_thread(rx: std::sync::mpsc::Receiver<()>) {
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
                eprintln!("Failed to play stream: {:?}", e);
                return;
            }
            println!("Recording started...");
            
            // Wait for key release signal
            let _ = rx.recv();
            
            // Drop stream to stop recording
            drop(s);
            CURRENT_VOLUME.store(0, Ordering::Relaxed);
            println!("Recording stopped.");
            
            let samples = samples_buf.lock().unwrap().clone();
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
        }
        Err(e) => {
            eprintln!("Failed to build input stream: {:?}", e);
        }
    }
}

// Resample raw PCM audio to 16,000 Hz Mono using linear interpolation (with fast integer ratio path)
pub fn resample_to_16k_mono(samples: &[i16], source_rate: u32, source_channels: u16) -> Vec<i16> {
    // Fast path: if the source sample rate is an integer multiple of 16,000 Hz (e.g. 48kHz, 32kHz, 16kHz)
    if source_rate % 16000 == 0 && source_rate >= 16000 {
        let step = (source_rate / 16000) as usize;
        let samples_per_frame = step * source_channels as usize;
        let mut resampled = Vec::with_capacity(samples.len() / samples_per_frame);
        
        for frame in samples.chunks_exact(samples_per_frame) {
            let mut sum: i32 = 0;
            for &s in frame {
                sum += s as i32;
            }
            resampled.push((sum / frame.len() as i32) as i16);
        }
        return resampled;
    }

    // Fallback: standard linear interpolation
    let mono_samples: Vec<i16> = if source_channels == 2 {
        samples.chunks_exact(2)
            .map(|chunk| ((chunk[0] as i32 + chunk[1] as i32) / 2) as i16)
            .collect()
    } else if source_channels > 2 {
        samples.chunks_exact(source_channels as usize)
            .map(|chunk| chunk[0])
            .collect()
    } else {
        samples.to_vec()
    };

    if source_rate == 16000 {
        return mono_samples;
    }

    let target_rate = 16000.0;
    let ratio = source_rate as f64 / target_rate;
    let target_length = (mono_samples.len() as f64 / ratio) as usize;
    let mut resampled = Vec::with_capacity(target_length);

    for i in 0..target_length {
        let pos = i as f64 * ratio;
        let index = pos as usize;
        let frac = pos - index as f64;

        if index + 1 < mono_samples.len() {
            let s0 = mono_samples[index] as f64;
            let s1 = mono_samples[index + 1] as f64;
            let interpolated = s0 + frac * (s1 - s0);
            resampled.push(interpolated as i16);
        } else if index < mono_samples.len() {
            resampled.push(mono_samples[index]);
        }
    }

    resampled
}

// Encode PCM samples to Opus packets containerized in an Ogg stream
pub fn encode_opus_ogg(samples: &[i16], source_rate: u32, source_channels: u16) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let resample_start = std::time::Instant::now();
    let resampled = resample_to_16k_mono(samples, source_rate, source_channels);
    let resample_dur = resample_start.elapsed();
    
    let init_start = std::time::Instant::now();
    let mut encoder = opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip)?;
    encoder.set_bitrate(opus::Bitrate::Bits(24000))?;
    encoder.set_complexity(1)?; // Set complexity to 1 (fastest for speech transcription)
    let init_dur = init_start.elapsed();

    let mut buffer = Vec::new();
    let mut writer = ogg::writing::PacketWriter::new(&mut buffer);
    let serial = 123456789;

    let mut head = Vec::new();
    head.extend_from_slice(b"OpusHead");
    head.push(1);
    head.push(1);
    head.extend_from_slice(&312u16.to_le_bytes());
    head.extend_from_slice(&16000u32.to_le_bytes());
    head.extend_from_slice(&0i16.to_le_bytes());
    head.push(0);
    
    writer.write_packet(head.into_boxed_slice(), serial, ogg::writing::PacketWriteEndInfo::EndPage, 0)?;

    let mut tags = Vec::new();
    tags.extend_from_slice(b"OpusTags");
    let vendor = b"speektome-client";
    tags.extend_from_slice(&(vendor.len() as u32).to_le_bytes());
    tags.extend_from_slice(vendor);
    tags.extend_from_slice(&0u32.to_le_bytes());
    
    writer.write_packet(tags.into_boxed_slice(), serial, ogg::writing::PacketWriteEndInfo::EndPage, 0)?;

    let chunk_size = 960; // 60ms frame size (decreases packet overhead and encoding calls by 3x)
    let chunks = resampled.chunks(chunk_size);
    let chunks_len = chunks.len();
    let mut granule_pos: u64 = 0;

    // Use a reusable stack-allocated buffer to prevent hot-loop allocations
    let mut encoded_buf = [0u8; 512];

    let mut encode_dur = std::time::Duration::ZERO;
    let mut write_dur = std::time::Duration::ZERO;

    for (idx, chunk) in chunks.enumerate() {
        // Zero-pad the final frame if it's smaller than the target chunk size
        let frame = if chunk.len() < chunk_size {
            let mut padded = chunk.to_vec();
            padded.resize(chunk_size, 0);
            padded
        } else {
            chunk.to_vec()
        };

        let t0 = std::time::Instant::now();
        let len = encoder.encode(&frame, &mut encoded_buf)?;
        encode_dur += t0.elapsed();

        let t1 = std::time::Instant::now();
        let packet = encoded_buf[..len].to_vec();
        granule_pos += 2880; // 960 samples * 3 = 2880 at 48kHz reference rate

        // Let the ogg library auto-segment page flushes, only marking EndStream on final chunk
        let end_info = if idx == chunks_len - 1 {
            ogg::writing::PacketWriteEndInfo::EndStream
        } else {
            ogg::writing::PacketWriteEndInfo::NormalPacket
        };

        writer.write_packet(packet.into_boxed_slice(), serial, end_info, granule_pos)?;
        write_dur += t1.elapsed();
    }

    println!("\n=== ENCODING PROFILE ===");
    println!("Resampling:      {:?}", resample_dur);
    println!("Init Encoder:    {:?}", init_dur);
    println!("Opus Encode:     {:?}", encode_dur);
    println!("Ogg Mux/Write:   {:?}", write_dur);
    println!("========================\n");

    Ok(buffer)
}

pub static MIC_VOLUME: AtomicU32 = AtomicU32::new(0);
pub static LOOPBACK_VOLUME: AtomicU32 = AtomicU32::new(0);

pub struct SendSyncWrapper<T>(pub T);
unsafe impl<T> Send for SendSyncWrapper<T> {}
unsafe impl<T> Sync for SendSyncWrapper<T> {}

pub struct MeetingRecordingState {
    pub mic_stream: Option<SendSyncWrapper<cpal::Stream>>,
    pub loopback_stream: Option<SendSyncWrapper<cpal::Stream>>,
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

        let mut loopback_carry: Vec<i16> = Vec::new();
        let mut mic_carry_encode: Vec<i16> = Vec::new();
        let mut loopback_carry_encode: Vec<i16> = Vec::new();
        let mut mixed_carry: Vec<i16> = Vec::new();
        let mut granule_pos: u64 = 0;

        let mut encoded_buf_mixed = [0u8; 512];
        let mut encoded_buf_mic = [0u8; 512];
        let mut encoded_buf_lb = [0u8; 512];

        loop {
            if shutdown_rx.recv_timeout(std::time::Duration::from_millis(100)).is_ok() {
                break;
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

            let mut loopback_all = std::mem::take(&mut loopback_carry);
            loopback_all.extend(loopback_16k);

            let mut loopback_aligned = Vec::new();
            if loopback_all.len() < mic_16k.len() {
                loopback_aligned.extend(&loopback_all);
                loopback_aligned.resize(mic_16k.len(), 0);
            } else {
                loopback_aligned.extend(loopback_all.iter().take(mic_16k.len()));
                loopback_carry = loopback_all[mic_16k.len()..].to_vec();
            }

            let mut mixed = Vec::with_capacity(mic_16k.len());
            for i in 0..mic_16k.len() {
                let sum = mic_16k[i] as i32 + loopback_aligned[i] as i32;
                let clamped = sum.clamp(i16::MIN as i32, i16::MAX as i32) as i16;
                mixed.push(clamped);
            }

            let mut mic_all = std::mem::take(&mut mic_carry_encode);
            mic_all.extend(mic_16k);

            let mut lb_all = std::mem::take(&mut loopback_carry_encode);
            lb_all.extend(loopback_aligned);

            let mut mixed_all = std::mem::take(&mut mixed_carry);
            mixed_all.extend(mixed);

            let chunk_size = 960;
            let chunks_count = mixed_all.len() / chunk_size;

            for c_idx in 0..chunks_count {
                let start = c_idx * chunk_size;
                let end = start + chunk_size;

                let mic_chunk = &mic_all[start..end];
                let lb_chunk = &lb_all[start..end];
                let mixed_chunk = &mixed_all[start..end];

                granule_pos += 2880;

                if let Ok(len) = encoder_mic.encode(mic_chunk, &mut encoded_buf_mic) {
                    let packet = encoded_buf_mic[..len].to_vec();
                    let _ = writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos);
                }

                if let Ok(len) = encoder_loopback.encode(lb_chunk, &mut encoded_buf_lb) {
                    let packet = encoded_buf_lb[..len].to_vec();
                    let _ = writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos);
                }

                if let Ok(len) = encoder_mixed.encode(mixed_chunk, &mut encoded_buf_mixed) {
                    let packet = encoded_buf_mixed[..len].to_vec();
                    let _ = writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos);
                }
            }

            mic_carry_encode = mic_all[chunks_count * chunk_size..].to_vec();
            loopback_carry_encode = lb_all[chunks_count * chunk_size..].to_vec();
            mixed_carry = mixed_all[chunks_count * chunk_size..].to_vec();
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

        let mut loopback_all = std::mem::take(&mut loopback_carry);
        loopback_all.extend(loopback_16k);

        let mut loopback_aligned = Vec::new();
        if loopback_all.len() < mic_16k.len() {
            loopback_aligned.extend(&loopback_all);
            loopback_aligned.resize(mic_16k.len(), 0);
        } else {
            loopback_aligned.extend(loopback_all.iter().take(mic_16k.len()));
        }

        let mut mixed = Vec::with_capacity(mic_16k.len());
        for i in 0..mic_16k.len() {
            let sum = mic_16k[i] as i32 + loopback_aligned[i] as i32;
            mixed.push(sum.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
        }

        let mut mic_all = std::mem::take(&mut mic_carry_encode);
        mic_all.extend(mic_16k);

        let mut lb_all = std::mem::take(&mut loopback_carry_encode);
        lb_all.extend(loopback_aligned);

        let mut mixed_all = std::mem::take(&mut mixed_carry);
        mixed_all.extend(mixed);

        let chunk_size = 960;
        let chunks_count = mixed_all.len() / chunk_size;

        for c_idx in 0..chunks_count {
            let start = c_idx * chunk_size;
            let end = start + chunk_size;

            let mic_chunk = &mic_all[start..end];
            let lb_chunk = &lb_all[start..end];
            let mixed_chunk = &mixed_all[start..end];

            granule_pos += 2880;

            if let Ok(len) = encoder_mic.encode(mic_chunk, &mut encoded_buf_mic) {
                let packet = encoded_buf_mic[..len].to_vec();
                let _ = writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos);
            }

            if let Ok(len) = encoder_loopback.encode(lb_chunk, &mut encoded_buf_lb) {
                let packet = encoded_buf_lb[..len].to_vec();
                let _ = writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos);
            }

            if let Ok(len) = encoder_mixed.encode(mixed_chunk, &mut encoded_buf_mixed) {
                let packet = encoded_buf_mixed[..len].to_vec();
                let _ = writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::NormalPacket, granule_pos);
            }
        }

        let mic_remainder = &mic_all[chunks_count * chunk_size..];
        let lb_remainder = &lb_all[chunks_count * chunk_size..];
        let mixed_remainder = &mixed_all[chunks_count * chunk_size..];

        if !mixed_remainder.is_empty() {
            let mut final_chunk_mic = mic_remainder.to_vec();
            final_chunk_mic.resize(chunk_size, 0);
            let mut final_chunk_lb = lb_remainder.to_vec();
            final_chunk_lb.resize(chunk_size, 0);
            let mut final_chunk_mixed = mixed_remainder.to_vec();
            final_chunk_mixed.resize(chunk_size, 0);

            granule_pos += 2880;

            if let Ok(len) = encoder_mic.encode(&final_chunk_mic, &mut encoded_buf_mic) {
                let packet = encoded_buf_mic[..len].to_vec();
                let _ = writer_mic.write_packet(packet.into_boxed_slice(), serial_mic, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos);
            }

            if let Ok(len) = encoder_loopback.encode(&final_chunk_lb, &mut encoded_buf_lb) {
                let packet = encoded_buf_lb[..len].to_vec();
                let _ = writer_loopback.write_packet(packet.into_boxed_slice(), serial_lb, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos);
            }

            if let Ok(len) = encoder_mixed.encode(&final_chunk_mixed, &mut encoded_buf_mixed) {
                let packet = encoded_buf_mixed[..len].to_vec();
                let _ = writer_mixed.write_packet(packet.into_boxed_slice(), serial_mixed, ogg::writing::PacketWriteEndInfo::EndStream, granule_pos);
            }
        }
    });

    let mut state = get_meeting_state().lock().unwrap();
    state.mic_stream = Some(SendSyncWrapper(mic_stream));
    state.loopback_stream = Some(SendSyncWrapper(loopback_stream));
    state.shutdown_tx = Some(shutdown_tx);

    Ok(())
}

pub fn stop_meeting_recording() {
    let mut state = get_meeting_state().lock().unwrap();

    if let Some(tx) = state.shutdown_tx.take() {
        let _ = tx.send(());
    }

    state.mic_stream = None;
    state.loopback_stream = None;

    MIC_VOLUME.store(0, Ordering::Relaxed);
    LOOPBACK_VOLUME.store(0, Ordering::Relaxed);
}

