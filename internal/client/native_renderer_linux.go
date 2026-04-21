//go:build native && linux && cgo

package client

/*
#cgo pkg-config: vpx sdl2 libavcodec libavutil
#include <vpx/vpx_decoder.h>
#include <vpx/vp8dx.h>
#include <vpx/vpx_image.h>
#include <SDL2/SDL.h>
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
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

typedef struct {
    AVCodecContext* ctx;
    AVFrame* frame;
    AVPacket* packet;
    int initialized;
} llrdc_av_decoder;

static int llrdc_av_init(llrdc_av_decoder* decoder, const char* codec_name) {
    if (decoder->initialized) {
        return 0;
    }
    enum AVCodecID codec_id = AV_CODEC_ID_NONE;
    if (strstr(codec_name, "h264") || strstr(codec_name, "H264")) {
        codec_id = AV_CODEC_ID_H264;
    }

    if (codec_id == AV_CODEC_ID_NONE) {
        return -1;
    }

    const AVCodec* codec = avcodec_find_decoder(codec_id);
    if (!codec) {
        return -2;
    }

    decoder->ctx = avcodec_alloc_context3(codec);
    if (!decoder->ctx) {
        return -3;
    }

    if (avcodec_open2(decoder->ctx, codec, NULL) < 0) {
        avcodec_free_context(&decoder->ctx);
        return -4;
    }

    decoder->frame = av_frame_alloc();
    decoder->packet = av_packet_alloc();
    decoder->initialized = 1;
    return 0;
}

static int llrdc_av_decode(llrdc_av_decoder* decoder, const unsigned char* data, unsigned int size) {
    if (!decoder->initialized) {
        return -1;
    }
    decoder->packet->data = (uint8_t*)data;
    decoder->packet->size = size;

    int ret = avcodec_send_packet(decoder->ctx, decoder->packet);
    if (ret < 0) {
        return ret;
    }

    ret = avcodec_receive_frame(decoder->ctx, decoder->frame);
    if (ret == AVERROR(EAGAIN)) {
        return 1; // Need more data
    }
    if (ret == AVERROR_EOF) {
        return 2; // EOF
    }
    return ret;
}

static void llrdc_av_close(llrdc_av_decoder* decoder) {
    if (!decoder->initialized) {
        return;
    }
    avcodec_free_context(&decoder->ctx);
    av_frame_free(&decoder->frame);
    av_packet_free(&decoder->packet);
    decoder->initialized = 0;
}
*/
import "C"

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
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
	overlay                 OverlayState
	samples                 chan nativeVideoSample
	streamResets            chan string
	resizeRequests          chan nativeResizeRequest
	snapshotRequests        chan chan nativeSnapshotResult
	stopCh                  chan struct{}
	doneCh                  chan struct{}

	mouseX int32
	mouseY int32

	videoWidth  int32
	videoHeight int32
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

type avDecoder struct {
	raw C.llrdc_av_decoder
}

type nativeResizeRequest struct {
	width  int
	height int
	result chan error
}

type nativeSnapshotResult struct {
	body []byte
	err  error
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
		resizeRequests:          make(chan nativeResizeRequest, 4),
		snapshotRequests:        make(chan chan nativeSnapshotResult, 2),
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

func (r *NativeRenderer) SetOverlayState(state OverlayState) {
	r.mu.Lock()
	r.overlay = cloneOverlayState(state)
	r.mu.Unlock()
}

func (r *NativeRenderer) SetLatencyProbe(enabled bool) {
	r.mu.Lock()
	r.probeLatency = enabled
	r.mu.Unlock()
}

func (r *NativeRenderer) SetDebugCursor(enabled bool) {
	r.mu.Lock()
	r.debugCursor = enabled
	r.mu.Unlock()
}

func (r *NativeRenderer) SetWindowSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid window size %dx%d", width, height)
	}
	r.mu.Lock()
	r.width = width
	r.height = height
	runStarted := r.runStarted
	r.mu.Unlock()
	if !runStarted {
		return nil
	}
	result := make(chan error, 1)
	req := nativeResizeRequest{width: width, height: height, result: result}
	select {
	case r.resizeRequests <- req:
	case <-r.stopCh:
		return fmt.Errorf("renderer has stopped")
	}
	return <-result
}

func (r *NativeRenderer) CaptureSnapshotPNG() ([]byte, error) {
	result := make(chan nativeSnapshotResult, 1)
	select {
	case r.snapshotRequests <- result:
	case <-r.stopCh:
		return nil, fmt.Errorf("renderer has stopped")
	}
	snapshot := <-result
	return snapshot.body, snapshot.err
}

