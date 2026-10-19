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
        draw: extern "C" fn(*mut c_void, f64, f64),
        placement: extern "C" fn(f64, f64, i32, f64, f64) -> Placement,
    );
    fn CGContextSaveGState(context: *mut c_void);
    fn CGContextRestoreGState(context: *mut c_void);
    fn CGContextClearRect(context: *mut c_void, rect: CgRect);
    fn CGContextSetRGBFillColor(context: *mut c_void, r: f64, g: f64, b: f64, a: f64);
    fn CGContextSetRGBStrokeColor(context: *mut c_void, r: f64, g: f64, b: f64, a: f64);
    fn CGContextSetLineWidth(context: *mut c_void, width: f64);
    fn CGContextAddPath(context: *mut c_void, path: Ref);
    fn CGContextDrawPath(context: *mut c_void, mode: i32);
    fn CGContextSetTextMatrix(context: *mut c_void, transform: Transform);
    fn CGPathCreateWithRoundedRect(rect: CgRect, rx: f64, ry: f64, transform: Ref) -> Ref;
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
        let utf8 = CString::new(text.replace('\0', "")).ok()?;
        let string = Owned::new(CFStringCreateWithCString(
            std::ptr::null(),
            utf8.as_ptr(),
            0x08000100,
        ))?;
        let font = Owned::new(CTFontCreateUIFontForLanguage(0, 13.0, std::ptr::null()))?;
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
                height: 88.0,
            },
            std::ptr::null_mut(),
        );
        Some(Self {
            text: text.to_owned(),
            color,
            setter,
            size: Size {
                width: size.width.ceil(),
                height: size.height.ceil().clamp(16.0, 88.0),
            },
            frame: None,
        })
    }

    unsafe fn draw(&mut self, context: *mut c_void, card: Rect) {
        if self.frame.as_ref().map(|(bounds, _)| *bounds) != Some(card) {
            let path = Owned::new(CGPathCreateWithRect(
                cg_rect(Rect {
                    x: card.x + 12.0,
                    y: card.y + 8.0,
                    width: card.width - 24.0,
                    height: card.height - 16.0,
                }),
                std::ptr::null(),
            ));
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
                .map(|frame| (card, frame));
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
    frame: u64,
    smoothed: f64,
    text: Option<TextLayout>,
}
thread_local! { static RENDERER: RefCell<Renderer> = RefCell::new(Renderer::default()); }

fn cg_rect(rect: Rect) -> CgRect {
    CgRect {
        origin: Point {
            x: rect.x,
            y: design::HEIGHT - rect.y - rect.height,
        },
        size: Size {
            width: rect.width,
            height: rect.height,
        },
    }
}

unsafe fn rounded(
    context: *mut c_void,
    rect: Rect,
    radius: f64,
    color: (f64, f64, f64),
    alpha: f64,
    stroke: bool,
) {
    if let Some(path) = Owned::new(CGPathCreateWithRoundedRect(
        cg_rect(rect),
        radius,
        radius,
        std::ptr::null(),
    )) {
        CGContextSetRGBFillColor(context, color.0, color.1, color.2, alpha);
        CGContextAddPath(context, path.0);
        CGContextDrawPath(context, if stroke { 3 } else { 0 }); // fill-stroke / fill
    }
}

extern "C" fn placement(width: f64, height: f64, position: i32, x: f64, y: f64) -> Placement {
    design::placement(width, height, position, x, y)
}

extern "C" fn draw(context: *mut c_void, x: f64, y: f64) {
    if context.is_null() {
        return;
    }
    let _ = std::panic::catch_unwind(|| unsafe {
        CGContextSaveGState(context);
        CGContextClearRect(
            context,
            cg_rect(Rect {
                x: 0.0,
                y: 0.0,
                width: design::WIDTH,
                height: design::HEIGHT,
            }),
        );
        let state = crate::macos_desktop::overlay_state();
        if state != 0 {
            RENDERER.with(|slot| {
                let mut renderer = slot.borrow_mut();
                let background = design::color(crate::ACTIVE_PILL_BG_COLOR.load(Ordering::Relaxed));
                let wave = design::color(crate::ACTIVE_PILL_WAVE_COLOR.load(Ordering::Relaxed));
                rounded(
                    context,
                    Rect {
                        x: x + 0.5,
                        y: y + 0.5,
                        width: 61.0,
                        height: 23.0,
                    },
                    11.5,
                    background,
                    235.0 / 255.0,
                    false,
                );
                let volume = if state == 3 {
                    crate::audio::MIC_VOLUME
                        .load(Ordering::Relaxed)
                        .max(crate::audio::LOOPBACK_VOLUME.load(Ordering::Relaxed))
                } else {
                    crate::audio::CURRENT_VOLUME.load(Ordering::Relaxed)
                };
                renderer.smoothed = design::smooth_volume(renderer.smoothed, volume as f64);
                for bar in design::bars(x, y, renderer.frame, renderer.smoothed, state == 2) {
                    rounded(context, bar, 1.2, wave, 1.0, false);
                }
                let text = crate::win32::get_preview_text();
                if !text.trim().is_empty() {
                    let color = design::text_color(background);
                    if renderer
                        .text
                        .as_ref()
                        .map(|layout| (layout.text.as_str(), layout.color))
                        != Some((text.as_str(), color))
                    {
                        renderer.text = TextLayout::new(&text, color);
                    }
                    if let Some(layout) = renderer.text.as_mut() {
                        let card =
                            design::preview_card(x, y, layout.size.width, layout.size.height);
                        CGContextSetRGBStrokeColor(context, wave.0, wave.1, wave.2, 0.64);
                        CGContextSetLineWidth(context, 1.0);
                        rounded(context, card, 7.0, background, 0.96, true);
                        layout.draw(context, card);
                    }
                } else {
                    renderer.text = None;
                }
                renderer.frame = renderer.frame.wrapping_add(1);
            });
        } else {
            RENDERER.with(|slot| *slot.borrow_mut() = Renderer::default());
        }
        CGContextRestoreGState(context);
    });
}

pub fn install() {
    unsafe {
        s2m_overlay_callbacks(design::WIDTH, design::HEIGHT, draw, placement);
    }
}
