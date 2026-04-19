//go:build native && darwin && cgo

#import <Cocoa/Cocoa.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <QuartzCore/QuartzCore.h>
#import <dispatch/dispatch.h>
#import <objc/runtime.h>
#import <string.h>

void llrdc_window_callback(void* renderer, int eventType, int data1, int data2, char* error);
void llrdc_input_callback(void* renderer, char* jsonMsg);
void llrdc_present_callback(void* renderer, int width, int height, uint32_t ts);

typedef void (*WindowEventCallback)(void* renderer, int eventType, int data1, int data2, char* error);
typedef void (*InputEventCallback)(void* renderer, char* jsonMsg);
typedef void (*PresentEventCallback)(void* renderer, int width, int height, uint32_t ts);
typedef struct {
    void* bytes;
    size_t len;
    char* error;
} llrdc_png_result;

int llrdc_test_mouse_payload(double contentW, double contentH, double videoW, double videoH, double pointX, double pointYFromTop, double* outX, double* outY, double* outFrameH);

@interface LLrdcView : NSView
@property (nonatomic, strong) AVSampleBufferDisplayLayer *videoLayer;
@property (nonatomic, strong) CATextLayer *hudLayer;
@property (nonatomic, strong) CALayer *menuBackgroundLayer;
@property (nonatomic, strong) CATextLayer *menuTitleLayer;
@property (nonatomic, strong) CATextLayer *menuHintLayer;
@property (nonatomic, strong) CATextLayer *menuItemsLayer;
@property (nonatomic, strong) CALayer *debugCursorLayer;
@property (nonatomic, assign) void* renderer;
@property (nonatomic, assign) InputEventCallback inputCallback;
@property (nonatomic, assign) WindowEventCallback windowCallback;
@property (nonatomic, assign) BOOL clicked;
@property (nonatomic, assign) BOOL autoStart;
@property (nonatomic, assign) BOOL debugCursorEnabled;
@property (nonatomic, assign) CGPoint debugCursorPoint;
@property (nonatomic, assign) NSSize videoContentSize;
@property (nonatomic, assign) NSSize remoteTargetSize;
@property (nonatomic, assign) NSInteger menuItemCount;
- (NSDictionary *)mouseMovePayloadForEvent:(NSEvent *)event;
- (NSDictionary *)mouseButtonPayloadForEvent:(NSEvent *)event button:(NSNumber *)button action:(NSString *)action;
- (void)sendInput:(NSDictionary*)dict;
- (void)updateDebugCursor;
@end

@interface LLrdcWindowDelegate : NSObject <NSWindowDelegate>
@property (nonatomic, assign) void* renderer;
@property (nonatomic, assign) WindowEventCallback callback;
@end

static NSSize llrdc_aligned_size(NSSize size) {
    NSInteger width = (NSInteger)llround(size.width);
    NSInteger height = (NSInteger)llround(size.height);
    if (width >= 8) {
        width = (width / 8) * 8;
    }
    if (height >= 8) {
        height = (height / 8) * 8;
    }
    return NSMakeSize(width, height);
}

static NSSize llrdc_target_size_for_content(NSSize contentSize, NSSize aspectSize) {
    // Simply align the contentSize to 8 pixels to match the server's requirements,
    // allowing the remote desktop to adopt the window's aspect ratio.
    return llrdc_aligned_size(contentSize);
}

int llrdc_test_mouse_payload(double contentW, double contentH, double videoW, double videoH, double pointX, double pointYFromTop, double* outX, double* outY, double* outFrameH) {
    @autoreleasepool {
        [NSApplication sharedApplication];

        NSRect contentRect = NSMakeRect(0, 0, contentW, contentH);
        NSWindow *window = [[NSWindow alloc] initWithContentRect:contentRect
                                                       styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskResizable | NSWindowStyleMaskMiniaturizable)
                                                         backing:NSBackingStoreBuffered
                                                           defer:NO];
        LLrdcView *view = [[LLrdcView alloc] initWithFrame:contentRect];
        view.videoContentSize = NSMakeSize(videoW, videoH);
        [window setContentView:view];
        [window layoutIfNeeded];

        NSPoint viewPoint = NSMakePoint(pointX, contentH - pointYFromTop);
        NSPoint windowPoint = [view convertPoint:viewPoint toView:nil];
        NSEvent *event = [NSEvent mouseEventWithType:NSEventTypeMouseMoved
                                            location:windowPoint
                                       modifierFlags:0
                                           timestamp:0
                                        windowNumber:[window windowNumber]
                                             context:nil
                                         eventNumber:0
                                          clickCount:0
                                            pressure:0];
        NSDictionary *payload = [view mouseMovePayloadForEvent:event];
        if (payload == nil) {
            return 0;
        }

        NSNumber *x = payload[@"x"];
        NSNumber *y = payload[@"y"];
        if (x == nil || y == nil) {
            return 0;
        }

        if (outX != NULL) {
            *outX = [x doubleValue];
        }
        if (outY != NULL) {
            *outY = [y doubleValue];
        }
        if (outFrameH != NULL) {
            *outFrameH = window.frame.size.height;
        }
        return 1;
    }
}

