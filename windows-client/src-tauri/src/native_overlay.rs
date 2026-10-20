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

pub fn smooth_volume(previous: f64, raw: f64) -> f64 {
    previous * 0.8 + raw * 0.2
}

pub fn bar_height(frame: u64, index: usize, volume: f64, processing: bool) -> f64 {
    if processing {
        let phase = frame as f64 * 0.12 + index as f64 * 0.75;
        2.2 + 2.5 * ((phase.sin() + 1.0) / 2.0)
    } else {
        let gain = (if volume > 2.0 { 5.0 } else { 2.5 }) + (volume / 4.0).min(2.5);
        let phase = frame as f64 * 0.1 + index as f64 * 0.7;
        (gain * (0.35 + 0.65 * phase.sin().abs())).clamp(2.0, 8.0)
    }
}

pub fn bars(x: f64, y: f64, frame: u64, volume: f64, processing: bool) -> [Rect; 5] {
    std::array::from_fn(|i| {
        let height = bar_height(frame, i, volume, processing);
        Rect {
            x: x + 20.75 + i as f64 * 4.5,
            y: y + 12.0 - height + 0.8,
            width: 2.5,
            height: height * 2.0 - 1.6,
        }
    })
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
    fn waveform_responds_to_audio_and_processing_animates_without_audio() {
        let quiet = bars(179.0, 0.0, 10, 0.0, false);
        let speech = bars(179.0, 0.0, 10, 30.0, false);
        assert!(speech.iter().zip(quiet).any(|(a, b)| a.height > b.height));
        assert_ne!(bars(0.0, 0.0, 0, 0.0, true), bars(0.0, 0.0, 10, 0.0, true));
        for rect in speech {
            assert!(rect.x >= 179.0 && rect.x + rect.width <= 179.0 + PILL_WIDTH);
            assert!(rect.y >= 0.0 && rect.y + rect.height <= PILL_HEIGHT);
        }
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
    frame: u64,
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
    let phase = (y - cy).atan2(x - cx) / (std::f64::consts::PI * 2.0) + frame as f64 * 0.008;
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
