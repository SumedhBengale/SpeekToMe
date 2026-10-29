use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
#[cfg(target_os = "linux")]
use libpulse_binding::{
    sample::{Format as PulseFormat, Spec as PulseSpec},
    stream::Direction as PulseDirection,
};
#[cfg(target_os = "linux")]
use libpulse_simple_binding::Simple as PulseSimple;
use serde::{Deserialize, Serialize};
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, Mutex};

pub use crate::audio_processing::{encode_opus_ogg, is_speech, resample_to_16k_mono};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct EnergySample {
    pub time: f32,
    pub mic: u32,
    pub lb: u32,
}

pub static CURRENT_VOLUME: AtomicU32 = AtomicU32::new(0);
/// The setup microphone check's latest peak, 0–100, while it listens.
pub static SETUP_MIC_LEVEL: AtomicU32 = AtomicU32::new(0);
pub use crate::dictation::recording_thread;
#[cfg(test)]
use crate::dictation::take_capture;

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

pub static MIC_VOLUME: AtomicU32 = AtomicU32::new(0);
pub static LOOPBACK_VOLUME: AtomicU32 = AtomicU32::new(0);

pub struct SendStream(pub cpal::Stream);
unsafe impl Send for SendStream {}
unsafe impl Sync for SendStream {}

pub struct MeetingRecordingState {
    pub identity: Option<String>,
    pub mic_stream: Option<SendStream>,
    pub loopback_stream: Option<SendStream>,
    #[cfg(target_os = "macos")]
    pub macos_loopback: Option<crate::macos_audio::Capture>,
    pub shutdown_tx: Option<std::sync::mpsc::Sender<()>>,
    pub recording_thread: Option<std::thread::JoinHandle<Result<(), String>>>,
    #[cfg(target_os = "linux")]
    pub pulse_loopback_shutdown: Option<std::sync::Arc<std::sync::atomic::AtomicBool>>,
    #[cfg(target_os = "linux")]
    pub pulse_loopback_thread: Option<std::thread::JoinHandle<()>>,
}

pub static MEETING_STATE: std::sync::OnceLock<Mutex<MeetingRecordingState>> =
    std::sync::OnceLock::new();

pub fn get_meeting_state() -> &'static Mutex<MeetingRecordingState> {
    MEETING_STATE.get_or_init(|| {
        Mutex::new(MeetingRecordingState {
            identity: None,
            mic_stream: None,
            loopback_stream: None,
            #[cfg(target_os = "macos")]
            macos_loopback: None,
            shutdown_tx: None,
            recording_thread: None,
            #[cfg(target_os = "linux")]
            pulse_loopback_shutdown: None,
            #[cfg(target_os = "linux")]
            pulse_loopback_thread: None,
        })
    })
}

fn create_recording_file(path: &std::path::Path) -> std::io::Result<std::fs::File> {
    let mut options = std::fs::OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    options.open(path)
}

/// 60 ms at 16 kHz: one Opus packet.
const OPUS_FRAME: usize = 960;
/// 560 ms at 16 kHz: one live-transcription chunk.
const LIVE_CHUNK: usize = 8960;

/// One mono 16 kHz Ogg Opus file of a meeting recording. Every packet ends a
/// page, so an interrupted recording loses at most its last 60 ms.
struct OggOpusChannel {
    encoder: opus::Encoder,
    writer: ogg::writing::PacketWriter<std::fs::File>,
    serial: u32,
    granule: u64,
    packet: [u8; 512],
}

