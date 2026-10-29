//! Speek, the mascot, for the native overlays. A port of the live rig in
//! `shared/speek/speek.js` so the pill shows exactly what the website does:
//! same shapes, moods, springs and timings. Change the two together; see
//! docs/speek-mascot.md. Coordinates are the web rig's 1254-unit box and times
//! are milliseconds, as there. The web-only M logo and pointer following are
//! not ported.
use std::f64::consts::PI;
use std::sync::Mutex;

const fn rgb(r: u8, g: u8, b: u8) -> (f64, f64, f64) {
    (r as f64 / 255.0, g as f64 / 255.0, b as f64 / 255.0)
}
pub const PINK: (f64, f64, f64) = rgb(0xFC, 0x5B, 0x90);
pub const CHEEK: (f64, f64, f64) = rgb(0xFF, 0xA3, 0xC2);
pub const FACE: (f64, f64, f64) = rgb(0x1C, 0x15, 0x20);
/// The box the resting mascot fills (the web's `SPEEK_ICON_VIEWBOX`): x, y, size.
pub const ICON_BOX: (f64, f64, f64) = (172.0, 215.0, 910.0);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Mood {
    Idle,
    Listen,
    Think,
    Happy,
    Admire,
    Surprised,
    Sleepy,
    Oops,
}

/// A quadratic stroke with round caps: x1, y1, qx, qy, x2, y2, width, opacity.
/// A straight one is a pill, so eyes and mouth bars share the body's shape.
pub type Stroke = [f64; 8];
/// A rounded rectangle: cx, cy, width, height, rotation in degrees, radius.
pub type Pill = [f64; 6];

fn pill(cx: f64, cy: f64, w: f64, h: f64, o: f64) -> Stroke {
    let d = ((h - w) / 2.0).max(0.5);
    [cx, cy - d, cx, cy, cx, cy + d, w, o]
}
#[allow(clippy::too_many_arguments)]
fn arc(x1: f64, y1: f64, qx: f64, qy: f64, x2: f64, y2: f64, w: f64, o: f64) -> Stroke {
    [x1, y1, qx, qy, x2, y2, w, o]
}
fn hidden(x: f64, y: f64) -> Stroke {
    pill(x, y, 30.0, 30.0, 0.0)
}

// Stroke slots: 0/4 cheeks, 1/3 eyes, 2 mouth, 5/6 brows,
// 7/8/12/13 spare mouth strokes, 9-11 thought dots.
const STROKES: usize = 14;
const DOTS: std::ops::RangeInclusive<usize> = 9..=11;
fn stroke_color(slot: usize) -> (f64, f64, f64) {
    match slot {
        0 | 4 => CHEEK,
        9..=11 => PINK,
        _ => FACE,
    }
}

