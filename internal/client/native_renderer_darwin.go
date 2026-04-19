//go:build native && darwin && cgo

package client

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework QuartzCore

#include <stdint.h>
#include <stdlib.h>

typedef void (*WindowEventCallback)(void* renderer, int eventType, int data1, int data2, char* error);
typedef void (*InputEventCallback)(void* renderer, char* jsonMsg);
typedef void (*PresentEventCallback)(void* renderer, int width, int height, uint32_t ts);

typedef struct {
	void* bytes;
	size_t len;
	char* error;
} llrdc_png_result;

void* llrdc_init_app(void* renderer, WindowEventCallback winCb, InputEventCallback inCb, PresentEventCallback presentCb, const char* title, int w, int h, int autoStart);
void llrdc_enqueue_h264(void* renderer, const uint8_t* data, size_t size, uint32_t ts, const uint8_t* sps, size_t spsSize, const uint8_t* pps, size_t ppsSize);
void llrdc_reset_video();
void llrdc_set_overlay_state(const char* hudText, int hudR, int hudG, int hudB, int hudA, int menuVisible, const char* menuTitle, const char* menuHint, const char* menuItems);
void llrdc_set_debug_cursor(int enabled);
void llrdc_set_mouse_position(double x, double y);
void llrdc_set_window_size(int w, int h);
llrdc_png_result llrdc_capture_png();
void llrdc_free_png_result(llrdc_png_result result);
void llrdc_run_app();
void llrdc_stop_app();

void llrdc_window_callback(void* renderer, int eventType, int data1, int data2, char* error);
void llrdc_input_callback(void* renderer, char* jsonMsg);
void llrdc_present_callback(void* renderer, int width, int height, uint32_t ts);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	rendererRegistry = make(map[uintptr]*NativeRenderer)
	rendererMu       sync.RWMutex
	rendererCounter  uintptr
)

func registerRenderer(r *NativeRenderer) uintptr {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	rendererCounter++
	rendererRegistry[rendererCounter] = r
	return rendererCounter
}

func unregisterRenderer(id uintptr) {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	delete(rendererRegistry, id)
}

func getRenderer(ptr uintptr) *NativeRenderer {
	rendererMu.RLock()
	defer rendererMu.RUnlock()
	return rendererRegistry[ptr]
}

type NativeRenderer struct {
	title     string
	width     int
	height    int
	autoStart bool

	mu           sync.RWMutex
	inputSink    func(map[string]any) error
	lifecycle    func(NativeWindowLifecycle)
	present      func(NativeFramePresented)
	overlay      OverlayState
	latencyProbe bool
	debugCursor  bool

	sps    []byte
	pps    []byte
	stopCh chan struct{}
	doneCh chan struct{}
}

func NewNativeRenderer(opts NativeRendererOptions) (WindowRenderer, error) {
	return &NativeRenderer{
		title:        opts.Title,
		width:        opts.Width,
		height:       opts.Height,
		autoStart:    opts.AutoStart,
		latencyProbe: opts.ProbeLatency,
		debugCursor:  opts.DebugCursor,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
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

func (r *NativeRenderer) SetOverlayState(state OverlayState) {
	r.mu.Lock()
	r.overlay = cloneOverlayState(state)
	r.mu.Unlock()

	hudText := strings.Join(state.HUDLines, "\n")
	menuItems := strings.Join(state.MenuItems, "\n")
	cHUD := C.CString(hudText)
	cTitle := C.CString(state.MenuTitle)
	cHint := C.CString(state.MenuHint)
	cItems := C.CString(menuItems)
	defer C.free(unsafe.Pointer(cHUD))
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cHint))
	defer C.free(unsafe.Pointer(cItems))
	C.llrdc_set_overlay_state(
		cHUD,
		C.int(state.HUDColor.R),
		C.int(state.HUDColor.G),
		C.int(state.HUDColor.B),
		C.int(state.HUDColor.A),
		C.int(boolToInt(state.MenuVisible)),
		cTitle,
		cHint,
		cItems,
	)
}

func (r *NativeRenderer) SetLatencyProbe(enabled bool) {
	r.mu.Lock()
	r.latencyProbe = enabled
	r.mu.Unlock()
}

func (r *NativeRenderer) SetDebugCursor(enabled bool) {
	r.mu.Lock()
	r.debugCursor = enabled
	r.mu.Unlock()
	C.llrdc_set_debug_cursor(C.int(boolToInt(enabled)))
}

func (r *NativeRenderer) SetWindowSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid window size %dx%d", width, height)
	}
	r.mu.Lock()
	r.width = width
	r.height = height
	r.mu.Unlock()
	C.llrdc_set_window_size(C.int(width), C.int(height))
	return nil
}

func (r *NativeRenderer) CaptureSnapshotPNG() ([]byte, error) {
	result := C.llrdc_capture_png()
	defer C.llrdc_free_png_result(result)
	if result.error != nil {
		return nil, errors.New(C.GoString(result.error))
	}
	if result.bytes == nil || result.len == 0 {
		return nil, fmt.Errorf("snapshot unavailable")
	}
	return C.GoBytes(result.bytes, C.int(result.len)), nil
}

func (r *NativeRenderer) Size() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.width, r.height
}

func (r *NativeRenderer) PreferredVideoCodec() string {
	return "h264"
}

func (r *NativeRenderer) SupportsWebSocketVideoFallback() bool {
	return true
}

