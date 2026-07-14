// Voice Activity Detection (VAD) and linear resampling algorithms

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
    
    // We require at least 3 active frames (150ms of active sound) to qualify as speech.
    speech_frames >= 3
}

pub fn resample_to_16k_mono(samples: &[i16], source_rate: u32, source_channels: u16) -> Vec<i16> {
    // Fast path: if the source sample rate is an integer multiple of 16,000 Hz
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

#[cfg(feature = "native")]
pub fn encode_opus_ogg(samples: &[i16], source_rate: u32, source_channels: u16) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let resampled = resample_to_16k_mono(samples, source_rate, source_channels);
    let mut encoder = opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip)?;
    encoder.set_bitrate(opus::Bitrate::Bits(24000))?;
    encoder.set_complexity(1)?; 

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

    let chunk_size = 960; 
    let chunks = resampled.chunks(chunk_size);
    let chunks_len = chunks.len();
    let mut granule_pos: u64 = 0;

    let mut encoded_buf = [0u8; 512];

    for (idx, chunk) in chunks.enumerate() {
        let frame = if chunk.len() < chunk_size {
            let mut padded = chunk.to_vec();
            padded.resize(chunk_size, 0);
            padded
        } else {
            chunk.to_vec()
        };

        let len = encoder.encode(&frame, &mut encoded_buf)?;
        let packet = encoded_buf[..len].to_vec();
        granule_pos += 2880; 

        let end_info = if idx == chunks_len - 1 {
            ogg::writing::PacketWriteEndInfo::EndStream
        } else {
            ogg::writing::PacketWriteEndInfo::NormalPacket
        };

        writer.write_packet(packet.into_boxed_slice(), serial, end_info, granule_pos)?;
    }

    Ok(buffer)
}