impl OggOpusChannel {
    fn create(path: &std::path::Path, serial: u32) -> Result<Self, String> {
        let init =
            |e: &dyn std::fmt::Debug| format!("Audio recording initialization failed: {:?}", e);
        let mut encoder = opus::Encoder::new(16000, opus::Channels::Mono, opus::Application::Voip)
            .map_err(|e| init(&e))?;
        let _ = encoder.set_bitrate(opus::Bitrate::Bits(24000));
        let _ = encoder.set_complexity(1);
        let file = create_recording_file(path).map_err(|e| init(&e))?;
        let mut channel = Self {
            encoder,
            writer: ogg::writing::PacketWriter::new(file),
            serial,
            granule: 0,
            packet: [0; 512],
        };
        let mut head = Vec::new();
        head.extend_from_slice(b"OpusHead");
        head.push(1); // version
        head.push(1); // mono
        head.extend_from_slice(&312u16.to_le_bytes()); // pre-skip
        head.extend_from_slice(&16000u32.to_le_bytes());
        head.extend_from_slice(&0i16.to_le_bytes()); // output gain
        head.push(0); // channel mapping family
        let vendor = b"speektome-client-meeting";
        let mut tags = Vec::new();
        tags.extend_from_slice(b"OpusTags");
        tags.extend_from_slice(&(vendor.len() as u32).to_le_bytes());
        tags.extend_from_slice(vendor);
        tags.extend_from_slice(&0u32.to_le_bytes());
        for header in [head, tags] {
            channel
                .writer
                .write_packet(
                    header.into_boxed_slice(),
                    serial,
                    ogg::writing::PacketWriteEndInfo::EndPage,
                    0,
                )
                .map_err(|e| format!("Audio recording write failed: {}", e))?;
        }
        channel
            .writer
            .inner()
            .sync_all()
            .map_err(|_| "Cannot save recording header".to_string())?;
        Ok(channel)
    }

    /// Encodes one 60 ms frame; `last` ends the Ogg stream.
    fn write(&mut self, frame: &[i16], last: bool) -> Result<(), String> {
        self.granule += 2880; // 60 ms at Opus's 48 kHz granule rate
        let len = self
            .encoder
            .encode(frame, &mut self.packet)
            .map_err(|_| "Audio encoding failed".to_string())?;
        let end = if last {
            ogg::writing::PacketWriteEndInfo::EndStream
        } else {
            ogg::writing::PacketWriteEndInfo::EndPage
        };
        self.writer
            .write_packet(
                self.packet[..len].to_vec().into_boxed_slice(),
                self.serial,
                end,
                self.granule,
            )
            .map_err(|e| format!("Audio recording write failed: {}", e))
    }
}

/// The three files of a meeting: microphone, system audio, and both mixed.
struct MeetingFiles {
    mixed: OggOpusChannel,
    mic: OggOpusChannel,
    loopback: OggOpusChannel,
}

impl MeetingFiles {
    fn create(mixed_path: &std::path::Path) -> Result<Self, String> {
        Ok(Self {
            mixed: OggOpusChannel::create(mixed_path, 987654321)?,
            mic: OggOpusChannel::create(&mixed_path.with_extension("mic.ogg"), 123456789)?,
            loopback: OggOpusChannel::create(
                &mixed_path.with_extension("loopback.ogg"),
                555555555,
            )?,
        })
    }

    /// Writes time-aligned 60 ms frames of both channels and their mix.
    fn write_frame(&mut self, mic: &[i16], loopback: &[i16], last: bool) -> Result<(), String> {
        let mut mixed = [0i16; OPUS_FRAME];
        for (k, sample) in mixed.iter_mut().enumerate() {
            *sample =
                (mic[k] as i32 + loopback[k] as i32).clamp(i16::MIN as i32, i16::MAX as i32) as i16;
        }
        self.mic.write(mic, last)?;
        self.loopback.write(loopback, last)?;
        self.mixed.write(&mixed, last)
    }

    fn write_frames(&mut self, mic: &[i16], loopback: &[i16]) -> Result<(), String> {
        for (mic, loopback) in mic
            .chunks_exact(OPUS_FRAME)
            .zip(loopback.chunks_exact(OPUS_FRAME))
        {
            self.write_frame(mic, loopback, false)?;
        }
        Ok(())
    }

    fn sync_data(&mut self) -> Result<(), String> {
        for channel in [&mut self.mixed, &mut self.mic, &mut self.loopback] {
            channel
                .writer
                .inner()
                .sync_data()
                .map_err(|e| e.to_string())?;
        }
        Ok(())
    }