@implementation LLrdcWindowDelegate
- (void)windowWillClose:(NSNotification *)notification {
    if (self.callback) self.callback(self.renderer, 13, 0, 0, NULL);
}

- (void)windowDidResize:(NSNotification *)notification {
    NSWindow *window = [notification object];
    NSSize size = [window contentRectForFrameRect:[window frame]].size;
    CGFloat scale = [window backingScaleFactor];
    NSSize pixelSize = NSMakeSize(size.width * scale, size.height * scale);
    LLrdcView *view = (LLrdcView *)[window contentView];
    if (view != nil) {
        NSSize aspectSize = view.videoContentSize.width > 0 && view.videoContentSize.height > 0 ? view.videoContentSize : view.remoteTargetSize;
        NSSize targetSize = llrdc_target_size_for_content(pixelSize, aspectSize);
        if ((NSInteger)llround(targetSize.width) > 0 && (NSInteger)llround(targetSize.height) > 0 &&
            ((NSInteger)llround(targetSize.width) != (NSInteger)llround(view.remoteTargetSize.width) ||
             (NSInteger)llround(targetSize.height) != (NSInteger)llround(view.remoteTargetSize.height))) {
            view.remoteTargetSize = targetSize;
            [view sendInput:@{@"type": @"resize", @"width": @((int)targetSize.width), @"height": @((int)targetSize.height)}];
        }
    }
    if (self.callback) self.callback(self.renderer, 5, (int)size.width, (int)size.height, NULL);
}

- (void)windowDidBecomeKey:(NSNotification *)notification {
    if (self.callback) self.callback(self.renderer, 2, 0, 0, NULL);
}

- (void)windowDidExpose:(NSNotification *)notification {
    if (self.callback) self.callback(self.renderer, 3, 0, 0, NULL);
}
@end

static struct {
    void* renderer;
    WindowEventCallback winCb;
    InputEventCallback inCb;
    PresentEventCallback presentCb;
    char* title;
    int w;
    int h;
    int autoStart;
    LLrdcView* view;
    NSWindow* window;
    CMVideoFormatDescriptionRef formatDesc;
    NSData* spsData;
    NSData* ppsData;
} g_app_state;

static void llrdc_reset_video_state_locked(void) {
    if (g_app_state.view && g_app_state.view.videoLayer) {
        AVSampleBufferVideoRenderer *renderer = g_app_state.view.videoLayer.sampleBufferRenderer;
        [renderer flushWithRemovalOfDisplayedImage:YES completionHandler:nil];
        g_app_state.view.videoLayer.hidden = NO;
    }
    if (g_app_state.formatDesc) {
        CFRelease(g_app_state.formatDesc);
        g_app_state.formatDesc = NULL;
    }
}

static BOOL llrdc_update_parameter_sets(NSData *spsData, NSData *ppsData) {
    BOOL changed = NO;

    if (spsData != nil && ![g_app_state.spsData isEqualToData:spsData]) {
        g_app_state.spsData = spsData;
        changed = YES;
    }
    if (ppsData != nil && ![g_app_state.ppsData isEqualToData:ppsData]) {
        g_app_state.ppsData = ppsData;
        changed = YES;
    }

    if (!changed) {
        return YES;
    }

    if (g_app_state.formatDesc) {
        CFRelease(g_app_state.formatDesc);
        g_app_state.formatDesc = NULL;
    }

    if (!g_app_state.spsData || !g_app_state.ppsData) {
        return NO;
    }

    const uint8_t* parameterSetPointers[2] = {
        (const uint8_t*)g_app_state.spsData.bytes,
        (const uint8_t*)g_app_state.ppsData.bytes,
    };
    size_t parameterSetSizes[2] = {
        g_app_state.spsData.length,
        g_app_state.ppsData.length,
    };
    OSStatus status = CMVideoFormatDescriptionCreateFromH264ParameterSets(
        NULL,
        2,
        parameterSetPointers,
        parameterSetSizes,
        4,
        &g_app_state.formatDesc
    );
    return status == noErr;
}

