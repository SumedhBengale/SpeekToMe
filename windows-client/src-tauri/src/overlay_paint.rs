//! Software renderer for the dictation pill and preview card, shared by the
//! Windows, Linux and macOS overlays so every effect looks the same. Each
//! platform only lays out and rasterises the preview text with its own text
//! engine, then shows the finished pixels.
//!
//! Pixels are premultiplied ARGB in native-endian `u32`s: what a Win32 DIB, a
//! cairo `ARgb32` surface and a CoreGraphics little-endian premultiplied-first
//! bitmap all store.
use crate::native_overlay::{
    blur, polish_color, processing_pixel, text_color, Polish, Rect, PILL_HEIGHT, PILL_WIDTH,
    POLISH_GLOW, PROCESSING_TURN_SECONDS, SPEEK_HEIGHT,
};
use crate::speek::{Frame, ICON_BOX, TAIL_ROUNDING};

pub const PILL_ALPHA: f64 = 235.0 / 255.0;
pub const CARD_ALPHA: f64 = 240.0 / 255.0;
pub const CARD_RADIUS: f64 = 7.0;
/// How far the processing halo reaches outside a shape.
pub const HALO: f64 = 3.5;

/// One frame of an overlay surface. Geometry is in logical pixels; `scale`
/// device pixels make up one logical pixel in the buffer.
pub struct Scene<'a> {
    pub scale: f64,
    /// Animation clock in seconds; every effect is timed by it, not by frames.
    pub seconds: f64,
    pub background: (f64, f64, f64),
    pub wave: (f64, f64, f64),
    pub processing: bool,
    pub pill: Option<Rect>,
    /// Speek, centred in the 62×24 box whose top left is given.
    pub speek: Option<(f64, f64, Frame)>,
    /// Coverage of a label drawn inside the pill, such as the meeting's.
    pub label: Option<&'a [f32]>,
    pub card: Option<Rect>,
    pub text: Option<Text<'a>>,
}

/// Preview text as coverage masks with the buffer's dimensions.
pub struct Text<'a> {
    pub color: (f64, f64, f64),
    /// Every glyph of the text.
    pub glyphs: &'a [f32],
    /// The glyphs of each changing word, with its look.
    pub words: &'a [(Vec<f32>, Polish)],
}

pub fn paint(scene: &Scene, pixels: &mut [u32], width: usize, height: usize) {
    pixels.fill(0);
    let s = scene.scale;
    let turns = scene.seconds / PROCESSING_TURN_SECONDS;
    // Device pixels within `grow` logical pixels of `rect`.
    let area = |rect: Rect, grow: f64| {
        let x0 = (((rect.x - grow) * s).floor().max(0.0) as usize).min(width);
        let x1 = (((rect.x + rect.width + grow) * s).ceil().max(0.0) as usize).min(width);
        let y0 = (((rect.y - grow) * s).floor().max(0.0) as usize).min(height);
        let y1 = (((rect.y + rect.height + grow) * s).ceil().max(0.0) as usize).min(height);
        (y0..y1).flat_map(move |py| (x0..x1).map(move |px| (px, py)))
    };
    let logical = |px: usize, py: usize| ((px as f64 + 0.5) / s, (py as f64 + 0.5) / s);

    if let Some(pill) = scene.pill {
        let radius = pill.height / 2.0;
        for (px, py) in area(pill, HALO) {
            let (x, y) = logical(px, py);
            let pixel = if scene.processing {
                processing_pixel(x, y, pill, radius, turns, scene.background)
            } else {
                let cover = (0.5 - round_rect_distance(x, y, pill, radius) * s).clamp(0.0, 1.0);
                premultiply(scene.background, PILL_ALPHA * cover)
            };
            let i = py * width + px;
            pixels[i] = over(pixels[i], pixel);
        }
    }
    if let Some((x, y, frame)) = &scene.speek {
        paint_speek(frame, *x, *y, s, pixels, width, height);
    }
    if let (Some(pill), Some(label)) = (scene.pill, scene.label) {
        let text = Text {
            color: text_color(scene.background),
            glyphs: label,
            words: &[],
        };
        paint_text(&text, pill, scene.scale, pixels, width, height);
    }
    if let Some(card) = scene.card {
        let radius = CARD_RADIUS.min(card.height / 2.0 - 1.0);
        for (px, py) in area(card, HALO) {
            let (x, y) = logical(px, py);
            let pixel = if scene.processing {
                processing_pixel(x, y, card, radius, turns, scene.background)
            } else {
                card_pixel(x, y, card, radius, scene, s)
            };
            let i = py * width + px;
            pixels[i] = over(pixels[i], pixel);
        }
        if let Some(text) = &scene.text {
            paint_text(text, card, scene.scale, pixels, width, height);
        }
    }
}

