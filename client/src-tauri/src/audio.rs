use std::sync::{Arc, Mutex};
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};

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