@interface LLrdcAppDelegate : NSObject <NSApplicationDelegate>
@end

@implementation LLrdcAppDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

    NSRect frame = NSMakeRect(0, 0, g_app_state.w, g_app_state.h);
    g_app_state.window = [[NSWindow alloc] initWithContentRect:frame
                                                     styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskResizable | NSWindowStyleMaskMiniaturizable)
                                                       backing:NSBackingStoreBuffered
                                                         defer:NO];
    [g_app_state.window setTitle:[NSString stringWithUTF8String:g_app_state.title]];
    [g_app_state.window setReleasedWhenClosed:NO];
    [g_app_state.window setBackgroundColor:[NSColor blackColor]];
    [g_app_state.window center];

    LLrdcWindowDelegate *winDelegate = [[LLrdcWindowDelegate alloc] init];
    winDelegate.renderer = g_app_state.renderer;
    winDelegate.callback = g_app_state.winCb;
    [g_app_state.window setDelegate:winDelegate];
    objc_setAssociatedObject(g_app_state.window, "winDelegate", winDelegate, OBJC_ASSOCIATION_RETAIN_NONATOMIC);

    g_app_state.view = [[LLrdcView alloc] initWithFrame:frame];
    g_app_state.view.renderer = g_app_state.renderer;
    g_app_state.view.inputCallback = g_app_state.inCb;
    g_app_state.view.windowCallback = g_app_state.winCb;
    g_app_state.view.autoStart = (g_app_state.autoStart != 0);
    g_app_state.view.videoContentSize = NSMakeSize(g_app_state.w, g_app_state.h);
    g_app_state.view.remoteTargetSize = llrdc_aligned_size(NSMakeSize(g_app_state.w, g_app_state.h));
    if (g_app_state.view.autoStart) {
        g_app_state.view.clicked = YES;
        g_app_state.view.videoLayer.hidden = NO;
    }
    [g_app_state.window setContentView:g_app_state.view];
    [g_app_state.window makeFirstResponder:g_app_state.view];
    [g_app_state.window makeKeyAndOrderFront:nil];
    [g_app_state.window orderFrontRegardless];
    [g_app_state.view setNeedsDisplay:YES];
    [NSApp activateIgnoringOtherApps:YES];

    NSSize contentSize = [g_app_state.window contentRectForFrameRect:[g_app_state.window frame]].size;
    CGFloat scale = [g_app_state.window backingScaleFactor];
    if (g_app_state.winCb) g_app_state.winCb(g_app_state.renderer, 5, (int)(contentSize.width * scale), (int)(contentSize.height * scale), NULL);
    if (g_app_state.winCb) g_app_state.winCb(g_app_state.renderer, 1, 0, 0, NULL);
    if (g_app_state.view.autoStart && g_app_state.winCb) g_app_state.winCb(g_app_state.renderer, 20, 0, 0, NULL);
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)theApplication {
    return YES;
}
@end

static NSString* nseventToDOMKey(NSEvent *event) {
    unsigned short code = [event keyCode];
    switch (code) {
        case 0x00: return @"KeyA"; case 0x0B: return @"KeyB"; case 0x08: return @"KeyC"; case 0x02: return @"KeyD";
        case 0x0E: return @"KeyE"; case 0x03: return @"KeyF"; case 0x05: return @"KeyG"; case 0x04: return @"KeyH";
        case 0x22: return @"KeyI"; case 0x26: return @"KeyJ"; case 0x28: return @"KeyK"; case 0x25: return @"KeyL";
        case 0x2E: return @"KeyM"; case 0x2D: return @"KeyN"; case 0x1F: return @"KeyO"; case 0x23: return @"KeyP";
        case 0x0C: return @"KeyQ"; case 0x0F: return @"KeyR"; case 0x01: return @"KeyS"; case 0x11: return @"KeyT";
        case 0x20: return @"KeyU"; case 0x09: return @"KeyV"; case 0x0D: return @"KeyW"; case 0x07: return @"KeyX";
        case 0x10: return @"KeyY"; case 0x06: return @"KeyZ";
        case 0x12: return @"Digit1"; case 0x13: return @"Digit2"; case 0x14: return @"Digit3"; case 0x15: return @"Digit4";
        case 0x17: return @"Digit5"; case 0x16: return @"Digit6"; case 0x1A: return @"Digit7"; case 0x1C: return @"Digit8";
        case 0x19: return @"Digit9"; case 0x1D: return @"Digit0";
        case 0x31: return @"Space"; case 0x24: return @"Enter"; case 0x35: return @"Escape"; case 0x33: return @"Backspace";
        case 0x30: return @"Tab"; case 0x2B: return @"Comma";
        case 0x7E: return @"ArrowUp"; case 0x7D: return @"ArrowDown"; case 0x7B: return @"ArrowLeft"; case 0x7C: return @"ArrowRight";
        case 0x7A: return @"F1";
        case 0x3B: return @"ControlLeft"; case 0x38: return @"ShiftLeft"; case 0x3A: return @"AltLeft"; case 0x37: return @"MetaLeft";
        default: return nil;
    }
}