fn face(mood: Mood) -> [Stroke; STROKES] {
    let mut s = [
        hidden(425.0, 800.0),
        hidden(497.0, 690.0),
        hidden(627.0, 840.0),
        hidden(757.0, 690.0),
        hidden(829.0, 800.0),
        hidden(497.0, 540.0),
        hidden(757.0, 540.0),
        hidden(587.0, 840.0),
        hidden(667.0, 840.0),
        hidden(1060.0, 600.0),
        hidden(1110.0, 500.0),
        hidden(1150.0, 390.0),
        hidden(547.0, 840.0),
        hidden(707.0, 840.0),
    ];
    let cheeks = |s: &mut [Stroke; STROKES], o: f64, dx: f64| {
        s[0] = arc(
            400.0 + dx,
            800.0,
            425.0 + dx,
            800.0,
            450.0 + dx,
            800.0,
            50.0,
            o,
        );
        s[4] = arc(
            804.0 + dx,
            800.0,
            829.0 + dx,
            800.0,
            854.0 + dx,
            800.0,
            50.0,
            o,
        );
    };
    match mood {
        Mood::Idle => {
            cheeks(&mut s, 0.9, 0.0);
            s[1] = pill(497.0, 690.0, 96.0, 200.0, 1.0);
            s[3] = pill(757.0, 690.0, 96.0, 200.0, 1.0);
            s[2] = arc(587.0, 830.0, 627.0, 862.0, 667.0, 830.0, 40.0, 1.0);
        }
        Mood::Sleepy => {
            cheeks(&mut s, 0.6, 0.0);
            s[1] = arc(447.0, 700.0, 497.0, 740.0, 547.0, 700.0, 30.0, 1.0);
            s[3] = arc(707.0, 700.0, 757.0, 740.0, 807.0, 700.0, 30.0, 1.0);
            s[2] = arc(602.0, 842.0, 627.0, 852.0, 652.0, 842.0, 34.0, 1.0);
            s[9] = pill(900.0, 450.0, 30.0, 30.0, 1.0);
            s[10] = pill(900.0, 450.0, 42.0, 42.0, 1.0);
            s[11] = pill(900.0, 450.0, 54.0, 54.0, 1.0);
        }
        Mood::Surprised => {
            s[1] = pill(497.0, 670.0, 124.0, 250.0, 1.0);
            s[3] = pill(757.0, 670.0, 124.0, 250.0, 1.0);
            s[5] = arc(442.0, 500.0, 497.0, 468.0, 552.0, 500.0, 32.0, 1.0);
            s[6] = arc(702.0, 500.0, 757.0, 468.0, 812.0, 500.0, 32.0, 1.0);
            s[2] = pill(627.0, 872.0, 72.0, 96.0, 1.0);
        }
        Mood::Listen => {
            cheeks(&mut s, 0.9, 0.0);
            s[1] = pill(497.0, 682.0, 100.0, 170.0, 1.0);
            s[3] = pill(757.0, 682.0, 100.0, 170.0, 1.0);
            s[5] = arc(447.0, 560.0, 497.0, 552.0, 547.0, 566.0, 30.0, 1.0);
            s[6] = arc(707.0, 566.0, 757.0, 552.0, 807.0, 560.0, 30.0, 1.0);
            s[2] = arc(604.0, 846.0, 627.0, 849.0, 650.0, 846.0, 36.0, 1.0);
        }
        Mood::Think => {
            s[1] = pill(527.0, 660.0, 88.0, 180.0, 1.0);
            s[3] = pill(787.0, 660.0, 88.0, 180.0, 1.0);
            s[5] = arc(462.0, 535.0, 507.0, 540.0, 552.0, 545.0, 32.0, 1.0);
            s[6] = arc(722.0, 500.0, 770.0, 462.0, 818.0, 480.0, 32.0, 1.0);
            s[2] = arc(610.0, 852.0, 640.0, 846.0, 672.0, 834.0, 38.0, 1.0);
            s[9] = pill(1060.0, 600.0, 40.0, 40.0, 1.0);
            s[10] = pill(1110.0, 500.0, 56.0, 56.0, 1.0);
            s[11] = pill(1150.0, 390.0, 72.0, 72.0, 1.0);
        }
        Mood::Happy => {
            cheeks(&mut s, 1.0, 0.0);
            s[1] = arc(437.0, 710.0, 497.0, 600.0, 557.0, 710.0, 46.0, 1.0);
            s[3] = arc(697.0, 710.0, 757.0, 600.0, 817.0, 710.0, 46.0, 1.0);
            s[2] = arc(537.0, 800.0, 627.0, 920.0, 717.0, 800.0, 48.0, 1.0);
        }
        Mood::Oops => {
            s[1] = pill(502.0, 712.0, 88.0, 150.0, 1.0);
            s[3] = pill(752.0, 712.0, 88.0, 150.0, 1.0);
            s[5] = arc(432.0, 592.0, 482.0, 572.0, 537.0, 550.0, 32.0, 1.0);
            s[6] = arc(717.0, 550.0, 772.0, 572.0, 822.0, 592.0, 32.0, 1.0);
            s[2] = arc(567.0, 872.0, 627.0, 822.0, 687.0, 872.0, 40.0, 1.0);
        }
        Mood::Admire => {
            cheeks(&mut s, 0.9, 25.0);
            s[1] = pill(552.0, 690.0, 96.0, 190.0, 1.0);
            s[3] = pill(812.0, 690.0, 96.0, 190.0, 1.0);
            s[2] = arc(612.0, 830.0, 652.0, 866.0, 692.0, 830.0, 40.0, 1.0);
        }
    }
    s
}

