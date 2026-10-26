//! The new-meeting setup page: live microphone and system-audio levels before
//! recording starts, and a test chime to prove system audio is captured.
//!
//! The preview opens the same captures a meeting records, through the same
//! helpers, so a missing permission or a wrong device shows up here rather than
//! as a meeting with silent tracks. Every sample is measured and discarded:
//! nothing is written, sent or kept.
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use tauri::Emitter;

struct Preview {
    stop: Arc<AtomicBool>,
    /// Closes once the preview has released every device.
    released: std::sync::mpsc::Receiver<()>,
}

static PREVIEW: Mutex<Option<Preview>> = Mutex::new(None);

/// A preview left open (a hidden window, a crashed page) stops on its own.
const PREVIEW_LIMIT: std::time::Duration = std::time::Duration::from_secs(15 * 60);

/// Starts the level preview. The result says whether each capture opened, with
/// a reason for the system audio when it did not.
pub fn start() -> Result<serde_json::Value, String> {
    if crate::ACTIVE_MEETING
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .is_some()
        || crate::meeting_session::is_starting()
    {
        return Err("A meeting is already recording.".into());
    }
    stop();
    let stop_flag = Arc::new(AtomicBool::new(false));
    let (opened_tx, opened_rx) = std::sync::mpsc::channel();
    let (released_tx, released) = std::sync::mpsc::channel::<()>();
    let thread_stop = stop_flag.clone();
    // cpal streams are created, polled and dropped on this one thread.
    std::thread::Builder::new()
        .name("speektome-meeting-preview".into())
        .spawn(move || {
            run(thread_stop, opened_tx);
            drop(released_tx);
        })
        .map_err(|_| "Audio preview could not start.".to_string())?;
    let opened = opened_rx
        .recv_timeout(std::time::Duration::from_secs(10))
        .map_err(|_| "Audio devices did not respond.".to_string())?;
    *PREVIEW.lock().unwrap_or_else(|e| e.into_inner()) = Some(Preview {
        stop: stop_flag,
        released,
    });
    opened
}

/// Stops the preview, if one is running, and waits until it has released the
/// devices: on macOS, system audio is one shared capture, and a preview torn
/// down after a meeting starts would stop the meeting's capture too.
pub fn stop() {
    let preview = PREVIEW.lock().unwrap_or_else(|e| e.into_inner()).take();
    if let Some(preview) = preview {
        preview.stop.store(true, Ordering::Relaxed);
        let _ = preview
            .released
            .recv_timeout(std::time::Duration::from_secs(3));
    }
}

fn run(stop: Arc<AtomicBool>, opened: std::sync::mpsc::Sender<Result<serde_json::Value, String>>) {
    let mic_buffer = Arc::new(Mutex::new(Vec::new()));
    let loopback_buffer = Arc::new(Mutex::new(Vec::new()));
    let failed = Arc::new(AtomicBool::new(false));
    let host = cpal::default_host();

    let mic = (|| -> Result<cpal::Stream, String> {
        let device = host
            .default_input_device()
            .ok_or("No microphone was found.")?;
        let config = device
            .default_input_config()
            .map_err(|_| "The microphone could not be configured.")?;
        let sink = crate::capture::CaptureSink {
            buffer: mic_buffer.clone(),
            limit: config.sample_rate().0 as usize * config.channels() as usize * 5,
            overflow: failed.clone(),
            keep_partial: true,
            level: Some((&crate::audio::MIC_VOLUME, 2500.0)),
        };
        let stream = crate::capture::input_stream(&device, &config, sink, failed.clone())
            .map_err(|_| "Microphone access was denied or the device is busy.")?;
        stream
            .play()
            .map_err(|_| "The microphone could not start.")?;
        Ok(stream)
    })();
    let mic_stream = match mic {
        Ok(stream) => stream,
        Err(error) => {
            let _ = opened.send(Err(error));
            return;
        }
    };

    // System audio uses the recorder's own capture on each platform. Each one
    // updates LOOPBACK_VOLUME as it reads.
    #[cfg(windows)]
    let loopback = crate::audio::windows_loopback(&host, loopback_buffer.clone(), failed.clone())
        .and_then(|(stream, _, _)| {
            stream
                .play()
                .map(|_| stream)
                .map_err(|_| "System audio capture could not start".to_string())
        });
    #[cfg(target_os = "linux")]
    let loopback = crate::audio::pulse_monitor(loopback_buffer.clone(), failed.clone())
        .map_err(|e| e.to_string());
    #[cfg(target_os = "macos")]
    let loopback = crate::macos_audio::Capture::start(loopback_buffer.clone());
    #[cfg(not(any(windows, target_os = "linux", target_os = "macos")))]
    let loopback: Result<(), String> = Err("System audio capture is not supported here.".into());

    let system_error = loopback.as_ref().err().cloned();
    let _ = opened.send(Ok(serde_json::json!({
        "microphone": true,
        "system_audio": system_error.is_none(),
        "system_audio_error": system_error,
    })));

    let started = std::time::Instant::now();
    while !stop.load(Ordering::Relaxed) && started.elapsed() < PREVIEW_LIMIT {
        std::thread::sleep(std::time::Duration::from_millis(80));
        // Discard the audio; only the levels are wanted.
        mic_buffer.lock().unwrap_or_else(|e| e.into_inner()).clear();
        loopback_buffer
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clear();
        failed.store(false, Ordering::Relaxed);
        if let Some(app) = crate::TAURI_APP.get() {
            let levels = (
                crate::audio::MIC_VOLUME.load(Ordering::Relaxed),
                crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed),
            );
            let _ = app.emit("meeting-levels", levels);
        }
    }

    drop(mic_stream);
    #[cfg(target_os = "linux")]
    if let Ok((shutdown, worker)) = loopback {
        shutdown.store(true, Ordering::Relaxed);
        // As in the recorder: a suspended monitor can block a read, so the
        // reader thread is not joined.
        drop(worker);
    }
    #[cfg(not(target_os = "linux"))]
    drop(loopback);
    crate::audio::MIC_VOLUME.store(0, Ordering::Relaxed);
    crate::audio::LOOPBACK_VOLUME.store(0, Ordering::Relaxed);
    if let Some(app) = crate::TAURI_APP.get() {
        let _ = app.emit("meeting-levels", (0u32, 0u32));
    }
}

