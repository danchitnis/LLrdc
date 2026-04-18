//go:build native && linux && cgo

package client

/*
#cgo pkg-config: vpx sdl2
#include <vpx/vpx_decoder.h>
#include <vpx/vp8dx.h>
#include <vpx/vpx_image.h>
#include <SDL2/SDL.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct {
        vpx_codec_ctx_t ctx;
        int initialized;
} llrdc_vpx_decoder;

static int llrdc_vpx_init(llrdc_vpx_decoder* decoder) {
        if (decoder->initialized) {
                return 0;
        }
        if (vpx_codec_dec_init(&decoder->ctx, vpx_codec_vp8_dx(), NULL, 0) != VPX_CODEC_OK) {
                return -1;
        }
        decoder->initialized = 1;
        return 0;
}

static int llrdc_vpx_decode(llrdc_vpx_decoder* decoder, const unsigned char* data, unsigned int size) {
        if (!decoder->initialized) {
                return -1;
        }
        return vpx_codec_decode(&decoder->ctx, data, size, NULL, 0);
}

static vpx_image_t* llrdc_vpx_get_frame(llrdc_vpx_decoder* decoder, vpx_codec_iter_t* iter) {
        if (!decoder->initialized) {
                return NULL;
        }
        return vpx_codec_get_frame(&decoder->ctx, iter);
}

static const char* llrdc_vpx_error(llrdc_vpx_decoder* decoder) {
        if (!decoder->initialized) {
                return "decoder not initialized";
        }
        return vpx_codec_error(&decoder->ctx);
}

static void llrdc_vpx_close(llrdc_vpx_decoder* decoder) {
        if (!decoder->initialized) {
                return;
        }
        vpx_codec_destroy(&decoder->ctx);
        decoder->initialized = 0;
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
)

type NativeRenderer struct {
	title        string
	width        int
	height       int
	autoStart    bool
	probeLatency bool
	debugCursor  bool

	mu                      sync.RWMutex
	runStarted              bool
	decoderAwaitingKeyframe bool
	inputSink               func(map[string]any) error
	lifecycle               func(NativeWindowLifecycle)
	present                 func(NativeFramePresented)
	samples                 chan nativeVideoSample
	streamResets            chan string
	stopCh                  chan struct{}
	doneCh                  chan struct{}

	mouseX int32
	mouseY int32
}

type nativeVideoSample struct {
	codec           string
	data            []byte
	packetTimestamp uint32
	receiveAt       time.Time
}

type nativeDecodedSample struct {
	frame           decodedFrame
	packetTimestamp uint32
	receiveAt       time.Time
	decodeReadyAt   time.Time
}

type vp8Decoder struct {
	raw C.llrdc_vpx_decoder
}

func NewNativeRenderer(opts NativeRendererOptions) (WindowRenderer, error) {
	width := opts.Width
	if width <= 0 {
		width = 1280
	}
	height := opts.Height
	if height <= 0 {
		height = 720
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "LLrdc Native Client"
	}
	return &NativeRenderer{
		title:                   title,
		width:                   width,
		height:                  height,
		autoStart:               opts.AutoStart,
		probeLatency:            opts.ProbeLatency,
		debugCursor:             opts.DebugCursor,
		decoderAwaitingKeyframe: true,
		samples:                 make(chan nativeVideoSample, 8),
		streamResets:            make(chan string, 1),
		stopCh:                  make(chan struct{}),
		doneCh:                  make(chan struct{}),
	}, nil
}

func (r *NativeRenderer) SetInputSink(fn func(map[string]any) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputSink = fn
}

func (r *NativeRenderer) SetLifecycleSink(fn func(NativeWindowLifecycle)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lifecycle = fn
}

func (r *NativeRenderer) SetPresentSink(fn func(NativeFramePresented)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.present = fn
}

func (r *NativeRenderer) SetStatusText(text string) {}

func (r *NativeRenderer) Size() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.width, r.height
}

func (r *NativeRenderer) ResetVideoStream(codec string) {
	if !strings.Contains(strings.ToLower(codec), "vp8") {
		return
	}
	select {
	case r.streamResets <- codec:
	default:
		select {
		case <-r.streamResets:
		default:
		}
		r.streamResets <- codec
	}
}

func (r *NativeRenderer) HandleVideoFrame(codec string, frame []byte, packetTimestamp uint32) error {
	if !strings.Contains(strings.ToLower(codec), "vp8") {
		return fmt.Errorf("native renderer currently supports VP8 only, got %s", codec)
	}
	sample := nativeVideoSample{
		codec:           codec,
		data:            append([]byte(nil), frame...),
		packetTimestamp: packetTimestamp,
		receiveAt:       time.Now(),
	}
	select {
	case r.samples <- sample:
		return nil
	default:
		select {
		case <-r.samples:
		default:
		}
		r.samples <- sample
		return nil
	}
}

func (r *NativeRenderer) RequestKeyframe() {
	r.mu.Lock()
	r.decoderAwaitingKeyframe = true
	r.mu.Unlock()
	r.emitLifecycle(NativeWindowLifecycle{DecoderStateChanged: true, DecoderAwaitingKeyframe: true})
}

func (r *NativeRenderer) Close() error {
	r.Stop()
	return nil
}

func (r *NativeRenderer) Stop() {
	select {
	case <-r.stopCh:
		r.mu.RLock()
		runStarted := r.runStarted
		r.mu.RUnlock()
		if runStarted {
			<-r.doneCh
		}
		return
	default:
		close(r.stopCh)
	}
	r.mu.RLock()
	runStarted := r.runStarted
	r.mu.RUnlock()
	if !runStarted {
		return
	}
	<-r.doneCh
}

func (r *NativeRenderer) drawClickToStart(renderer *sdl.Renderer) {
	var w, h int32
	if outputW, outputH, err := renderer.GetOutputSize(); err == nil {
		w, h = outputW, outputH
	} else {
		w, h = int32(r.width), int32(r.height)
	}

	_ = renderer.SetDrawColor(24, 24, 28, 255)
	_ = renderer.Clear()

	bw, bh := int32(240), int32(100)
	bx, by := w/2-bw/2, h/2-bh/2

	// Button background
	_ = renderer.SetDrawColor(60, 60, 75, 255)
	rect := sdl.Rect{X: bx, Y: by, W: bw, H: bh}
	_ = renderer.FillRect(&rect)
	_ = renderer.SetDrawColor(120, 120, 140, 255)
	_ = renderer.DrawRect(&rect)

	// Play triangle (pointing right)
	_ = renderer.SetDrawColor(255, 255, 255, 255)
	side := int32(40)
	tx := w/2 - side/3
	ty := h / 2
	for i := int32(0); i < side; i++ {
		halfH := (side - i) / 2
		_ = renderer.DrawLine(tx+i, ty-halfH, tx+i, ty+halfH)
	}

	renderer.Present()
}

func (r *NativeRenderer) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(r.doneCh)
	r.mu.Lock()
	r.runStarted = true
	r.mu.Unlock()

	// Essential Wayland stability hints for Ubuntu 25.04
	sdl.SetHint("SDL_VIDEO_WAYLAND_ALLOW_LIBDECOR", "1")
	sdl.SetHint("SDL_VIDEO_X11_NET_WM_BYPASS_COMPOSITOR", "0")
	sdl.SetHint("SDL_APP_NAME", "LLrdc")
	sdl.SetHint("SDL_VIDEO_WAYLAND_WM_CLASS", "llrdc-client")

	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		r.emitLifecycle(NativeWindowLifecycle{Error: fmt.Sprintf("sdl init: %v", err)})
		return fmt.Errorf("sdl init: %w", err)
	}
	defer sdl.Quit()

	driver, _ := sdl.GetCurrentVideoDriver()

	// Create window
	window, err := sdl.CreateWindow(
		r.title,
		sdl.WINDOWPOS_CENTERED,
		sdl.WINDOWPOS_CENTERED,
		int32(r.width),
		int32(r.height),
		sdl.WINDOW_SHOWN|sdl.WINDOW_RESIZABLE,
	)
	if err != nil {
		r.emitLifecycle(NativeWindowLifecycle{Error: fmt.Sprintf("create window: %v", err)})
		return fmt.Errorf("create window: %w", err)
	}
	defer window.Destroy()

	// Center and show
	window.SetPosition(sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED)
	window.Raise()

	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED|sdl.RENDERER_PRESENTVSYNC)
	if err != nil {
		renderer, err = sdl.CreateRenderer(window, -1, sdl.RENDERER_SOFTWARE)
		if err != nil {
			r.emitLifecycle(NativeWindowLifecycle{Error: fmt.Sprintf("create renderer: %v", err)})
			return fmt.Errorf("create renderer: %w", err)
		}
	}
	defer renderer.Destroy()

	windowState := nativeWindowState{
		created: true,
		shown:   true,
	}
	if driver != "" {
		windowState.backend = driver
		r.emitLifecycle(windowState.snapshot("created", true))
	}

	// Click to Start Loop
	clicked := r.autoStart
	for !clicked {
		r.drawClickToStart(renderer)

		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch e := event.(type) {
			case *sdl.QuitEvent:
				return nil
			case *sdl.MouseButtonEvent:
				if e.Type == sdl.MOUSEBUTTONDOWN && e.Button == sdl.BUTTON_LEFT {
					clicked = true
					fmt.Println("DEBUG: BUTTON CLICKED - STARTING STREAM")
				}
			case *sdl.WindowEvent:
				if e.Event == sdl.WINDOWEVENT_CLOSE {
					return nil
				}
				windowState.applyEvent(e.Event)
				lifecycle := windowState.snapshot(windowEventName(e.Event), false)
				if e.Event == sdl.WINDOWEVENT_SIZE_CHANGED {
					r.mu.Lock()
					r.width = int(e.Data1)
					r.height = int(e.Data2)
					r.mu.Unlock()
					lifecycle.Width = int(e.Data1)
					lifecycle.Height = int(e.Data2)
				}
				r.emitLifecycle(lifecycle)
			}
		}

		select {
		case <-r.stopCh:
			return nil
		case <-time.After(16 * time.Millisecond):
		}
	}

	// Once clicked, notify main.go to start the session
	r.emitLifecycle(windowState.snapshot("started", false))
	r.emitLifecycle(NativeWindowLifecycle{RenderLoopStarted: true})

	decodedFrames := make(chan nativeDecodedSample, 2)
	go func() {
		decoder := &vp8Decoder{}
		if err := decoder.Init(); err != nil {
			r.emitLifecycle(NativeWindowLifecycle{Error: fmt.Sprintf("decoder init: %v", err)})
			return
		}
		defer decoder.Close()

		r.mu.Lock()
		r.decoderAwaitingKeyframe = true
		r.mu.Unlock()

		for {
			select {
			case <-r.stopCh:
				return
			case <-r.streamResets:
				decoder.Close()
				decoder = &vp8Decoder{}
				_ = decoder.Init()
				r.mu.Lock()
				r.decoderAwaitingKeyframe = true
				r.mu.Unlock()
			case sample, ok := <-r.samples:
				if !ok {
					return
				}
				r.mu.RLock()
				awaiting := r.decoderAwaitingKeyframe
				r.mu.RUnlock()

				if awaiting {
					if !isVP8Keyframe(sample.data) {
						continue
					}
					r.mu.Lock()
					r.decoderAwaitingKeyframe = false
					r.mu.Unlock()
					r.emitLifecycle(NativeWindowLifecycle{DecoderStateChanged: true, DecoderAwaitingKeyframe: false})
				}
				frame, err := decoder.Decode(sample.data)
				decodeReadyAt := time.Now()
				if err != nil {
					r.mu.Lock()
					r.decoderAwaitingKeyframe = true
					r.mu.Unlock()
					select {
					case r.streamResets <- "decode_error":
					default:
					}
					r.emitLifecycle(NativeWindowLifecycle{DecodeError: true, DecoderStateChanged: true, DecoderAwaitingKeyframe: true})
					continue
				}
				if frame.width > 0 && frame.height > 0 {
					select {
					case decodedFrames <- nativeDecodedSample{
						frame:           frame,
						packetTimestamp: sample.packetTimestamp,
						receiveAt:       sample.receiveAt,
						decodeReadyAt:   decodeReadyAt,
					}:
					default:
					}
				}
			}
		}
	}()

	var texture *sdl.Texture
	var textureWidth, textureHeight int32
	defer func() {
		if texture != nil {
			texture.Destroy()
		}
	}()

	for {
		select {
		case <-r.stopCh:
			return nil
		default:
		}

		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch e := event.(type) {
			case *sdl.QuitEvent:
				return nil
			case *sdl.MouseMotionEvent:
				r.mu.Lock()
				w, h := r.width, r.height
				r.mouseX = e.X
				r.mouseY = e.Y
				r.mu.Unlock()
				if w > 0 && h > 0 {
					r.sendInput(map[string]any{
						"type": "mousemove",
						"x":    float64(e.X) / float64(w),
						"y":    float64(e.Y) / float64(h),
					})
				}
			case *sdl.MouseButtonEvent:
				action := "mousedown"
				if e.Type == sdl.MOUSEBUTTONUP {
					action = "mouseup"
				}
				r.sendInput(map[string]any{
					"type":   "mousebtn",
					"button": sdlButtonToDOM(e.Button),
					"action": action,
				})
			case *sdl.MouseWheelEvent:
				r.sendInput(map[string]any{
					"type":   "wheel",
					"deltaX": float64(e.X) * 100,
					"deltaY": float64(-e.Y) * 100,
				})
			case *sdl.KeyboardEvent:
				if e.Repeat == 0 {
					action := "keydown"
					if e.Type == sdl.KEYUP {
						action = "keyup"
					}
					keyName := sdlScancodeToDOM(e.Keysym.Scancode)
					if keyName != "" {
						r.sendInput(map[string]any{
							"type": action,
							"key":  keyName,
						})
					}
				}
			case *sdl.WindowEvent:
				if e.Event == sdl.WINDOWEVENT_CLOSE {
					return nil
				}
				windowState.applyEvent(e.Event)
				lifecycle := windowState.snapshot(windowEventName(e.Event), false)
				if e.Event == sdl.WINDOWEVENT_SIZE_CHANGED {
					r.mu.Lock()
					r.width = int(e.Data1)
					r.height = int(e.Data2)
					r.mu.Unlock()
					lifecycle.Width = int(e.Data1)
					lifecycle.Height = int(e.Data2)
					r.sendInput(map[string]any{
						"type":   "resize",
						"width":  int(e.Data1),
						"height": int(e.Data2),
					})
				}
				r.emitLifecycle(lifecycle)
			}
		}

		select {
		case decoded := <-decodedFrames:
			frame := decoded.frame
			if texture == nil || textureWidth != frame.width || textureHeight != frame.height {
				if texture != nil {
					texture.Destroy()
				}
				var err error
				texture, err = renderer.CreateTexture(uint32(sdl.PIXELFORMAT_IYUV), sdl.TEXTUREACCESS_STREAMING, frame.width, frame.height)
				if err != nil {
					return fmt.Errorf("create texture: %w", err)
				}
				textureWidth, textureHeight = frame.width, frame.height
			}
			_ = texture.UpdateYUV(nil, frame.yPlane, int(frame.yStride), frame.uPlane, int(frame.uStride), frame.vPlane, int(frame.vStride))
			_ = renderer.Clear()
			brightness := -1
			if r.probeLatency {
				// Compute center pixel brightness (Y channel)
				cx := int(decoded.frame.width / 2)
				cy := int(decoded.frame.height / 2)
				offset := cy*int(decoded.frame.yStride) + cx
				if offset >= 0 && offset < len(decoded.frame.yPlane) {
					brightness = int(decoded.frame.yPlane[offset])
				}
			}

			// Ensure the renderer's logical size matches the window size so drawing coordinates match mouse coordinates
			r.mu.RLock()
			ww, wh := r.width, r.height
			r.mu.RUnlock()

			if ww > 0 && wh > 0 {
				lw, lh := renderer.GetLogicalSize()
				if lw != int32(ww) || lh != int32(wh) {
					_ = renderer.SetLogicalSize(int32(ww), int32(wh))
				}
			}

			_ = renderer.Copy(texture, nil, nil)

			if r.debugCursor {
				r.mu.RLock()
				mx, my := r.mouseX, r.mouseY
				r.mu.RUnlock()

				_ = renderer.SetDrawColor(255, 0, 0, 255)
				_ = renderer.FillRect(&sdl.Rect{X: mx - 5, Y: my - 5, W: 10, H: 10})
			}

			renderer.Present()
			r.emitPresent(NativeFramePresented{
				Width:           int(decoded.frame.width),
				Height:          int(decoded.frame.height),
				PacketTimestamp: decoded.packetTimestamp,
				Brightness:      brightness,
				ReceiveAt:       decoded.receiveAt,
				DecodeReadyAt:   decoded.decodeReadyAt,
				PresentationAt:  time.Now(),
			})

		case <-time.After(10 * time.Millisecond):
		case <-r.stopCh:
			return nil
		}
	}
}

func (r *NativeRenderer) emitLifecycle(event NativeWindowLifecycle) {
	r.mu.RLock()
	fn := r.lifecycle
	awaiting := r.decoderAwaitingKeyframe
	r.mu.RUnlock()

	event.DecoderAwaitingKeyframe = awaiting
	if fn != nil {
		fn(event)
	}
}

func (r *NativeRenderer) emitPresent(event NativeFramePresented) {
	r.mu.RLock()
	fn := r.present
	r.mu.RUnlock()
	if fn != nil {
		fn(event)
	}
}

type nativeWindowState struct {
	backend    string
	windowID   uint64
	created    bool
	shown      bool
	mapped     bool
	visible    bool
	hasFocus   bool
	hasSurface bool
	flags      uint32
	desktop    int
}

func (s *nativeWindowState) applyEvent(event uint8) {
	switch event {
	case sdl.WINDOWEVENT_SHOWN, sdl.WINDOWEVENT_EXPOSED, sdl.WINDOWEVENT_RESTORED, sdl.WINDOWEVENT_MAXIMIZED:
		s.mapped = true
		s.hasSurface = true
		s.visible = true
		s.shown = true
		s.flags |= sdl.WINDOW_SHOWN
	case sdl.WINDOWEVENT_HIDDEN, sdl.WINDOWEVENT_MINIMIZED:
		s.hasSurface = false
		s.visible = false
		s.shown = false
		s.flags &^= sdl.WINDOW_SHOWN
	case sdl.WINDOWEVENT_CLOSE:
		s.mapped = false
		s.visible = false
		s.shown = false
		s.hasSurface = false
	case sdl.WINDOWEVENT_FOCUS_GAINED, sdl.WINDOWEVENT_ENTER:
		s.hasFocus = true
		s.flags |= sdl.WINDOW_INPUT_FOCUS
	case sdl.WINDOWEVENT_FOCUS_LOST, sdl.WINDOWEVENT_LEAVE:
		s.hasFocus = false
		s.flags &^= sdl.WINDOW_INPUT_FOCUS
	}
}

func (s nativeWindowState) snapshot(event string, created bool) NativeWindowLifecycle {
	return NativeWindowLifecycle{
		Backend:    s.backend,
		WindowID:   s.windowID,
		Created:    created,
		Shown:      s.shown,
		Mapped:     s.mapped,
		Visible:    s.visible,
		Event:      event,
		Flags:      s.flags,
		HasFocus:   s.hasFocus,
		HasSurface: s.hasSurface,
		Desktop:    s.desktop,
	}
}

func windowEventName(event uint8) string {
	switch event {
	case sdl.WINDOWEVENT_SHOWN:
		return "shown"
	case sdl.WINDOWEVENT_HIDDEN:
		return "hidden"
	case sdl.WINDOWEVENT_EXPOSED:
		return "exposed"
	case sdl.WINDOWEVENT_MOVED:
		return "moved"
	case sdl.WINDOWEVENT_RESIZED:
		return "resized"
	case sdl.WINDOWEVENT_SIZE_CHANGED:
		return "size_changed"
	case sdl.WINDOWEVENT_MINIMIZED:
		return "minimized"
	case sdl.WINDOWEVENT_MAXIMIZED:
		return "maximized"
	case sdl.WINDOWEVENT_RESTORED:
		return "restored"
	case sdl.WINDOWEVENT_ENTER:
		return "enter"
	case sdl.WINDOWEVENT_LEAVE:
		return "leave"
	case sdl.WINDOWEVENT_FOCUS_GAINED:
		return "focus_gained"
	case sdl.WINDOWEVENT_FOCUS_LOST:
		return "focus_lost"
	case sdl.WINDOWEVENT_CLOSE:
		return "close"
	default:
		return fmt.Sprintf("event_%d", event)
	}
}

func (r *NativeRenderer) UpdateMouse(x, y float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mouseX = int32(x * float64(r.width))
	r.mouseY = int32(y * float64(r.height))
}

func (r *NativeRenderer) sendInput(msg map[string]any) {
	r.mu.RLock()
	fn := r.inputSink
	r.mu.RUnlock()
	if fn != nil {
		_ = fn(msg)
	}
}

type decodedFrame struct {
	width   int32
	height  int32
	yPlane  []byte
	uPlane  []byte
	vPlane  []byte
	yStride int32
	uStride int32
	vStride int32
}

func (d *vp8Decoder) Init() error {
	if rc := C.llrdc_vpx_init(&d.raw); rc != 0 {
		return fmt.Errorf("init vp8 decoder: %d", int(rc))
	}
	return nil
}

func (d *vp8Decoder) Decode(data []byte) (decodedFrame, error) {
	if len(data) == 0 {
		return decodedFrame{}, nil
	}
	if rc := C.llrdc_vpx_decode(&d.raw, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))); rc != 0 {
		return decodedFrame{}, fmt.Errorf("decode vp8 frame: %s", C.GoString(C.llrdc_vpx_error(&d.raw)))
	}
	var iter C.vpx_codec_iter_t
	img := C.llrdc_vpx_get_frame(&d.raw, &iter)
	if img == nil {
		return decodedFrame{}, nil
	}

	width := int32(img.d_w)
	height := int32(img.d_h)
	yStride := int32(img.stride[0])
	uStride := int32(img.stride[1])
	vStride := int32(img.stride[2])

	return decodedFrame{
		width:   width,
		height:  height,
		yPlane:  C.GoBytes(unsafe.Pointer(img.planes[0]), C.int(yStride*height)),
		uPlane:  C.GoBytes(unsafe.Pointer(img.planes[1]), C.int(uStride*((height+1)/2))),
		vPlane:  C.GoBytes(unsafe.Pointer(img.planes[2]), C.int(vStride*((height+1)/2))),
		yStride: yStride,
		uStride: uStride,
		vStride: vStride,
	}, nil
}

func (d *vp8Decoder) Close() {
	C.llrdc_vpx_close(&d.raw)
}

func isVP8Keyframe(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return data[0]&0x01 == 0
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func sdlButtonToDOM(button uint8) int {
	switch button {
	case sdl.BUTTON_LEFT:
		return 0
	case sdl.BUTTON_MIDDLE:
		return 1
	case sdl.BUTTON_RIGHT:
		return 2
	default:
		return 0
	}
}

func sdlScancodeToDOM(code sdl.Scancode) string {
	switch {
	case code >= sdl.SCANCODE_A && code <= sdl.SCANCODE_Z:
		return "Key" + string(rune('A'+(code-sdl.SCANCODE_A)))
	case code >= sdl.SCANCODE_1 && code <= sdl.SCANCODE_9:
		return "Digit" + string(rune('1'+(code-sdl.SCANCODE_1)))
	}

	switch code {
	case sdl.SCANCODE_0:
		return "Digit0"
	case sdl.SCANCODE_RETURN:
		return "Enter"
	case sdl.SCANCODE_ESCAPE:
		return "Escape"
	case sdl.SCANCODE_BACKSPACE:
		return "Backspace"
	case sdl.SCANCODE_TAB:
		return "Tab"
	case sdl.SCANCODE_SPACE:
		return "Space"
	case sdl.SCANCODE_MINUS:
		return "Minus"
	case sdl.SCANCODE_EQUALS:
		return "Equal"
	case sdl.SCANCODE_LEFTBRACKET:
		return "BracketLeft"
	case sdl.SCANCODE_RIGHTBRACKET:
		return "BracketRight"
	case sdl.SCANCODE_BACKSLASH:
		return "Backslash"
	case sdl.SCANCODE_SEMICOLON:
		return "Semicolon"
	case sdl.SCANCODE_APOSTROPHE:
		return "Quote"
	case sdl.SCANCODE_GRAVE:
		return "Backquote"
	case sdl.SCANCODE_COMMA:
		return "Comma"
	case sdl.SCANCODE_PERIOD:
		return "Period"
	case sdl.SCANCODE_SLASH:
		return "Slash"
	case sdl.SCANCODE_CAPSLOCK:
		return "CapsLock"
	case sdl.SCANCODE_F1:
		return "F1"
	case sdl.SCANCODE_F2:
		return "F2"
	case sdl.SCANCODE_F3:
		return "F3"
	case sdl.SCANCODE_F4:
		return "F4"
	case sdl.SCANCODE_F5:
		return "F5"
	case sdl.SCANCODE_F6:
		return "F6"
	case sdl.SCANCODE_F7:
		return "F7"
	case sdl.SCANCODE_F8:
		return "F8"
	case sdl.SCANCODE_F9:
		return "F9"
	case sdl.SCANCODE_F10:
		return "F10"
	case sdl.SCANCODE_F11:
		return "F11"
	case sdl.SCANCODE_F12:
		return "F12"
	case sdl.SCANCODE_PRINTSCREEN:
		return "PrintScreen"
	case sdl.SCANCODE_SCROLLLOCK:
		return "ScrollLock"
	case sdl.SCANCODE_PAUSE:
		return "Pause"
	case sdl.SCANCODE_INSERT:
		return "Insert"
	case sdl.SCANCODE_HOME:
		return "Home"
	case sdl.SCANCODE_PAGEUP:
		return "PageUp"
	case sdl.SCANCODE_DELETE:
		return "Delete"
	case sdl.SCANCODE_END:
		return "End"
	case sdl.SCANCODE_PAGEDOWN:
		return "PageDown"
	case sdl.SCANCODE_RIGHT:
		return "ArrowRight"
	case sdl.SCANCODE_LEFT:
		return "ArrowLeft"
	case sdl.SCANCODE_DOWN:
		return "ArrowDown"
	case sdl.SCANCODE_UP:
		return "ArrowUp"
	case sdl.SCANCODE_LCTRL:
		return "ControlLeft"
	case sdl.SCANCODE_LSHIFT:
		return "ShiftLeft"
	case sdl.SCANCODE_LALT:
		return "AltLeft"
	case sdl.SCANCODE_LGUI:
		return "MetaLeft"
	case sdl.SCANCODE_RCTRL:
		return "ControlRight"
	case sdl.SCANCODE_RSHIFT:
		return "ShiftRight"
	case sdl.SCANCODE_RALT:
		return "AltRight"
	case sdl.SCANCODE_RGUI:
		return "MetaRight"
	default:
		return ""
	}
}