/// Left ear angle, right ear angle, ear lift, squash, head tilt.
fn pose(mood: Mood) -> [f64; 5] {
    match mood {
        Mood::Idle => [10.0, 10.0, 0.0, 1.0, 0.0],
        Mood::Sleepy => [52.0, 52.0, 30.0, 0.97, 6.0],
        Mood::Surprised => [-4.0, -4.0, -70.0, 1.05, 0.0],
        Mood::Listen => [4.0, 4.0, 0.0, 1.0, 2.0],
        Mood::Think => [46.0, 8.0, 0.0, 1.0, -4.0],
        Mood::Happy => [22.0, 22.0, -10.0, 1.0, 0.0],
        Mood::Oops => [70.0, 70.0, 40.0, 0.98, 0.0],
        Mood::Admire => [14.0, 6.0, 0.0, 1.0, 4.0],
    }
}
fn tail_length(mood: Mood) -> f64 {
    match mood {
        Mood::Sleepy => 0.85,
        Mood::Surprised => 1.15,
        Mood::Think => 0.95,
        Mood::Happy => 1.05,
        Mood::Oops => 0.55,
        _ => 1.0,
    }
}

/// An ear pivots inside the head; `stretch` lengthens it upward from there.
fn ear(px: f64, py: f64, angle: f64, stretch: f64) -> Pill {
    let r = angle * PI / 180.0;
    let reach = 120.0 + stretch / 2.0;
    [
        px + reach * r.sin(),
        py - reach * r.cos(),
        190.0,
        420.0 + stretch,
        angle,
        95.0,
    ]
}
const HEAD: Pill = [627.0, 710.0, 760.0, 560.0, 0.0, 270.0];
fn body(p: [f64; 5], stretch: [f64; 2]) -> [Pill; 5] {
    [
        ear(407.0, 560.0 + p[2], -p[0], stretch[0]),
        HEAD,
        HEAD,
        HEAD,
        ear(847.0, 560.0 + p[2], p[1], stretch[1]),
    ]
}

/// While listening the ears follow the voice level (0–1), each on its own
/// rhythm so they flop independently. Two sines per ear, rates in 1/seconds.
const EAR_RHYTHMS: [(f64, f64, f64); 2] = [(7.3, 11.9, 0.4), (8.4, 6.9, 5.2)];
fn ear_rhythm(side: usize, time: f64) -> f64 {
    let (a, b, phase) = EAR_RHYTHMS[side];
    let seconds = time / 1000.0;
    0.6 + 0.4 * (0.6 * (seconds * a + phase).sin() + 0.4 * (seconds * b + phase * 1.7).sin())
}

const TAIL_BASE: [[f64; 2]; 2] = [[430.0, 930.0], [565.0, 965.0]];
const TAIL_PIVOT: [f64; 2] = [497.5, 947.5];
const TAIL_TIP: [f64; 2] = [-132.5, 147.5];
/// The tail's rounded triangle (drawn with a 50-unit round join).
pub const TAIL_ROUNDING: f64 = 25.0;
fn tail(length: f64, angle: f64) -> [[f64; 2]; 3] {
    let (s, c) = (angle * PI / 180.0).sin_cos();
    let tip = [
        TAIL_PIVOT[0] + (TAIL_TIP[0] * c - TAIL_TIP[1] * s) * length,
        TAIL_PIVOT[1] + (TAIL_TIP[0] * s + TAIL_TIP[1] * c) * length,
    ];
    [TAIL_BASE[0], TAIL_BASE[1], tip]
}

fn shift(stroke: &mut Stroke, dx: f64, dy: f64) {
    for k in [0, 2, 4] {
        stroke[k] += dx;
    }
    for k in [1, 3, 5] {
        stroke[k] += dy;
    }
}

/// One drawable pose, in the 1254-unit box.
#[derive(Clone, Debug, PartialEq)]
pub struct Frame {
    /// Ears (0, 4), the inner pills that form the head (1, 3) and the head (2).
    pub pills: [Pill; 5],
    pub tail: [[f64; 2]; 3],
    pub tail_opacity: f64,
    /// Every face stroke with its colour, in drawing order.
    pub strokes: [(Stroke, (f64, f64, f64)); STROKES],
    /// Hop x, hop y, head tilt in degrees and squash, applied around (627, 990).
    pub hop: (f64, f64),
    pub tilt: f64,
    pub squash: f64,
}