@implementation LLrdcView
- (instancetype)initWithFrame:(NSRect)frameRect {
    self = [super initWithFrame:frameRect];
    if (self) {
        self.wantsLayer = YES;
        self.videoLayer = [[AVSampleBufferDisplayLayer alloc] init];
        self.videoLayer.frame = self.bounds;
        self.videoLayer.videoGravity = AVLayerVideoGravityResize;
        self.videoLayer.hidden = YES;
        [self.layer addSublayer:self.videoLayer];

        self.hudLayer = [[CATextLayer alloc] init];
        self.hudLayer.fontSize = 12;
        self.hudLayer.foregroundColor = [NSColor colorWithDeviceRed:68/255.0 green:255/255.0 blue:68/255.0 alpha:1.0].CGColor;
        self.hudLayer.backgroundColor = [NSColor colorWithDeviceRed:0 green:0 blue:0 alpha:0.55].CGColor;
        self.hudLayer.cornerRadius = 4.0;
        self.hudLayer.contentsScale = [[NSScreen mainScreen] backingScaleFactor];
        self.hudLayer.hidden = YES;
        self.hudLayer.alignmentMode = kCAAlignmentLeft;
        self.hudLayer.wrapped = YES;
        [self.layer addSublayer:self.hudLayer];

        self.menuBackgroundLayer = [[CALayer alloc] init];
        self.menuBackgroundLayer.backgroundColor = [NSColor colorWithDeviceRed:12/255.0 green:14/255.0 blue:18/255.0 alpha:0.88].CGColor;
        self.menuBackgroundLayer.borderColor = [NSColor colorWithDeviceRed:96/255.0 green:124/255.0 blue:255/255.0 alpha:1.0].CGColor;
        self.menuBackgroundLayer.borderWidth = 1.0;
        self.menuBackgroundLayer.cornerRadius = 12.0;
        self.menuBackgroundLayer.hidden = YES;
        [self.layer addSublayer:self.menuBackgroundLayer];

        self.menuTitleLayer = [[CATextLayer alloc] init];
        self.menuTitleLayer.fontSize = 22;
        self.menuTitleLayer.foregroundColor = [NSColor whiteColor].CGColor;
        self.menuTitleLayer.contentsScale = [[NSScreen mainScreen] backingScaleFactor];
        self.menuTitleLayer.hidden = YES;
        [self.layer addSublayer:self.menuTitleLayer];

        self.menuHintLayer = [[CATextLayer alloc] init];
        self.menuHintLayer.fontSize = 11;
        self.menuHintLayer.foregroundColor = [NSColor colorWithDeviceRed:180/255.0 green:188/255.0 blue:204/255.0 alpha:1.0].CGColor;
        self.menuHintLayer.contentsScale = [[NSScreen mainScreen] backingScaleFactor];
        self.menuHintLayer.wrapped = YES;
        self.menuHintLayer.hidden = YES;
        [self.layer addSublayer:self.menuHintLayer];

        self.menuItemsLayer = [[CATextLayer alloc] init];
        self.menuItemsLayer.fontSize = 14;
        self.menuItemsLayer.foregroundColor = [NSColor colorWithDeviceRed:240/255.0 green:244/255.0 blue:255/255.0 alpha:1.0].CGColor;
        self.menuItemsLayer.contentsScale = [[NSScreen mainScreen] backingScaleFactor];
        self.menuItemsLayer.wrapped = YES;
        self.menuItemsLayer.hidden = YES;
        [self.layer addSublayer:self.menuItemsLayer];

        self.debugCursorLayer = [[CALayer alloc] init];
        self.debugCursorLayer.backgroundColor = [NSColor colorWithDeviceRed:1.0 green:0.0 blue:0.0 alpha:0.95].CGColor;
        self.debugCursorLayer.cornerRadius = 5.0;
        self.debugCursorLayer.hidden = YES;
        [self.layer addSublayer:self.debugCursorLayer];

        NSTrackingAreaOptions options = (NSTrackingActiveAlways | NSTrackingInVisibleRect | NSTrackingMouseEnteredAndExited | NSTrackingMouseMoved);
        NSTrackingArea *area = [[NSTrackingArea alloc] initWithRect:[self bounds] options:options owner:self userInfo:nil];
        [self addTrackingArea:area];
    }
    return self;
}

