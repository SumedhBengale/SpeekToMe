//! Builds cpal capture streams that feed a bounded queue of i16 samples.
//! Dictation, the meeting microphone and Windows system-audio loopback all
//! share this, whatever sample format the device delivers.
use cpal::traits::DeviceTrait;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Mutex};

/// Where a capture callback delivers its samples.
#[derive(Clone)]
pub struct CaptureSink {
    pub buffer: Arc<Mutex<Vec<i16>>>,
    /// Most samples the queue may hold before the consumer drains it.
    pub limit: usize,
    /// Raised when the queue is full.
    pub overflow: Arc<AtomicBool>,
    /// Accept the part of a callback that fits (dictation) instead of dropping
    /// the whole callback (meetings, which stop on any gap).
    pub keep_partial: bool,
    /// Live level meter and its scale: RMS × scale, clamped to 1000.
    pub level: Option<(&'static AtomicU32, f32)>,
}

impl CaptureSink {
    fn push<T: Copy>(&self, data: &[T], convert: fn(T) -> i16) {
        if let Some((meter, scale)) = self.level {
            if !data.is_empty() {
                let sum: f32 = data
                    .iter()
                    .map(|&sample| {
                        let value = convert(sample) as f32 / i16::MAX as f32;
                        value * value
                    })
                    .sum();
                let rms = (sum / data.len() as f32).sqrt();
                meter.store((rms * scale).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
            }
        }
        let Ok(mut buffer) = self.buffer.lock() else {
            return;
        };
        let remaining = self.limit.saturating_sub(buffer.len());
        if data.len() > remaining {
            self.overflow.store(true, Ordering::Relaxed);
            if !self.keep_partial {
                return;
            }
        }
        buffer.extend(data.iter().take(remaining).map(|&sample| convert(sample)));
    }
}

// Float to integer `as` casts saturate, so out-of-range samples clip.
fn from_f32(sample: f32) -> i16 {
    (sample * i16::MAX as f32) as i16
}
fn from_i16(sample: i16) -> i16 {
    sample
}
fn from_u16(sample: u16) -> i16 {
    (sample as i32 - 32768) as i16
}

fn build<T: cpal::SizedSample + Send + 'static>(
    device: &cpal::Device,
    config: &cpal::StreamConfig,
    sink: CaptureSink,
    failed: Arc<AtomicBool>,
    convert: fn(T) -> i16,
) -> Result<cpal::Stream, cpal::BuildStreamError> {
    device.build_input_stream(
        config,
        move |data: &[T], _: &cpal::InputCallbackInfo| sink.push(data, convert),
        move |_| failed.store(true, Ordering::Relaxed),
        None,
    )
}

#[derive(Debug)]
pub enum CaptureError {
    UnsupportedFormat(cpal::SampleFormat),
    Build(cpal::BuildStreamError),
}

impl std::fmt::Display for CaptureError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::UnsupportedFormat(format) => write!(f, "unsupported sample format {format:?}"),
            Self::Build(error) => write!(f, "{error}"),
        }
    }
}

impl std::error::Error for CaptureError {}

/// Opens an input stream on `device` (paused until played). `failed` is
/// raised if the device reports an error.
pub fn input_stream(
    device: &cpal::Device,
    config: &cpal::SupportedStreamConfig,
    sink: CaptureSink,
    failed: Arc<AtomicBool>,
) -> Result<cpal::Stream, CaptureError> {
    let stream_config: cpal::StreamConfig = config.clone().into();
    match config.sample_format() {
        cpal::SampleFormat::F32 => build::<f32>(device, &stream_config, sink, failed, from_f32),
        cpal::SampleFormat::I16 => build::<i16>(device, &stream_config, sink, failed, from_i16),
        cpal::SampleFormat::U16 => build::<u16>(device, &stream_config, sink, failed, from_u16),
        other => return Err(CaptureError::UnsupportedFormat(other)),
    }
    .map_err(CaptureError::Build)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sink(limit: usize, keep_partial: bool) -> CaptureSink {
        CaptureSink {
            buffer: Arc::new(Mutex::new(Vec::new())),
            limit,
            overflow: Arc::new(AtomicBool::new(false)),
            keep_partial,
            level: None,
        }
    }

    #[test]
    fn converts_each_sample_format() {
        assert_eq!(from_f32(1.0), i16::MAX);
        assert_eq!(from_f32(-2.0), i16::MIN);
        assert_eq!(from_u16(32768), 0);
        assert_eq!(from_u16(0), i16::MIN);
        assert_eq!(from_i16(-5), -5);
    }

    #[test]
    fn partial_sink_keeps_what_fits_and_flags_overflow() {
        let s = sink(3, true);
        s.push(&[1i16, 2, 3, 4], from_i16);
        assert_eq!(*s.buffer.lock().unwrap(), vec![1, 2, 3]);
        assert!(s.overflow.load(Ordering::Relaxed));
    }

    #[test]
    fn whole_callback_sink_drops_a_callback_that_does_not_fit() {
        let s = sink(3, false);
        s.push(&[1i16, 2], from_i16);
        s.push(&[3i16, 4], from_i16);
        assert_eq!(*s.buffer.lock().unwrap(), vec![1, 2]);
        assert!(s.overflow.load(Ordering::Relaxed));
    }

    #[test]
    fn level_meter_reports_scaled_rms() {
        static METER: AtomicU32 = AtomicU32::new(0);
        let mut s = sink(100, true);
        s.level = Some((&METER, 1000.0));
        s.push(&[0.5f32, -0.5], from_f32);
        let level = METER.load(Ordering::Relaxed);
        assert!((499..=500).contains(&level), "{level}");
    }
}
