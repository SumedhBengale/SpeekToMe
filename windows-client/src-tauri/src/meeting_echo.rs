//! Per-meeting acoustic echo cancellation. The system recording is the render
//! reference; only microphone samples are changed, before recording and ASR.
use sonora::{config::EchoCanceller, AudioProcessing, Config, StreamConfig};
const FRAME: usize = 160; // 10 ms, 16 kHz mono

pub struct MeetingEchoCanceller {
    processor: AudioProcessing,
    failed: bool,
}

impl MeetingEchoCanceller {
    pub fn new() -> Self {
        let processor = AudioProcessing::builder()
            .config(Config {
                echo_canceller: Some(EchoCanceller::default()),
                ..Default::default()
            })
            .capture_config(StreamConfig::new(16000, 1))
            .render_config(StreamConfig::new(16000, 1))
            .build();
        Self {
            processor,
            failed: false,
        }
    }

    pub fn process(&mut self, microphone: &mut [i16], reference: &[i16]) {
        assert_eq!(microphone.len(), reference.len());
        if self.failed {
            return;
        }
        for (capture, render) in microphone.chunks_mut(FRAME).zip(reference.chunks(FRAME)) {
            let mut near = [0i16; FRAME];
            let mut far = [0i16; FRAME];
            let mut render_output = [0i16; FRAME];
            let mut clean = [0i16; FRAME];
            near[..capture.len()].copy_from_slice(capture);
            far[..render.len()].copy_from_slice(render);
            // Both streams use the same recorded sample clock. AEC3 estimates
            // the remaining acoustic/device delay from the render reference.
            let result = self
                .processor
                .process_render_i16(&far, &mut render_output)
                .and_then(|_| self.processor.set_stream_delay_ms(0))
                .and_then(|_| self.processor.process_capture_i16(&near, &mut clean));
            if let Err(error) = result {
                // Preserve genuine speech if processing ever fails; log once.
                eprintln!("[MEETING ECHO] Cancellation disabled for this recording: {error:?}");
                self.failed = true;
                return;
            }
            capture.copy_from_slice(&clean[..capture.len()]);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn energy(samples: &[i16]) -> f64 {
        samples.iter().map(|&x| (x as f64).powi(2)).sum::<f64>() / samples.len().max(1) as f64
    }
    fn reference(length: usize) -> Vec<i16> {
        let mut state = 918273u32;
        let mut previous = 0f64;
        (0..length)
            .map(|_| {
                state = state.wrapping_mul(1664525).wrapping_add(1013904223);
                previous = previous * 0.7 + ((state >> 16) as i16 as f64) * 0.09;
                previous as i16
            })
            .collect()
    }
    #[test]
    fn suppresses_delayed_echo_without_changing_reference_or_sample_count() {
        let far = reference(16000 * 8);
        let original = far.clone();
        let mut near: Vec<i16> = (0..far.len())
            .map(|i| {
                if i >= 800 {
                    (far[i - 800] as f32 * 0.6) as i16
                } else {
                    0
                }
            })
            .collect();
        let before = energy(&near[16000 * 5..]);
        let mut aec = MeetingEchoCanceller::new();
        aec.process(&mut near, &far);
        let after = energy(&near[16000 * 5..]);
        eprintln!(
            "echo energy reduction: {:.1} dB",
            10.0 * (before / after.max(1.0)).log10()
        );
        assert!(
            after < before * 0.1,
            "echo was not attenuated: {before} -> {after}"
        );
        assert_eq!(far, original);
        assert_eq!(near.len(), far.len());
    }
    #[test]
    fn keeps_near_end_speech_during_simultaneous_playback() {
        let far = reference(16000 * 12);
        let local: Vec<i16> = (0..far.len())
            .map(|i| {
                let t = i as f64 / 16000.0;
                (5500.0 * (2.0 * std::f64::consts::PI * 347.0 * t).sin()) as i16
            })
            .collect();
        let mut near: Vec<i16> = (0..far.len())
            .map(|i| {
                let echo = if i >= 800 { far[i - 800] as i32 / 2 } else { 0 };
                let speech = if i >= 16000 * 5 { local[i] as i32 } else { 0 };
                (echo + speech).clamp(i16::MIN as i32, i16::MAX as i32) as i16
            })
            .collect();
        let mut aec = MeetingEchoCanceller::new();
        aec.process(&mut near, &far);
        let ratio = energy(&near[16000 * 9..]) / energy(&local[16000 * 9..]);
        eprintln!("double-talk near-end energy ratio: {ratio:.3}");
        assert!(
            ratio > 0.5 && ratio < 1.5,
            "local speech was suppressed or echo dominated: {ratio}"
        );
    }
    #[test]
    fn preserves_quiet_local_audio_when_playback_is_silent() {
        let mut near: Vec<i16> = reference(16000 * 3).iter().map(|x| x / 12).collect();
        let before = energy(&near[16000..]);
        let far = vec![0; near.len()];
        MeetingEchoCanceller::new().process(&mut near, &far);
        let ratio = energy(&near[16000..]) / before;
        assert!(
            ratio > 0.7 && ratio < 1.2,
            "quiet local speech changed unexpectedly: {ratio}"
        );
    }
    #[test]
    fn state_is_continuous_across_recording_blocks() {
        let far = reference(16000 * 3);
        let original: Vec<i16> = far.iter().map(|x| x / 2).collect();
        let mut whole = original.clone();
        MeetingEchoCanceller::new().process(&mut whole, &far);
        let mut blocks = original;
        let mut processor = MeetingEchoCanceller::new();
        for (near, reference) in blocks.chunks_mut(960).zip(far.chunks(960)) {
            processor.process(near, reference);
        }
        assert_eq!(whole, blocks);
    }
    #[test]
    fn silence_and_partial_final_frame_remain_finite_and_aligned() {
        let mut near = vec![0; 173];
        MeetingEchoCanceller::new().process(&mut near, &vec![0; 173]);
        assert_eq!(near, vec![0; 173]);
    }
}
