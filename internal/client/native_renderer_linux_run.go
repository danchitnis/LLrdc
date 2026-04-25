//go:build native && linux && cgo

package client

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/veandco/go-sdl2/sdl"
)

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
	windowFlags := uint32(sdl.WINDOW_SHOWN)
	if !r.fullscreen {
		windowFlags |= sdl.WINDOW_RESIZABLE
	}
	window, err := sdl.CreateWindow(
		r.title,
		sdl.WINDOWPOS_CENTERED,
		sdl.WINDOWPOS_CENTERED,
		int32(r.width),
		int32(r.height),
		windowFlags,
	)
	if err != nil {
		r.emitLifecycle(NativeWindowLifecycle{Error: fmt.Sprintf("create window: %v", err)})
		return fmt.Errorf("create window: %w", err)
	}
	defer window.Destroy()

	// Center and show
	window.SetPosition(sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED)
	window.Raise()
	if r.fullscreen {
		if err := window.SetFullscreen(sdl.WINDOW_FULLSCREEN_DESKTOP); err != nil {
			r.emitLifecycle(NativeWindowLifecycle{Error: fmt.Sprintf("set fullscreen: %v", err)})
			return fmt.Errorf("set fullscreen: %w", err)
		}
	}

	r.mu.RLock()
	initialLowLatency := r.lowLatency
	r.mu.RUnlock()
	rendererFlags := uint32(sdl.RENDERER_ACCELERATED)
	if !initialLowLatency {
		rendererFlags |= sdl.RENDERER_PRESENTVSYNC
	}

	renderer, err := sdl.CreateRenderer(window, -1, rendererFlags)
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
		r.drawOverlay(renderer)
		renderer.Present()

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

		r.processWindowRequests(window, renderer)

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
		defer close(decodedFrames)
		var currentCodec string
		var vp8 *vp8Decoder
		var av *avDecoder

		closeDecoders := func() {
			if vp8 != nil {
				vp8.Close()
				vp8 = nil
			}
			if av != nil {
				av.Close()
				av = nil
			}
		}
		defer closeDecoders()

		for {
			select {
			case <-r.stopCh:
				return
			case codec := <-r.streamResets:
				if codec == "decode_error" {
					// Keep current decoder but wait for keyframe
					r.mu.Lock()
					r.decoderAwaitingKeyframe = true
					r.mu.Unlock()
					continue
				}
				sCodec := strings.ToLower(codec)
				cCodec := strings.ToLower(currentCodec)
				if currentCodec != "" && !strings.Contains(sCodec, cCodec) && !strings.Contains(cCodec, sCodec) {
					log.Printf("Resetting stream to codec %s (previous %s)", codec, currentCodec)
					closeDecoders()
					currentCodec = codec
					r.mu.Lock()
					r.decoderAwaitingKeyframe = true
					r.mu.Unlock()
				}
			case sample, ok := <-r.samples:
				if !ok {
					return
				}

				sCodec := strings.ToLower(sample.codec)
				cCodec := strings.ToLower(currentCodec)

				if currentCodec == "" {
					currentCodec = sample.codec
					cCodec = strings.ToLower(currentCodec)
				}

				if currentCodec != "" && !strings.Contains(sCodec, cCodec) && !strings.Contains(cCodec, sCodec) {
					log.Printf("Codec mismatch in sample (got %s, expected %s), resetting decoders", sample.codec, currentCodec)
					closeDecoders()
					currentCodec = sample.codec
					r.mu.Lock()
					r.decoderAwaitingKeyframe = true
					r.mu.Unlock()
					// Signal main loop to recreate texture
					select {
					case r.streamResets <- "reset_texture":
					default:
					}
				}

				r.mu.RLock()
				awaiting := r.decoderAwaitingKeyframe
				r.mu.RUnlock()

				if awaiting {
					if !isKeyframe(sample.codec, sample.data) {
						continue
					}
					r.mu.Lock()
					r.decoderAwaitingKeyframe = false
					r.mu.Unlock()
					r.emitLifecycle(NativeWindowLifecycle{DecoderStateChanged: true, DecoderAwaitingKeyframe: false})
				}

				if strings.Contains(strings.ToLower(sample.codec), "vp8") {
					if vp8 == nil {
						log.Printf("Initializing VP8 decoder for %s", sample.codec)
						vp8 = &vp8Decoder{}
						if err := vp8.Init(); err != nil {
							log.Printf("VP8 init error: %v", err)
							r.emitLifecycle(NativeWindowLifecycle{Error: err.Error()})
							continue
						}
					}
					frame, err := vp8.Decode(sample.data)
					if err != nil {
						log.Printf("VP8 decode error: %v", err)
						r.mu.Lock()
						r.decoderAwaitingKeyframe = true
						r.mu.Unlock()
						r.emitLifecycle(NativeWindowLifecycle{DecodeError: true, DecoderStateChanged: true, DecoderAwaitingKeyframe: true})
						continue
					}
					if frame.width > 0 && frame.height > 0 {
						r.mu.RLock()
						lowLatency := r.lowLatency
						r.mu.RUnlock()
						enqueueDecodedFrame(decodedFrames, nativeDecodedSample{
							frame:                        frame,
							packetTimestamp:              sample.packetTimestamp,
							firstPacketSequenceNumber:    sample.firstPacketSequenceNumber,
							firstDecryptedPacketQueuedAt: sample.firstDecryptedPacketQueuedAt,
							firstRemotePacketAt:          sample.firstRemotePacketAt,
							firstPacketReadAt:            sample.firstPacketReadAt,
							receiveAt:                    sample.receiveAt,
							decodeReadyAt:                benchmarkClockNowMs(),
						}, lowLatency)
					}
				} else {
					if av == nil {
						log.Printf("Initializing FFmpeg decoder for %s", sample.codec)
						av = &avDecoder{}
						if err := av.Init(sample.codec); err != nil {
							log.Printf("FFmpeg init error: %v", err)
							r.emitLifecycle(NativeWindowLifecycle{Error: err.Error()})
							continue
						}
					}
					frame, err := av.Decode(sample.data)
					if err != nil {
						log.Printf("FFmpeg decode error: %v", err)
						r.mu.Lock()
						r.decoderAwaitingKeyframe = true
						r.mu.Unlock()
						r.emitLifecycle(NativeWindowLifecycle{DecodeError: true, DecoderStateChanged: true, DecoderAwaitingKeyframe: true})
						continue
					}
					if frame.width > 0 && frame.height > 0 {
						r.mu.RLock()
						lowLatency := r.lowLatency
						r.mu.RUnlock()
						enqueueDecodedFrame(decodedFrames, nativeDecodedSample{
							frame:                        frame,
							packetTimestamp:              sample.packetTimestamp,
							firstPacketSequenceNumber:    sample.firstPacketSequenceNumber,
							firstDecryptedPacketQueuedAt: sample.firstDecryptedPacketQueuedAt,
							firstRemotePacketAt:          sample.firstRemotePacketAt,
							firstPacketReadAt:            sample.firstPacketReadAt,
							receiveAt:                    sample.receiveAt,
							decodeReadyAt:                benchmarkClockNowMs(),
						}, lowLatency)
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
		r.processWindowRequests(window, renderer)

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
				ww, wh := int32(r.width), int32(r.height)
				vw, vh := textureWidth, textureHeight
				r.mouseX = e.X
				r.mouseY = e.Y
				r.mu.Unlock()
				if vw <= 0 || vh <= 0 {
					vw, vh = ww, wh
				}

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

					x := (float64(e.X) - float64(dx)) / float64(dw)
					y := (float64(e.Y) - float64(dy)) / float64(dh)

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

					r.sendInput(map[string]any{
						"type": "mousemove",
						"x":    x,
						"y":    y,
					})
				}
			case *sdl.MouseButtonEvent:
				action := "mousedown"
				if e.Type == sdl.MOUSEBUTTONUP {
					action = "mouseup"
				}
				r.mu.RLock()
				ww, wh := int32(r.width), int32(r.height)
				vw, vh := textureWidth, textureHeight
				r.mu.RUnlock()
				if vw <= 0 || vh <= 0 {
					vw, vh = ww, wh
				}

				x := 0.0
				y := 0.0

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

					x = (float64(e.X) - float64(dx)) / float64(dw)
					y = (float64(e.Y) - float64(dy)) / float64(dh)

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
				}

				r.sendInput(map[string]any{
					"type":   "mousebtn",
					"button": sdlButtonToDOM(e.Button),
					"action": action,
					"x":      x,
					"y":      y,
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
		case codec := <-r.streamResets:
			if codec == "reset_texture" {
				log.Printf("Resetting SDL texture for codec change")
				if texture != nil {
					texture.Destroy()
					texture = nil
				}
				continue
			}
			r.mu.Lock()
			r.decoderAwaitingKeyframe = true
			r.mu.Unlock()
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
				r.mu.Lock()
				r.videoWidth = frame.width
				r.videoHeight = frame.height
				r.mu.Unlock()
			}
			_ = texture.UpdateYUV(nil, frame.yPlane, int(frame.yStride), frame.uPlane, int(frame.uStride), frame.vPlane, int(frame.vStride))
			_ = renderer.Clear()
			r.mu.RLock()
			ww, wh := r.width, r.height
			probeLatency := r.probeLatency
			debugCursor := r.debugCursor
			mx, my := r.mouseX, r.mouseY
			r.mu.RUnlock()

			brightness := -1
			probeMarker := 0
			if probeLatency {
				// Compute center pixel brightness (Y channel)
				cx := int(decoded.frame.width / 2)
				cy := int(decoded.frame.height / 2)
				offset := cy*int(decoded.frame.yStride) + cx
				if offset >= 0 && offset < len(decoded.frame.yPlane) {
					brightness = int(decoded.frame.yPlane[offset])
				}
				if brightness >= 0 {
					probeMarker = decodeProbeMarker(decoded.frame)
				}
			}

			// Ensure the renderer's logical size matches the window size so drawing coordinates match mouse coordinates
			if ww > 0 && wh > 0 {
				lw, lh := renderer.GetLogicalSize()
				if lw != int32(ww) || lh != int32(wh) {
					_ = renderer.SetLogicalSize(int32(ww), int32(wh))
				}
			}

			// Compute aspect-fit destination rect
			var dstRect *sdl.Rect
			if textureWidth > 0 && textureHeight > 0 && ww > 0 && wh > 0 {
				videoAspect := float64(textureWidth) / float64(textureHeight)
				windowAspect := float64(ww) / float64(wh)

				var dw, dh int32
				var dx, dy int32

				if windowAspect > videoAspect {
					// Pillarboxed (bars on sides)
					dh = int32(wh)
					dw = int32(float64(dh) * videoAspect)
					dx = (int32(ww) - dw) / 2
					dy = 0
				} else {
					// Letterboxed (bars on top/bottom)
					dw = int32(ww)
					dh = int32(float64(dw) / videoAspect)
					dx = 0
					dy = (int32(wh) - dh) / 2
				}
				dstRect = &sdl.Rect{X: dx, Y: dy, W: dw, H: dh}
			}

			_ = renderer.Copy(texture, nil, dstRect)

			if debugCursor {
				_ = renderer.SetDrawColor(255, 0, 0, 255)
				_ = renderer.FillRect(&sdl.Rect{X: mx - 5, Y: my - 5, W: 10, H: 10})
			}

			r.drawOverlay(renderer)
			renderer.Present()
			presentedAt := benchmarkClockNowMs()
			r.emitPresent(NativeFramePresented{
				Width:                        int(decoded.frame.width),
				Height:                       int(decoded.frame.height),
				PacketTimestamp:              decoded.packetTimestamp,
				FirstPacketSequenceNumber:    decoded.firstPacketSequenceNumber,
				Brightness:                   brightness,
				ProbeMarker:                  probeMarker,
				FirstDecryptedPacketQueuedAt: decoded.firstDecryptedPacketQueuedAt,
				FirstRemotePacketAt:          decoded.firstRemotePacketAt,
				FirstPacketReadAt:            decoded.firstPacketReadAt,
				ReceiveAt:                    decoded.receiveAt,
				DecodeReadyAt:                decoded.decodeReadyAt,
				PresentationAt:               presentedAt,
				PresentationSource:           "render_present",
			})

		case <-time.After(10 * time.Millisecond):
			if texture != nil {
				_ = renderer.Clear()

				r.mu.RLock()
				ww, wh := r.width, r.height
				debugCursor := r.debugCursor
				mx, my := r.mouseX, r.mouseY
				r.mu.RUnlock()

				// Compute aspect-fit destination rect
				var dstRect *sdl.Rect
				if textureWidth > 0 && textureHeight > 0 && ww > 0 && wh > 0 {
					videoAspect := float64(textureWidth) / float64(textureHeight)
					windowAspect := float64(ww) / float64(wh)
					var dw, dh int32
					var dx, dy int32
					if windowAspect > videoAspect {
						dh = int32(wh)
						dw = int32(float64(dh) * videoAspect)
						dx = (int32(ww) - dw) / 2
						dy = 0
					} else {
						dw = int32(ww)
						dh = int32(float64(dw) / videoAspect)
						dx = 0
						dy = (int32(wh) - dh) / 2
					}
					dstRect = &sdl.Rect{X: dx, Y: dy, W: dw, H: dh}
				}

				_ = renderer.Copy(texture, nil, dstRect)
				if debugCursor {
					_ = renderer.SetDrawColor(255, 0, 0, 255)
					_ = renderer.FillRect(&sdl.Rect{X: mx - 5, Y: my - 5, W: 10, H: 10})
				}
				r.drawOverlay(renderer)
				renderer.Present()
			}
		case <-r.stopCh:
			return nil
		}
	}
}