    fn sync_all(&mut self) -> Result<(), String> {
        for channel in [&mut self.mixed, &mut self.mic, &mut self.loopback] {
            channel
                .writer
                .inner()
                .sync_all()
                .map_err(|e| e.to_string())?;
        }
        Ok(())
    }
}

/// Sends 560 ms chunks of each channel to the live transcription preview.
struct LiveFeed {
    tx: std::sync::mpsc::Sender<crate::meeting_stream::MeetingStreamAudioMessage>,
    mic: Vec<i16>,
    loopback: Vec<i16>,
}

impl LiveFeed {
    fn send(&self, tag: u8, samples: &[i16]) {
        if let Ok(bytes) = encode_opus_ogg(samples, 16000, 1) {
            let _ = self
                .tx
                .send(crate::meeting_stream::MeetingStreamAudioMessage::ChannelChunk(tag, bytes));
        }
    }

    fn push(&mut self, mic: &[i16], loopback: &[i16]) {
        self.mic.extend_from_slice(mic);
        self.loopback.extend_from_slice(loopback);
        while self.mic.len() >= LIVE_CHUNK {
            let chunk: Vec<i16> = self.mic.drain(..LIVE_CHUNK).collect();
            self.send(0x01, &chunk);
        }
        while self.loopback.len() >= LIVE_CHUNK {
            let chunk: Vec<i16> = self.loopback.drain(..LIVE_CHUNK).collect();
            self.send(0x02, &chunk);
        }
    }

    fn finish(mut self, mic: &[i16], loopback: &[i16]) {
        self.mic.extend_from_slice(mic);
        self.loopback.extend_from_slice(loopback);
        if !self.mic.is_empty() {
            self.send(0x01, &self.mic);
        }
        if !self.loopback.is_empty() {
            self.send(0x02, &self.loopback);
        }
        let _ = self
            .tx
            .send(crate::meeting_stream::MeetingStreamAudioMessage::EndOfMeeting);
    }
}

