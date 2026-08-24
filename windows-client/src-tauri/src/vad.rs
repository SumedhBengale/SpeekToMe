pub use silero_vad_crs::*;

pub struct SileroVadSession {
    vad: silero_vad_crs::SileroVad,
    min_speech_frames: usize,
    min_silence_frames: usize,
    speech_frame_count: usize,
    silence_frame_count: usize,
    is_speaking: bool,
}

impl SileroVadSession {
    pub fn new() -> Result<Self, String> {
        let vad = silero_vad_crs::SileroVad::new()
            .map_err(|e| format!("Failed to initialize SileroVad: {:?}", e))?;

        Ok(Self {
            vad,
            min_speech_frames: 3,   // ~96ms of speech to confirm speech onset
            min_silence_frames: 14, // ~450ms of silence to trigger a pause commit
            speech_frame_count: 0,
            silence_frame_count: 0,
            is_speaking: false,
        })
    }

    /// Processes 512 samples of 16kHz mono audio (32ms frame).
    /// Returns (prob, is_speaking, pause_detected).
    pub fn process_frame(&mut self, samples_512: &[f32]) -> (f32, bool, bool) {
        let prob = self.vad.forward_chunk(samples_512).unwrap_or(0.0);

        let is_speech_instant = prob >= 0.5;
        let mut pause_detected = false;

        if is_speech_instant {
            self.speech_frame_count += 1;
            self.silence_frame_count = 0;
            if self.speech_frame_count >= self.min_speech_frames {
                self.is_speaking = true;
            }
        } else {
            self.silence_frame_count += 1;
            self.speech_frame_count = 0;
            if self.is_speaking && self.silence_frame_count >= self.min_silence_frames {
                self.is_speaking = false;
                pause_detected = true;
            }
        }

        (prob, self.is_speaking, pause_detected)
    }

    pub fn reset(&mut self) {
        self.vad.reset();
        self.speech_frame_count = 0;
        self.silence_frame_count = 0;
        self.is_speaking = false;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_silero_vad_crs() {
        let mut vad = SileroVadSession::new().expect("Failed to create SileroVadSession");

        let silence_frame = vec![0.0f32; 512];
        let (prob, is_speech, pause) = vad.process_frame(&silence_frame);
        println!("Silero silence probability: {}", prob);
        assert!(prob < 0.2, "Expected low prob for silence, got {}", prob);
        assert!(!is_speech);
        assert!(!pause);

        let mut sine_frame = Vec::with_capacity(512);
        for i in 0..512 {
            let t = i as f32 / 16000.0;
            sine_frame.push((2.0 * std::f32::consts::PI * 300.0 * t).sin() * 0.6);
        }
        let _ = vad.process_frame(&sine_frame);
        vad.reset();
    }
}
