//! Native-package helper for modifier-only shortcuts on Wayland.
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
    Press(std::time::Instant),
    Release(std::time::Instant),
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

pub fn chord_for(hotkey: &str, _mode: &str) -> Option<&'static str> {
    // Modifier shortcuts need real edges in both modes. A compositor may
    // otherwise report the activation only on release, losing a quick double tap.
    match hotkey {
        "Ctrl + Win" => Some("ctrl-meta"),
        "Ctrl + Shift" => Some("ctrl-shift"),
        "Ctrl + Alt" => Some("ctrl-alt"),
        "Right Alt" => Some("right-alt"),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn modifier_shortcuts_use_physical_edges_in_both_modes() {
        for mode in ["hold", "toggle"] {
            assert_eq!(super::chord_for("Ctrl + Win", mode), Some("ctrl-meta"));
            assert_eq!(super::chord_for("Right Alt", mode), Some("right-alt"));
            assert_eq!(super::chord_for("F8", mode), None);
            assert_eq!(super::chord_for("Ctrl + Space", mode), None);
        }
    }
}

pub fn start(chord: &str) -> Result<(Keywatch, UnboundedReceiver<Event>), String> {
    let metadata = std::fs::metadata(HELPER)
        .map_err(|_| "Modifier-only shortcuts require the native SpeekTo.Me package and its keyboard helper".to_string())?;
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
                Ok("P") => Event::Press(std::time::Instant::now()),
                Ok("R") => Event::Release(std::time::Instant::now()),
                _ => break,
            };
            if tx.send(event).is_err() {
                break;
            }
        }
    });
    Ok((Keywatch { child }, rx))
}