/// Encodes captured meeting audio until `shutdown_rx` fires, then flushes.
#[allow(clippy::too_many_arguments)]
fn meeting_writer(
    output_ogg_path: std::path::PathBuf,
    mic_buffer: Arc<Mutex<Vec<i16>>>,
    loopback_buffer: Arc<Mutex<Vec<i16>>>,
    (mic_rate, mic_channels): (u32, u16),
    (loopback_rate, loopback_channels): (u32, u16),
    shutdown_rx: std::sync::mpsc::Receiver<()>,
    recording_ready_tx: std::sync::mpsc::Sender<()>,
    stream_tx: Option<std::sync::mpsc::Sender<crate::meeting_stream::MeetingStreamAudioMessage>>,
) -> Result<(), String> {
    let mut files = MeetingFiles::create(&output_ogg_path)?;
    #[cfg(unix)]
    std::fs::File::open(
        output_ogg_path
            .parent()
            .ok_or("Recording directory unavailable")?,
    )
    .and_then(|directory| directory.sync_all())
    .map_err(|_| "Cannot save recording directory")?;
    let _ = recording_ready_tx.send(());

    let mut live = stream_tx.map(|tx| LiveFeed {
        tx,
        mic: Vec::new(),
        loopback: Vec::new(),
    });
    let mut mic_carry: Vec<i16> = Vec::new();
    let mut loopback_carry: Vec<i16> = Vec::new();
    let mut mic_resampler = crate::audio_processing::CaptureResampler::new(mic_rate, mic_channels);
    let mut loopback_resampler =
        crate::audio_processing::CaptureResampler::new(loopback_rate, loopback_channels);
    let mut total_mic_samples = 0;
    let mut echo_canceller = crate::meeting_echo::MeetingEchoCanceller::new();
    let mut energy_log = Vec::new();
    let mut loop_count: u64 = 0;

    loop {
        if shutdown_rx
            .recv_timeout(std::time::Duration::from_millis(50))
            .is_ok()
        {
            break;
        }
        loop_count += 1;
        if loop_count % 20 == 0 {
            files.sync_data()?;
        }
        let mic_16k = mic_resampler.push(&crate::dictation::take_capture(&mic_buffer));
        let loopback_16k =
            loopback_resampler.push(&crate::dictation::take_capture(&loopback_buffer));
        total_mic_samples += mic_16k.len();
        mic_carry.extend_from_slice(&mic_16k);
        loopback_carry.extend_from_slice(&loopback_16k);

        // Let independently scheduled capture callbacks catch up. Only pad
        // a channel after 200 ms without matching audio (e.g. suspended WASAPI).
        const MAX_CHANNEL_LAG: usize = 3200;
        if mic_carry.len() > loopback_carry.len() + MAX_CHANNEL_LAG {
            loopback_carry.resize(mic_carry.len() - MAX_CHANNEL_LAG, 0);
        } else if loopback_carry.len() > mic_carry.len() + MAX_CHANNEL_LAG {
            mic_carry.resize(loopback_carry.len() - MAX_CHANNEL_LAG, 0);
        }

        // Encode and save time-synchronized aligned audio chunks
        let encode_len = mic_carry.len().min(loopback_carry.len()) / OPUS_FRAME * OPUS_FRAME;
        if encode_len == 0 {
            continue;
        }
        let mut mic_block = mic_carry[..encode_len].to_vec();
        let loopback_block = &loopback_carry[..encode_len];
        echo_canceller.process(&mut mic_block, loopback_block);
        let rms = (mic_block
            .iter()
            .map(|&s| (s as f64 / 32768.0).powi(2))
            .sum::<f64>()
            / mic_block.len() as f64)
            .sqrt();
        MIC_VOLUME.store((rms * 2500.0).clamp(0.0, 1000.0) as u32, Ordering::Relaxed);
        if let Some(live) = live.as_mut() {
            live.push(&mic_block, loopback_block);
        }
        if loop_count % 5 == 0 {
            energy_log.push(EnergySample {
                time: total_mic_samples as f32 / 16000.0,
                mic: MIC_VOLUME.load(Ordering::Relaxed),
                lb: LOOPBACK_VOLUME.load(Ordering::Relaxed),
            });
        }
        files.write_frames(&mic_block, loopback_block)?;
        mic_carry.drain(..encode_len);
        loopback_carry.drain(..encode_len);
    }

    // Final flush at end of recording
    mic_carry.extend(mic_resampler.push(&crate::dictation::take_capture(&mic_buffer)));
    mic_carry.extend(mic_resampler.finish());
    loopback_carry
        .extend(loopback_resampler.push(&crate::dictation::take_capture(&loopback_buffer)));
    loopback_carry.extend(loopback_resampler.finish());
    let length = mic_carry.len().max(loopback_carry.len());
    mic_carry.resize(length, 0);
    loopback_carry.resize(length, 0);
    echo_canceller.process(&mut mic_carry, &loopback_carry);
    if let Some(live) = live {
        live.finish(&mic_carry, &loopback_carry);
    }
    let whole = length / OPUS_FRAME * OPUS_FRAME;
    files.write_frames(&mic_carry[..whole], &loopback_carry[..whole])?;
    // A trailing partial frame is padded with silence and ends each stream.
    if whole < length {
        let mut mic_final = mic_carry[whole..].to_vec();
        let mut loopback_final = loopback_carry[whole..].to_vec();
        mic_final.resize(OPUS_FRAME, 0);
        loopback_final.resize(OPUS_FRAME, 0);
        files.write_frame(&mic_final, &loopback_final, true)?;
    }
    files.sync_all()?;
    if let Ok(json_bytes) = serde_json::to_vec(&energy_log) {
        let _ = std::fs::write(output_ogg_path.with_extension("energy.json"), json_bytes);
    }
    Ok(())
}

