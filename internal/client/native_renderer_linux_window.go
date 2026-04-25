//go:build native && linux && cgo

package client

import (
	"fmt"

	"github.com/veandco/go-sdl2/sdl"
)

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
	pointerIn  bool
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
	case sdl.WINDOWEVENT_FOCUS_GAINED:
		s.hasFocus = true
		s.flags |= sdl.WINDOW_INPUT_FOCUS
	case sdl.WINDOWEVENT_FOCUS_LOST:
		s.hasFocus = false
		s.flags &^= sdl.WINDOW_INPUT_FOCUS
	case sdl.WINDOWEVENT_ENTER:
		s.pointerIn = true
	case sdl.WINDOWEVENT_LEAVE:
		s.pointerIn = false
	}
}

func (s nativeWindowState) snapshot(event string, created bool) NativeWindowLifecycle {
	return NativeWindowLifecycle{
		Backend:       s.backend,
		WindowID:      s.windowID,
		Created:       created,
		Shown:         s.shown,
		Mapped:        s.mapped,
		Visible:       s.visible,
		Event:         event,
		Flags:         s.flags,
		HasFocus:      s.hasFocus,
		PointerInside: s.pointerIn,
		HasSurface:    s.hasSurface,
		Desktop:       s.desktop,
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

	ww, wh := int32(r.width), int32(r.height)
	vw, vh := r.videoWidth, r.videoHeight

	if ww > 0 && wh > 0 && vw > 0 && vh > 0 {
		videoAspect := float64(vw) / float64(vh)
		windowAspect := float64(ww) / float64(wh)

		var dw, dh int32
		var dx, dy int32
		if windowAspect > videoAspect {
			dh = wh
			dw = int32(float64(dh) * videoAspect)
			dx = (ww - dw) / 2
			dy = 0
		} else {
			dw = ww
			dh = int32(float64(dw) / videoAspect)
			dx = 0
			dy = (wh - dh) / 2
		}

		r.mouseX = dx + int32(x*float64(dw))
		r.mouseY = dy + int32(y*float64(dh))
	} else {
		r.mouseX = int32(x * float64(r.width))
		r.mouseY = int32(y * float64(r.height))
	}
}

func (r *NativeRenderer) TestMouseMapping(windowW, windowH int32, videoW, videoH int32, mouseX, mouseY int32) (float64, float64) {
	if windowW <= 0 || windowH <= 0 || videoW <= 0 || videoH <= 0 {
		return 0, 0
	}

	videoAspect := float64(videoW) / float64(videoH)
	windowAspect := float64(windowW) / float64(windowH)

	var dw, dh int32
	var dx, dy int32
	if windowAspect > videoAspect {
		dh = windowH
		dw = int32(float64(dh) * videoAspect)
		dx = (windowW - dw) / 2
		dy = 0
	} else {
		dw = windowW
		dh = int32(float64(dw) / videoAspect)
		dx = 0
		dy = (windowH - dh) / 2
	}

	x := (float64(mouseX) - float64(dx)) / float64(dw)
	y := (float64(mouseY) - float64(dy)) / float64(dh)

	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	if y < 0 {
		y = 0
	}
	if y > 1 {
		y = 1
	}

	return x, y
}

func (r *NativeRenderer) sendInput(msg map[string]any) {
	r.mu.RLock()
	fn := r.inputSink
	r.mu.RUnlock()
	if fn != nil {
		_ = fn(msg)
	}
}
