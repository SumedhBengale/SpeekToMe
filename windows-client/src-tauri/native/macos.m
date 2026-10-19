#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <AVFoundation/AVFoundation.h>
#import <Carbon/Carbon.h>
#import <UserNotifications/UserNotifications.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreMedia/CoreMedia.h>
#import <WebKit/WebKit.h>
#include <mach/mach.h>
#include <malloc/malloc.h>
#include <stdatomic.h>
#include <unistd.h>

// Rust owns capture, routing and configuration. This bridge only supplies native
// desktop facilities. AppKit operations always run on the main queue.
static const int64_t kSpeekEvent = 0x535045454b;
static _Atomic(uint32_t) shortcutCombo;
static _Atomic(uint32_t) keyMap[128];
static id keyboardLayoutObserver;
static void refreshKeyMap(void);
static _Atomic(bool) listenerReady;
static void (*shortcutEdge)(bool);
static CFMachPortRef eventTap;
static bool chordActive, triggerDown;
static NSPanel *panel;
typedef struct { double x, y, pill_x, pill_y; } S2MOverlayPlacement;
static void (*overlayDraw)(CGContextRef, double, double);
static S2MOverlayPlacement (*overlayPlacement)(double, double, int, double, double);
static NSTimer *overlayTimer;
static double surfaceWidth, surfaceHeight, pillX, pillY;
static uint64_t drawFrames;
static int overlayState;
static NSString *previewText = @"";
static int placement;
static double customX, customY;

static void mainAsync(dispatch_block_t block) {
    if ([NSThread isMainThread]) block(); else dispatch_async(dispatch_get_main_queue(), block);
}