/// Plays a short two-note chime on the default output device, so the system
/// audio meter can show whether system audio is captured.
pub fn play_test_sound() -> Result<(), String> {
    let device = cpal::default_host()
        .default_output_device()
        .ok_or("No speaker or headphone output was found.")?;
    let config = device
        .default_output_config()
        .map_err(|_| "The audio output could not be configured.")?;
    let rate = config.sample_rate().0 as f32;
    let channels = config.channels() as usize;
    let total = (rate * CHIME_SECONDS) as usize;
    let mut frame = 0usize;
    let mut next = move || {
        let sample = chime(frame as f32 / rate);
        frame += 1;
        if frame > total {
            0.0
        } else {
            sample
        }
    };
    macro_rules! build {
        ($kind:ty, $convert:expr) => {
            device.build_output_stream(
                &config.clone().into(),
                move |data: &mut [$kind], _: &cpal::OutputCallbackInfo| {
                    for frame in data.chunks_mut(channels) {
                        let value = $convert(next());
                        frame.fill(value);
                    }
                },
                |_| {},
                None,
            )
        };
    }
    let stream = match config.sample_format() {
        cpal::SampleFormat::F32 => build!(f32, |s: f32| s),
        cpal::SampleFormat::I16 => build!(i16, |s: f32| (s * i16::MAX as f32) as i16),
        cpal::SampleFormat::U16 => build!(u16, |s: f32| ((s + 1.0) * 0.5 * u16::MAX as f32) as u16),
        _ => return Err("This audio output format is not supported.".into()),
    }
    .map_err(|_| "The audio output is busy or unavailable.".to_string())?;
    stream
        .play()
        .map_err(|_| "The test sound could not play.".to_string())?;
    std::thread::sleep(std::time::Duration::from_secs_f32(CHIME_SECONDS + 0.2));
    Ok(())
}

const CHIME_SECONDS: f32 = 1.0;

/// Two soft notes (A5, then E6), each with a quick attack and a gentle decay.
fn chime(t: f32) -> f32 {
    let note = |start: f32, frequency: f32| {
        let local = t - start;
        if local < 0.0 {
            return 0.0;
        }
        let attack = (local / 0.01).min(1.0);
        attack * (-local * 5.0).exp() * (std::f32::consts::TAU * frequency * local).sin()
    };
    0.25 * (note(0.0, 880.0) + note(0.35, 1318.5))
}

#[cfg(test)]
mod tests {
    use super::chime;

    #[test]
    fn chime_is_audible_but_never_clips() {
        let rate = 48_000.0;
        let samples: Vec<f32> = (0..48_000).map(|i| chime(i as f32 / rate)).collect();
        let peak = samples.iter().fold(0.0_f32, |m, s| m.max(s.abs()));
        assert!(peak > 0.1 && peak < 0.5, "peak {peak}");
        // Both notes sound: there is energy after the second note starts.
        let late: f32 = samples[20_000..30_000].iter().map(|s| s * s).sum();
        assert!(late > 1.0);
    }
}
