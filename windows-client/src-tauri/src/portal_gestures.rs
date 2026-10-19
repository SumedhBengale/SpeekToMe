//! GlobalShortcuts has separate activation/deactivation streams. Sort a short
//! batch by compositor timestamps rather than the order select! polled them.
use std::time::{Duration, Instant};

#[derive(Default)]
pub struct PortalEdges {
    anchor: Option<(Duration, Instant)>,
    pending: Vec<(Duration, bool)>,
}

impl PortalEdges {
    pub fn push(&mut self, timestamp: Duration, pressed: bool, arrival: Instant) {
        self.anchor.get_or_insert((timestamp, arrival));
        self.pending.push((timestamp, pressed));
    }

    pub fn drain(&mut self) -> Vec<(bool, Instant)> {
        self.pending
            .sort_by_key(|&(timestamp, pressed)| (timestamp, !pressed));
        let Some((source, local)) = self.anchor else {
            return vec![];
        };
        self.pending
            .drain(..)
            .map(|(timestamp, pressed)| {
                let at = if timestamp >= source {
                    local.checked_add(timestamp - source)
                } else {
                    local.checked_sub(source - timestamp)
                }
                .unwrap_or(local);
                (pressed, at)
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::gestures::{Action, Mode, Recognizer, Route};

    #[test]
    fn queued_double_tap_survives_two_separate_portal_streams() {
        let now = Instant::now();
        let mut events = PortalEdges::default();
        // Both activations were polled before either deactivation.
        for (ms, down) in [(1000, true), (1200, true), (1100, false), (1300, false)] {
            events.push(Duration::from_millis(ms), down, now);
        }
        let mut r = Recognizer::new(Mode::Toggle, true);
        let actions: Vec<_> = events
            .drain()
            .into_iter()
            .flat_map(|(p, t)| r.edge(p, t))
            .collect();
        assert_eq!(actions, [Action::Capture, Action::Select(Route::Command)]);
    }

    #[test]
    fn release_polled_first_preserves_actual_hold_duration() {
        let now = Instant::now();
        let mut events = PortalEdges::default();
        events.push(Duration::from_millis(2000), false, now);
        events.push(Duration::from_millis(1000), true, now);
        let mut r = Recognizer::new(Mode::Hold, true);
        let actions: Vec<_> = events
            .drain()
            .into_iter()
            .flat_map(|(p, t)| r.edge(p, t))
            .collect();
        assert_eq!(
            actions,
            [
                Action::Capture,
                Action::Select(Route::Dictation),
                Action::Stop {
                    submit: true,
                    held_in_toggle: false
                }
            ]
        );
    }
}