// Weak tracking proves the WKWebView/configuration is deallocated, rather than
// merely removed from Tauri's window map. It never keeps browser objects alive.
static NSHashTable *settingsViews, *settingsStores, *settingsPools;
static id miniaturizeObserver, hideObserver;
void s2m_settings_observers(void (*background)(bool)) {
    mainAsync(^{
        if (!miniaturizeObserver) miniaturizeObserver = [NSNotificationCenter.defaultCenter
            addObserverForName:NSWindowDidMiniaturizeNotification object:nil queue:NSOperationQueue.mainQueue
            usingBlock:^(NSNotification *note) { dispatch_async(dispatch_get_main_queue(), ^{ background(true); }); }];
        if (!hideObserver) hideObserver = [NSNotificationCenter.defaultCenter
            addObserverForName:NSApplicationDidHideNotification object:nil queue:NSOperationQueue.mainQueue
            usingBlock:^(NSNotification *note) { dispatch_async(dispatch_get_main_queue(), ^{
                if (!NSApp.isHidden) return;
                background(false);
                // AppKit keeps hidden windows for a later unhide. Flush that
                // retention after closing Settings; the accessory app stays
                // in the tray with no Settings window and takes no focus.
                dispatch_async(dispatch_get_main_queue(), ^{ [NSApp unhideWithoutActivation]; });
            }); }];
    });
}
void s2m_track_settings(void *pointer) {
    @autoreleasepool {
    if (NSApp.isHidden) [NSApp unhideWithoutActivation];
    WKWebView *view = (__bridge WKWebView *)pointer;
    if (!view) return;
    if (!settingsViews) {
        settingsViews = NSHashTable.weakObjectsHashTable;
        settingsStores = NSHashTable.weakObjectsHashTable;
        settingsPools = NSHashTable.weakObjectsHashTable;
    }
    [settingsViews addObject:view];
    [settingsStores addObject:view.configuration.websiteDataStore];
    [settingsPools addObject:view.configuration.processPool];
    }
}
void s2m_prepare_settings_close(void *pointer) {
    @autoreleasepool {
        WKWebView *view = (__bridge WKWebView *)pointer;
        if (!view) return;
        [view stopLoading];
        view.navigationDelegate = nil;
        view.UIDelegate = nil;
        NSWindow *window = view.window;
        [window makeFirstResponder:nil];
        window.initialFirstResponder = nil;
        [view removeFromSuperview];
        if (window) window.contentView = [[NSView alloc] initWithFrame:window.contentView.frame];
    }
}
void s2m_finish_settings_close(void) {
    mainAsync(^{
        @autoreleasepool {
            // Advance the AppKit event/autorelease cycle even with no visible
            // windows. No keyboard/mouse input or focus change is generated.
            NSEvent *event = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
                context:nil subtype:0 data1:0 data2:0];
            [NSApp postEvent:event atStart:NO];
        }
    });
}
void s2m_browser_idle_cleanup(void) {
    @autoreleasepool { malloc_zone_pressure_relief(NULL, 0); }
}
char *s2m_settings_snapshot(void) {
    __block char *result = NULL;
    dispatch_block_t block = ^{
        @autoreleasepool {
            struct task_vm_info info = {0};
            mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
            task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&info, &count);
            NSDictionary *value = @{
                @"live_webviews": @(settingsViews.allObjects.count),
                @"live_data_stores": @(settingsStores.allObjects.count),
                @"live_process_pools": @(settingsPools.allObjects.count),
                @"resident_bytes": @(info.resident_size), @"footprint_bytes": @(info.phys_footprint)
            };
            NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
            result = strdup([[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String);
        }
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return result;
}

uint32_t s2m_permissions(void) {
    uint32_t value = 0;
    if ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio] == AVAuthorizationStatusAuthorized) value |= 1;
    if (AXIsProcessTrusted()) value |= 2;
    if (CGPreflightListenEventAccess()) value |= 4;
    if (CGPreflightScreenCaptureAccess()) value |= 8;
    return value;
}
bool s2m_listener_ready(void) { return atomic_load(&listenerReady); }
void s2m_request_permissions(void) {
    mainAsync(^{
        if ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio] == AVAuthorizationStatusNotDetermined)
            [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {}];
        NSDictionary *options = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt: @YES};
        AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
        CGRequestListenEventAccess();
        // Take the user to the missing grant, including when macOS retained an
        // obsolete entry for a differently signed development build.
        NSString *pane = !AXIsProcessTrusted() ? @"Privacy_Accessibility" :
            !CGPreflightListenEventAccess() ? @"Privacy_ListenEvent" : nil;
        if (pane) [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:
            [@"x-apple.systempreferences:com.apple.preference.security?" stringByAppendingString:pane]]];
    });
}
void s2m_set_shortcut(uint32_t combo) {
    atomic_store(&shortcutCombo, combo);
    mainAsync(^{
        refreshKeyMap();
        if (!keyboardLayoutObserver) {
            keyboardLayoutObserver = [NSDistributedNotificationCenter.defaultCenter
                addObserverForName:(__bridge NSString *)kTISNotifySelectedKeyboardInputSourceChanged
                object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) { refreshKeyMap(); }];
        }
    });
}
static uint32_t virtualKey(CGKeyCode code) {
    switch (code) {
        case 36: case 76: return 0x0d; case 48: return 9; case 49: return 0x20;
        case 51: return 8; case 53: return 0x1b; case 61: return 0xa5;
        case 123: return 0x25; case 124: return 0x27; case 125: return 0x28; case 126: return 0x26;
        case 115: return 0x24; case 119: return 0x23; case 116: return 0x21; case 121: return 0x22; case 117: return 0x2e;
        case 122: return 0x70; case 120: return 0x71; case 99: return 0x72; case 118: return 0x73;
        case 96: return 0x74; case 97: return 0x75; case 98: return 0x76; case 100: return 0x77;
        case 101: return 0x78; case 109: return 0x79; case 103: return 0x7a; case 111: return 0x7b;
        default: break;
    }
    return code < 128 ? atomic_load(&keyMap[code]) : 0;
}
static void refreshKeyMap(void) {
    // TISGetInputSourceProperty is main-thread only on Tahoe. The event-tap
    // callback reads only this immutable-at-read-time atomic key map.
    TISInputSourceRef input = TISCopyCurrentKeyboardLayoutInputSource();
    if (!input) return;
    CFDataRef data = TISGetInputSourceProperty(input, kTISPropertyUnicodeKeyLayoutData);
    for (CGKeyCode code = 0; code < 128; code++) {
        uint32_t result = 0, dead = 0;
        UniChar chars[4]; UniCharCount length = 0;
        if (data && UCKeyTranslate((const UCKeyboardLayout *)CFDataGetBytePtr(data), code, kUCKeyActionDown,
            0, LMGetKbdType(), kUCKeyTranslateNoDeadKeysBit, &dead, 4, &length, chars) == noErr && length == 1) {
            UniChar c = chars[0];
            if (c >= 'a' && c <= 'z') c -= 32;
            if ((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) result = c;
        }
        atomic_store(&keyMap[code], result);
    }
    CFRelease(input);
}
static CGEventRef onEvent(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *context) {
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (chordActive && shortcutEdge) shortcutEdge(false);
        chordActive = triggerDown = false;
        CGEventTapEnable(eventTap, true);
        return event;
    }
    if (CGEventGetIntegerValueField(event, kCGEventSourceUserData) == kSpeekEvent) return event;
    if (type == kCGEventKeyDown && CGEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat)) return event;
    uint32_t combo = atomic_load(&shortcutCombo), wanted = combo >> 16;
    CGEventFlags flags = CGEventGetFlags(event);
    uint32_t mask = ((flags & kCGEventFlagMaskControl) ? 1 : 0) | ((flags & kCGEventFlagMaskShift) ? 2 : 0)
        | ((flags & kCGEventFlagMaskAlternate) ? 4 : 0) | ((flags & kCGEventFlagMaskCommand) ? 8 : 0);
    CGKeyCode code = (CGKeyCode)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
    bool down = type == kCGEventKeyDown;
    if (wanted == 0xa5 && code == 61 && type == kCGEventFlagsChanged) {
        down = CGEventSourceKeyState(kCGEventSourceStateCombinedSessionState, 61);
        triggerDown = down;
        mask &= ~4u;
    } else if (wanted && virtualKey(code) == wanted) {
        if (type == kCGEventKeyDown) triggerDown = true;
        if (type == kCGEventKeyUp) triggerDown = false;
    }
    bool active = mask == (combo & 0xffff) && (wanted == 0 || triggerDown);
    // A release of an extra modifier must not restart a cancelled recording.
    bool freshPress = (wanted && down && virtualKey(code) == wanted) || (type == kCGEventFlagsChanged && active && !chordActive &&
        ((code == 59 || code == 62) ? (flags & kCGEventFlagMaskControl) :
         (code == 56 || code == 60) ? (flags & kCGEventFlagMaskShift) :
         (code == 58 || code == 61) ? (flags & kCGEventFlagMaskAlternate) :
         (code == 55 || code == 54) ? (flags & kCGEventFlagMaskCommand) : 0));
    if (active != chordActive && (!active || freshPress)) {
        chordActive = active;
        if (shortcutEdge) shortcutEdge(active);
    }
    return event;
}
int s2m_run_listener(void (*edge)(bool)) {
    @autoreleasepool {
        if (!CGPreflightListenEventAccess() && !AXIsProcessTrusted()) return 0;
        shortcutEdge = edge;
        chordActive = triggerDown = false;
        eventTap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionListenOnly,
            CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventKeyUp) | CGEventMaskBit(kCGEventFlagsChanged), onEvent, NULL);
        if (!eventTap) return -1;
        CFRunLoopSourceRef source = CFMachPortCreateRunLoopSource(NULL, eventTap, 0);
        CFRunLoopAddSource(CFRunLoopGetCurrent(), source, kCFRunLoopCommonModes);
        CGEventTapEnable(eventTap, true);
        atomic_store(&listenerReady, true);
        fprintf(stderr, "[MACOS SHORTCUT] Global keyboard listener ready\n");
        CFRunLoopRun();
        atomic_store(&listenerReady, false);
        CFRunLoopRemoveSource(CFRunLoopGetCurrent(), source, kCFRunLoopCommonModes);
        CFRelease(source); CFRelease(eventTap); eventTap = NULL;
        return 1;
    }
}
static void postKey(CGKeyCode code, bool down, CGEventFlags flags, const UniChar *text, size_t count) {
    CGEventRef event = CGEventCreateKeyboardEvent(NULL, code, down);
    if (!event) return;
    CGEventSetFlags(event, flags);
    CGEventSetIntegerValueField(event, kCGEventSourceUserData, kSpeekEvent);
    if (count) CGEventKeyboardSetUnicodeString(event, count, text);
    CGEventPost(kCGSessionEventTap, event); CFRelease(event);
}
static bool focusedPasswordField(void) {
    AXUIElementRef system = AXUIElementCreateSystemWide();
    CFTypeRef element = NULL, subrole = NULL;
    bool secure = false;
    if (AXUIElementCopyAttributeValue(system, kAXFocusedUIElementAttribute, &element) == kAXErrorSuccess && element) {
        if (AXUIElementCopyAttributeValue((AXUIElementRef)element, kAXSubroleAttribute, &subrole) == kAXErrorSuccess && subrole)
            secure = CFEqual(subrole, kAXSecureTextFieldSubrole);
    }
    if (subrole) CFRelease(subrole);
    if (element) CFRelease(element);
    CFRelease(system);
    return secure;
}
int s2m_chord(bool paste) {
    if (!AXIsProcessTrusted()) return 0;
    if (focusedPasswordField()) return -1;
    CGKeyCode code = paste ? 9 : 8; // macOS Command+V / Command+C.
    postKey(code, true, kCGEventFlagMaskCommand, NULL, 0);
    postKey(code, false, kCGEventFlagMaskCommand, NULL, 0);
    return 1;
}
int s2m_type(const uint16_t *text, size_t count) {
    if (!AXIsProcessTrusted()) return 0;
    if (focusedPasswordField()) return -1;
    for (size_t i = 0; i < count;) {
        size_t n = (text[i] >= 0xd800 && text[i] <= 0xdbff && i + 1 < count) ? 2 : 1;
        if (text[i] == '\r') { i++; continue; }
        if (text[i] == '\n') { postKey(36, true, 0, NULL, 0); postKey(36, false, 0, NULL, 0); }
        else { postKey(0, true, 0, text + i, n); postKey(0, false, 0, text + i, n); }
        i += n;
        usleep(1000);
    }
    return 1;
}
int s2m_key(uint32_t vk, bool down) {
    if (!AXIsProcessTrusted()) return 0;
    CGKeyCode code = vk == 8 ? 51 : vk == 13 ? 36 : 0xffff;
    if (code == 0xffff) return 0;
    postKey(code, down, 0, NULL, 0); return 1;
}
// AppKit owns only the window, redraw scheduling and native context. The shared
// design, waveform animation, colors and CoreText rendering are all Rust.
void s2m_overlay_callbacks(double width, double height,
    void (*draw)(CGContextRef, double, double),
    S2MOverlayPlacement (*layout)(double, double, int, double, double)) {
    mainAsync(^{ surfaceWidth = width; surfaceHeight = height; overlayDraw = draw; overlayPlacement = layout; });
}
@interface S2MPanelView : NSView
@end
@implementation S2MPanelView
- (BOOL)isOpaque { return NO; }
- (void)drawRect:(NSRect)dirtyRect {
    if (overlayDraw) { overlayDraw(NSGraphicsContext.currentContext.CGContext, pillX, pillY); drawFrames++; }
}
@end
static void updatePanel(void) {
    if (!overlayDraw || !overlayPlacement) return;
    if (!panel) {
        panel = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, surfaceWidth, surfaceHeight)
            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
        panel.level = NSStatusWindowLevel;
        panel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
        panel.hidesOnDeactivate = NO; panel.opaque = NO; panel.hasShadow = NO;
        panel.ignoresMouseEvents = YES;
        panel.backgroundColor = NSColor.clearColor;
        panel.contentView = [[S2MPanelView alloc] initWithFrame:NSMakeRect(0, 0, surfaceWidth, surfaceHeight)];
    }
    NSScreen *screen = NSScreen.mainScreen ?: NSScreen.screens.firstObject;
    NSRect frame = screen.visibleFrame;
    S2MOverlayPlacement layout = overlayPlacement(frame.size.width, frame.size.height, placement, customX, customY);
    pillX = layout.pill_x; pillY = layout.pill_y;
    [panel setFrameOrigin:NSMakePoint(NSMinX(frame) + layout.x, NSMaxY(frame) - layout.y - surfaceHeight)];
    [panel.contentView setNeedsDisplay:YES];
    if (overlayState) {
        if (NSApp.isHidden) [NSApp unhideWithoutActivation];
        [panel orderFrontRegardless];
        if (!overlayTimer) {
            overlayTimer = [NSTimer timerWithTimeInterval:1.0/30.0 repeats:YES block:^(NSTimer *timer) {
                [panel.contentView setNeedsDisplay:YES];
            }];
            [NSRunLoop.mainRunLoop addTimer:overlayTimer forMode:NSRunLoopCommonModes];
        }
    } else {
        [overlayTimer invalidate]; overlayTimer = nil;
        [panel.contentView display]; // Clear cached Rust text/layout before hiding.
        [panel orderOut:nil];
    }
}
void s2m_overlay(int state, const char *text) {
    NSString *value = text ? [NSString stringWithUTF8String:text] : @"";
    mainAsync(^{ overlayState = state; previewText = value ?: @""; updatePanel(); });
}
void s2m_overlay_style(int position, double x, double y) {
    mainAsync(^{ placement = position; customX = x; customY = y; if (panel) updatePanel(); });
}
void s2m_chime(bool start) {
    mainAsync(^{ [[NSSound soundNamed:start ? @"Tink" : @"Pop"] play]; });
}
void s2m_notify(const char *title, const char *body) {
    NSString *t = [NSString stringWithUTF8String:title], *b = [NSString stringWithUTF8String:body];
    mainAsync(^{
        UNMutableNotificationContent *content = [UNMutableNotificationContent new];
        content.title = t ?: @"SpeekTo.Me"; content.body = b ?: @"";
        UNUserNotificationCenter *center = UNUserNotificationCenter.currentNotificationCenter;
        [center getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
            if (settings.authorizationStatus == UNAuthorizationStatusAuthorized)
                [center addNotificationRequest:[UNNotificationRequest requestWithIdentifier:NSUUID.UUID.UUIDString content:content trigger:nil] withCompletionHandler:nil];
        }];
    });
}
void s2m_request_notifications(void) {
    mainAsync(^{ [UNUserNotificationCenter.currentNotificationCenter requestAuthorizationWithOptions:UNAuthorizationOptionAlert | UNAuthorizationOptionSound completionHandler:^(BOOL granted, NSError *error) {}]; });
}