/// The listening card: the background with a thin border tinted by the wave
/// colour.
fn card_pixel(x: f64, y: f64, card: Rect, radius: f64, scene: &Scene, s: f64) -> u32 {
    let distance = round_rect_distance(x, y, card, radius);
    let cover = (0.5 - distance * s).clamp(0.0, 1.0);
    if cover <= 0.0 {
        return 0;
    }
    let blend = ((distance + 1.2) / 1.2).clamp(0.0, 1.0) * 0.40;
    let (bg, wave) = (scene.background, scene.wave);
    premultiply(
        (
            bg.0 + (wave.0 - bg.0) * blend,
            bg.1 + (wave.1 - bg.1) * blend,
            bg.2 + (wave.2 - bg.2) * blend,
        ),
        CARD_ALPHA * cover,
    )
}

/// Draws text inside `card` (or the pill, for a label). A changing word
/// takes the website's polish gradient over a pink drop-shadow.
fn paint_text(
    text: &Text,
    card: Rect,
    scale: f64,
    pixels: &mut [u32],
    width: usize,
    height: usize,
) {
    let count = width * height;
    if text.glyphs.len() != count {
        return;
    }
    let mut polished: Vec<Option<((f64, f64, f64), f64)>> = Vec::new();
    let mut halo: Vec<f32> = Vec::new();
    if !text.words.is_empty() {
        polished = vec![None; count];
        halo = vec![0.0; count];
    }
    for (word, look) in text.words {
        if word.len() != count {
            continue;
        }
        let inked = word
            .iter()
            .enumerate()
            .filter(|(_, w)| **w > 0.05)
            .map(|(i, _)| (i % width, i / width));
        let (Some(left), Some(right), Some(top)) = (
            inked.clone().map(|(x, _)| x).min(),
            inked.clone().map(|(x, _)| x).max(),
            inked.map(|(_, y)| y).min(),
        ) else {
            continue;
        };
        let span = (right - left + 1) as f64;
        for (i, w) in word.iter().enumerate() {
            if *w <= 0.0 {
                continue;
            }
            let x = (i % width) as f64 - left as f64;
            let y = (i / width) as f64 - top as f64;
            let swept = polish_color(x / span, y / span, look.position, text.color);
            let color = (
                swept.0 + (text.color.0 - swept.0) * look.plain,
                swept.1 + (text.color.1 - swept.1) * look.plain,
                swept.2 + (text.color.2 - swept.2) * look.plain,
            );
            polished[i] = Some((color, look.opacity));
            halo[i] += w * look.glow as f32;
        }
    }
    if !halo.is_empty() {
        // ~7px CSS drop-shadow blur.
        blur(
            &mut halo,
            width,
            height,
            (3.0 * scale).round().max(1.0) as usize,
        );
    }
    // Only the solid card interior, never its halo or the pill.
    let x0 = ((card.x * scale).floor().max(0.0) as usize).min(width);
    let x1 = (((card.x + card.width) * scale).ceil().max(0.0) as usize).min(width);
    let y0 = ((card.y * scale).floor().max(0.0) as usize).min(height);
    let y1 = (((card.y + card.height) * scale).ceil().max(0.0) as usize).min(height);
    for py in y0..y1 {
        for px in x0..x1 {
            let i = py * width + px;
            if pixels[i] >> 24 < 180 {
                continue;
            }
            let glow = halo.get(i).map_or(0.0, |h| (h * 2.5).min(1.0)) as f64;
            if glow > 0.004 {
                pixels[i] = over(pixels[i], premultiply(POLISH_GLOW, glow));
            }
            let glyph = text.glyphs[i] as f64;
            if glyph > 0.0 {
                let (color, opacity) = polished
                    .get(i)
                    .copied()
                    .flatten()
                    .unwrap_or((text.color, 1.0));
                pixels[i] = over(
                    pixels[i],
                    premultiply(color, glyph * opacity * 245.0 / 255.0),
                );
            }
        }
    }
}