/// Default system output captured through Windows loopback on the output device.
#[cfg(windows)]
pub(crate) fn windows_loopback(
    host: &cpal::Host,
    buffer: Arc<Mutex<Vec<i16>>>,
    failed: Arc<std::sync::atomic::AtomicBool>,
) -> Result<(cpal::Stream, u32, u16), String> {
    println!("[AUDIO MEETING LOG] Querying default output (loopback) device...");
    let unavailable =
        || "System audio capture could not start. Check the output device.".to_string();
    let device = host.default_output_device().ok_or_else(unavailable)?;
    let config = device.default_output_config().map_err(|_| unavailable())?;
    let (rate, channels) = (config.sample_rate().0, config.channels());
    let sink = crate::capture::CaptureSink {
        buffer,
        limit: rate as usize * channels as usize * 5,
        overflow: failed.clone(),
        keep_partial: false,
        level: Some((&LOOPBACK_VOLUME, 2500.0)),
    };
    let stream = crate::capture::input_stream(&device, &config, sink, failed).map_err(|e| {
        eprintln!("[AUDIO MEETING LOG WARNING] Loopback capture unavailable: {e}");
        unavailable()
    })?;
    println!(
        "[AUDIO MEETING LOG] Windows loopback device '{}', {rate}Hz, {channels} channels",
        device
            .name()
            .unwrap_or_else(|_| "Unknown Output".to_string())
    );
    Ok((stream, rate, channels))
}

/// The default sink's monitor through the PulseAudio protocol, which PipeWire
/// also provides. Returns its shutdown flag and reader thread.
#[cfg(target_os = "linux")]
pub(crate) fn pulse_monitor(
    buffer: Arc<Mutex<Vec<i16>>>,
    failed: Arc<std::sync::atomic::AtomicBool>,
) -> Result<
    (
        Arc<std::sync::atomic::AtomicBool>,
        std::thread::JoinHandle<()>,
    ),
    Box<dyn std::error::Error>,
> {
    // PipeWire provides this PulseAudio protocol source even when the session uses
    // PipeWire natively. The special name resolves to the monitor of the default sink.
    let rate = 48_000u32;
    let channels = 2u8;
    let spec = PulseSpec {
        format: PulseFormat::S16NE,
        channels,
        rate,
    };
    if !spec.is_valid() {
        return Err("Invalid PulseAudio monitor capture format".into());
    }
    let stream = PulseSimple::new(
        None,
        "SpeekTo.Me",
        PulseDirection::Record,
        Some("@DEFAULT_MONITOR@"),
        "Meeting output capture",
        &spec,
        None,
        None,
    )
    .map_err(|e| {
        format!("Could not open the default output monitor through PulseAudio/PipeWire: {e}")
    })?;
    let shutdown = Arc::new(std::sync::atomic::AtomicBool::new(false));
    let thread_shutdown = shutdown.clone();
    let thread = std::thread::Builder::new()
        .name("speektome-output-monitor".into())
        .spawn(move || {
            // Read 10 ms blocks so shutdown is observed promptly and the capture buffer
            // advances continuously, including during silence, preserving the 48 kHz clock.
            const FRAMES: usize = 480;
            let mut bytes = [0u8; FRAMES * 2 * 2];
            while !thread_shutdown.load(Ordering::Relaxed) {
                if let Err(e) = stream.read(&mut bytes) {
                    failed.store(true, Ordering::Relaxed);
                    eprintln!("[AUDIO MEETING LOG ERROR] Output monitor capture stopped: {e}");
                    break;
                }
                if thread_shutdown.load(Ordering::Relaxed) {
                    break;
                }
                let rms = bytes
                    .chunks_exact(2)
                    .map(|b| {
                        let x = i16::from_ne_bytes([b[0], b[1]]) as f32 / i16::MAX as f32;
                        x * x
                    })
                    .sum::<f32>()
                    / (FRAMES * channels as usize) as f32;
                LOOPBACK_VOLUME.store(
                    (rms.sqrt() * 2500.0).clamp(0.0, 1000.0) as u32,
                    Ordering::Relaxed,
                );
                if let Ok(mut out) = buffer.lock() {
                    if out.len() + FRAMES * channels as usize
                        > rate as usize * channels as usize * 5
                    {
                        failed.store(true, Ordering::Relaxed);
                        break;
                    }
                    out.extend(
                        bytes
                            .chunks_exact(2)
                            .map(|b| i16::from_ne_bytes([b[0], b[1]])),
                    );
                }
            }
        })?;
    println!("[AUDIO MEETING LOG] PulseAudio/PipeWire default sink monitor capture started at {rate}Hz, {channels} channels");
    Ok((shutdown, thread))
}