impl Frame {
    /// Maps a point of the drawn mascot back into its unposed box.
    pub fn unpose(&self, x: f64, y: f64) -> (f64, f64) {
        let (x, y) = (x - self.hop.0 - 627.0, y - self.hop.1 - 990.0);
        let (s, c) = (-self.tilt * PI / 180.0).sin_cos();
        let (x, y) = (x * c - y * s, x * s + y * c);
        (627.0 + x / (2.0 - self.squash), 990.0 + y / self.squash)
    }
}

/// Small xorshift generator for glances, blinks and twitches.
struct Random(u64);
impl Random {
    fn next(&mut self) -> f64 {
        self.0 ^= self.0 << 13;
        self.0 ^= self.0 >> 7;
        self.0 ^= self.0 << 17;
        (self.0 >> 11) as f64 / (1u64 << 53) as f64
    }
}

/// The live rig: springs and timers stepped at the web's 60 Hz frame rate.
pub struct Rig {
    mood: Mood,
    mood_start: f64,
    time: f64,
    body: [Pill; 5],
    body_velocity: [Pill; 5],
    strokes: [Stroke; STROKES],
    stroke_velocity: [Stroke; STROKES],
    pose: [f64; 5],
    tail_length: f64,
    tail_length_velocity: f64,
    tail_angle: f64,
    tail_angle_velocity: f64,
    look: (f64, f64),
    look_target: (f64, f64),
    previous_hop: f64,
    next_glance: f64,
    next_blink: f64,
    blink_at: f64,
    next_twitch: f64,
    twitch_at: f64,
    twitch_ear: usize,
    next_flick: f64,
    flick_at: f64,
    random: Random,
    frame: Frame,
}

const STEP_MS: f64 = 1000.0 / 60.0;
const BLINKING: [Mood; 6] = [
    Mood::Idle,
    Mood::Think,
    Mood::Listen,
    Mood::Oops,
    Mood::Surprised,
    Mood::Admire,
];

impl Rig {
    /// A rig resting in `mood` at `time` milliseconds.
    pub fn new(mood: Mood, time: f64, seed: u64) -> Rig {
        let mut rig = Rig {
            mood,
            mood_start: time,
            time,
            body: body(pose(mood), [0.0; 2]),
            body_velocity: [[0.0; 6]; 5],
            strokes: face(mood),
            stroke_velocity: [[0.0; 8]; STROKES],
            pose: pose(mood),
            tail_length: tail_length(mood),
            tail_length_velocity: 0.0,
            tail_angle: 0.0,
            tail_angle_velocity: 0.0,
            look: (0.0, 0.0),
            look_target: (0.0, 0.0),
            previous_hop: 0.0,
            next_glance: 0.0,
            next_blink: 0.0,
            blink_at: -1e9,
            next_twitch: 0.0,
            twitch_at: -1e9,
            twitch_ear: 0,
            next_flick: 0.0,
            flick_at: -1e9,
            random: Random(seed | 1),
            frame: Frame {
                pills: body(pose(mood), [0.0; 2]),
                tail: tail(tail_length(mood), 0.0),
                tail_opacity: 1.0,
                strokes: std::array::from_fn(|slot| (face(mood)[slot], stroke_color(slot))),
                hop: (0.0, 0.0),
                tilt: pose(mood)[4],
                squash: pose(mood)[3],
            },
        };
        rig.render(time, 0.0, 0.0, 0.0);
        rig
    }

    pub fn set_mood(&mut self, mood: Mood) {
        if mood != self.mood {
            self.mood = mood;
            self.mood_start = self.time;
        }
    }

    /// Advances to `time` milliseconds. `voice` is the microphone level
    /// (0–1), used while listening.
    pub fn advance(&mut self, time: f64, voice: f64) -> &Frame {
        // A hair of slack keeps rounding from dropping a step.
        let steps = ((time - self.time + 1e-6) / STEP_MS)
            .floor()
            .clamp(0.0, 8.0) as usize;
        for _ in 0..steps {
            self.step(self.time + STEP_MS, voice);
        }
        if steps == 8 {
            self.time = time;
        }
        &self.frame
    }

