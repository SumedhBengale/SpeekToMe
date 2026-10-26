//! Rust native drawing adapter for the shared pill/preview design. AppKit only
//! hosts a nonactivating surface; CoreGraphics and CoreText draw into it here.
use crate::native_overlay::{self as design, Placement, Rect};
use std::{
    cell::RefCell,
    ffi::{c_char, c_void, CString},
    sync::atomic::Ordering,
};

type Ref = *const c_void;
#[repr(C)]
#[derive(Clone, Copy)]
struct Point {
    x: f64,
    y: f64,
}
#[repr(C)]
#[derive(Clone, Copy)]
struct Size {
    width: f64,
    height: f64,
}
#[repr(C)]
#[derive(Clone, Copy)]
struct CgRect {
    origin: Point,
    size: Size,
}
#[repr(C)]
#[derive(Clone, Copy)]
struct Range {
    location: isize,
    length: isize,
}
#[repr(C)]
struct Transform {
    a: f64,
    b: f64,
    c: f64,
    d: f64,
    tx: f64,
    ty: f64,
}

unsafe extern "C" {
    fn s2m_overlay_callbacks(
        width: f64,
        height: f64,
        meeting_width: f64,
        draw: extern "C" fn(*mut c_void, f64, f64),
        placement: extern "C" fn(f64, f64, i32, f64, f64) -> Placement,
    );
    fn CGColorSpaceCreateDeviceRGB() -> Ref;
    fn CGBitmapContextCreate(
        data: *mut c_void,
        width: usize,
        height: usize,
        bits_per_component: usize,
        bytes_per_row: usize,
        space: Ref,
        bitmap_info: u32,
    ) -> *mut c_void;
    fn CGBitmapContextGetData(context: *mut c_void) -> *mut c_void;
    fn CGBitmapContextGetBytesPerRow(context: *mut c_void) -> usize;
    fn CGBitmapContextCreateImage(context: *mut c_void) -> Ref;
    fn CGContextDrawImage(context: *mut c_void, rect: CgRect, image: Ref);
    fn CGContextScaleCTM(context: *mut c_void, sx: f64, sy: f64);
    fn CGContextConvertRectToDeviceSpace(context: *mut c_void, rect: CgRect) -> CgRect;
    fn CGContextSaveGState(context: *mut c_void);
    fn CGContextRestoreGState(context: *mut c_void);
    fn CGContextClearRect(context: *mut c_void, rect: CgRect);
    fn CGContextSetTextMatrix(context: *mut c_void, transform: Transform);
    fn CGPathCreateWithRect(rect: CgRect, transform: Ref) -> Ref;
    fn CGColorCreateGenericRGB(r: f64, g: f64, b: f64, a: f64) -> Ref;
    fn CFRelease(value: Ref);
    fn CFStringCreateWithCString(allocator: Ref, text: *const c_char, encoding: u32) -> Ref;
    fn CFDictionaryCreate(
        allocator: Ref,
        keys: *const Ref,
        values: *const Ref,
        count: isize,
        key_callbacks: Ref,
        value_callbacks: Ref,
    ) -> Ref;
    fn CFAttributedStringCreate(allocator: Ref, text: Ref, attributes: Ref) -> Ref;
    fn CTFontCreateUIFontForLanguage(kind: u32, size: f64, language: Ref) -> Ref;
    fn CTFramesetterCreateWithAttributedString(text: Ref) -> Ref;
    fn CTFramesetterSuggestFrameSizeWithConstraints(
        setter: Ref,
        range: Range,
        attributes: Ref,
        size: Size,
        fit_range: *mut Range,
    ) -> Size;
    fn CTFramesetterCreateFrame(setter: Ref, range: Range, path: Ref, attributes: Ref) -> Ref;
    fn CTFrameDraw(frame: Ref, context: *mut c_void);
    static kCTFontAttributeName: Ref;
    static kCTForegroundColorAttributeName: Ref;
    static kCFTypeDictionaryKeyCallBacks: u8;
    static kCFTypeDictionaryValueCallBacks: u8;
}

struct Owned(Ref);
impl Owned {
    fn new(value: Ref) -> Option<Self> {
        (!value.is_null()).then_some(Self(value))
    }
}
impl Drop for Owned {
    fn drop(&mut self) {
        unsafe {
            CFRelease(self.0);
        }
    }
}

