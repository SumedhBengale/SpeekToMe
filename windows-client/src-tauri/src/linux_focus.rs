//! Best-effort cross-application focus classification using AT-SPI.
//! Unknown accessibility state is distinct from a known non-editable target.

use std::ffi::c_void;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Focus {
    Editable,
    NotEditable,
    Unknown,
}

#[link(name = "atspi")]
unsafe extern "C" {
    fn atspi_init() -> i32;
    fn atspi_get_desktop(index: i32) -> *mut c_void;
    fn atspi_accessible_get_child_count(obj: *mut c_void, error: *mut *mut c_void) -> i32;
    fn atspi_accessible_get_child_at_index(
        obj: *mut c_void,
        index: i32,
        error: *mut *mut c_void,
    ) -> *mut c_void;
    fn atspi_accessible_get_state_set(obj: *mut c_void) -> *mut c_void;
    fn atspi_state_set_contains(set: *mut c_void, state: i32) -> i32;
}

#[link(name = "gobject-2.0")]
unsafe extern "C" {
    fn g_object_unref(object: *mut c_void);
}

const ACTIVE: i32 = 1;
const EDITABLE: i32 = 7;
const FOCUSED: i32 = 12;
const MAX_NODES: usize = 2500;

unsafe fn has_state(object: *mut c_void, state: i32) -> bool {
    let states = atspi_accessible_get_state_set(object);
    if states.is_null() {
        return false;
    }
    let found = atspi_state_set_contains(states, state) != 0;
    g_object_unref(states);
    found
}

unsafe fn descend(object: *mut c_void, remaining: &mut usize) -> Option<Focus> {
    if *remaining == 0 {
        return None;
    }
    *remaining -= 1;
    let count = atspi_accessible_get_child_count(object, std::ptr::null_mut());
    for index in 0..count.max(0).min(500) {
        let child = atspi_accessible_get_child_at_index(object, index, std::ptr::null_mut());
        if child.is_null() {
            continue;
        }
        let found = descend(child, remaining);
        g_object_unref(child);
        if found.is_some() {
            return found;
        }
    }
    if has_state(object, FOCUSED) {
        Some(if has_state(object, EDITABLE) {
            Focus::Editable
        } else {
            Focus::NotEditable
        })
    } else {
        None
    }
}

pub fn focused_target() -> Focus {
    // AT-SPI can be absent, disabled, or incompletely implemented by an app.
    // Only suppress injection when it explicitly reports a non-editable focus.
    unsafe {
        if atspi_init() != 0 {
            return Focus::Unknown;
        }
        let desktop = atspi_get_desktop(0);
        if desktop.is_null() {
            return Focus::Unknown;
        }
        let mut remaining = MAX_NODES;
        let apps = atspi_accessible_get_child_count(desktop, std::ptr::null_mut());
        let mut result = None;
        for app_index in 0..apps.max(0).min(100) {
            let app = atspi_accessible_get_child_at_index(desktop, app_index, std::ptr::null_mut());
            if app.is_null() {
                continue;
            }
            let windows = atspi_accessible_get_child_count(app, std::ptr::null_mut());
            for window_index in 0..windows.max(0).min(100) {
                let window =
                    atspi_accessible_get_child_at_index(app, window_index, std::ptr::null_mut());
                if window.is_null() {
                    continue;
                }
                if has_state(window, ACTIVE) {
                    result = descend(window, &mut remaining);
                }
                g_object_unref(window);
                if result.is_some() {
                    break;
                }
            }
            g_object_unref(app);
            if result.is_some() {
                break;
            }
        }
        g_object_unref(desktop);
        result.unwrap_or(Focus::Unknown)
    }
}