    fn step(&mut self, time: f64, voice: f64) {
        self.time = time;
        let mood = self.mood;
        let elapsed = time - self.mood_start;
        let fade = |window: f64| (1.0 - elapsed / window).max(0.0);
        let mut face_target = face(mood);
        let mut pose_target = pose(mood);
        let mut stretch = [0.0; 2];
        let level = if mood == Mood::Listen {
            voice.clamp(0.0, 1.0)
        } else {
            0.0
        };

        if mood == Mood::Idle || mood == Mood::Listen {
            if time > self.next_twitch {
                self.twitch_at = time;
                self.twitch_ear = if self.random.next() < 0.5 { 0 } else { 1 };
                self.next_twitch = time + 2500.0 + self.random.next() * 3000.0;
            }
            if time - self.twitch_at < 220.0 {
                pose_target[self.twitch_ear] += ((time - self.twitch_at) / 220.0 * PI).sin() * 18.0;
            }
        }
        if mood == Mood::Idle {
            if time > self.next_glance {
                const GLANCES: [(f64, f64); 8] = [
                    (0.0, 0.0),
                    (-1.0, 0.0),
                    (1.0, 0.0),
                    (0.0, -1.0),
                    (0.0, 1.0),
                    (-0.7, -0.6),
                    (0.8, 0.5),
                    (0.0, 0.0),
                ];
                self.look_target = GLANCES[(self.random.next() * 8.0) as usize % 8];
                self.next_glance = time + 900.0 + self.random.next() * 1600.0;
            }
            if time > self.next_flick {
                self.flick_at = time;
                self.next_flick = time + 3000.0 + self.random.next() * 3500.0;
            }
            pose_target[4] += self.look.0 * 3.0;
        } else {
            self.look_target = (0.0, 0.0);
        }
        self.look.0 += (self.look_target.0 - self.look.0) * 0.2;
        self.look.1 += (self.look_target.1 - self.look.1) * 0.2;
        let look = self.look;

        if mood == Mood::Idle {
            for slot in [1, 3] {
                shift(&mut face_target[slot], look.0 * 60.0, look.1 * 40.0);
            }
            shift(&mut face_target[2], look.0 * 30.0, look.1 * 20.0);
            for slot in [0, 4] {
                shift(&mut face_target[slot], look.0 * 20.0, look.1 * 12.0);
            }
        }
        if mood == Mood::Listen {
            // Concentrating: the ears carry the voice. Each stretches and flops
            // on its own rhythm; loud speech flares them out and widens the eyes.
            let flare = ((level - 0.6) / 0.4).max(0.0) * 10.0;
            for side in 0..2 {
                let share = level * ear_rhythm(side, time);
                stretch[side] = 110.0 * share;
                pose_target[side] += 10.0 * share + flare;
            }
            let peak = level * level;
            for (slot, x) in [(1, 497.0), (3, 757.0)] {
                face_target[slot] = pill(x, 682.0, 100.0 + 16.0 * peak, 170.0 + 90.0 * peak, 1.0);
            }
            for slot in [5, 6] {
                shift(&mut face_target[slot], 0.0, -30.0 * peak);
            }
        }
        if mood == Mood::Think {
            let drift = (time / 700.0).sin() * 22.0;
            for slot in [1, 3, 5, 6] {
                shift(&mut face_target[slot], drift, 0.0);
            }
        }
        if mood == Mood::Happy {
            let wiggle = (elapsed / 80.0).sin() * 14.0 * fade(1200.0);
            pose_target[0] += wiggle;
            pose_target[1] -= wiggle;
        }
        if mood == Mood::Admire && elapsed > 2000.0 {
            // Done reading: glance back at the viewer.
            for slot in [1, 3, 2] {
                shift(
                    &mut face_target[slot],
                    if slot == 2 { -25.0 } else { -55.0 },
                    0.0,
                );
            }
            for slot in [0, 4] {
                shift(&mut face_target[slot], -25.0, 0.0);
            }
            pose_target[4] = 0.0;
        }

        let mut tail_length_target = tail_length(mood);
        let mut tail_angle_target;
        match mood {
            Mood::Idle => {
                tail_angle_target = (time / 900.0).sin() * 6.0 - look.0 * 8.0;
                if time - self.flick_at < 320.0 {
                    tail_angle_target += ((time - self.flick_at) / 320.0 * PI * 2.0).sin() * 22.0;
                }
            }
            Mood::Listen => {
                tail_length_target = 1.0 + 0.22 * level;
                tail_angle_target = (time / 300.0).sin() * 4.0 - level * 6.0;
            }
            Mood::Happy => {
                tail_angle_target = (elapsed / 60.0).sin() * 26.0 * fade(1600.0).max(0.25)
            }
            Mood::Sleepy => {
                tail_angle_target = -20.0 + (time / 1100.0).sin() * 4.0;
                tail_length_target = 0.85 + 0.04 * (time / 900.0).sin();
            }
            Mood::Think => {
                let beat = (time % 700.0) / 700.0;
                tail_angle_target = 6.0
                    - if beat < 0.18 {
                        (beat / 0.18 * PI).sin() * 22.0
                    } else {
                        0.0
                    };
            }
            Mood::Surprised => {
                tail_angle_target = (elapsed / 28.0).sin() * 12.0 * fade(500.0) + 14.0 * fade(250.0)
            }
            Mood::Oops => {
                tail_angle_target = 24.0 + (elapsed / 35.0).sin() * 8.0 * fade(600.0);
                tail_length_target = 0.55 + 0.05 * (time / 180.0).sin();
            }
            Mood::Admire => tail_angle_target = (time / 600.0).sin() * 8.0,
        }

        let body_target = body(pose_target, stretch);
        for part in 0..5 {
            for k in 0..6 {
                self.body_velocity[part][k] = (self.body_velocity[part][k]
                    + (body_target[part][k] - self.body[part][k]) * 0.16)
                    * 0.68;
                self.body[part][k] += self.body_velocity[part][k];
            }
        }
        for slot in 0..STROKES {
            for k in 0..8 {
                self.stroke_velocity[slot][k] = (self.stroke_velocity[slot][k]
                    + (face_target[slot][k] - self.strokes[slot][k]) * 0.16)
                    * 0.68;
                self.strokes[slot][k] += self.stroke_velocity[slot][k];
            }
        }
        self.tail_length_velocity =
            (self.tail_length_velocity + (tail_length_target - self.tail_length) * 0.2) * 0.65;
        self.tail_length += self.tail_length_velocity;
        for k in 0..5 {
            self.pose[k] += (pose_target[k] - self.pose[k]) * 0.22;
        }

        let mut blink = 0.0;
        if BLINKING.contains(&mood) {
            if time > self.next_blink {
                self.blink_at = time;
                self.next_blink = time + 2200.0 + self.random.next() * 2600.0;
            }
            let since = time - self.blink_at;
            if (0.0..200.0).contains(&since) && elapsed > 300.0 {
                blink = 1.0 - (since - 100.0).abs() / 100.0;
            }
        }

        let (mut hop_x, mut hop_y, mut squash) = (0.0, 0.0, self.pose[3]);
        match mood {
            Mood::Idle | Mood::Listen => squash *= 1.0 + 0.012 * (time / 600.0).sin(),
            Mood::Sleepy => squash *= 1.0 + 0.03 * (time / 900.0).sin(),
            Mood::Happy => {
                hop_y = -(elapsed / 150.0).sin().abs() * 45.0 * fade(950.0);
                squash *= 1.0 + 0.05 * (elapsed / 75.0).cos() * fade(950.0);
            }
            Mood::Surprised => hop_y = -(elapsed / 130.0).sin().abs() * 70.0 * fade(400.0),
            Mood::Oops => hop_x = (elapsed / 35.0).sin() * 16.0 * fade(600.0),
            Mood::Admire if elapsed > 250.0 && elapsed < 650.0 => {
                hop_y = ((elapsed - 250.0) / 400.0 * PI).sin() * 18.0
            }
            _ => {}
        }
        // The tail lags behind hops and shakes, then settles on its own spring.
        let swing = tail_angle_target + (hop_y - self.previous_hop) * 1.2 - hop_x * 0.4;
        self.previous_hop = hop_y;
        self.tail_angle_velocity =
            (self.tail_angle_velocity + (swing - self.tail_angle) * 0.14) * 0.8;
        self.tail_angle += self.tail_angle_velocity;

        self.render(time, blink, hop_x, hop_y);
        self.frame.squash = squash;
    }