func (r *NativeRenderer) Size() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.width, r.height
}

func (r *NativeRenderer) PreferredVideoCodec() string {
	return "vp8"
}

func (r *NativeRenderer) SupportedVideoCodecs() []string {
	return []string{"vp8", "h264"}
}

func (r *NativeRenderer) ResetVideoStream(codec string) {
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

}

func (r *NativeRenderer) drawOverlay(renderer *sdl.Renderer) {
	r.mu.RLock()
	state := cloneOverlayState(r.overlay)
	r.mu.RUnlock()

	if len(state.HUDLines) > 0 {
		maxWidth := int32(0)
		lineHeight := int32(16)
		for _, line := range state.HUDLines {
			width, _ := measureBitmapText(line, 2)
			if width > maxWidth {
				maxWidth = width
			}
		}
		panel := sdl.Rect{X: 8, Y: 8, W: maxWidth + 16, H: int32(len(state.HUDLines))*lineHeight + 8}
		drawOverlayPanel(renderer, panel, OverlayColor{R: 0, G: 0, B: 0, A: 160}, OverlayColor{R: state.HUDColor.R, G: state.HUDColor.G, B: state.HUDColor.B, A: 220})
		for idx, line := range state.HUDLines {
			drawBitmapText(renderer, panel.X+8, panel.Y+6+int32(idx)*lineHeight, 2, line, state.HUDColor)
		}
	}

	if !state.MenuVisible {
		return
	}

	outputW, outputH, err := renderer.GetOutputSize()
	if err != nil {
		return
	}
	layout := computeMenuLayout(int(outputW), int(outputH), len(state.MenuItems))
	itemHeight := int32(layout.itemHeight)
	panel := sdl.Rect{
		X: int32(layout.panelX),
		Y: int32(layout.panelY),
		W: int32(layout.panelW),
		H: int32(layout.panelH),
	}
	drawOverlayPanel(renderer, panel, OverlayColor{R: 12, G: 14, B: 18, A: 220}, OverlayColor{R: 96, G: 124, B: 255, A: 255})
	drawBitmapText(renderer, panel.X+16, panel.Y+14, 3, state.MenuTitle, OverlayColor{R: 255, G: 255, B: 255, A: 255})
	drawBitmapText(renderer, panel.X+16, panel.Y+42, 2, state.MenuHint, OverlayColor{R: 180, G: 188, B: 204, A: 255})

	for idx, line := range state.MenuItems {
		y := panel.Y + int32(layout.itemsStart) + int32(idx)*itemHeight
		if idx == state.SelectedIndex {
			highlight := sdl.Rect{X: panel.X + 10, Y: y - 2, W: panel.W - 20, H: itemHeight}
			drawOverlayPanel(renderer, highlight, OverlayColor{R: 34, G: 52, B: 98, A: 180}, OverlayColor{R: 86, G: 118, B: 230, A: 255})
		}
		drawBitmapText(renderer, panel.X+20, y, 2, line, OverlayColor{R: 240, G: 244, B: 255, A: 255})
	}
}

func (r *NativeRenderer) processWindowRequests(window *sdl.Window, renderer *sdl.Renderer) {
	for {
		select {
		case req := <-r.resizeRequests:
			window.SetSize(int32(req.width), int32(req.height))
			r.mu.Lock()
			r.width = req.width
			r.height = req.height
			r.mu.Unlock()
			req.result <- nil
		case response := <-r.snapshotRequests:
			body, err := captureRendererPNG(renderer)
			response <- nativeSnapshotResult{body: body, err: err}
		default:
			return
		}
	}
}

func measureBitmapText(text string, scale int32) (int32, int32) {
	if scale <= 0 {
		scale = 2
	}
	width := int32(len(text)) * (5*scale + scale)
	if len(text) > 0 {
		width -= scale
	}
	return width, 7 * scale
}

func drawBitmapText(renderer *sdl.Renderer, x, y, scale int32, text string, c OverlayColor) {
	if scale <= 0 {
		scale = 2
	}
	_ = renderer.SetDrawBlendMode(sdl.BLENDMODE_BLEND)
	_ = renderer.SetDrawColor(c.R, c.G, c.B, c.A)
	cursorX := x
	for _, raw := range strings.ToUpper(text) {
		glyph := glyphForRune(raw)
		for row := int32(0); row < 7; row++ {
			for col := int32(0); col < 5; col++ {
				if glyph[row][col] != '1' {
					continue
				}
				_ = renderer.FillRect(&sdl.Rect{
					X: cursorX + col*scale,
					Y: y + row*scale,
					W: scale,
					H: scale,
				})
			}
		}
		cursorX += 6 * scale
	}
}