/// Watches a running meeting and stops it if a device, encoder or disk write fails.
fn watch_meeting(identity: String, failed: Arc<std::sync::atomic::AtomicBool>) {
    std::thread::spawn(move || loop {
        std::thread::sleep(std::time::Duration::from_millis(250));
        let broken = {
            let state = get_meeting_state()
                .lock()
                .unwrap_or_else(|e| e.into_inner());
            if state.identity.as_deref() != Some(identity.as_str()) {
                return;
            }
            #[cfg(target_os = "macos")]
            if crate::macos_audio::failed() {
                failed.store(true, Ordering::Relaxed);
            }
            failed.load(Ordering::Relaxed)
                || state
                    .recording_thread
                    .as_ref()
                    .is_some_and(|worker| worker.is_finished())
        };
        if broken {
            if crate::ACTIVE_MEETING
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .is_none()
            {
                continue;
            }
            let _ = tauri::async_runtime::block_on(crate::stop_meeting(None));
            crate::account_errors::notify("Meetings", "Recording stopped because an audio device, encoder, or disk write failed. Local audio has been retained; check device access and available disk space.");
            if let Some(app) = crate::TAURI_APP.get() {
                use tauri::Emitter;
                let _ = app.emit("meeting-recording-stopped", ());
            }
            return;
        }
    });
}

pub fn start_meeting_recording(
    output_ogg_path: std::path::PathBuf,
    stream_tx: Option<std::sync::mpsc::Sender<crate::meeting_stream::MeetingStreamAudioMessage>>,
) -> Result<(), Box<dyn std::error::Error>> {
    println!("[AUDIO MEETING LOG] Querying default input (microphone) device...");
    let host = cpal::default_host();
    let mic_device = host.default_input_device().ok_or("No input device found")?;
    let mic_config = mic_device.default_input_config()?;
    let mic_rate = mic_config.sample_rate().0;
    let mic_channels = mic_config.channels();
    println!(
        "[AUDIO MEETING LOG] Microphone device: '{}', rate: {}Hz, channels: {}, format: {:?}",
        mic_device
            .name()
            .unwrap_or_else(|_| "Unknown Mic".to_string()),
        mic_rate,
        mic_channels,
        mic_config.sample_format()
    );

    let mic_buffer = Arc::new(Mutex::new(Vec::new()));
    let loopback_buffer = Arc::new(Mutex::new(Vec::new()));
    // Any device error or queue overflow stops the meeting with its audio kept.
    let failed = Arc::new(std::sync::atomic::AtomicBool::new(false));
    let mic_sink = crate::capture::CaptureSink {
        buffer: mic_buffer.clone(),
        limit: mic_rate as usize * mic_channels as usize * 5,
        overflow: failed.clone(),
        keep_partial: false,
        level: None, // Measured after echo cancellation instead.
    };
    let mic_stream =
        crate::capture::input_stream(&mic_device, &mic_config, mic_sink, failed.clone()).map_err(
            |e| match e {
                crate::capture::CaptureError::UnsupportedFormat(_) => {
                    "Unsupported microphone sample format".to_string()
                }
                other => other.to_string(),
            },
        )?;

    #[cfg(windows)]
    let (loopback_stream_opt, loopback_rate, loopback_channels) = {
        let (stream, rate, channels) =
            windows_loopback(&host, loopback_buffer.clone(), failed.clone())?;
        (Some(stream), rate, channels)
    };
    #[cfg(target_os = "linux")]
    let (loopback_rate, loopback_channels) = (48_000u32, 2u16);
    #[cfg(target_os = "linux")]
    let (pulse_loopback_shutdown, pulse_loopback_thread) = {
        let (shutdown, thread) = pulse_monitor(loopback_buffer.clone(), failed.clone())?;
        (Some(shutdown), Some(thread))
    };
    #[cfg(target_os = "macos")]
    let macos_loopback = crate::macos_audio::Capture::start(loopback_buffer.clone())?;
    #[cfg(target_os = "macos")]
    let (loopback_rate, loopback_channels) = (48_000u32, 2u16);
    #[cfg(not(any(windows, target_os = "linux", target_os = "macos")))]
    let (loopback_rate, loopback_channels) = (48000u32, 2u16);

    println!("[AUDIO MEETING LOG] Starting mic stream playback...");
    mic_stream.play()?;
    #[cfg(windows)]
    if let Some(ref lb) = loopback_stream_opt {
        lb.play()
            .map_err(|_| "System audio capture could not start")?;
    }

    let (shutdown_tx, shutdown_rx) = std::sync::mpsc::channel::<()>();
    let (recording_ready_tx, recording_ready_rx) = std::sync::mpsc::channel();
    let writer_mic_buffer = mic_buffer.clone();
    let writer_loopback_buffer = loopback_buffer.clone();
    let recording_thread = std::thread::spawn(move || {
        meeting_writer(
            output_ogg_path,
            writer_mic_buffer,
            writer_loopback_buffer,
            (mic_rate, mic_channels),
            (loopback_rate, loopback_channels),
            shutdown_rx,
            recording_ready_tx,
            stream_tx,
        )
    });

    let mut state = get_meeting_state()
        .lock()
        .unwrap_or_else(|e| e.into_inner());
    let identity = uuid::Uuid::new_v4().to_string();
    state.identity = Some(identity.clone());
    state.mic_stream = Some(SendStream(mic_stream));
    #[cfg(target_os = "macos")]
    {
        state.macos_loopback = Some(macos_loopback);
    }
    #[cfg(windows)]
    {
        state.loopback_stream = loopback_stream_opt.map(SendStream);
    }
    #[cfg(not(windows))]
    {
        state.loopback_stream = None;
    }
    #[cfg(target_os = "linux")]
    {
        state.pulse_loopback_shutdown = pulse_loopback_shutdown;
        state.pulse_loopback_thread = pulse_loopback_thread;
    }
    state.shutdown_tx = Some(shutdown_tx);
    state.recording_thread = Some(recording_thread);
    drop(state);
    if recording_ready_rx
        .recv_timeout(std::time::Duration::from_secs(5))
        .is_err()
    {
        let error = stop_meeting_recording()
            .err()
            .unwrap_or_else(|| "Recording storage initialization timed out".to_string());
        return Err(error.into());
    }
    watch_meeting(identity, failed);
    Ok(())
}