    fn render(&mut self, time: f64, blink: f64, hop_x: f64, hop_y: f64) {
        let mood = self.mood;
        self.frame.pills = self.body;
        self.frame.tail = tail(self.tail_length.max(0.0), self.tail_angle);
        self.frame.tail_opacity = (self.tail_length * 3.0).clamp(0.0, 1.0);
        self.frame.hop = (hop_x, hop_y);
        self.frame.tilt = self.pose[4];
        self.frame.squash = self.pose[3];
        for slot in 0..STROKES {
            let mut stroke = self.strokes[slot];
            if (slot == 1 || slot == 3) && blink > 0.0 {
                let (x, y) = (stroke[2], stroke[3]);
                let closed = [x - 50.0, y, x, y + 10.0, x + 50.0, y, 24.0];
                for k in 0..7 {
                    stroke[k] += (closed[k] - stroke[k]) * blink;
                }
            }
            if DOTS.contains(&slot) {
                let order = (slot - DOTS.start()) as f64;
                if mood == Mood::Think {
                    stroke[7] *= 0.25 + 0.75 * ((time / 250.0 - order * 1.1).sin() + 1.0) / 2.0;
                }
                if mood == Mood::Sleepy {
                    let rise = (time / 2600.0 + order / 3.0) % 1.0;
                    shift(&mut stroke, rise * 40.0, -rise * 300.0);
                    stroke[6] *= 0.6 + rise * 0.6;
                    stroke[7] *= (rise * PI).sin();
                }
            }
            stroke[6] = stroke[6].max(1.0);
            stroke[7] = stroke[7].clamp(0.0, 1.0);
            self.frame.strokes[slot] = (stroke, stroke_color(slot));
        }
    }
}