// CoreText and all retained graphics objects stay on the AppKit main thread.
struct TextLayout {
    text: String,
    color: (f64, f64, f64),
    setter: Owned,
    size: Size,
    frame: Option<(Rect, Owned)>,
}
impl TextLayout {
    unsafe fn new(text: &str, color: (f64, f64, f64)) -> Option<Self> {
        // kCTFontUIFontUser
        Self::with_font(text, color, 0, 13.0)
    }

    unsafe fn with_font(text: &str, color: (f64, f64, f64), kind: u32, size: f64) -> Option<Self> {
        let utf8 = CString::new(text.replace('\0', "")).ok()?;
        let string = Owned::new(CFStringCreateWithCString(
            std::ptr::null(),
            utf8.as_ptr(),
            0x08000100,
        ))?;
        let font = Owned::new(CTFontCreateUIFontForLanguage(kind, size, std::ptr::null()))?;
        let foreground = Owned::new(CGColorCreateGenericRGB(color.0, color.1, color.2, 1.0))?;
        let keys = [kCTFontAttributeName, kCTForegroundColorAttributeName];
        let values = [font.0, foreground.0];
        let attributes = Owned::new(CFDictionaryCreate(
            std::ptr::null(),
            keys.as_ptr(),
            values.as_ptr(),
            2,
            &raw const kCFTypeDictionaryKeyCallBacks as Ref,
            &raw const kCFTypeDictionaryValueCallBacks as Ref,
        ))?;
        let attributed = Owned::new(CFAttributedStringCreate(
            std::ptr::null(),
            string.0,
            attributes.0,
        ))?;
        let setter = Owned::new(CTFramesetterCreateWithAttributedString(attributed.0))?;
        let size = CTFramesetterSuggestFrameSizeWithConstraints(
            setter.0,
            Range {
                location: 0,
                length: 0,
            },
            std::ptr::null(),
            Size {
                width: 370.0,
                height: 1_000_000_000.0,
            },
            std::ptr::null_mut(),
        );
        Some(Self {
            text: text.to_owned(),
            color,
            setter,
            size: Size {
                width: size.width.ceil(),
                height: size.height.ceil().max(16.0),
            },
            frame: None,
        })
    }

    /// Draws the text laid out in `text_box`, from its top left.
    unsafe fn draw(&mut self, context: *mut c_void, text_box: Rect) {
        if self.frame.as_ref().map(|(bounds, _)| *bounds) != Some(text_box) {
            let path = Owned::new(CGPathCreateWithRect(cg_rect(text_box), std::ptr::null()));
            self.frame = path
                .and_then(|path| {
                    Owned::new(CTFramesetterCreateFrame(
                        self.setter.0,
                        Range {
                            location: 0,
                            length: 0,
                        },
                        path.0,
                        std::ptr::null(),
                    ))
                })
                .map(|frame| (text_box, frame));
        }
        if let Some((_, frame)) = &self.frame {
            CGContextSetTextMatrix(
                context,
                Transform {
                    a: 1.0,
                    b: 0.0,
                    c: 0.0,
                    d: 1.0,
                    tx: 0.0,
                    ty: 0.0,
                },
            );
            CTFrameDraw(frame.0, context);
        }
    }
}

#[derive(Default)]
struct Renderer {
    smoothed: f64,
    text: Option<TextLayout>,
}
thread_local! { static RENDERER: RefCell<Renderer> = RefCell::new(Renderer::default()); }

fn cg_rect(rect: Rect) -> CgRect {
    CgRect {
        origin: Point {
            x: rect.x,
            y: surface_height() - rect.y - rect.height,
        },
        size: Size {
            width: rect.width,
            height: rect.height,
        },
    }
}