- (void)setFrame:(NSRect)frame {
    [super setFrame:frame];
    [self layout];
}

- (void)layout {
    [super layout];
    self.videoLayer.frame = self.bounds;
    self.videoLayer.contentsScale = self.window.backingScaleFactor;

    CGFloat scale = self.window.backingScaleFactor > 0 ? self.window.backingScaleFactor : [[NSScreen mainScreen] backingScaleFactor];
    CGFloat margin = 10.0;
    CGFloat width = 500.0; // Fixed width for right-alignment logic
    if (width > self.bounds.size.width - 2 * margin) {
        width = self.bounds.size.width - 2 * margin;
    }
    self.hudLayer.frame = NSMakeRect(margin, self.bounds.size.height - 36.0, width, 24.0);
    self.hudLayer.contentsScale = scale;

    CGFloat panelWidth = MIN(self.bounds.size.width - 40.0, 640.0);
    if (panelWidth < 280.0) {
        panelWidth = self.bounds.size.width;
    }
    NSInteger itemCount = self.menuItemCount > 0 ? self.menuItemCount : 1;
    CGFloat panelHeight = MIN(self.bounds.size.height - 40.0, MAX(160.0, 108.0 + itemCount * 22.0));
    if (panelHeight < 120.0) {
        panelHeight = self.bounds.size.height;
    }
    CGFloat panelX = (self.bounds.size.width - panelWidth) / 2.0;
    CGFloat panelY = (self.bounds.size.height - panelHeight) / 2.0;
    self.menuBackgroundLayer.frame = NSMakeRect(panelX, panelY, panelWidth, panelHeight);
    self.menuTitleLayer.frame = NSMakeRect(panelX + 18.0, panelY + panelHeight - 46.0, panelWidth - 36.0, 28.0);
    self.menuHintLayer.frame = NSMakeRect(panelX + 18.0, panelY + panelHeight - 86.0, panelWidth - 36.0, 34.0);
    CGFloat itemsHeight = MAX(22.0, itemCount * 22.0);
    CGFloat itemsTop = panelY + 86.0;
    CGFloat itemsBottom = self.bounds.size.height - (itemsTop + itemsHeight);
    self.menuItemsLayer.frame = NSMakeRect(panelX + 18.0, itemsBottom, panelWidth - 36.0, itemsHeight);

    [self updateDebugCursor];
}

- (void)updateDebugCursor {
    if (!self.debugCursorEnabled) {
        self.debugCursorLayer.hidden = YES;
        return;
    }
    CGFloat x = self.debugCursorPoint.x * self.bounds.size.width;
    CGFloat y = (1.0 - self.debugCursorPoint.y) * self.bounds.size.height;
    self.debugCursorLayer.hidden = NO;
    self.debugCursorLayer.frame = NSMakeRect(x - 5.0, y - 5.0, 10.0, 10.0);
}

- (void)drawRect:(NSRect)dirtyRect {
    if (self.clicked || self.autoStart) {
        return;
    }

    [[NSColor colorWithDeviceRed:24/255.0 green:24/255.0 blue:28/255.0 alpha:1.0] setFill];
    NSRectFill(self.bounds);

    CGFloat bw = 240, bh = 100;
    NSRect btnRect = NSMakeRect((self.bounds.size.width - bw)/2, (self.bounds.size.height - bh)/2, bw, bh);

    [[NSColor colorWithDeviceRed:60/255.0 green:60/255.0 blue:75/255.0 alpha:1.0] setFill];
    NSBezierPath *path = [NSBezierPath bezierPathWithRect:btnRect];
    [path fill];

    [[NSColor colorWithDeviceRed:120/255.0 green:120/255.0 blue:140/255.0 alpha:1.0] setStroke];
    [path setLineWidth:2.0];
    [path stroke];

    [[NSColor whiteColor] setFill];
    CGFloat side = 40;
    NSBezierPath *tri = [NSBezierPath bezierPath];
    NSPoint center = NSMakePoint(self.bounds.size.width/2, self.bounds.size.height/2);
    [tri moveToPoint:NSMakePoint(center.x - side/3, center.y + side/2)];
    [tri lineToPoint:NSMakePoint(center.x - side/3, center.y - side/2)];
    [tri lineToPoint:NSMakePoint(center.x + side*2/3, center.y)];
    [tri closePath];
    [tri fill];
}

