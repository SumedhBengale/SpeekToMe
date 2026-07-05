use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicU32, Ordering};
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};

pub static CURRENT_VOLUME: AtomicU32 = AtomicU32::new(0);

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
                        for &sample in data {
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
                        buf.extend_from_slice(data);
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
                        for &sample in data {
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
    let vendor = b"speekey-client";
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