extern "C" fn placement(width: f64, height: f64, position: i32, x: f64, y: f64) -> Placement {
    let text = crate::win32::get_preview_text();
    let state = crate::macos_desktop::overlay_state();
    let text = if state == 0 || state == MEETING {
        ""
    } else {
        design::preview_text(&text, state == 2)
    };
    let color = design::text_color(design::color(
        crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed),
    ));
    let text_height = RENDERER.with(|slot| {
        let mut renderer = slot.borrow_mut();
        if text.is_empty() {
            renderer.text = None;
            return 0.0;
        }
        if renderer
            .text
            .as_ref()
            .map(|layout| (layout.text.as_str(), layout.color))
            != Some((text, color))
        {
            renderer.text = unsafe { TextLayout::new(text, color) };
        }
        renderer
            .text
            .as_ref()
            .map(|layout| layout.size.height)
            .unwrap_or(0.0)
    });
    let surface_height = design::surface_height_at(text_height, height, position, y);
    SURFACE_HEIGHT.store(surface_height.to_bits(), Ordering::Relaxed);
    let mut placement =
        design::placement_with_height(width, height, position, x, y, surface_height);
    if state == MEETING {
        // AppKit hit-tests the meeting pill from this position.
        placement.pill_x = design::meeting_pill(placement.pill_x, placement.pill_y).x;
    }
    placement
}

const MEETING: u8 = 3;

extern "C" fn draw(context: *mut c_void, x: f64, y: f64) {
    if context.is_null() {
        return;
    }
    let _ = std::panic::catch_unwind(|| unsafe {
        CGContextSaveGState(context);
        let bounds = cg_rect(Rect {
            x: 0.0,
            y: 0.0,
            width: design::WIDTH,
            height: surface_height(),
        });
        CGContextClearRect(context, bounds);
        let state = crate::macos_desktop::overlay_state();
        if state != 0 {
            RENDERER.with(|slot| {
                let mut renderer = slot.borrow_mut();
                render(&mut renderer, context, bounds, x, y, state);
            });
        } else {
            RENDERER.with(|slot| *slot.borrow_mut() = Renderer::default());
        }
        CGContextRestoreGState(context);
    });
}