/// Draws Speek's pose with its resting box filling `SPEEK_HEIGHT` and centred in
/// the 62×24 box at `x`, `y`.
fn paint_speek(
    frame: &Frame,
    x: f64,
    y: f64,
    s: f64,
    pixels: &mut [u32],
    width: usize,
    height: usize,
) {
    let box_x = x + PILL_WIDTH / 2.0 - SPEEK_HEIGHT / 2.0;
    let box_y = y + (PILL_HEIGHT - SPEEK_HEIGHT) / 2.0;
    paint_speek_box(frame, box_x, box_y, SPEEK_HEIGHT, s, pixels, width, height);
}

/// Speek resting and smiling, filling a `size`×`size` icon: premultiplied
/// ARGB like the overlay, for the tray and window icons.
pub fn speek_icon(size: usize) -> Vec<u32> {
    let frame = crate::speek::Rig::new(crate::speek::Mood::Idle, 0.0, 1)
        .advance(0.0, 0.0)
        .clone();
    let mut pixels = vec![0u32; size * size];
    paint_speek_box(&frame, 0.0, 0.0, size as f64, 1.0, &mut pixels, size, size);
    pixels
}

/// `speek_icon` as straight (not premultiplied) RGBA bytes, as tray icons take.
#[cfg(any(target_os = "linux", target_os = "macos"))]
pub fn speek_icon_rgba(size: usize) -> Vec<u8> {
    speek_icon(size)
        .into_iter()
        .flat_map(|pixel| {
            let alpha = pixel >> 24;
            let straight = |shift: u32| {
                let value = (pixel >> shift) & 0xFF;
                if alpha == 0 {
                    0
                } else {
                    ((value * 255 + alpha / 2) / alpha).min(255) as u8
                }
            };
            [straight(16), straight(8), straight(0), alpha as u8]
        })
        .collect()
}

/// Draws Speek's pose with its resting box (`ICON_BOX`) as the `side`-sized
/// square at `x`, `y`. Shapes are drawn in the web rig's order, each
/// anti-aliased from its signed distance like the pill.
#[allow(clippy::too_many_arguments)]
fn paint_speek_box(
    frame: &Frame,
    x: f64,
    y: f64,
    side: f64,
    s: f64,
    pixels: &mut [u32],
    width: usize,
    height: usize,
) {
    let (box_x, box_y, box_size) = ICON_BOX;
    let unit = side / box_size;
    let left = x - box_x * unit;
    let top = y - box_y * unit;
    // Device pixels per mascot unit, to turn distances into coverage.
    let pixel = unit * s;
    let x0 = ((left * s).floor().max(0.0) as usize).min(width);
    let x1 = (((left + 1254.0 * unit) * s).ceil().max(0.0) as usize).min(width);
    let y0 = ((top * s).floor().max(0.0) as usize).min(height);
    let y1 = (((top + 1254.0 * unit) * s).ceil().max(0.0) as usize).min(height);
    let pink = crate::speek::PINK;
    for py in y0..y1 {
        for px in x0..x1 {
            let ux = ((px as f64 + 0.5) / s - left) / unit;
            let uy = ((py as f64 + 0.5) / s - top) / unit;
            let (ux, uy) = frame.unpose(ux, uy);
            let i = py * width + px;
            let mut paint = |distance: f64, color: (f64, f64, f64), opacity: f64| {
                let cover = (0.5 - distance * pixel).clamp(0.0, 1.0) * opacity;
                if cover > 0.0 {
                    pixels[i] = over(pixels[i], premultiply(color, cover));
                }
            };
            for part in [0, 4] {
                paint(pill_distance(ux, uy, frame.pills[part]), pink, 1.0);
            }
            let [a, b, c] = frame.tail;
            paint(
                triangle_distance([ux, uy], a, b, c) - TAIL_ROUNDING,
                pink,
                frame.tail_opacity,
            );
            for part in [1, 3, 2] {
                paint(pill_distance(ux, uy, frame.pills[part]), pink, 1.0);
            }
            for (stroke, color) in &frame.strokes {
                if stroke[7] > 0.0 {
                    paint(
                        stroke_distance(ux, uy, stroke) - stroke[6] / 2.0,
                        *color,
                        stroke[7],
                    );
                }
            }
        }
    }
}