- (BOOL)acceptsFirstResponder { return YES; }

- (void)sendInput:(NSDictionary*)dict {
    if (!self.inputCallback || (!self.clicked && !self.autoStart)) return;
    NSError *error;
    NSData *jsonData = [NSJSONSerialization dataWithJSONObject:dict options:0 error:&error];
    if (jsonData) {
        NSString *jsonString = [[NSString alloc] initWithData:jsonData encoding:NSUTF8StringEncoding];
        self.inputCallback(self.renderer, (char *)[jsonString UTF8String]);
    }
}

- (NSRect)currentVideoRect {
    return self.bounds;
}

- (NSDictionary *)mouseMovePayloadForEvent:(NSEvent *)event {
    NSPoint location = [self convertPoint:[event locationInWindow] fromView:nil];
    NSRect bounds = self.bounds;
    if (bounds.size.width <= 0 || bounds.size.height <= 0) {
        return nil;
    }

    // Since videoGravity is AVLayerVideoGravityResize, the video fills the bounds.
    // Map points directly to 0.0-1.0 range.
    double x = location.x / bounds.size.width;
    double y = 1.0 - (location.y / bounds.size.height);

    if (x < 0.0) x = 0.0;
    if (x > 1.0) x = 1.0;
    if (y < 0.0) y = 0.0;
    if (y > 1.0) y = 1.0;

    return @{@"type": @"mousemove", @"x": @(x), @"y": @(y)};
}

- (void)sendMouseMoveForEvent:(NSEvent *)event {
    NSDictionary *payload = [self mouseMovePayloadForEvent:event];
    if (payload != nil) {
        [self sendInput:payload];
    }
}

- (NSDictionary *)mouseButtonPayloadForEvent:(NSEvent *)event button:(NSNumber *)button action:(NSString *)action {
    NSMutableDictionary *payload = [NSMutableDictionary dictionaryWithDictionary:@{
        @"type": @"mousebtn",
        @"button": button,
        @"action": action,
    }];
    NSDictionary *movePayload = [self mouseMovePayloadForEvent:event];
    if (movePayload[@"x"] != nil && movePayload[@"y"] != nil) {
        payload[@"x"] = movePayload[@"x"];
        payload[@"y"] = movePayload[@"y"];
    }
    return payload;
}

- (void)mouseMoved:(NSEvent *)event {
    [self sendMouseMoveForEvent:event];
}

- (void)mouseDragged:(NSEvent *)event {
    [self sendMouseMoveForEvent:event];
}

- (void)rightMouseDragged:(NSEvent *)event {
    [self sendMouseMoveForEvent:event];
}

- (void)mouseDown:(NSEvent *)event {
    if (!self.clicked && !self.autoStart) {
        self.clicked = YES;
        self.videoLayer.hidden = NO;
        [self setNeedsDisplay:YES];
        if (self.windowCallback) self.windowCallback(self.renderer, 20, 0, 0, NULL);
        return;
    }
    [self sendMouseMoveForEvent:event];
    [self sendInput:[self mouseButtonPayloadForEvent:event button:@0 action:@"mousedown"]];
}

- (void)mouseUp:(NSEvent *)event { [self sendInput:[self mouseButtonPayloadForEvent:event button:@0 action:@"mouseup"]]; }
- (void)rightMouseDown:(NSEvent *)event {
    [self sendMouseMoveForEvent:event];
    [self sendInput:[self mouseButtonPayloadForEvent:event button:@2 action:@"mousedown"]];
}
- (void)rightMouseUp:(NSEvent *)event { [self sendInput:[self mouseButtonPayloadForEvent:event button:@2 action:@"mouseup"]]; }

- (void)scrollWheel:(NSEvent *)event {
    [self sendInput:@{@"type": @"wheel", @"deltaX": @([event scrollingDeltaX] * 10), @"deltaY": @(-[event scrollingDeltaY] * 10)}];
}

- (void)keyDown:(NSEvent *)event {
    NSString *key = nseventToDOMKey(event);
    if (key) [self sendInput:@{@"type": @"keydown", @"key": key}];
}