func (r *NativeRenderer) ResetVideoStream(codec string) {
	if !strings.Contains(strings.ToLower(codec), "h264") {
		return
	}
	r.mu.Lock()
	r.sps = nil
	r.pps = nil
	r.mu.Unlock()
	C.llrdc_reset_video()
}

func (r *NativeRenderer) HandleVideoFrame(codec string, frame []byte, packetTimestamp uint32) error {
	if !strings.Contains(strings.ToLower(codec), "h264") {
		return fmt.Errorf("macOS native renderer requires H.264, got %s", codec)
	}
	if len(frame) == 0 {
		return nil
	}

	unit, err := buildH264AccessUnit(frame)
	if err != nil {
		return err
	}

	r.mu.Lock()
	if len(unit.SPS) > 0 {
		r.sps = append(r.sps[:0], unit.SPS...)
	}
	if len(unit.PPS) > 0 {
		r.pps = append(r.pps[:0], unit.PPS...)
	}
	currentSPS := append([]byte(nil), r.sps...)
	currentPPS := append([]byte(nil), r.pps...)
	r.mu.Unlock()

	var spsPtr *C.uint8_t
	if len(currentSPS) > 0 {
		spsPtr = (*C.uint8_t)(unsafe.Pointer(&currentSPS[0]))
	}
	var ppsPtr *C.uint8_t
	if len(currentPPS) > 0 {
		ppsPtr = (*C.uint8_t)(unsafe.Pointer(&currentPPS[0]))
	}

	C.llrdc_enqueue_h264(
		nil,
		(*C.uint8_t)(unsafe.Pointer(&unit.AVCC[0])),
		C.size_t(len(unit.AVCC)),
		C.uint32_t(packetTimestamp),
		spsPtr,
		C.size_t(len(currentSPS)),
		ppsPtr,
		C.size_t(len(currentPPS)),
	)
	return nil
}

func (r *NativeRenderer) RequestKeyframe() {
	C.llrdc_reset_video()
}

func (r *NativeRenderer) Close() error {
	r.Stop()
	return nil
}

func (r *NativeRenderer) Stop() {
	select {
	case <-r.stopCh:
		return
	default:
		close(r.stopCh)
	}
	C.llrdc_stop_app()
}

func (r *NativeRenderer) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(r.doneCh)

	title := C.CString(r.title)
	defer C.free(unsafe.Pointer(title))

	autoStart := 0
	if r.autoStart {
		autoStart = 1
	}

	id := registerRenderer(r)
	defer unregisterRenderer(id)

	C.llrdc_init_app(
		unsafe.Pointer(id),
		(C.WindowEventCallback)(C.llrdc_window_callback),
		(C.InputEventCallback)(C.llrdc_input_callback),
		(C.PresentEventCallback)(C.llrdc_present_callback),
		title,
		C.int(r.width),
		C.int(r.height),
		C.int(autoStart),
	)

	C.llrdc_run_app()
	return nil
}

func (r *NativeRenderer) emitLifecycle(event NativeWindowLifecycle) {
	r.mu.RLock()
	fn := r.lifecycle
	r.mu.RUnlock()
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

func (r *NativeRenderer) UpdateMouse(x, y float64) {
	C.llrdc_set_mouse_position(C.double(x), C.double(y))
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

//export llrdc_window_callback
func llrdc_window_callback(idPtr unsafe.Pointer, eventType C.int, data1 C.int, data2 C.int, errStr *C.char) {
	r := getRenderer(uintptr(idPtr))
	if r == nil {
		return
	}
	event := NativeWindowLifecycle{}
	switch eventType {
	case 1:
		event.Event = "created"
		event.Created = true
	case 2:
		event.Event = "shown"
		event.Shown = true
	case 3:
		event.Event = "visible"
		event.Visible = true
	case 5:
		event.Event = "size_changed"
		event.Width = int(data1)
		event.Height = int(data2)
	case 13:
		event.Event = "close"
	case 20:
		event.Event = "started"
	}
	if errStr != nil {
		event.Error = C.GoString(errStr)
	}
	r.emitLifecycle(event)
}

//export llrdc_input_callback
func llrdc_input_callback(idPtr unsafe.Pointer, jsonMsg *C.char) {
	r := getRenderer(uintptr(idPtr))
	if r == nil {
		return
	}
	msgStr := C.GoString(jsonMsg)
	var msg map[string]any
	if err := json.Unmarshal([]byte(msgStr), &msg); err == nil {
		if msgType, _ := msg["type"].(string); msgType == "resize" {
			if width, ok := msg["width"].(float64); ok {
				r.mu.Lock()
				r.width = int(width)
				r.mu.Unlock()
			}
			if height, ok := msg["height"].(float64); ok {
				r.mu.Lock()
				r.height = int(height)
				r.mu.Unlock()
			}
		}
		r.mu.RLock()
		fn := r.inputSink
		r.mu.RUnlock()
		if fn != nil {
			_ = fn(msg)
		}
	}
}

//export llrdc_present_callback
func llrdc_present_callback(idPtr unsafe.Pointer, width C.int, height C.int, ts C.uint32_t) {
	r := getRenderer(uintptr(idPtr))
	if r == nil {
		return
	}
	now := time.Now()
	r.emitPresent(NativeFramePresented{
		Width:           int(width),
		Height:          int(height),
		PacketTimestamp: uint32(ts),
		ReceiveAt:       now,
		DecodeReadyAt:   now,
		PresentationAt:  now,
	})
}