/// Distance from a rotated rounded rectangle: cx, cy, width, height, degrees, radius.
fn pill_distance(x: f64, y: f64, [cx, cy, w, h, rotation, radius]: [f64; 6]) -> f64 {
    let (sin, cos) = (-rotation.to_radians()).sin_cos();
    let (dx, dy) = (x - cx, y - cy);
    let (w, h) = (w.max(0.0), h.max(0.0));
    let rect = Rect {
        x: -w / 2.0,
        y: -h / 2.0,
        width: w,
        height: h,
    };
    round_rect_distance(
        dx * cos - dy * sin,
        dx * sin + dy * cos,
        rect,
        radius.min(w / 2.0).min(h / 2.0),
    )
}

/// Distance from the centre line of a quadratic stroke, sampled as segments.
fn stroke_distance(x: f64, y: f64, s: &[f64; 8]) -> f64 {
    const SEGMENTS: usize = 12;
    let point = |t: f64| {
        let u = 1.0 - t;
        (
            u * u * s[0] + 2.0 * u * t * s[2] + t * t * s[4],
            u * u * s[1] + 2.0 * u * t * s[3] + t * t * s[5],
        )
    };
    let mut nearest = f64::MAX;
    let mut from = point(0.0);
    for step in 1..=SEGMENTS {
        let to = point(step as f64 / SEGMENTS as f64);
        let (ex, ey) = (to.0 - from.0, to.1 - from.1);
        let length = ex * ex + ey * ey;
        let t = if length > 0.0 {
            (((x - from.0) * ex + (y - from.1) * ey) / length).clamp(0.0, 1.0)
        } else {
            0.0
        };
        nearest = nearest.min((x - from.0 - ex * t).hypot(y - from.1 - ey * t));
        from = to;
    }
    nearest
}

/// Signed distance from a triangle; negative inside.
fn triangle_distance(p: [f64; 2], a: [f64; 2], b: [f64; 2], c: [f64; 2]) -> f64 {
    let sub = |u: [f64; 2], v: [f64; 2]| [u[0] - v[0], u[1] - v[1]];
    let dot = |u: [f64; 2], v: [f64; 2]| u[0] * v[0] + u[1] * v[1];
    let cross = |u: [f64; 2], v: [f64; 2]| u[0] * v[1] - u[1] * v[0];
    let edges = [(a, sub(b, a)), (b, sub(c, b)), (c, sub(a, c))];
    let winding = cross(sub(b, a), sub(c, a)).signum();
    let mut nearest = f64::MAX;
    let mut inside = true;
    for (start, edge) in edges {
        let v = sub(p, start);
        let t = (dot(v, edge) / dot(edge, edge)).clamp(0.0, 1.0);
        let gap = [v[0] - edge[0] * t, v[1] - edge[1] * t];
        nearest = nearest.min(dot(gap, gap));
        inside &= winding * cross(edge, v) >= 0.0;
    }
    if inside {
        -nearest.sqrt()
    } else {
        nearest.sqrt()
    }
}

