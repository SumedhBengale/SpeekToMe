//! Native-package helper for modifier-only hold shortcuts on Wayland.
//! The portal reports these combinations only after release on KDE. The
//! helper watches physical key transitions and sends only P/R lines.

use std::io::{BufRead, BufReader};
use std::os::unix::fs::MetadataExt;
use std::process::{Child, Command, Stdio};
use tokio::sync::mpsc::{unbounded_channel, UnboundedReceiver};

const HELPER: &str = "/usr/libexec/speektome-keywatch";

#[derive(Debug)]
pub enum Event {
    Ready,
    Press,
    Release,
}

pub struct Keywatch {
    child: Child,
}

impl Drop for Keywatch {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

pub fn chord_for(hotkey: &str, mode: &str) -> Option<&'static str> {
    if mode != "hold" {
        return None;
    }
    match hotkey {
        "Ctrl + Win" => Some("ctrl-meta"),
        "Ctrl + Shift" => Some("ctrl-shift"),
        "Ctrl + Alt" => Some("ctrl-alt"),
        "Right Alt" => Some("right-alt"),
        _ => None,
    }
}

pub fn start(chord: &str) -> Result<(Keywatch, UnboundedReceiver<Event>), String> {
    let metadata = std::fs::metadata(HELPER)
        .map_err(|_| "Modifier-only Hold requires the native SpeekTo.Me package and its keyboard helper".to_string())?;
    if !metadata.is_file() || metadata.uid() != 0 || metadata.mode() & 0o022 != 0 {
        return Err("The installed keyboard helper has unsafe file ownership or permissions".into());
    }
    let mut child = Command::new("pkexec")
        .arg(HELPER)
        .arg(chord)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .map_err(|err| format!("Could not request keyboard helper access: {err}"))?;
    let output = child.stdout.take().ok_or("Keyboard helper output is unavailable")?;
    let (tx, rx) = unbounded_channel();
    std::thread::spawn(move || {
        for line in BufReader::new(output).lines() {
            let event = match line.as_deref() {
                Ok("READY") => Event::Ready,
                Ok("P") => Event::Press,
                Ok("R") => Event::Release,
                _ => break,
            };
            if tx.send(event).is_err() {
                break;
            }
        }
    });
    Ok((Keywatch { child }, rx))
}
