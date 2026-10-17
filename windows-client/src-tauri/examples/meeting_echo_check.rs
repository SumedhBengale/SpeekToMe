//! Offline, bounded-memory check of the same processor used by meeting capture.
//! cargo run --release --example meeting_echo_check -- mic.wav system.wav clean.wav
#[path = "../src/meeting_echo.rs"]
mod meeting_echo;
use meeting_echo::MeetingEchoCanceller;
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<_> = std::env::args().collect();
    if args.len() != 4 { return Err("expected mic.wav system.wav clean.wav (16 kHz mono PCM16)".into()); }
    let mut mic = hound::WavReader::open(&args[1])?;
    let mut far = hound::WavReader::open(&args[2])?;
    let spec = mic.spec();
    if spec.sample_rate != 16000 || spec.channels != 1 || spec.bits_per_sample != 16 || far.spec() != spec {
        return Err("inputs must have matching 16 kHz mono PCM16 format".into());
    }
    let mut out = hound::WavWriter::create(&args[3], spec)?;
    let mut mic = mic.samples::<i16>();
    let mut far = far.samples::<i16>();
    let mut processor = MeetingEchoCanceller::new();
    let start = std::time::Instant::now();
    let (mut count, mut before, mut after) = (0u64, 0f64, 0f64);
    loop {
        let (mut near, mut reference) = ([0i16; 160], [0i16; 160]);
        let mut n = 0;
        for i in 0..160 {
            match mic.next() {
                Some(s) => { near[i] = s?; n += 1; },
                None => break,
            }
            reference[i] = far.next().transpose()?.unwrap_or(0);
            before += (near[i] as f64).powi(2);
        }
        if n == 0 { break; }
        processor.process(&mut near, &reference);
        for &sample in &near[..n] { out.write_sample(sample)?; after += (sample as f64).powi(2); }
        count += n as u64;
    }
    out.finalize()?;
    println!("{{\"audio_seconds\":{},\"wall_seconds\":{},\"input_energy\":{},\"output_energy\":{},\"samples\":{}}}", count as f64/16000.0,start.elapsed().as_secs_f64(),before/count.max(1) as f64,after/count.max(1) as f64,count);
    Ok(())
}