- (void)keyUp:(NSEvent *)event {
    NSString *key = nseventToDOMKey(event);
    if (key) [self sendInput:@{@"type": @"keyup", @"key": key}];
}
@end

void* llrdc_init_app(void* renderer, WindowEventCallback winCb, InputEventCallback inCb, PresentEventCallback presentCb, const char* title, int w, int h, int autoStart) {
    g_app_state.renderer = renderer;
    g_app_state.winCb = winCb;
    g_app_state.inCb = inCb;
    g_app_state.presentCb = presentCb;
    g_app_state.title = strdup(title);
    g_app_state.w = w;
    g_app_state.h = h;
    g_app_state.autoStart = autoStart;
    g_app_state.formatDesc = NULL;
    g_app_state.spsData = nil;
    g_app_state.ppsData = nil;
    return NULL;
}

void llrdc_enqueue_h264(void* renderer, const uint8_t* data, size_t size, uint32_t ts, const uint8_t* sps, size_t spsSize, const uint8_t* pps, size_t ppsSize) {
    if (!data || size == 0) {
        return;
    }

    NSData *sampleData = [[NSData alloc] initWithBytes:data length:size];
    NSData *spsData = nil;
    NSData *ppsData = nil;
    if (sps && spsSize > 0) {
        spsData = [[NSData alloc] initWithBytes:sps length:spsSize];
    }
    if (pps && ppsSize > 0) {
        ppsData = [[NSData alloc] initWithBytes:pps length:ppsSize];
    }

    dispatch_async(dispatch_get_main_queue(), ^{
        LLrdcView *view = g_app_state.view;
        if (!view || !view.videoLayer) {
            return;
        }
        AVSampleBufferVideoRenderer *renderer = view.videoLayer.sampleBufferRenderer;

        if (!renderer.readyForMoreMediaData) {
            [renderer flushWithRemovalOfDisplayedImage:NO completionHandler:nil];
        }
        if (renderer.status == AVQueuedSampleBufferRenderingStatusFailed) {
            [renderer flushWithRemovalOfDisplayedImage:YES completionHandler:nil];
        }

        if (!llrdc_update_parameter_sets(spsData, ppsData) || !g_app_state.formatDesc) {
            return;
        }

        CMBlockBufferRef blockBuffer = NULL;
        void *memory = malloc(sampleData.length);
        if (!memory) {
            return;
        }
        memcpy(memory, sampleData.bytes, sampleData.length);
        OSStatus status = CMBlockBufferCreateWithMemoryBlock(
            NULL,
            memory,
            sampleData.length,
            kCFAllocatorMalloc,
            NULL,
            0,
            sampleData.length,
            0,
            &blockBuffer
        );
        if (status != kCMBlockBufferNoErr) {
            free(memory);
            return;
        }

        CMSampleTimingInfo timingInfo = {
            .duration = kCMTimeInvalid,
            .presentationTimeStamp = CMTimeMake(ts, 90000),
            .decodeTimeStamp = kCMTimeInvalid,
        };
        size_t sampleSize = sampleData.length;
        CMSampleBufferRef sampleBuffer = NULL;
        status = CMSampleBufferCreate(
            NULL,
            blockBuffer,
            true,
            NULL,
            NULL,
            g_app_state.formatDesc,
            1,
            1,
            &timingInfo,
            1,
            &sampleSize,
            &sampleBuffer
        );
        if (status != noErr) {
            CFRelease(blockBuffer);
            return;
        }

        CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, YES);
        if (attachments && CFArrayGetCount(attachments) > 0) {
            CFMutableDictionaryRef dict = (CFMutableDictionaryRef)CFArrayGetValueAtIndex(attachments, 0);
            CFDictionarySetValue(dict, kCMSampleAttachmentKey_DisplayImmediately, kCFBooleanTrue);
        }

        view.videoLayer.hidden = NO;
        CGSize presentationSize = CMVideoFormatDescriptionGetPresentationDimensions(g_app_state.formatDesc, true, true);
        CMVideoDimensions dimensions = CMVideoFormatDescriptionGetDimensions(g_app_state.formatDesc);
        if (presentationSize.width <= 0 || presentationSize.height <= 0) {
            presentationSize = CGSizeMake(dimensions.width, dimensions.height);
        }
        if (presentationSize.width > 0 && presentationSize.height > 0) {
            view.videoContentSize = NSMakeSize(presentationSize.width, presentationSize.height);
        }
        [renderer enqueueSampleBuffer:sampleBuffer];
        if (g_app_state.presentCb) {
            g_app_state.presentCb(g_app_state.renderer, (int)lround(presentationSize.width), (int)lround(presentationSize.height), ts);
        }

        CFRelease(sampleBuffer);
        CFRelease(blockBuffer);
    });
}