func drawOverlayPanel(renderer *sdl.Renderer, rect sdl.Rect, fill OverlayColor, border OverlayColor) {
	_ = renderer.SetDrawBlendMode(sdl.BLENDMODE_BLEND)
	_ = renderer.SetDrawColor(fill.R, fill.G, fill.B, fill.A)
	_ = renderer.FillRect(&rect)
	_ = renderer.SetDrawColor(border.R, border.G, border.B, border.A)
	_ = renderer.DrawRect(&rect)
}

func captureRendererPNG(renderer *sdl.Renderer) ([]byte, error) {
	width, height, err := renderer.GetOutputSize()
	if err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("renderer output is unavailable")
	}
	pitch := int(width) * 4
	pixels := make([]byte, int(height)*pitch)
	if err := renderer.ReadPixels(nil, sdl.PIXELFORMAT_ARGB8888, unsafe.Pointer(&pixels[0]), pitch); err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	for y := 0; y < int(height); y++ {
		for x := 0; x < int(width); x++ {
			offset := y*pitch + x*4
			img.SetRGBA(x, y, color.RGBA{
				R: pixels[offset+2],
				G: pixels[offset+1],
				B: pixels[offset+0],
				A: pixels[offset+3],
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
						select {
						case decodedFrames <- nativeDecodedSample{
							frame:           frame,
							packetTimestamp: sample.packetTimestamp,
							receiveAt:       sample.receiveAt,
							decodeReadyAt:   time.Now(),
						}:
						default:
						}
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
						select {
						case decodedFrames <- nativeDecodedSample{
							frame:           frame,
							packetTimestamp: sample.packetTimestamp,
							receiveAt:       sample.receiveAt,
							decodeReadyAt:   time.Now(),
						}:
						default:
						}
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
			if probeLatency {
				// Compute center pixel brightness (Y channel)
				cx := int(decoded.frame.width / 2)
				cy := int(decoded.frame.height / 2)
				offset := cy*int(decoded.frame.yStride) + cx
				if offset >= 0 && offset < len(decoded.frame.yPlane) {
					brightness = int(decoded.frame.yPlane[offset])
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

func (d *avDecoder) Init(codec string) error {
	cStr := C.CString(codec)
	defer C.free(unsafe.Pointer(cStr))
	if rc := C.llrdc_av_init(&d.raw, cStr); rc != 0 {
		return fmt.Errorf("init av decoder (%s): %d", codec, int(rc))
	}
	return nil
}

func (d *avDecoder) Decode(data []byte) (decodedFrame, error) {
	if len(data) == 0 {
		return decodedFrame{}, nil
	}
	rc := C.llrdc_av_decode(&d.raw, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data)))
	if rc != 0 {
		if int(rc) == 1 { // Need more data
			return decodedFrame{}, nil
		}
		if int(rc) == 2 { // EOF
			return decodedFrame{}, nil
		}
		return decodedFrame{}, fmt.Errorf("decode av frame: %d", int(rc))
	}

	f := d.raw.frame
	width := int32(f.width)
	height := int32(f.height)

	yStride := int32(f.linesize[0])
	uStride := int32(f.linesize[1])
	vStride := int32(f.linesize[2])

	return decodedFrame{
		width:   width,
		height:  height,
		yPlane:  C.GoBytes(unsafe.Pointer(f.data[0]), C.int(yStride*height)),
		uPlane:  C.GoBytes(unsafe.Pointer(f.data[1]), C.int(uStride*((height+1)/2))),
		vPlane:  C.GoBytes(unsafe.Pointer(f.data[2]), C.int(vStride*((height+1)/2))),
		yStride: yStride,
		uStride: uStride,
		vStride: vStride,
	}, nil
}

func (d *avDecoder) Close() {
	C.llrdc_av_close(&d.raw)
}

func isKeyframe(codec string, data []byte) bool {
	if strings.Contains(strings.ToLower(codec), "vp8") {
		if len(data) == 0 {
			return false
		}
		return data[0]&0x01 == 0
	}
	if strings.Contains(strings.ToLower(codec), "h264") {
		return isH264KeyframePayload(data)
	}
	return false
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