#include <sys/file.h>
#include <fcntl.h>
#include <unistd.h>
#import <ServiceManagement/ServiceManagement.h>
bool s2m_claim_instance(const char *path) {
    static int lockFD = -1;
    if (lockFD >= 0) return true;
    int fd = open(path, O_CREAT | O_RDWR, 0600);
    if (fd < 0) return false;
    if (flock(fd, LOCK_EX | LOCK_NB) != 0) { close(fd); return false; }
    lockFD = fd;
    return true;
}
bool s2m_login_item(bool enabled) {
    @autoreleasepool {
        SMAppService *service = SMAppService.mainAppService;
        if (enabled && service.status != SMAppServiceStatusEnabled && service.status != SMAppServiceStatusRequiresApproval)
            return [service registerAndReturnError:nil];
        if (!enabled && (service.status == SMAppServiceStatusEnabled || service.status == SMAppServiceStatusRequiresApproval))
            return [service unregisterAndReturnError:nil];
        return true;
    }
}

// System audio is delivered into the same Rust buffers used by Windows/Linux.
// No screen frames are requested or retained.
@interface S2MAudio : NSObject <SCStreamOutput, SCStreamDelegate>
@property(nonatomic) void (*samples)(const float *, size_t);
@end
@implementation S2MAudio
- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sample ofType:(SCStreamOutputType)type {
    if (type != SCStreamOutputTypeAudio || !CMSampleBufferDataIsReady(sample) || !self.samples) return;
    const AudioStreamBasicDescription *format = CMAudioFormatDescriptionGetStreamBasicDescription(CMSampleBufferGetFormatDescription(sample));
    if (!format || format->mFormatID != kAudioFormatLinearPCM || !(format->mFormatFlags & kAudioFormatFlagIsFloat) || format->mBitsPerChannel != 32) return;
    size_t needed = 0;
    CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(sample, &needed, NULL, 0, NULL, NULL, 0, NULL);
    AudioBufferList *list = malloc(needed);
    if (!list) return;
    CMBlockBufferRef block = NULL;
    OSStatus status = CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(sample, NULL, list, needed, NULL, NULL, kCMSampleBufferFlag_AudioBufferList_Assure16ByteAlignment, &block);
    if (status == noErr && list->mNumberBuffers) {
        size_t frames = CMSampleBufferGetNumSamples(sample);
        size_t channels = format->mChannelsPerFrame;
        if (channels > 0 && channels <= 8 && frames <= 48000) {
            float *stereo = malloc(frames * 2 * sizeof(float));
            if (stereo) {
                bool planar = (format->mFormatFlags & kAudioFormatFlagIsNonInterleaved) != 0;
                bool valid = true;
                for (size_t frame = 0; frame < frames && valid; frame++) {
                    for (size_t channel = 0; channel < 2; channel++) {
                        size_t source = channels == 1 ? 0 : channel;
                        size_t buffer = planar ? source : 0;
                        size_t index = planar ? frame : frame * channels + source;
                        if (buffer >= list->mNumberBuffers || !list->mBuffers[buffer].mData || (index + 1) * sizeof(float) > list->mBuffers[buffer].mDataByteSize) { valid = false; break; }
                        stereo[frame * 2 + channel] = ((float *)list->mBuffers[buffer].mData)[index];
                    }
                }
                if (valid) self.samples(stereo, frames * 2);
                free(stereo);
            }
        }
    }
    if (block) CFRelease(block);
    free(list);
}
- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
    NSLog(@"[SpeekTo.Me] System audio capture stopped: %@", error.localizedDescription);
}
@end
static SCStream *audioStream;
static S2MAudio *audioOutput;
static _Atomic(uint64_t) audioGeneration;
void s2m_audio_stop(void) {
    atomic_fetch_add(&audioGeneration, 1);
    @synchronized([S2MAudio class]) {
        SCStream *stream = audioStream;
        audioStream = nil; audioOutput = nil;
        if (stream) [stream stopCaptureWithCompletionHandler:^(NSError *error) {}];
    }
}
int s2m_audio_start(void (*samples)(const float *, size_t)) {
    @autoreleasepool {
        if ([NSThread isMainThread]) return -1;
        if (!CGPreflightScreenCaptureAccess()) { CGRequestScreenCaptureAccess(); return 0; }
        uint64_t generation = atomic_fetch_add(&audioGeneration, 1) + 1;
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        __block int result = -1;
        [SCShareableContent getShareableContentExcludingDesktopWindows:YES onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *content, NSError *error) {
            if (error || !content.displays.count || atomic_load(&audioGeneration) != generation) { dispatch_semaphore_signal(done); return; }
            SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:content.displays.firstObject excludingWindows:@[]];
            SCStreamConfiguration *config = [SCStreamConfiguration new];
            config.width = 2; config.height = 2; config.minimumFrameInterval = CMTimeMake(1, 1);
            config.capturesAudio = YES; config.sampleRate = 48000; config.channelCount = 2;
            config.excludesCurrentProcessAudio = YES;
            S2MAudio *output = [S2MAudio new]; output.samples = samples;
            SCStream *stream = [[SCStream alloc] initWithFilter:filter configuration:config delegate:output];
            NSError *addError = nil;
            if (![stream addStreamOutput:output type:SCStreamOutputTypeAudio sampleHandlerQueue:dispatch_queue_create("me.speekto.system-audio", DISPATCH_QUEUE_SERIAL) error:&addError]) { dispatch_semaphore_signal(done); return; }
            [stream startCaptureWithCompletionHandler:^(NSError *startError) {
                @synchronized([S2MAudio class]) {
                    if (!startError && atomic_load(&audioGeneration) == generation) {
                        audioStream = stream; audioOutput = output; result = 1;
                    } else { [stream stopCaptureWithCompletionHandler:^(NSError *error) {}]; }
                }
                dispatch_semaphore_signal(done);
            }];
        }];
        if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 15 * NSEC_PER_SEC)) != 0) {
            atomic_fetch_add(&audioGeneration, 1); return -2;
        }
        return result;
    }
}

