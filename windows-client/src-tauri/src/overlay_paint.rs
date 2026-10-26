//! Software renderer for the dictation pill and preview card, shared by the
//! Windows, Linux and macOS overlays so every effect looks the same. Each
//! platform only lays out and rasterises the preview text with its own text
//! engine, then shows the finished pixels.
//!
//! Pixels are premultiplied ARGB in native-endian `u32`s: what a Win32 DIB, a
//! cairo `ARgb32` surface and a CoreGraphics little-endian premultiplied-first
//! bitmap all store.
use crate::native_overlay::{
    bars, blur, polish_color, processing_pixel, Polish, Rect, POLISH_GLOW, PROCESSING_TURN_SECONDS,
};

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
    /// Top left of the 62×24 logo box, when the logo is drawn.
    pub logo: Option<(f64, f64)>,
    /// Smoothed microphone level for the logo.
    pub volume: f64,
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
    if let Some((x, y)) = scene.logo {
        let logo = crate::native_overlay::color(crate::native_overlay::LOGO_COLOR);
        for bar in bars(x, y, scene.seconds, scene.volume, scene.processing) {
            let radius = bar.width / 2.0;
            for (px, py) in area(bar, 1.0) {
                let (lx, ly) = logical(px, py);
                let cover = (0.5 - round_rect_distance(lx, ly, bar, radius) * s).clamp(0.0, 1.0);
                if cover > 0.0 {
                    let i = py * width + px;
                    pixels[i] = over(pixels[i], premultiply(logo, cover));
                }
            }
        }
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

/// Draws the text inside the card. A changing word takes the website's
/// polish gradient over a pink drop-shadow.
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
            logo: Some((4.0, 4.0)),
            volume: 0.0,
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
        // Pill body away from the logo.
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
    fn higher_scales_draw_the_same_scene_with_more_pixels() {
        let mut big = scene(false, 0.0);
        big.scale = 2.0;
        let mut pixels = vec![0; 440 * 180];
        paint(&big, &mut pixels, 440, 180);
        assert_eq!(pixels[120 * 440 + 200], premultiply(BG, CARD_ALPHA));
        assert_eq!(pixels[32 * 440 + 16] >> 24, 235);
    }
}
