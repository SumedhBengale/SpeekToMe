//! Platform-independent shortcut recognition. Adapters supply physical edges,
//! never OS key-repeat events. Time is supplied so gesture boundaries are testable.
use std::time::{Duration, Instant};

pub const TAP_LIMIT: Duration = Duration::from_millis(350);
pub const DOUBLE_TAP_GAP: Duration = Duration::from_millis(400);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Mode {
    Hold,
    Toggle,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Route {
    Dictation,
    Command,
}

/// Windows raw-key adapter. Track physical state rather than recording state or
/// GetAsyncKeyState (which also changes when we inject Ctrl+C for selection).
#[cfg(any(target_os = "windows", test))]
pub struct ChordLatch {
    keys: [bool; 256],
    active: bool,
}

#[cfg(any(target_os = "windows", test))]
impl Default for ChordLatch {
    fn default() -> Self {
        Self {
            keys: [false; 256],
            active: false,
        }
    }
}

#[cfg(any(target_os = "windows", test))]
impl ChordLatch {
    pub fn active(&self) -> bool {
        self.active
    }

    pub fn edge(&mut self, vk: u32, pressed: bool, combo: u32) -> Option<bool> {
        let key = self.keys.get_mut(vk as usize)?;
        if *key == pressed {
            return None;
        }
        *key = pressed;
        let ctrl = self.keys[0x11] || self.keys[0xA2] || self.keys[0xA3];
        let shift = self.keys[0x10] || self.keys[0xA0] || self.keys[0xA1];
        let alt = self.keys[0x12] || self.keys[0xA4] || self.keys[0xA5];
        let win = self.keys[0x5B] || self.keys[0x5C];
        let mask = u32::from(ctrl)
            | (u32::from(shift) << 1)
            | (u32::from(alt) << 2)
            | (u32::from(win) << 3);
        let required = combo & 0xFFFF;
        let trigger = combo >> 16;
        let active = if trigger == 0 {
            mask == required
        } else {
            (mask == required || trigger == 0xA5 && required == 0)
                && self.keys.get(trigger as usize).copied().unwrap_or(false)
        };
        if active == self.active {
            return None;
        }
        // Releasing an extra modifier can make a cancelled chord match again.
        // Only a fresh physical press may activate it; a key-up cannot restart.
        if active && !pressed {
            return None;
        }
        self.active = active;
        Some(active)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Action {
    Capture,
    Select(Route),
    Stop { submit: bool, held_in_toggle: bool },
}

#[derive(Clone, Copy, Debug)]
enum Phase {
    Idle,
    Pending {
        pressed_at: Instant,
        released_at: Option<Instant>,
    },
    Listening,
    Processing,
}

pub struct Recognizer {
    mode: Mode,
    commands: bool,
    phase: Phase,
    down: bool,
    pressed_at: Option<Instant>,
}

impl Recognizer {
    pub fn new(mode: Mode, commands: bool) -> Self {
        Self {
            mode,
            commands,
            phase: Phase::Idle,
            down: false,
            pressed_at: None,
        }
    }

    pub fn is_capturing(&self) -> bool {
        matches!(self.phase, Phase::Pending { .. } | Phase::Listening)
    }

    pub fn deadline(&self) -> Option<Instant> {
        match self.phase {
            Phase::Pending {
                pressed_at,
                released_at,
            } => Some(match released_at {
                Some(at) => at + DOUBLE_TAP_GAP + Duration::from_millis(1),
                None => pressed_at + TAP_LIMIT,
            }),
            _ => None,
        }
    }

    fn stop(&mut self, submit: bool, held_in_toggle: bool) -> Action {
        self.phase = Phase::Processing;
        Action::Stop {
            submit,
            held_in_toggle,
        }
    }

    pub fn tick(&mut self, now: Instant) -> Vec<Action> {
        let Phase::Pending {
            pressed_at,
            released_at,
        } = self.phase
        else {
            return vec![];
        };
        if let Some(released_at) = released_at {
            if now.saturating_duration_since(released_at) > DOUBLE_TAP_GAP {
                if self.mode == Mode::Toggle {
                    self.phase = Phase::Listening;
                    return vec![Action::Select(Route::Dictation)];
                }
                return vec![self.stop(false, false)];
            }
        } else if now.saturating_duration_since(pressed_at) >= TAP_LIMIT {
            self.phase = Phase::Listening;
            return vec![Action::Select(Route::Dictation)];
        }
        vec![]
    }

    pub fn edge(&mut self, pressed: bool, now: Instant) -> Vec<Action> {
        let mut actions = self.tick(now);
        // Duplicate press/release signals cannot become another gesture.
        if pressed == self.down {
            return actions;
        }
        self.down = pressed;
        if pressed {
            self.pressed_at = Some(now);
            match self.phase {
                Phase::Idle => {
                    actions.push(Action::Capture);
                    if self.commands {
                        self.phase = Phase::Pending {
                            pressed_at: now,
                            released_at: None,
                        };
                    } else {
                        self.phase = Phase::Listening;
                        actions.push(Action::Select(Route::Dictation));
                    }
                }
                Phase::Pending {
                    released_at: Some(_),
                    ..
                } => {
                    self.phase = Phase::Listening;
                    actions.push(Action::Select(Route::Command));
                }
                Phase::Listening if self.mode == Mode::Toggle => {
                    actions.push(self.stop(true, false));
                }
                _ => {}
            }
        } else {
            let elapsed = self
                .pressed_at
                .take()
                .map(|at| now.saturating_duration_since(at))
                .unwrap_or_default();
            match self.phase {
                Phase::Pending { pressed_at, .. } => {
                    self.phase = Phase::Pending {
                        pressed_at,
                        released_at: Some(now),
                    };
                }
                Phase::Listening => {
                    let held_in_toggle = self.mode == Mode::Toggle && elapsed >= TAP_LIMIT;
                    if self.mode == Mode::Hold || held_in_toggle {
                        actions.push(self.stop(true, held_in_toggle));
                    }
                }
                _ => {}
            }
        }
        actions
    }

    pub fn cancel(&mut self) -> Vec<Action> {
        if self.is_capturing() {
            vec![self.stop(false, false)]
        } else {
            vec![]
        }
    }

    pub fn capture_finished(&mut self) {
        self.phase = Phase::Processing;
    }

    pub fn complete(&mut self) {
        self.phase = Phase::Idle;
        // Keep the physical latch until release, including on silence/error.
        // Otherwise an auto-repeat after completion would start a new session.
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn at(base: Instant, ms: u64) -> Instant {
        base + Duration::from_millis(ms)
    }
    fn stop() -> Action {
        Action::Stop {
            submit: true,
            held_in_toggle: false,
        }
    }

    #[test]
    fn raw_chord_repeat_and_trailing_modifier_releases_are_single_edges() {
        let mut chord = ChordLatch::default();
        let ctrl_win = 0x09;
        assert_eq!(chord.edge(0xA2, true, ctrl_win), None);
        assert_eq!(chord.edge(0x5B, true, ctrl_win), Some(true));
        assert_eq!(chord.edge(0x5B, true, ctrl_win), None);
        assert_eq!(chord.edge(0xA2, false, ctrl_win), Some(false));
        assert_eq!(chord.edge(0x5B, false, ctrl_win), None);
        assert!(!chord.active());
    }

    #[test]
    fn raw_action_key_repeats_cannot_become_toggle_double_taps() {
        let mut chord = ChordLatch::default();
        let f8 = 0x77 << 16;
        assert_eq!(chord.edge(0x77, true, f8), Some(true));
        for _ in 0..10 {
            assert_eq!(chord.edge(0x77, true, f8), None);
        }
        assert_eq!(chord.edge(0x77, false, f8), Some(false));
        assert_eq!(chord.edge(0x77, true, f8), Some(true));
    }

    #[test]
    fn raw_chord_supports_either_modifier_order_and_sides() {
        let mut chord = ChordLatch::default();
        let ctrl_space = (0x20 << 16) | 1;
        assert_eq!(chord.edge(0x20, true, ctrl_space), None);
        assert_eq!(chord.edge(0xA3, true, ctrl_space), Some(true));
        assert_eq!(chord.edge(0xA2, true, ctrl_space), None);
        assert_eq!(chord.edge(0xA3, false, ctrl_space), None);
        assert_eq!(chord.edge(0xA2, false, ctrl_space), Some(false));
        assert_eq!(chord.edge(0x20, false, ctrl_space), None);
    }

    #[test]
    fn custom_chord_requires_exact_modifiers_and_ignores_key_repeat() {
        let mut chord = ChordLatch::default();
        let combo = crate::hotkey::parse("Ctrl + Alt + K").unwrap().combo;
        assert_eq!(chord.edge(0xA2, true, combo), None);
        assert_eq!(chord.edge(0xA4, true, combo), None);
        assert_eq!(chord.edge(0x4B, true, combo), Some(true));
        assert_eq!(chord.edge(0x4B, true, combo), None);
        assert_eq!(chord.edge(0xA0, true, combo), Some(false));
        assert_eq!(chord.edge(0xA0, false, combo), None);
        assert_eq!(chord.edge(0x4B, false, combo), None);
        assert_eq!(chord.edge(0x4B, true, combo), Some(true));
        assert_eq!(chord.edge(0xA4, false, combo), Some(false));
    }

    #[test]
    fn releasing_an_extra_modifier_never_restarts_a_cancelled_hold() {
        let mut chord = ChordLatch::default();
        chord.edge(0xA2, true, 9);
        assert_eq!(chord.edge(0x5B, true, 9), Some(true));
        assert_eq!(chord.edge(0xA4, true, 9), Some(false));
        assert_eq!(chord.edge(0xA4, false, 9), None);
        assert_eq!(chord.edge(0x5B, true, 9), None);
        assert_eq!(chord.edge(0x5B, false, 9), None);
        assert_eq!(chord.edge(0x5B, true, 9), Some(true));
    }

    #[test]
    fn hold_routes_once_and_submits_on_release() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        assert_eq!(r.edge(true, t), [Action::Capture]);
        assert_eq!(r.tick(at(t, 350)), [Action::Select(Route::Dictation)]);
        assert_eq!(r.edge(false, at(t, 2000)), [stop()]);
    }

    #[test]
    fn tap_then_hold_is_command_with_one_capture() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        assert_eq!(r.edge(true, t), [Action::Capture]);
        assert!(r.edge(false, at(t, 100)).is_empty());
        assert_eq!(r.edge(true, at(t, 200)), [Action::Select(Route::Command)]);
        assert!(r.tick(at(t, 900)).is_empty());
        assert_eq!(r.edge(false, at(t, 2000)), [stop()]);
    }

    #[test]
    fn hold_double_tap_also_selects_command() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        r.edge(false, at(t, 60));
        assert_eq!(r.edge(true, at(t, 120)), [Action::Select(Route::Command)]);
        assert_eq!(r.edge(false, at(t, 180)), [stop()]);
    }

    #[test]
    fn lone_tap_in_hold_mode_never_uploads_silence() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        r.edge(false, at(t, 100));
        assert_eq!(
            r.tick(at(t, 501)),
            [Action::Stop {
                submit: false,
                held_in_toggle: false
            }]
        );
        assert!(r.tick(at(t, 900)).is_empty());
    }

    #[test]
    fn toggle_single_tap_listens_until_next_tap() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Toggle, true);
        assert_eq!(r.edge(true, t), [Action::Capture]);
        assert!(r.edge(false, at(t, 80)).is_empty());
        assert_eq!(r.tick(at(t, 481)), [Action::Select(Route::Dictation)]);
        assert_eq!(r.edge(true, at(t, 1500)), [stop()]);
        assert!(r.edge(false, at(t, 1600)).is_empty());
        r.complete();
        assert_eq!(r.edge(true, at(t, 1700)), [Action::Capture]);
    }

    #[test]
    fn toggle_double_tap_commands_until_third_tap() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Toggle, true);
        r.edge(true, t);
        r.edge(false, at(t, 80));
        assert_eq!(r.edge(true, at(t, 160)), [Action::Select(Route::Command)]);
        assert!(r.edge(false, at(t, 240)).is_empty());
        assert_eq!(r.edge(true, at(t, 320)), [stop()]);
    }

    #[test]
    fn repeat_and_duplicate_releases_never_select_command_or_stop_toggle() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Toggle, true);
        r.edge(true, t);
        for ms in [20, 60, 100] {
            assert!(r.edge(true, at(t, ms)).is_empty());
        }
        r.edge(false, at(t, 150));
        assert!(r.edge(false, at(t, 200)).is_empty());
        assert_eq!(r.tick(at(t, 551)), [Action::Select(Route::Dictation)]);
        assert!(r.edge(false, at(t, 600)).is_empty());
    }

    #[test]
    fn complete_hold_does_not_arm_command_for_next_recording() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        r.edge(false, at(t, 1000));
        r.complete();
        assert_eq!(r.edge(true, at(t, 1100)), [Action::Capture]);
        assert_eq!(r.tick(at(t, 1450)), [Action::Select(Route::Dictation)]);
    }

    #[test]
    fn toggle_hold_submits_with_warning_instead_of_sticking() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Toggle, true);
        r.edge(true, t);
        assert_eq!(
            r.edge(false, at(t, 1000)),
            [
                Action::Select(Route::Dictation),
                Action::Stop {
                    submit: true,
                    held_in_toggle: true
                }
            ]
        );
    }

    #[test]
    fn exact_boundaries_are_deterministic() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        r.edge(false, at(t, 349));
        assert_eq!(r.edge(true, at(t, 749)), [Action::Select(Route::Command)]);
        let mut r = Recognizer::new(Mode::Toggle, true);
        r.edge(true, t);
        r.edge(false, at(t, 349));
        assert_eq!(
            r.edge(true, at(t, 750)),
            [Action::Select(Route::Dictation), stop()]
        );
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        assert_eq!(
            r.edge(false, at(t, 350)),
            [Action::Select(Route::Dictation), stop()]
        );
    }

    #[test]
    fn disabled_command_mode_has_no_double_tap_delay() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Toggle, false);
        assert_eq!(
            r.edge(true, t),
            [Action::Capture, Action::Select(Route::Dictation)]
        );
        r.edge(false, at(t, 60));
        assert_eq!(r.edge(true, at(t, 120)), [stop()]);
        let mut r = Recognizer::new(Mode::Hold, false);
        r.edge(true, t);
        assert_eq!(r.edge(false, at(t, 60)), [stop()]);
    }

    #[test]
    fn abort_silence_and_errors_do_not_restart_a_held_key() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        r.capture_finished();
        r.complete();
        assert!(r.edge(true, at(t, 500)).is_empty());
        assert!(r.edge(false, at(t, 600)).is_empty());
        assert_eq!(r.edge(true, at(t, 700)), [Action::Capture]);
        assert_eq!(
            r.cancel(),
            [Action::Stop {
                submit: false,
                held_in_toggle: false
            }]
        );
        assert!(r.cancel().is_empty());
    }

    #[test]
    fn input_during_processing_cannot_reclassify_the_current_session() {
        let t = Instant::now();
        let mut r = Recognizer::new(Mode::Hold, true);
        r.edge(true, t);
        r.edge(false, at(t, 900));
        assert!(r.edge(true, at(t, 1000)).is_empty());
        assert!(r.edge(false, at(t, 1100)).is_empty());
        r.complete();
        assert_eq!(r.edge(true, at(t, 1200)), [Action::Capture]);
    }
}