void llrdc_reset_video() {
    dispatch_async(dispatch_get_main_queue(), ^{
        llrdc_reset_video_state_locked();
    });
}

void llrdc_set_overlay_state(const char* hudText, int hudR, int hudG, int hudB, int hudA, int menuVisible, const char* menuTitle, const char* menuHint, const char* menuItems) {
    NSString *hud = hudText ? [NSString stringWithUTF8String:hudText] : @"";
    NSString *title = menuTitle ? [NSString stringWithUTF8String:menuTitle] : @"";
    NSString *hint = menuHint ? [NSString stringWithUTF8String:menuHint] : @"";
    NSString *items = menuItems ? [NSString stringWithUTF8String:menuItems] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        LLrdcView *view = g_app_state.view;
        if (!view) {
            return;
        }
        view.hudLayer.string = hud;
        view.hudLayer.hidden = (hud.length == 0);
        view.hudLayer.foregroundColor = [NSColor colorWithDeviceRed:hudR/255.0 green:hudG/255.0 blue:hudB/255.0 alpha:hudA/255.0].CGColor;

        BOOL showMenu = menuVisible != 0;
        view.menuBackgroundLayer.hidden = !showMenu;
        view.menuTitleLayer.hidden = !showMenu;
        view.menuHintLayer.hidden = !showMenu;
        view.menuItemsLayer.hidden = !showMenu;
        view.menuTitleLayer.string = title;
        view.menuHintLayer.string = hint;
        view.menuItemsLayer.string = items;
        view.menuItemCount = items.length > 0 ? [[items componentsSeparatedByString:@"\n"] count] : 0;
        [view setNeedsLayout:YES];
        [view layoutSubtreeIfNeeded];
    });
}

void llrdc_set_debug_cursor(int enabled) {
    dispatch_async(dispatch_get_main_queue(), ^{
        LLrdcView *view = g_app_state.view;
        if (!view) {
            return;
        }
        view.debugCursorEnabled = (enabled != 0);
        [view updateDebugCursor];
    });
}

void llrdc_set_mouse_position(double x, double y) {
    dispatch_async(dispatch_get_main_queue(), ^{
        LLrdcView *view = g_app_state.view;
        if (!view) {
            return;
        }
        view.debugCursorPoint = CGPointMake(x, y);
        [view updateDebugCursor];
    });
}

void llrdc_set_window_size(int w, int h) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!g_app_state.window) {
            return;
        }
        [g_app_state.window setContentSize:NSMakeSize(w, h)];
    });
}

llrdc_png_result llrdc_capture_png() {
    __block llrdc_png_result result = {0};
    dispatch_sync(dispatch_get_main_queue(), ^{
        LLrdcView *view = g_app_state.view;
        if (!view) {
            result.error = strdup("view unavailable");
            return;
        }
        NSRect bounds = [view bounds];
        NSBitmapImageRep *rep = [view bitmapImageRepForCachingDisplayInRect:bounds];
        if (!rep) {
            result.error = strdup("bitmap capture failed");
            return;
        }
        [view cacheDisplayInRect:bounds toBitmapImageRep:rep];
        NSData *data = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        if (!data || data.length == 0) {
            result.error = strdup("png encoding failed");
            return;
        }
        void *copy = malloc(data.length);
        if (!copy) {
            result.error = strdup("png allocation failed");
            return;
        }
        memcpy(copy, data.bytes, data.length);
        result.bytes = copy;
        result.len = data.length;
    });
    return result;
}

void llrdc_free_png_result(llrdc_png_result result) {
    if (result.bytes) {
        free(result.bytes);
    }
    if (result.error) {
        free(result.error);
    }
}

void llrdc_run_app() {
    [NSApplication sharedApplication];

    LLrdcAppDelegate *delegate = [[LLrdcAppDelegate alloc] init];
    [NSApp setDelegate:delegate];
    objc_setAssociatedObject(NSApp, "appDelegate", delegate, OBJC_ASSOCIATION_RETAIN_NONATOMIC);

    [NSApp run];
}

void llrdc_stop_app() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        NSEvent* event = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                            location:NSMakePoint(0,0)
                                       modifierFlags:0
                                           timestamp:0
                                        windowNumber:0
                                             context:nil
                                             subtype:0
                                               data1:0
                                               data2:0];
        [NSApp postEvent:event atStart:YES];
    });
}