/// The overlay's mood: happy while a finished dictation's words settle,
/// thinking while it is polished, otherwise listening (meetings included).
pub fn overlay_mood(state: u8, finished: bool) -> Mood {
    if finished {
        Mood::Happy
    } else if state == 2 {
        Mood::Think
    } else {
        Mood::Listen
    }
}

/// How long Speek is startled when the pill appears.
const SURPRISE_MS: f64 = 350.0;
/// A pause this long between frames means the pill was hidden and reappears.
const REAPPEAR_MS: f64 = 500.0;

struct Overlay {
    rig: Rig,
    shown_at: f64,
}
static OVERLAY: Mutex<Option<Overlay>> = Mutex::new(None);

/// The pill's Speek at `seconds` on the overlay clock. Each platform draws one
/// pill, so one rig serves them all; calling twice at the same time is free.
/// `volume` is the overlay's smoothed microphone level.
pub fn overlay_frame(seconds: f64, mood: Mood, volume: f64) -> Frame {
    let time = seconds * 1000.0;
    let mut slot = OVERLAY.lock().unwrap_or_else(|e| e.into_inner());
    let reappeared = slot
        .as_ref()
        .is_none_or(|o| time - o.rig.time > REAPPEAR_MS || time < o.rig.time);
    if reappeared {
        *slot = Some(Overlay {
            rig: Rig::new(Mood::Idle, time, time.to_bits()),
            shown_at: time,
        });
    }
    let overlay = slot.as_mut().expect("rig was just created");
    let startled = mood == Mood::Listen && time - overlay.shown_at < SURPRISE_MS;
    overlay
        .rig
        .set_mood(if startled { Mood::Surprised } else { mood });
    overlay.rig.advance(time, voice(volume)).clone()
}