char *s2m_overlay_snapshot(void) {
    __block char *result = NULL;
    dispatch_block_t block = ^{
        NSDictionary *info = @{
            @"visible": @(panel.isVisible), @"state": @(overlayState),
            @"text": previewText ?: @"",
            @"renderer": overlayDraw ? @"Rust/CoreGraphics/CoreText" : @"none",
            @"width": @(surfaceWidth), @"height": @(surfaceHeight),
            @"pill_x": @(pillX), @"pill_y": @(pillY),
            @"draw_frames": @(drawFrames), @"animation_active": @(overlayTimer != nil), @"panel_class": panel ? NSStringFromClass(panel.class) : @"none",
            @"content_class": panel ? NSStringFromClass(panel.contentView.class) : @"none",
            @"takes_focus": @(panel.canBecomeKeyWindow),
            @"bundle_id": NSBundle.mainBundle.bundleIdentifier ?: @"unbundled"
        };
        NSData *data = [NSJSONSerialization dataWithJSONObject:info options:0 error:nil];
        result = strdup([[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String);
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return result;
}
void s2m_free(char *value) { free(value); }

bool s2m_save_overlay_image(const char *path) {
    NSString *destination = [NSString stringWithUTF8String:path];
    __block bool saved = false;
    dispatch_block_t block = ^{
        if (!panel || !panel.isVisible) return;
        NSView *view = panel.contentView;
        NSBitmapImageRep *image = [view bitmapImageRepForCachingDisplayInRect:view.bounds];
        [view cacheDisplayInRect:view.bounds toBitmapImageRep:image];
        NSData *png = [image representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        saved = [png writeToFile:destination atomically:YES];
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return saved;
}