pub fn stop_meeting_recording() -> Result<(), String> {
    let mut state = get_meeting_state()
        .lock()
        .unwrap_or_else(|e| e.into_inner());

    state.identity = None;
    if let Some(tx) = state.shutdown_tx.take() {
        let _ = tx.send(());
    }

    state.mic_stream = None;
    state.loopback_stream = None;
    #[cfg(target_os = "macos")]
    {
        state.macos_loopback = None;
    }
    #[cfg(target_os = "linux")]
    {
        if let Some(stop) = state.pulse_loopback_shutdown.take() {
            stop.store(true, Ordering::Relaxed);
        }
        if let Some(worker) = state.pulse_loopback_thread.take() {
            // pa_simple_read may block if the monitor is suspended. The stop
            // flag is checked after it wakes; do not freeze the UI on join.
            drop(worker);
        }
    }

    MIC_VOLUME.store(0, Ordering::Relaxed);
    LOOPBACK_VOLUME.store(0, Ordering::Relaxed);
    let writer = state.recording_thread.take();
    drop(state);
    if let Some(writer) = writer {
        writer
            .join()
            .map_err(|_| "Meeting audio finalization failed".to_string())??;
    }
    Ok(())
}

#[cfg(test)]
mod capture_tests {
    use super::*;
    #[test]
    fn meeting_files_write_aligned_ogg_opus_streams() {
        let directory =
            std::env::temp_dir().join(format!("speektome-meeting-test-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(&directory).unwrap();
        let mixed = directory.join("meeting.ogg");
        let mut files = MeetingFiles::create(&mixed).unwrap();
        let tone: Vec<i16> = (0..OPUS_FRAME * 5)
            .map(|i| ((i as f32 / 8.0).sin() * 8000.0) as i16)
            .collect();
        files.write_frames(&tone, &tone).unwrap();
        files
            .write_frame(&tone[..OPUS_FRAME], &tone[..OPUS_FRAME], true)
            .unwrap();
        files.sync_all().unwrap();
        for path in [
            mixed.clone(),
            mixed.with_extension("mic.ogg"),
            mixed.with_extension("loopback.ogg"),
        ] {
            let data = std::fs::read(&path).unwrap();
            assert!(data.starts_with(b"OggS"));
            let pages = data.windows(4).filter(|w| w == b"OggS").count();
            assert_eq!(pages, 2 + 6, "{}", path.display());
            assert!(data.windows(8).any(|w| w == b"OpusHead"));
        }
        std::fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn stereo_capture_continues_past_the_old_125_second_limit() {
        let queue = Mutex::new(Vec::new());
        let chunk = vec![1000i16; 4800];
        let mut batch = Vec::new();
        for _ in 0..3000 {
            queue.lock().unwrap().extend_from_slice(&chunk);
            batch.extend(resample_to_16k_mono(&take_capture(&queue), 48000, 2));
            assert!(queue.lock().unwrap().is_empty());
        }
        assert_eq!(batch.len(), 16000 * 150);
    }
}

/// User-initiated, bounded local check. Samples are reduced to a peak value;
/// no audio buffer, file, upload, or transcription history is created.
pub fn check_setup_microphone() -> serde_json::Value {
    let host = cpal::default_host();
    let Some(device) = host.default_input_device() else {
        return serde_json::json!({"ready":false,"error":"No default microphone was found."});
    };
    let name = device
        .name()
        .unwrap_or_else(|_| "Default microphone".into());
    let Ok(config) = device.default_input_config() else {
        return serde_json::json!({"ready":false,"error":"The default microphone could not be configured."});
    };
    let peak = Arc::new(AtomicU32::new(0));
    let failed = Arc::new(std::sync::atomic::AtomicBool::new(false));
    let received = Arc::new(std::sync::atomic::AtomicBool::new(false));
    macro_rules! probe {
        ($kind:ty,$amplitude:expr) => {{
            let volume = peak.clone();
            let errors = failed.clone();
            let samples = received.clone();
            device.build_input_stream(
                &config.clone().into(),
                move |data: &[$kind], _: &cpal::InputCallbackInfo| {
                    if !data.is_empty() {
                        samples.store(true, Ordering::Relaxed);
                    }
                    let level = data.iter().map($amplitude).fold(0.0_f32, f32::max);
                    let percent = (level.min(1.0) * 100.0) as u32;
                    volume.fetch_max(percent, Ordering::Relaxed);
                    SETUP_MIC_LEVEL.store(percent, Ordering::Relaxed);
                },
                move |_| {
                    errors.store(true, Ordering::Relaxed);
                },
                None,
            )
        }};
    }
    let stream = match config.sample_format() {
        cpal::SampleFormat::F32 => probe!(f32, |s: &f32| s.abs()),
        cpal::SampleFormat::I16 => probe!(i16, |s: &i16| (*s as f32 / 32768.0).abs()),
        cpal::SampleFormat::U16 => probe!(u16, |s: &u16| ((*s as f32 - 32768.0) / 32768.0).abs()),
        _ => {
            return serde_json::json!({"ready":false,"error":"This microphone format is not supported. Choose another default input in system settings."})
        }
    };
    let Ok(stream) = stream else {
        return serde_json::json!({"ready":false,"error":"Microphone access was denied or the device is busy. Check system audio and privacy settings."});
    };
    if stream.play().is_err() {
        return serde_json::json!({"ready":false,"error":"The microphone could not start."});
    }
    std::thread::sleep(std::time::Duration::from_secs(3));
    drop(stream);
    SETUP_MIC_LEVEL.store(0, Ordering::Relaxed);
    serde_json::json!({"ready":!failed.load(Ordering::Relaxed)&&received.load(Ordering::Relaxed),"device":name,"level":peak.load(Ordering::Relaxed)})
}
