//go:build native && linux && cgo

package client

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"strings"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
)

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
		case enabled := <-r.vsyncRequests:
			if err := renderer.RenderSetVSync(enabled); err != nil {
				log.Printf("failed to set SDL renderer vsync=%t: %v", enabled, err)
			}
		default:
			return
		}
	}
}

func enqueueDecodedFrame(ch chan nativeDecodedSample, sample nativeDecodedSample, lowLatency bool) {
	if lowLatency {
		for len(ch) >= 1 {
			select {
			case <-ch:
			default:
				goto enqueueDecoded
			}
		}
	}

enqueueDecoded:
	select {
	case ch <- sample:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- sample:
		default:
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