/// Signed distance from a rounded rectangle's edge; negative inside.
pub fn round_rect_distance(x: f64, y: f64, rect: Rect, radius: f64) -> f64 {
    let qx = (x - rect.x - rect.width / 2.0).abs() - (rect.width / 2.0 - radius);
    let qy = (y - rect.y - rect.height / 2.0).abs() - (rect.height / 2.0 - radius);
    qx.max(0.0).hypot(qy.max(0.0)) + qx.max(qy).min(0.0) - radius
}

fn premultiply(color: (f64, f64, f64), alpha: f64) -> u32 {
    let a = alpha.clamp(0.0, 1.0);
    let channel = |c: f64| ((c * a * 255.0).round() as u32).min(255);
    (((a * 255.0).round() as u32) << 24)
        | (channel(color.0) << 16)
        | (channel(color.1) << 8)
        | channel(color.2)
}

/// Premultiplied source-over.
fn over(dst: u32, src: u32) -> u32 {
    let keep = 255 - (src >> 24);
    let channel = |shift: u32| {
        let s = (src >> shift) & 0xFF;
        let d = (dst >> shift) & 0xFF;
        (s + (d * keep + 127) / 255).min(255)
    };
    (channel(24) << 24) | (channel(16) << 16) | (channel(8) << 8) | channel(0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::native_overlay::{deciding, polish};

    const BG: (f64, f64, f64) = (0.97, 0.97, 0.97);
    const WAVE: (f64, f64, f64) = (0.9, 0.3, 0.5);

    fn scene(processing: bool, seconds: f64) -> Scene<'static> {
        Scene {
            scale: 1.0,
            seconds,
            background: BG,
            wave: WAVE,
            processing,
            pill: Some(Rect {
                x: 4.0,
                y: 4.0,
                width: 62.0,
                height: 24.0,
            }),
            speek: Some((
                4.0,
                4.0,
                crate::speek::Rig::new(crate::speek::Mood::Listen, 0.0, 7)
                    .advance(0.0, 0.0)
                    .clone(),
            )),
            label: None,
            card: Some(Rect {
                x: 4.0,
                y: 40.0,
                width: 200.0,
                height: 40.0,
            }),
            text: None,
        }
    }

    fn render(scene: &Scene) -> Vec<u32> {
        let mut pixels = vec![0; 220 * 90];
        paint(scene, &mut pixels, 220, 90);
        pixels
    }

    #[test]
    fn listening_pill_and_card_are_the_background_with_a_wave_border() {
        let pixels = render(&scene(false, 0.0));
        // Pill body away from Speek.
        assert_eq!(pixels[16 * 220 + 8] >> 24, 235);
        // Card interior and its tinted edge.
        let inside = pixels[60 * 220 + 100];
        assert_eq!(inside >> 24, 240);
        assert_eq!(inside, premultiply(BG, CARD_ALPHA));
        assert_ne!(pixels[40 * 220 + 100], inside);
        // Nothing outside the shapes.
        assert_eq!(pixels[85 * 220 + 215], 0);
        assert_eq!(pixels[34 * 220 + 100], 0);
    }

    #[test]
    fn processing_ring_turns_with_time_and_glows_outside_the_shapes() {
        let a = render(&scene(true, 0.0));
        let b = render(&scene(true, 0.35));
        let c = render(&scene(true, PROCESSING_TURN_SECONDS));
        // The card's top edge changes colour as the ring turns, and a full
        // turn later it is back where it started.
        let edge = 40 * 220 + 100;
        assert_ne!(a[edge], b[edge]);
        assert_eq!(a[edge], c[edge]);
        // Soft halo just outside the card.
        let halo = a[38 * 220 + 100] >> 24;
        assert!(halo > 0 && halo < 150);
    }

    #[test]
    fn changing_words_take_the_polish_gradient_and_a_pink_glow() {
        let count = 220 * 90;
        let mut glyphs = vec![0f32; count];
        let mut word = vec![0f32; count];
        for y in 55..65 {
            for x in 20..60 {
                glyphs[y * 220 + x] = 1.0;
            }
            for x in 80..120 {
                glyphs[y * 220 + x] = 1.0;
                word[y * 220 + x] = 1.0;
            }
        }
        let text_color = (0.1, 0.1, 0.1);
        let words = vec![(word, polish(0.35).unwrap())];
        let mut with_text = scene(false, 0.0);
        with_text.text = Some(Text {
            color: text_color,
            glyphs: &glyphs,
            words: &words,
        });
        let pixels = render(&with_text);
        let plain = pixels[60 * 220 + 40];
        // Plain text is drawn at 96% over the card.
        assert!((plain >> 16) & 0xFF < 0x40 && plain >> 24 >= 250);
        // Mid-sweep the word is coloured, not plain text.
        let swept: Vec<u32> = (80..120).map(|x| pixels[60 * 220 + x]).collect();
        assert!(swept.iter().any(|p| *p != plain));
        // The glow tints the card around the word, but not far from it.
        let card = premultiply(BG, CARD_ALPHA);
        assert_ne!(pixels[52 * 220 + 100], card);
        assert_eq!(pixels[75 * 220 + 180], card);

        // A word still being decided shimmers the same way.
        let words = vec![(words[0].0.clone(), deciding(0.7))];
        with_text.text = Some(Text {
            color: text_color,
            glyphs: &glyphs,
            words: &words,
        });
        let shimmer = render(&with_text);
        assert!((80..120).any(|x| shimmer[60 * 220 + x] != plain));
    }

    #[test]
    fn meeting_pill_shows_its_label_and_speek_inside_the_pill() {
        let pill = crate::native_overlay::meeting_pill(79.0, 4.0);
        let text_box = crate::native_overlay::meeting_label_box(pill);
        let mut label = vec![0f32; 220 * 90];
        for y in 10..20 {
            for x in text_box.x as usize..text_box.x as usize + 20 {
                label[y * 220 + x] = 1.0;
            }
        }
        let meeting = Scene {
            pill: Some(pill),
            speek: {
                let (x, y) = crate::native_overlay::meeting_logo(pill);
                Some((
                    x,
                    y,
                    crate::speek::Rig::new(crate::speek::Mood::Listen, 0.0, 7)
                        .advance(0.0, 0.0)
                        .clone(),
                ))
            },
            label: Some(&label),
            card: None,
            ..scene(false, 0.0)
        };
        let pixels = render(&meeting);
        let at = 15 * 220 + text_box.x as usize + 5;
        assert!(
            (pixels[at] >> 16) & 0xFF < 0x40,
            "label is dark text on the pill"
        );
        // Speek sits at the left of the pill, before the label: its head is
        // pink and nothing of it reaches the label.
        let (x, y) = crate::native_overlay::meeting_logo(pill);
        let centre = ((y + 13.0) as usize) * 220 + (x + PILL_WIDTH / 2.0) as usize;
        assert_eq!(pixels[centre] >> 24, 255);
        assert!((pixels[centre] >> 16) & 0xFF > 0xE0, "head is pink");
        let clear = (pill.x + pill.width - 12.0) as usize;
        for row in 6..26 {
            let at = row * 220 + text_box.x as usize - 2;
            assert_eq!(pixels[at], pixels[row * 220 + clear]);
        }
    }

    #[test]
    fn higher_scales_draw_the_same_scene_with_more_pixels() {
        let mut big = scene(false, 0.0);
        big.scale = 2.0;
        let mut pixels = vec![0; 440 * 180];
        paint(&big, &mut pixels, 440, 180);
        assert_eq!(pixels[120 * 440 + 200], premultiply(BG, CARD_ALPHA));
        assert_eq!(pixels[32 * 440 + 16] >> 24, 235);
    }
}
