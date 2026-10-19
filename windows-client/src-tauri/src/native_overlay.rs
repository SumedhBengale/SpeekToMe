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
    let custom = position == 4;
    let left = position == 5 || position == 7;
    let right = position == 6 || position == 8;
    let bottom = position == 0 || position == 7 || position == 8;
    Placement {
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
            y
        } else if bottom {
            height - HEIGHT - 80.0
        } else {
            50.0
        })
        .clamp(0.0, (height - HEIGHT).max(0.0)),
        pill_x: if custom || left {
            0.0
        } else if right {
            WIDTH - PILL_WIDTH
        } else {
            (WIDTH - PILL_WIDTH) / 2.0
        },
        pill_y: if bottom && !custom {
            HEIGHT - PILL_HEIGHT
        } else {
            0.0
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
    let width = (text_width + 24.0).clamp(160.0, 394.0);
    let height = (text_height + 16.0).min(104.0);
    Rect {
        x: (x + 31.0 - width / 2.0).clamp(4.0, WIDTH - width - 4.0),
        y: if y > 70.0 {
            (y - height - 8.0).max(4.0)
        } else {
            (y + 32.0).min(HEIGHT - height - 4.0)
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
