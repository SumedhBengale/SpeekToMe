//! Shared native overlay design. Platform adapters supply windows, paths and
//! text shaping; no renderer creates a browser or owns capture/routing state.
pub const WIDTH: f64 = 420.0;
pub const HEIGHT: f64 = 140.0;
pub const PILL_WIDTH: f64 = 62.0;
pub const PILL_HEIGHT: f64 = 24.0;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Rect {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

// Coordinates start at the top left on every platform, in logical pixels.
#[repr(C)]
#[derive(Clone, Copy, Debug)]
pub struct Placement {
    pub height: f64,
    pub x: f64,
    pub y: f64,
    pub pill_x: f64,
    pub pill_y: f64,
}

pub fn position_code(name: &str) -> i32 {
    match name {
        "Top Center" => 1,
        "Top Left" => 5,
        "Top Right" => 6,
        "Bottom Left" => 7,
        "Bottom Right" => 8,
        _ => 0,
    }
}

pub fn placement(width: f64, height: f64, position: i32, x: f64, y: f64) -> Placement {
    placement_with_height(width, height, position, x, y, HEIGHT)
}
pub fn placement_with_height(
    width: f64,
    height: f64,
    position: i32,
    x: f64,
    y: f64,
    surface_height: f64,
) -> Placement {
    let custom = position == 4;
    let left = position == 5 || position == 7;
    let right = position == 6 || position == 8;
    let bottom = position == 0 || position == 7 || position == 8 || (custom && y > height / 2.0);
    Placement {
        height: surface_height,
        x: (if custom {
            x
        } else if left {
            50.0
        } else if right {
            width - WIDTH - 50.0
        } else {
            (width - WIDTH) / 2.0
        })
        .clamp(0.0, (width - WIDTH).max(0.0)),
        y: (if custom {
            if bottom {
                y - (surface_height - PILL_HEIGHT - 4.0)
            } else {
                y - 4.0
            }
        } else if bottom {
            height - surface_height - 80.0
        } else {
            50.0
        })
        .clamp(0.0, (height - surface_height).max(0.0)),
        pill_x: if custom || left {
            4.0
        } else if right {
            WIDTH - PILL_WIDTH - 4.0
        } else {
            (WIDTH - PILL_WIDTH) / 2.0
        },
        pill_y: if bottom {
            surface_height - PILL_HEIGHT - 4.0
        } else {
            4.0
        },
    }
}

// The supplied logo uses #FA5388; settings colors elsewhere use COLORREF order.
pub const LOGO_COLOR: u32 = 0x8853FA;

pub fn smooth_volume(previous: f64, raw: f64) -> f64 {
    // Capture publishes RMS * 1000. Bound loud transients before smoothing so
    // clipping cannot leave the logo stuck at full height. Renderers tick at 30 Hz.
    let target = raw.clamp(0.0, 120.0);
    let previous = previous.clamp(0.0, 120.0);
    let response = if target > previous { 0.6 } else { 0.16 };
    previous + (target - previous) * response
}

pub fn bars(x: f64, y: f64, seconds: f64, volume: f64, processing: bool) -> [Rect; 5] {
    // Preserve the SVG's coordinates and rounded capsules, fitting its tallest
    // bar into 18 logical pixels. In silence the logo rests in its SVG shape;
    // speech lifts every bar on its own rhythm, like a live waveform, and any
    // of them can reach the full height. All recording movement comes from
    // real audio.
    let scale = 18.0 / 316.0;
    let width = 40.0 * scale;
    // Suppress the noise floor; a square-root curve keeps soft speech visible.
    let level = if processing {
        0.0
    } else {
        ((volume - 2.0) / 118.0).clamp(0.0, 1.0).sqrt()
    };
    let positions = [108.0, 173.0, 236.0, 301.0, 364.0];
    let heights = [120.0, 208.0, 316.0, 208.0, 120.0];
    std::array::from_fn(|i| {
        let activity = if processing {
            // Processing has no microphone input; a gentle pulse travelling
            // across the bars marks that state.
            0.55 + 0.15 * (seconds * 3.6 - i as f64 * 0.9).sin()
        } else {
            0.2
        };
        let resting = width + (heights[i] * scale - width) * activity;
        let speaking = width + (18.0 - width) * level * waveform(i, seconds);
        let height = resting.max(speaking);
        Rect {
            x: x + PILL_WIDTH / 2.0 + (positions[i] - 256.0) * scale,
            y: y + PILL_HEIGHT / 2.0 - height / 2.0,
            width,
            height,
        }
    })
}

/// Bar `i`'s share (0.2–1) of the current level. Two sines at unrelated
/// rates per bar keep the pattern from repeating or mirroring.
fn waveform(i: usize, seconds: f64) -> f64 {
    const RATES: [(f64, f64, f64); 5] = [
        (7.3, 11.9, 0.4),
        (9.1, 5.7, 2.1),
        (6.2, 13.3, 4.0),
        (10.7, 8.3, 1.3),
        (8.4, 6.9, 5.2),
    ];
    let (a, b, phase) = RATES[i];
    let mix = 0.6 * (seconds * a + phase).sin() + 0.4 * (seconds * b + phase * 1.7).sin();
    0.6 + 0.4 * mix
}

pub fn preview_card(x: f64, y: f64, text_width: f64, text_height: f64) -> Rect {
    preview_card_in_surface(x, y, text_width, text_height, HEIGHT)
}
pub fn preview_card_in_surface(
    x: f64,
    y: f64,
    text_width: f64,
    text_height: f64,
    surface_height: f64,
) -> Rect {
    let width = (text_width + 24.0).clamp(160.0, 394.0);
    let height = (text_height + 16.0).min((surface_height - PILL_HEIGHT - 16.0).max(16.0));
    Rect {
        x: (x + 31.0 - width / 2.0).clamp(4.0, WIDTH - width - 4.0),
        y: if y > surface_height / 2.0 {
            (y - height - 8.0).max(4.0)
        } else {
            (y + 32.0).min(surface_height - height - 4.0)
        },
        width,
        height,
    }
}

// Settings persist Windows COLORREF/BGR values, including on Linux and macOS.
pub fn color(value: u32) -> (f64, f64, f64) {
    (
        (value & 255) as f64 / 255.0,
        ((value >> 8) & 255) as f64 / 255.0,
        ((value >> 16) & 255) as f64 / 255.0,
    )
}

pub fn text_color(background: (f64, f64, f64)) -> (f64, f64, f64) {
    if background.0 * 0.299 + background.1 * 0.587 + background.2 * 0.114 > 0.5 {
        (24.0 / 255.0, 16.0 / 255.0, 34.0 / 255.0)
    } else {
        (245.0 / 255.0, 245.0 / 255.0, 250.0 / 255.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn card_stays_beside_pill_and_inside_surface_at_every_anchor() {
        for position in [0, 1, 4, 5, 6, 7, 8] {
            let p = placement(1920.0, 1080.0, position, 0.0, 0.0);
            for (w, h) in [(12.0, 16.0), (370.0, 88.0), (1000.0, 500.0)] {
                let card = preview_card(p.pill_x, p.pill_y, w, h);
                assert!(card.x >= 0.0 && card.x + card.width <= WIDTH);
                assert!(card.y >= 0.0 && card.y + card.height <= HEIGHT);
                assert!(
                    card.y >= p.pill_y + PILL_HEIGHT + 8.0
                        || card.y + card.height + 8.0 <= p.pill_y
                );
            }
        }
    }

    #[test]
    fn logo_responds_to_audio_and_processing_animates_without_audio() {
        let quiet = bars(179.0, 0.0, 0.3, 0.0, false);
        let speech = bars(179.0, 0.0, 0.3, 30.0, false);
        assert!(speech.iter().zip(quiet).any(|(a, b)| a.height > b.height));
        assert_ne!(
            bars(0.0, 0.0, 0.0, 0.0, true),
            bars(0.0, 0.0, 0.3, 0.0, true)
        );
        for rect in speech {
            assert!(rect.x >= 179.0 && rect.x + rect.width <= 179.0 + PILL_WIDTH);
            assert!(rect.y >= 0.0 && rect.y + rect.height <= PILL_HEIGHT);
        }
    }

    #[test]
    fn recording_logo_rests_in_its_svg_shape_and_moves_like_a_waveform() {
        // Silence and the noise floor leave the logo still, in its SVG shape.
        let rest = bars(0.0, 0.0, 0.0, 0.0, false);
        assert_eq!(rest, bars(0.0, 0.0, 16.0, 0.0, false));
        assert_eq!(rest, bars(0.0, 0.0, 0.0, 2.0, false));
        assert_eq!(rest[0].height, rest[4].height);
        assert_eq!(rest[1].height, rest[3].height);
        assert!(rest[0].height < rest[1].height && rest[1].height < rest[2].height);
        for (bar, original) in rest.iter().zip([120.0, 208.0, 316.0, 208.0, 120.0]) {
            let svg = bar.width + (original * 18.0 / 316.0 - bar.width) * 0.2;
            assert!((bar.height - svg).abs() < 1e-10);
        }

        // Speech moves every bar on its own rhythm: not mirrored, and the
        // tallest bar is not always the middle one.
        let mut tallest = std::collections::BTreeSet::new();
        let mut mirrored = true;
        for step in 0..200 {
            let shape = bars(0.0, 0.0, step as f64 * 0.05, 60.0, false);
            mirrored &= shape[0].height == shape[4].height && shape[1].height == shape[3].height;
            let top = (0..5)
                .max_by(|&a, &b| shape[a].height.total_cmp(&shape[b].height))
                .unwrap();
            tallest.insert(top);
            for (bar, resting) in shape.iter().zip(rest) {
                assert!(bar.height >= resting.height);
                assert!((bar.y + bar.height / 2.0 - 12.0).abs() < 1e-10);
            }
        }
        assert!(!mirrored);
        assert!(tallest.len() >= 4, "tallest bars: {tallest:?}");
        assert_ne!(
            bars(0.0, 0.0, 0.0, 60.0, false),
            bars(0.0, 0.0, 0.3, 60.0, false)
        );

        // Louder speech reaches higher, never past the pill.
        for volume in [30.0, 60.0, 120.0, 1000.0] {
            for step in 0..100 {
                for bar in bars(0.0, 0.0, step as f64 * 0.07, volume, false) {
                    assert!(bar.height <= 18.0 + 1e-10);
                }
            }
        }
        let peak = |volume: f64| {
            (0..100)
                .map(|step| bars(0.0, 0.0, step as f64 * 0.07, volume, false)[0].height)
                .fold(0.0, f64::max)
        };
        assert!(peak(120.0) > peak(30.0));
        assert_eq!(
            color(LOGO_COLOR),
            (250.0 / 255.0, 83.0 / 255.0, 136.0 / 255.0)
        );
    }

    #[test]
    fn envelope_attacks_quickly_and_releases_without_a_clipping_tail() {
        assert_eq!(smooth_volume(0.0, 1000.0), smooth_volume(0.0, 120.0));
        assert!(smooth_volume(0.0, 100.0) > 100.0 - smooth_volume(100.0, 0.0));
        let mut level = 120.0;
        for _ in 0..30 {
            level = smooth_volume(level, 0.0);
        }
        assert!(
            level < 2.0,
            "one second of silence should reach the resting shape"
        );
    }

    #[test]
    fn settings_colors_follow_windows_colorref_order() {
        assert_eq!(color(0x0000ff), (1.0, 0.0, 0.0));
        assert_eq!(color(0xff0000), (0.0, 0.0, 1.0));
        assert!(text_color(color(0xffffff)).0 < 0.1);
        assert!(text_color(color(0)).0 > 0.9);
    }
}

/// Content determines height; only the physical display bounds limit the surface.
pub fn surface_height(text_height: f64, screen_height: f64) -> f64 {
    (text_height + 16.0 + PILL_HEIGHT + 8.0 + 8.0)
        .max(HEIGHT)
        .min((screen_height - 100.0).max(64.0))
}
pub fn processing_color(u: f64) -> (f64, f64, f64) {
    let palette = [
        (155.0, 60.0, 245.0),
        (236.0, 72.0, 153.0),
        (245.0, 158.0, 11.0),
        (6.0, 182.0, 212.0),
    ];
    let phase = u.rem_euclid(1.0) * 4.0;
    let index = phase.floor() as usize;
    let t = phase - index as f64;
    let t = t * t * (3.0 - 2.0 * t);
    let a = palette[index % 4];
    let b = palette[(index + 1) % 4];
    (
        (a.0 + (b.0 - a.0) * t) / 255.0,
        (a.1 + (b.1 - a.1) * t) / 255.0,
        (a.2 + (b.2 - a.2) * t) / 255.0,
    )
}
pub fn preview_text(raw: &str, processing: bool) -> &str {
    if processing && raw.trim().is_empty() {
        "Polishing..."
    } else {
        raw
    }
}

#[cfg(test)]
mod processing_design_tests {
    use super::*;
    #[test]
    fn long_preview_grows_past_the_old_four_line_limit() {
        let height = surface_height(500.0, 1080.0);
        assert!(height > 500.0);
        for position in [0, 1, 4, 5, 6, 7, 8] {
            let p = placement_with_height(1920.0, 1080.0, position, 0.0, 0.0, height);
            let card = preview_card_in_surface(p.pill_x, p.pill_y, 370.0, 500.0, height);
            assert_eq!(card.height, 516.0);
            assert!(card.y >= 4.0 && card.y + card.height <= height - 4.0);
        }
    }
    #[test]
    fn processing_palette_matches_the_original_windows_colors() {
        assert_eq!(
            processing_color(0.0),
            (155.0 / 255.0, 60.0 / 255.0, 245.0 / 255.0)
        );
        assert_eq!(
            processing_color(0.25),
            (236.0 / 255.0, 72.0 / 255.0, 153.0 / 255.0)
        );
        assert_eq!(processing_color(0.0), processing_color(1.0));
        assert_ne!(processing_color(0.0), processing_color(0.02));
    }
}

/// Premultiplied Win32 BGRA pixel, using the original processing border profile.
pub fn processing_pixel(
    x: f64,
    y: f64,
    rect: Rect,
    radius: f64,
    turns: f64,
    background: (f64, f64, f64),
) -> u32 {
    let cx = rect.x + rect.width / 2.0;
    let cy = rect.y + rect.height / 2.0;
    let qx = (x - cx).abs() - (rect.width / 2.0 - radius);
    let qy = (y - cy).abs() - (rect.height / 2.0 - radius);
    let distance = qx.max(0.0).hypot(qy.max(0.0)) + qx.max(qy).min(0.0) - radius;
    if distance > 3.5 {
        return 0;
    }
    let phase = (y - cy).atan2(x - cx) / (std::f64::consts::PI * 2.0) + turns;
    let color = processing_color(phase);
    let (color, alpha) = if distance > 0.0 {
        (color, (150.0 * (1.0 - distance / 3.5).powi(2)) as u32)
    } else {
        let mix = if distance >= -1.2 {
            0.85
        } else {
            (0.85 * (distance + 2.8) / 1.6).clamp(0.0, 0.85)
        };
        (
            (
                background.0 * (1.0 - mix) + color.0 * mix,
                background.1 * (1.0 - mix) + color.1 * mix,
                background.2 * (1.0 - mix) + color.2 * mix,
            ),
            242,
        )
    };
    (alpha << 24)
        | (((color.0 * alpha as f64) as u32) << 16)
        | (((color.1 * alpha as f64) as u32) << 8)
        | ((color.2 * alpha as f64) as u32)
}

/// Custom anchors keep their pill position while the card grows toward the
/// larger free side of the monitor, rather than pushing the pill up the screen.
pub fn surface_height_at(
    text_height: f64,
    screen_height: f64,
    position: i32,
    custom_y: f64,
) -> f64 {
    if position != 4 {
        return surface_height(text_height, screen_height);
    }
    let available = if custom_y > screen_height / 2.0 {
        custom_y + PILL_HEIGHT + 4.0
    } else {
        screen_height - custom_y + 4.0
    };
    (text_height + 16.0 + PILL_HEIGHT + 16.0)
        .max(HEIGHT)
        .min(available.max(64.0))
}
#[cfg(test)]
mod custom_anchor_tests {
    use super::*;
    #[test]
    fn tall_previews_do_not_move_a_custom_pill() {
        for y in [100.0, 540.0, 950.0] {
            let h = surface_height_at(800.0, 1080.0, 4, y);
            let p = placement_with_height(1920.0, 1080.0, 4, 100.0, y, h);
            assert!((p.y + p.pill_y - y).abs() < 0.01);
            let card = preview_card_in_surface(p.pill_x, p.pill_y, 370.0, 800.0, h);
            assert!(card.y >= 4.0 && card.y + card.height <= h - 4.0);
        }
    }
}

// AI "polish" from the website hero (`voice-text-glow`/`voice-text-settle`
// and `vocabulary-polish`): a 100deg gradient through the processing palette,
// 300% wide and clipped to the glyphs, slides left to right under a soft pink
// drop-shadow; then the text settles from 55% opacity under a fading glow.
pub const POLISH_GLOW: (f64, f64, f64) = (236.0 / 255.0, 72.0 / 255.0, 153.0 / 255.0);
pub const POLISH_SECONDS: f64 = 0.75;
pub const SETTLE_SECONDS: f64 = 0.65;
// The site's pill ring (`voice-pill-glow`) turns once every 1.4 s.
pub const PROCESSING_TURN_SECONDS: f64 = 1.4;
// One shimmer across a word still being decided.
pub const DECIDING_SECONDS: f64 = 1.4;

/// CSS `ease-in-out`.
fn ease_in_out(t: f64) -> f64 {
    let t = t.clamp(0.0, 1.0);
    t * t * (3.0 - 2.0 * t)
}

/// How a polishing word looks `t` seconds after it appeared, or `None` once
/// it is plain text again.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Polish {
    /// CSS background-position x (1 → 0) of the 300%-wide gradient.
    pub position: f64,
    /// Opacity of the pink glow behind the glyphs.
    pub glow: f64,
    /// Glyph opacity.
    pub opacity: f64,
    /// How far the gradient has faded into the plain text colour.
    pub plain: f64,
}

pub fn polish(t: f64) -> Option<Polish> {
    if t < 0.0 || t >= POLISH_SECONDS + SETTLE_SECONDS {
        return None;
    }
    if t < POLISH_SECONDS {
        let e = ease_in_out(t / POLISH_SECONDS);
        return Some(Polish {
            position: 1.0 - e,
            glow: 0.2,
            opacity: 1.0,
            plain: 0.0,
        });
    }
    // Settle: background 35% → 0, drop-shadow 33% → none, opacity .55 → 1.
    let e = ease_in_out((t - POLISH_SECONDS) / SETTLE_SECONDS);
    Some(Polish {
        position: 0.35 * (1.0 - e),
        glow: 0.33 * (1.0 - e),
        opacity: 0.55 + 0.45 * e,
        plain: e,
    })
}

/// The polish gradient's stops; `text` replaces the site's #29292f ends.
pub fn polish_stops(text: (f64, f64, f64)) -> [(f64, (f64, f64, f64)); 6] {
    let rgb = |r: f64, g: f64, b: f64| (r / 255.0, g / 255.0, b / 255.0);
    [
        (0.15, text),
        (0.32, rgb(155.0, 60.0, 245.0)),
        (0.42, rgb(236.0, 72.0, 153.0)),
        (0.49, rgb(245.0, 158.0, 11.0)),
        (0.56, rgb(6.0, 182.0, 212.0)),
        (0.74, text),
    ]
}

/// Where `x` (0–1 across the word) and `y` (in word widths) fall on the
/// gradient. The visible box is one third of the gradient, offset by the
/// position; 100deg tilts the bands by tan(10deg).
pub fn polish_offset(x: f64, y: f64, position: f64) -> f64 {
    (x + 0.176 * y + 2.0 * position) / 3.0
}

/// Gradient colour at `x` (0–1 across the word) and the vertical offset `y`
/// in word widths. `text` replaces the site's #29292f end stops so words
/// settle into the preview's own text colour.
pub fn polish_color(x: f64, y: f64, position: f64, text: (f64, f64, f64)) -> (f64, f64, f64) {
    let stops = polish_stops(text);
    let g = polish_offset(x, y, position);
    if g <= stops[0].0 {
        return text;
    }
    for pair in stops.windows(2) {
        let ((a, ca), (b, cb)) = (pair[0], pair[1]);
        if g <= b {
            let k = (g - a) / (b - a);
            return (
                ca.0 + (cb.0 - ca.0) * k,
                ca.1 + (cb.1 - ca.1) * k,
                ca.2 + (cb.2 - ca.2) * k,
            );
        }
    }
    text
}

/// How a word the vocabulary engine is still deciding looks `t` seconds
/// after it was proposed: the polish gradient keeps sweeping across it under
/// a pulsing pink glow until the decision lands.
pub fn deciding(t: f64) -> Polish {
    let phase = (t / DECIDING_SECONDS).rem_euclid(1.0);
    Polish {
        // 1.25 and -0.4 both leave the word in its own colour, so the loop
        // has no visible seam.
        position: 1.25 - 1.65 * phase,
        glow: 0.12 + 0.08 * (1.0 - (phase * std::f64::consts::TAU).cos()) / 2.0,
        opacity: 1.0,
        plain: 0.0,
    }
}

type Words = Vec<(std::ops::Range<usize>, String, f64)>;

/// Words the preview is changing. A word the vocabulary engine is deciding
/// shimmers; a word a decision or the final text put in polishes once.
/// Later texts that still carry the same word keep its timing.
#[derive(Debug, Default)]
pub struct TextGlow {
    text: String,
    polishing: Words,
    deciding: Words,
}

impl TextGlow {
    /// `replacements` and `deciding` are the server's byte ranges of decided
    /// words and of words still waiting for a decision in `text`.
    /// Returns whether the preview needs to animate.
    pub fn update(
        &mut self,
        text: &str,
        replacements: &[std::ops::Range<usize>],
        deciding: &[std::ops::Range<usize>],
        seconds: f64,
    ) -> bool {
        self.text = text.to_string();
        self.polishing = carry(
            text,
            replacements,
            std::mem::take(&mut self.polishing),
            seconds,
        );
        self.deciding = carry(text, deciding, std::mem::take(&mut self.deciding), seconds);
        let fresh = self.polishing.iter().any(|(_, _, at)| *at == seconds);
        fresh || !self.deciding.is_empty()
    }

    /// Shows the final text: every word that differs from the last preview
    /// polishes, and words already polishing keep going.
    /// Returns whether the preview needs to animate.
    pub fn finish(&mut self, text: &str, seconds: f64) -> bool {
        if text == self.text {
            return !self.sample(seconds).is_empty();
        }
        let old = words(&self.text);
        let new = words(text);
        let matched = common_words(&self.text, &old, text, &new);
        let mut polishing = Words::new();
        if !old.is_empty() {
            let mut run: Option<std::ops::Range<usize>> = None;
            for (j, word) in new.iter().enumerate() {
                if matched[j].is_none() {
                    let start = run.take().map_or(word.start, |r| r.start);
                    run = Some(start..word.end);
                } else if let Some(r) = run.take() {
                    polishing.push((r.clone(), text[r].to_string(), seconds));
                }
            }
            if let Some(r) = run {
                polishing.push((r.clone(), text[r].to_string(), seconds));
            }
        }
        // Old words map to new positions through the matched words.
        for (range, word, at) in std::mem::take(&mut self.polishing) {
            if polish(seconds - at).is_none() {
                continue;
            }
            let moved: Vec<_> = (0..new.len())
                .filter(|&j| matched[j].is_some_and(|i| range.contains(&old[i].start)))
                .collect();
            if let (Some(&first), Some(&last)) = (moved.first(), moved.last()) {
                if text[new[first].start..new[last].end] == word {
                    polishing.push((new[first].start..new[last].end, word, at));
                }
            }
        }
        polishing.sort_by_key(|(range, _, _)| range.start);
        self.text = text.to_string();
        self.polishing = polishing;
        self.deciding.clear();
        !self.sample(seconds).is_empty()
    }

    pub fn reset(&mut self) {
        *self = Self::default();
    }

    /// Seconds until every polishing word is plain text again.
    pub fn remaining(&self, seconds: f64) -> f64 {
        self.polishing
            .iter()
            .map(|(_, _, at)| POLISH_SECONDS + SETTLE_SECONDS - (seconds - at))
            .fold(0.0, f64::max)
    }

    /// Each range still changing, with its look.
    pub fn sample(&self, seconds: f64) -> Vec<(std::ops::Range<usize>, Polish)> {
        let mut looks: Vec<_> = self
            .polishing
            .iter()
            .filter_map(|(range, _, at)| Some((range.clone(), polish(seconds - at)?)))
            .chain(
                self.deciding
                    .iter()
                    .map(|(range, _, at)| (range.clone(), deciding(seconds - at))),
            )
            .collect();
        looks.sort_by_key(|(range, _)| range.start);
        looks
    }
}

/// The valid `ranges` of `text`, each keeping the start time of the same word
/// in `previous`. Earlier texts list ranges in text order, so the first unused
/// match is the same word, possibly shifted by edits before it.
fn carry(
    text: &str,
    ranges: &[std::ops::Range<usize>],
    mut previous: Words,
    seconds: f64,
) -> Words {
    let mut kept = Words::new();
    for range in ranges {
        if range.start >= range.end
            || range.end > text.len()
            || !text.is_char_boundary(range.start)
            || !text.is_char_boundary(range.end)
        {
            continue;
        }
        let word = &text[range.clone()];
        let started = previous
            .iter()
            .position(|(_, w, _)| w == word)
            .map_or(seconds, |i| previous.remove(i).2);
        kept.push((range.clone(), word.to_string(), started));
    }
    kept
}

fn words(text: &str) -> Vec<std::ops::Range<usize>> {
    let mut out = Vec::new();
    let mut start = None;
    for (i, c) in text.char_indices() {
        match (c.is_whitespace(), start) {
            (true, Some(s)) => {
                out.push(s..i);
                start = None;
            }
            (false, None) => start = Some(i),
            _ => {}
        }
    }
    if let Some(s) = start {
        out.push(s..text.len());
    }
    out
}

/// For each word of `new`, the index of the same word in `old` along their
/// longest common subsequence.
fn common_words(
    old_text: &str,
    old: &[std::ops::Range<usize>],
    new_text: &str,
    new: &[std::ops::Range<usize>],
) -> Vec<Option<usize>> {
    let mut matched = vec![None; new.len()];
    let (n, m) = (old.len(), new.len());
    if n == 0 || m == 0 || n * m > 1_000_000 {
        return matched;
    }
    let same = |i: usize, j: usize| old_text[old[i].clone()] == new_text[new[j].clone()];
    // lengths[i][j] is the LCS of old[i..] and new[j..].
    let mut lengths = vec![0u32; (n + 1) * (m + 1)];
    for i in (0..n).rev() {
        for j in (0..m).rev() {
            lengths[i * (m + 1) + j] = if same(i, j) {
                lengths[(i + 1) * (m + 1) + j + 1] + 1
            } else {
                lengths[(i + 1) * (m + 1) + j].max(lengths[i * (m + 1) + j + 1])
            };
        }
    }
    let (mut i, mut j) = (0, 0);
    while i < n && j < m {
        if same(i, j) {
            matched[j] = Some(i);
            i += 1;
            j += 1;
        } else if lengths[(i + 1) * (m + 1) + j] >= lengths[i * (m + 1) + j + 1] {
            i += 1;
        } else {
            j += 1;
        }
    }
    matched
}

/// Approximate Gaussian blur of a coverage map (three box passes per axis).
pub fn blur(values: &mut [f32], width: usize, height: usize, radius: usize) {
    if radius == 0 || width == 0 || height == 0 {
        return;
    }
    let mut line = vec![0f32; width.max(height)];
    let scale = 1.0 / (radius * 2 + 1) as f32;
    let mut pass =
        |values: &mut [f32], len: usize, count: usize, at: &dyn Fn(usize, usize) -> usize| {
            for n in 0..count {
                for i in 0..len {
                    line[i] = values[at(n, i)];
                }
                let mut sum: f32 = (0..=radius).map(|i| line[i.min(len - 1)]).sum::<f32>()
                    + line[0] * radius as f32;
                for i in 0..len {
                    values[at(n, i)] = sum * scale;
                    sum += line[(i + radius + 1).min(len - 1)] - line[i.saturating_sub(radius)];
                }
            }
        };
    for _ in 0..3 {
        pass(values, width, height, &|row, i| row * width + i);
        pass(values, height, width, &|column, i| i * width + column);
    }
}

#[cfg(test)]
mod polish_tests {
    use super::*;

    #[test]
    fn polish_sweeps_the_palette_across_then_settles_into_text() {
        let text = (0.1, 0.1, 0.1);
        // Before and after the sweep the word shows its own colour.
        assert_eq!(polish_color(0.5, 0.0, 1.0, text), text);
        assert_eq!(polish_color(0.0, 0.0, 0.0, text), text);
        // Mid-sweep the pink stop crosses the word.
        let pink = polish_color(0.5, 0.0, 0.38, text);
        assert!((pink.0 - 236.0 / 255.0).abs() < 0.05 && pink.2 > 0.5);

        let start = polish(0.0).unwrap();
        assert_eq!(
            (start.position, start.opacity, start.plain),
            (1.0, 1.0, 0.0)
        );
        let settling = polish(POLISH_SECONDS).unwrap();
        assert!((settling.position - 0.35).abs() < 1e-9 && settling.opacity == 0.55);
        assert!(settling.glow > start.glow);
        assert!(polish(POLISH_SECONDS + SETTLE_SECONDS).is_none());
    }

    #[test]
    fn replaced_words_polish_once() {
        let mut glow = TextGlow::default();
        glow.update("why don't you speak to me", &[], &[], 0.0);
        assert!(glow.sample(0.0).is_empty());

        assert!(glow.update("why don't you SpeekToMe", &[14..23], &[], 1.0));
        assert!(!glow.update("why don't you SpeekToMe.", &[14..23], &[], 1.5));
        assert_eq!(glow.sample(1.0), vec![(14..23, polish(0.0).unwrap())]);

        // Later interims shift it and keep its timing; a new decision starts fresh.
        glow.update(
            "so why don't you SpeekToMe about PostgreSQL",
            &[17..26, 33..43],
            &[],
            2.0,
        );
        let spans = glow.sample(2.0);
        assert_eq!(spans.len(), 2);
        assert_eq!(
            (spans[0].0.clone(), spans[0].1),
            (17..26, polish(1.0).unwrap())
        );
        assert_eq!(spans[1].0, 33..43);
        assert_eq!(glow.sample(3.0).len(), 1);
        assert!(glow.sample(4.0).is_empty());

        // Out-of-range or split-character spans are ignored.
        glow.update("नमस्ते", &[1..3, 0..40], &[], 6.0);
        assert!(glow.sample(6.0).is_empty());
    }

    #[test]
    fn deciding_words_shimmer_until_the_decision_polishes_them() {
        let text = (0.1, 0.1, 0.1);
        // The shimmer loops through plain text, so it has no seam.
        for t in [0.0, DECIDING_SECONDS - 1e-9] {
            let look = deciding(t);
            for x in [0.0, 0.5, 1.0] {
                let c = polish_color(x, 0.3, look.position, text);
                assert!((c.0 - text.0).abs() < 0.02 && (c.2 - text.2).abs() < 0.02);
            }
        }
        let mid = deciding(DECIDING_SECONDS / 2.0);
        assert_ne!(polish_color(0.5, 0.0, mid.position, text), text);

        let mut glow = TextGlow::default();
        assert!(glow.update("why don't you speak to me", &[], &[14..25], 0.0));
        assert!(glow.update("why don't you speak to me so", &[], &[14..25], 0.5));
        assert_eq!(glow.sample(0.5), vec![(14..25, deciding(0.5))]);

        // The decision lands: the shimmer stops and the replacement polishes.
        assert!(glow.update("why don't you SpeekToMe so", &[14..23], &[], 1.0));
        assert_eq!(glow.sample(1.0), vec![(14..23, polish(0.0).unwrap())]);
        // Rejected proposals just stop shimmering.
        assert!(!glow.update("why don't you SpeekToMe so", &[14..23], &[], 3.0));
        assert!(glow.sample(3.0).is_empty());
    }

    #[test]
    fn final_text_polishes_what_changed() {
        let mut glow = TextGlow::default();
        glow.update(
            "so why don't you SpeekToMe about postgres",
            &[17..26],
            &[33..41],
            0.0,
        );
        // The final text changes two words; the earlier decision keeps its timing.
        assert!(glow.finish("So why don't you SpeekToMe about PostgreSQL?", 0.5));
        assert_eq!(
            glow.sample(0.5),
            vec![
                (0..2, polish(0.0).unwrap()),
                (17..26, polish(0.5).unwrap()),
                (33..44, polish(0.0).unwrap()),
            ]
        );
        assert!((glow.remaining(0.5) - (POLISH_SECONDS + SETTLE_SECONDS)).abs() < 1e-9);
        // Showing the same text again changes nothing.
        assert!(glow.finish("So why don't you SpeekToMe about PostgreSQL?", 0.6));
        assert_eq!(glow.sample(0.6).len(), 3);
        assert!(!glow.finish("So why don't you SpeekToMe about PostgreSQL?", 5.0));
        assert_eq!(glow.remaining(5.0), 0.0);

        // Without an earlier preview there is nothing to compare.
        let mut fresh = TextGlow::default();
        assert!(!fresh.finish("Hello there.", 0.0));
    }

    #[test]
    fn blur_spreads_coverage_without_losing_much() {
        let (w, h) = (21, 21);
        let mut values = vec![0f32; w * h];
        values[10 * w + 10] = 1.0;
        blur(&mut values, w, h, 2);
        assert!(values[10 * w + 10] < 1.0 && values[10 * w + 13] > 0.0);
        assert!((values.iter().sum::<f32>() - 1.0).abs() < 1e-3);
    }
}
