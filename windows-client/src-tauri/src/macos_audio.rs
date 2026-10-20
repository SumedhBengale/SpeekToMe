//! ScreenCaptureKit stereo 48 kHz input, owned by the shared meeting lifecycle.
use std::sync::atomic::Ordering;
use std::sync::{Arc, Mutex, OnceLock};
type Buffer = Arc<Mutex<Vec<i16>>>;
static BUFFER: OnceLock<Mutex<Option<Buffer>>> = OnceLock::new();
unsafe extern "C" {
    fn s2m_audio_start(samples: extern "C" fn(*const f32, usize)) -> i32;
    fn s2m_audio_stop();
    fn s2m_audio_failed() -> bool;
}
extern "C" fn samples(data: *const f32, count: usize) {
    if data.is_null() || count == 0 || count > 96_000 {
        return;
    }
    let _ = std::panic::catch_unwind(|| {
        let buffer = BUFFER.get().and_then(|b| b.lock().ok()?.clone());
        if let Some(buffer) = buffer {
            let data = unsafe { std::slice::from_raw_parts(data, count) };
            let energy = data.iter().map(|x| x * x).sum::<f32>() / count as f32;
            crate::audio::LOOPBACK_VOLUME.store(
                (energy.sqrt() * 2500.0).clamp(0.0, 1000.0) as u32,
                Ordering::Relaxed,
            );
            if let Ok(mut buffer) = buffer.lock() {
                // Bound native delivery when a suspended consumer falls behind.
                const MAX_SAMPLES: usize = 48_000 * 2 * 30;
                if buffer.len() + count > MAX_SAMPLES {
                    OVERFLOW.store(true,Ordering::Relaxed);
                    return;
                }
                buffer.extend(data.iter().map(|x| (x.clamp(-1.0, 1.0) * 32767.0) as i16));
            }
        }
    });
}
pub fn failed()->bool { (unsafe{s2m_audio_failed()}) || OVERFLOW.load(Ordering::Relaxed) }
static OVERFLOW: std::sync::atomic::AtomicBool=std::sync::atomic::AtomicBool::new(false);
pub struct Capture;
impl Capture {
    pub fn start(buffer: Buffer) -> Result<Self, String> {
        OVERFLOW.store(false,Ordering::Relaxed);
        *BUFFER
            .get_or_init(|| Mutex::new(None))
            .lock()
            .unwrap_or_else(|e| e.into_inner()) = Some(buffer);
        match unsafe { s2m_audio_start(samples) } {
            1 => Ok(Self),
            result => {
                *BUFFER
                    .get()
                    .unwrap()
                    .lock()
                    .unwrap_or_else(|e| e.into_inner()) = None;
                Err(match result {
                    0 => "Allow SpeekTo.Me in System Settings → Privacy & Security → Screen & System Audio Recording, then start the meeting again.",
                    -2 => "Timed out starting macOS system audio capture. Check recording permissions and try again.",
                    _ => "macOS system audio capture could not start. Check recording permissions and the current display session.",
                }.into())
            }
        }
    }
}
impl Drop for Capture {
    fn drop(&mut self) {
        unsafe {
            s2m_audio_stop();
        }
        if let Some(buffer) = BUFFER.get() {
            *buffer.lock().unwrap_or_else(|e| e.into_inner()) = None;
        }
    }
}
