// Native adapter tests create events without posting them to the desktop.
#import "../macos.m"
#include <assert.h>
static int presses, releases;
static void edge(bool down) { if (down) presses++; else releases++; }
static void event(CGEventType type, CGKeyCode code, CGEventFlags flags, bool repeat) {
    CGEventRef e = CGEventCreateKeyboardEvent(NULL, code, type == kCGEventKeyDown);
    CGEventSetFlags(e, flags);
    CGEventSetIntegerValueField(e, kCGKeyboardEventAutorepeat, repeat);
    onEvent(NULL, type, e, NULL);
    CFRelease(e);
}
int main(void) {
    @autoreleasepool {
        shortcutEdge = edge;
        assert(virtualKey(122) == 0x70 && virtualKey(51) == 8 && virtualKey(49) == 0x20);
        // Modifier-only shortcut supports either order, duplicate edges, and release.
        s2m_set_shortcut(9);
        uint32_t mapped = virtualKey(0);
        // The event-tap thread must only read the cached layout, never call TIS.
        dispatch_sync(dispatch_get_global_queue(QOS_CLASS_DEFAULT, 0), ^{ assert(virtualKey(0) == mapped); });
        event(kCGEventFlagsChanged, 59, kCGEventFlagMaskControl, false);
        event(kCGEventFlagsChanged, 55, kCGEventFlagMaskControl | kCGEventFlagMaskCommand, false);
        event(kCGEventFlagsChanged, 55, kCGEventFlagMaskControl | kCGEventFlagMaskCommand, false);
        assert(presses == 1 && releases == 0);
        event(kCGEventFlagsChanged, 55, kCGEventFlagMaskControl, false);
        assert(releases == 1);
        event(kCGEventFlagsChanged, 59, 0, false);
        // Use F8 so this test is independent of the current letter layout.
        presses = releases = 0; triggerDown = chordActive = false;
        s2m_set_shortcut((0x77 << 16) | 1);
        event(kCGEventKeyDown, 100, kCGEventFlagMaskControl, false);
        event(kCGEventKeyDown, 100, kCGEventFlagMaskControl, true);
        assert(presses == 1);
        event(kCGEventFlagsChanged, 56, kCGEventFlagMaskControl | kCGEventFlagMaskShift, false);
        assert(releases == 1);
        event(kCGEventFlagsChanged, 56, kCGEventFlagMaskControl, false);
        event(kCGEventKeyDown, 100, kCGEventFlagMaskControl, true);
        event(kCGEventKeyDown, 49, kCGEventFlagMaskControl, false);
        assert(presses == 1); // Neither repeat nor an unrelated key can restart.
        event(kCGEventKeyUp, 100, kCGEventFlagMaskControl, false);
        event(kCGEventKeyDown, 100, kCGEventFlagMaskControl, false);
        assert(presses == 2);
        event(kCGEventKeyUp, 100, kCGEventFlagMaskControl, false);
        assert(releases == 2);
        // Our own injected events must never be recognized as hotkeys.
        CGEventRef e = CGEventCreateKeyboardEvent(NULL, 100, true);
        CGEventSetFlags(e, kCGEventFlagMaskControl);
        CGEventSetIntegerValueField(e, kCGEventSourceUserData, kSpeekEvent);
        onEvent(NULL, kCGEventKeyDown, e, NULL); CFRelease(e);
        assert(presses == 2);
        puts("PASS: native macOS shortcut edges, repeat suppression, modifier cancellation and injected-event filtering");
    }
}
