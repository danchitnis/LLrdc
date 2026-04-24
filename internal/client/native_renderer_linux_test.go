//go:build native && linux && cgo

package client

import (
	"testing"
	"time"

	"github.com/veandco/go-sdl2/sdl"
)

func TestNewNativeRendererDefaults(t *testing.T) {
	t.Parallel()

	renderer, err := NewNativeRenderer(NativeRendererOptions{})
	if err != nil {
		t.Fatalf("NewNativeRenderer returned error: %v", err)
	}
	defer renderer.Close()

	width, height := renderer.Size()
	if width != 1280 || height != 720 {
		t.Fatalf("unexpected default size: got %dx%d want 1280x720", width, height)
	}
}

func TestClampUnit(t *testing.T) {
	t.Parallel()

	if got := clampUnit(-1); got != 0 {
		t.Fatalf("clampUnit(-1) = %v, want 0", got)
	}
	if got := clampUnit(0.25); got != 0.25 {
		t.Fatalf("clampUnit(0.25) = %v, want 0.25", got)
	}
	if got := clampUnit(2); got != 1 {
		t.Fatalf("clampUnit(2) = %v, want 1", got)
	}
}

func TestSDLButtonToDOM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		button uint8
		want   int
	}{
		{button: sdl.BUTTON_LEFT, want: 0},
		{button: sdl.BUTTON_MIDDLE, want: 1},
		{button: sdl.BUTTON_RIGHT, want: 2},
	}

	for _, tt := range tests {
		if got := sdlButtonToDOM(tt.button); got != tt.want {
			t.Fatalf("sdlButtonToDOM(%d) = %d, want %d", tt.button, got, tt.want)
		}
	}
}

func TestSDLScancodeToDOM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code sdl.Scancode
		want string
	}{
		{code: sdl.SCANCODE_A, want: "KeyA"},
		{code: sdl.SCANCODE_9, want: "Digit9"},
		{code: sdl.SCANCODE_RETURN, want: "Enter"},
		{code: sdl.SCANCODE_LEFT, want: "ArrowLeft"},
		{code: sdl.SCANCODE_RSHIFT, want: "ShiftRight"},
	}

	for _, tt := range tests {
		if got := sdlScancodeToDOM(tt.code); got != tt.want {
			t.Fatalf("sdlScancodeToDOM(%v) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestHandleVideoFrameLowLatencyKeepsNewestSample(t *testing.T) {
	t.Parallel()

	renderer, err := NewNativeRenderer(NativeRendererOptions{})
	if err != nil {
		t.Fatalf("NewNativeRenderer returned error: %v", err)
	}
	defer renderer.Close()

	nativeRenderer, ok := renderer.(*NativeRenderer)
	if !ok {
		t.Fatalf("unexpected renderer type %T", renderer)
	}

	nativeRenderer.SetLowLatency(true)
	if err := nativeRenderer.handleVideoFrameWithTiming("video/VP8", []byte{0x01}, 1, 100, 110); err != nil {
		t.Fatalf("first handleVideoFrameWithTiming returned error: %v", err)
	}
	if err := nativeRenderer.handleVideoFrameWithTiming("video/VP8", []byte{0x02}, 2, 200, 210); err != nil {
		t.Fatalf("second handleVideoFrameWithTiming returned error: %v", err)
	}

	if got := len(nativeRenderer.samples); got != 1 {
		t.Fatalf("unexpected sample queue length: got %d want 1", got)
	}

	sample := <-nativeRenderer.samples
	if sample.packetTimestamp != 2 {
		t.Fatalf("unexpected queued packet timestamp: got %d want 2", sample.packetTimestamp)
	}
	if sample.firstPacketReadAt != 200 || sample.receiveAt != 210 {
		t.Fatalf("unexpected queued timing: %+v", sample)
	}
}

func TestEnqueueDecodedFrameLowLatencyKeepsNewest(t *testing.T) {
	t.Parallel()

	decodedFrames := make(chan nativeDecodedSample, 2)
	enqueueDecodedFrame(decodedFrames, nativeDecodedSample{packetTimestamp: 1}, true)
	enqueueDecodedFrame(decodedFrames, nativeDecodedSample{packetTimestamp: 2}, true)

	if got := len(decodedFrames); got != 1 {
		t.Fatalf("unexpected decoded queue length: got %d want 1", got)
	}

	decoded := <-decodedFrames
	if decoded.packetTimestamp != 2 {
		t.Fatalf("unexpected decoded frame timestamp: got %d want 2", decoded.packetTimestamp)
	}
}

func TestNativeRendererInputs(t *testing.T) {
	// This test requires a headed environment or a virtual framebuffer (XVFB)
	// because it initializes SDL video and creating a window.
	if testing.Short() {
		t.Skip("skipping headed test in short mode")
	}

	opts := NativeRendererOptions{
		Title:     "LLrdc Test Window",
		Width:     640,
		Height:    480,
		AutoStart: true,
	}

	renderer, err := NewNativeRenderer(opts)
	if err != nil {
		t.Skipf("skipping test: native renderer unavailable (likely no DISPLAY): %v", err)
	}
	defer renderer.Close()

	inputMsgs := make(chan map[string]any, 10)
	renderer.SetInputSink(func(msg map[string]any) error {
		inputMsgs <- msg
		return nil
	})

	// Start the renderer in a background goroutine
	done := make(chan error, 1)
	go func() {
		done <- renderer.Run()
	}()

	// Wait for the renderer to initialize and start its loop
	// We check for RenderLoopStarted event
	loopStarted := make(chan struct{})
	renderer.SetLifecycleSink(func(event NativeWindowLifecycle) {
		if event.RenderLoopStarted {
			close(loopStarted)
		}
	})

	select {
	case <-loopStarted:
	case err := <-done:
		t.Fatalf("renderer failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for renderer loop to start")
	}

	// 1. Test Mouse Motion
	sdl.PushEvent(&sdl.MouseMotionEvent{
		Type: sdl.MOUSEMOTION,
		X:    320,
		Y:    240,
	})

	select {
	case msg := <-inputMsgs:
		if msg["type"] != "mousemove" {
			t.Errorf("expected mousemove, got %v", msg["type"])
		}
		// x should be 320/640 = 0.5, y should be 240/480 = 0.5
		if x, ok := msg["x"].(float64); !ok || x != 0.5 {
			t.Errorf("expected x=0.5, got %v", msg["x"])
		}
		if y, ok := msg["y"].(float64); !ok || y != 0.5 {
			t.Errorf("expected y=0.5, got %v", msg["y"])
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for mousemove event")
	}

	// 2. Test Mouse Button
	sdl.PushEvent(&sdl.MouseButtonEvent{
		Type:   sdl.MOUSEBUTTONDOWN,
		Button: sdl.BUTTON_LEFT,
	})

	select {
	case msg := <-inputMsgs:
		if msg["type"] != "mousebtn" || msg["action"] != "mousedown" || msg["button"] != 0 {
			t.Errorf("unexpected mousebtn msg: %v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for mousedown event")
	}

	// 3. Test Keyboard
	sdl.PushEvent(&sdl.KeyboardEvent{
		Type: sdl.KEYDOWN,
		Keysym: sdl.Keysym{
			Scancode: sdl.SCANCODE_A,
		},
	})

	select {
	case msg := <-inputMsgs:
		if msg["type"] != "keydown" || msg["key"] != "KeyA" {
			t.Errorf("unexpected keydown msg: %v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for keydown event")
	}

	renderer.Stop()
	err = <-done
	if err != nil {
		t.Errorf("renderer.Start returned error: %v", err)
	}
}