/// The microphone level (0–1) from the overlay's smoothed volume. A square
/// root keeps soft speech visible; the noise floor leaves Speek at rest.
pub fn voice(volume: f64) -> f64 {
    ((volume - 2.0) / 118.0).clamp(0.0, 1.0).sqrt()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resting_shapes_match_the_web_rig() {
        // Values from speekSvg({ mode: 'idle' }) in shared/speek/speek.js.
        let frame = Rig::new(Mood::Idle, 0.0, 7).frame;
        let left = frame.pills[0];
        assert!((left[0] - 386.162).abs() < 0.01 && (left[1] - 441.823).abs() < 0.01);
        assert_eq!(left[4], -10.0);
        assert_eq!(frame.pills[2], HEAD);
        assert_eq!(
            frame.tail,
            [[430.0, 930.0], [565.0, 965.0], [365.0, 1095.0]]
        );
        assert_eq!(
            frame.strokes[1].0,
            [497.0, 638.0, 497.0, 690.0, 497.0, 742.0, 96.0, 1.0]
        );
        assert_eq!(
            frame.strokes[2].0,
            [587.0, 830.0, 627.0, 862.0, 667.0, 830.0, 40.0, 1.0]
        );
        assert_eq!(frame.strokes[0].1, CHEEK);
        assert_eq!(frame.strokes[9].0[7], 0.0);
    }

    #[test]
    fn listening_ears_and_eyes_follow_the_voice_and_rest_in_silence() {
        let listen = |volume: f64| {
            let mut rig = Rig::new(Mood::Listen, 0.0, 7);
            rig.advance(500.0, voice(volume)).clone()
        };
        let (quiet, loud) = (listen(0.0), listen(120.0));
        // Loud speech stretches both ears and widens the eyes.
        for ear in [0, 4] {
            assert!(loud.pills[ear][3] > quiet.pills[ear][3] + 40.0);
        }
        let eye = |frame: &Frame| frame.strokes[1].0[5] - frame.strokes[1].0[1];
        assert!(eye(&loud) > eye(&quiet) + 30.0);
        // The ears flop on their own rhythms, not as a mirror pair.
        assert_ne!(loud.pills[0][3], loud.pills[4][3]);
        // The noise floor is silence, and the mouth stays a calm line.
        assert_eq!(listen(2.0), quiet);
        assert_eq!(loud.strokes[2].0[6], 36.0);
        assert!((loud.strokes[7].0[7]).abs() < 1e-9);
    }

    #[test]
    fn the_rig_steps_at_sixty_hertz_whatever_the_render_rate() {
        let mut fast = Rig::new(Mood::Happy, 0.0, 7);
        let mut slow = Rig::new(Mood::Happy, 0.0, 7);
        for frame in 1..=60 {
            fast.advance(frame as f64 * STEP_MS, 0.0);
        }
        for frame in 1..=30 {
            slow.advance(frame as f64 * STEP_MS * 2.0, 0.0);
        }
        assert_eq!(fast.frame, slow.frame);
        // Drawing twice at the same moment changes nothing.
        let before = slow.frame.clone();
        slow.advance(1000.0, 0.0);
        assert_eq!(slow.frame, before);
    }

    #[test]
    fn each_overlay_state_has_its_mood() {
        assert_eq!(overlay_mood(1, false), Mood::Listen);
        assert_eq!(overlay_mood(3, false), Mood::Listen);
        assert_eq!(overlay_mood(2, false), Mood::Think);
        assert_eq!(overlay_mood(1, true), Mood::Happy);
    }

    #[test]
    fn unposing_undoes_hop_tilt_and_squash() {
        let mut frame = Rig::new(Mood::Idle, 0.0, 7).frame;
        frame.hop = (5.0, -20.0);
        frame.tilt = 6.0;
        frame.squash = 1.05;
        // Forward: the SVG group transform, applied to (700, 700).
        let (x, y) = (700.0 - 627.0, 700.0 - 990.0);
        let (x, y) = (x * (2.0 - 1.05), y * 1.05);
        let (s, c) = (6.0f64 * PI / 180.0).sin_cos();
        let (x, y) = (x * c - y * s + 627.0 + 5.0, x * s + y * c + 990.0 - 20.0);
        let (ux, uy) = frame.unpose(x, y);
        assert!((ux - 700.0).abs() < 1e-9 && (uy - 700.0).abs() < 1e-9);
    }
}