/// Paints the pill and preview with the shared renderer at the display's
/// backing scale; CoreText only supplies the text's coverage.
unsafe fn render(
    renderer: &mut Renderer,
    context: *mut c_void,
    bounds: CgRect,
    x: f64,
    y: f64,
    state: u8,
) -> Option<()> {
    let unit = CGContextConvertRectToDeviceSpace(
        context,
        CgRect {
            origin: Point { x: 0.0, y: 0.0 },
            size: Size {
                width: 1.0,
                height: 1.0,
            },
        },
    );
    let scale = unit.size.width.abs().max(1.0);
    let width = (design::WIDTH * scale).ceil() as usize;
    let height = (surface_height() * scale).ceil() as usize;
    let background = design::color(crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed));
    let volume = if state == 3 {
        crate::audio::MIC_VOLUME
            .load(Ordering::Relaxed)
            .max(crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed))
    } else {
        crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed)
    };
    renderer.smoothed = design::smooth_volume(renderer.smoothed, volume as f64);

    let meeting = state == MEETING;
    let raw = if meeting {
        String::new()
    } else {
        crate::win32::get_preview_text()
    };
    let text = design::preview_text(&raw, state == 2);
    let color = design::text_color(background);
    let mut card = None;
    let mut glyphs = Vec::new();
    let mut words = Vec::new();
    if text.trim().is_empty() {
        renderer.text = None;
    } else {
        if renderer
            .text
            .as_ref()
            .map(|layout| (layout.text.as_str(), layout.color))
            != Some((text, color))
        {
            renderer.text = TextLayout::new(text, color);
        }
        if let Some(layout) = renderer.text.as_mut() {
            let shape = design::preview_card_in_surface(
                x,
                y,
                layout.size.width,
                layout.size.height,
                surface_height(),
            );
            let text_box = Rect {
                x: shape.x + 12.0,
                y: shape.y + 8.0,
                width: shape.width - 24.0,
                height: shape.height - 16.0,
            };
            glyphs = coverage(width, height, scale, |mask| layout.draw(mask, text_box));
            for (range, look) in crate::win32::preview_glow() {
                if range.end > text.len()
                    || !text.is_char_boundary(range.start)
                    || !text.is_char_boundary(range.end)
                {
                    continue;
                }
                // A word's pixels are what text[..end] adds over
                // text[..start]; greedy wrapping lays out both prefixes
                // exactly as the full text.
                let prefix = |end: usize| {
                    TextLayout::new(&text[..end], color).map_or_else(
                        || vec![0.0; width * height],
                        |mut layout| {
                            coverage(width, height, scale, |mask| layout.draw(mask, text_box))
                        },
                    )
                };
                let before = prefix(range.start);
                let word = prefix(range.end)
                    .iter()
                    .zip(before)
                    .map(|(after, before)| (after - before).max(0.0))
                    .collect();
                words.push((word, look));
            }
            card = Some(shape);
        }
    }
    // Placement already moved a meeting's `x` to the meeting pill.
    let pill = Rect {
        x,
        y,
        width: if meeting {
            design::MEETING_PILL_WIDTH
        } else {
            design::PILL_WIDTH
        },
        height: design::PILL_HEIGHT,
    };
    let logo = if meeting {
        design::meeting_logo(pill)
    } else {
        (x, y)
    };
    let mut label = Vec::new();
    if meeting {
        // kCTFontUIFontEmphasizedSystem
        if let Some(mut layout) =
            TextLayout::with_font(design::MEETING_LABEL, color, 3, design::MEETING_LABEL_SIZE)
        {
            let text_box = design::meeting_label_box(pill);
            let line = Rect {
                y: text_box.y + (text_box.height - layout.size.height) / 2.0,
                height: layout.size.height,
                ..text_box
            };
            label = coverage(width, height, scale, |mask| layout.draw(mask, line));
        }
    }
    let mut pixels = vec![0u32; width * height];
    crate::overlay_paint::paint(
        &crate::overlay_paint::Scene {
            scale,
            seconds: crate::win32::animation_seconds(),
            background,
            wave: design::color(crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed)),
            processing: state == 2,
            pill: Some(pill),
            logo: Some(logo),
            volume: renderer.smoothed,
            label: meeting.then_some(label.as_slice()),
            card,
            text: card.map(|_| crate::overlay_paint::Text {
                color,
                glyphs: &glyphs,
                words: &words,
            }),
        },
        &mut pixels,
        width,
        height,
    );
    let space = Owned::new(CGColorSpaceCreateDeviceRGB())?;
    // Premultiplied-first, 32-bit little-endian: the shared renderer's pixels.
    let bitmap = CGBitmapContextCreate(
        pixels.as_mut_ptr() as *mut c_void,
        width,
        height,
        8,
        width * 4,
        space.0,
        2 | (2 << 12),
    );
    let bitmap = Owned::new(bitmap as Ref)?;
    // Drawn while `pixels` is alive: the image may share its memory.
    let image = Owned::new(CGBitmapContextCreateImage(bitmap.0 as *mut c_void))?;
    CGContextDrawImage(context, bounds, image.0);
    Some(())
}

/// Coverage of what `draw` puts in an alpha-only bitmap, one value per
/// device pixel, top row first.
unsafe fn coverage(
    width: usize,
    height: usize,
    scale: f64,
    draw: impl FnOnce(*mut c_void),
) -> Vec<f32> {
    let mut values = vec![0f32; width * height];
    // kCGImageAlphaOnly with no colour space.
    let mask = CGBitmapContextCreate(
        std::ptr::null_mut(),
        width,
        height,
        8,
        width,
        std::ptr::null(),
        7,
    );
    let Some(owned) = Owned::new(mask as Ref) else {
        return values;
    };
    CGContextScaleCTM(mask, scale, scale);
    draw(mask);
    let data = CGBitmapContextGetData(mask) as *const u8;
    let stride = CGBitmapContextGetBytesPerRow(mask);
    if !data.is_null() {
        for (i, value) in values.iter_mut().enumerate() {
            *value = *data.add((i / width) * stride + i % width) as f32 / 255.0;
        }
    }
    drop(owned);
    values
}

pub fn install() {
    unsafe {
        s2m_overlay_callbacks(
            design::WIDTH,
            design::HEIGHT,
            design::MEETING_PILL_WIDTH,
            draw,
            placement,
        );
    }
}

static SURFACE_HEIGHT: std::sync::atomic::AtomicU64 =
    std::sync::atomic::AtomicU64::new(design::HEIGHT.to_bits());
fn surface_height() -> f64 {
    f64::from_bits(SURFACE_HEIGHT.load(Ordering::Relaxed))
}
