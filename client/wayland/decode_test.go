//go:build linux && native && cgo

package wayland

import "testing"

func markerFrame(marker int) decodedFrame {
	const (
		width    = 1024
		height   = 128
		stride   = width
		startX   = 120
		startY   = 40
		cellSize = 18
		cellGap  = 7
	)
	y := make([]byte, stride*height)
	for i := range y {
		y[i] = 128
	}
	cell := func(x, value int) {
		level := byte(224)
		if value != 0 {
			level = 32
		}
		for row := startY; row < startY+cellSize; row++ {
			for col := x; col < x+cellSize; col++ {
				y[row*stride+col] = level
			}
		}
	}
	cell(40, 1)
	cell(72, 0)
	for bit := 0; bit < 32; bit++ {
		value := 0
		if bit < 16 {
			value = (marker >> bit) & 1
		} else {
			value = 1 - ((marker >> (bit - 16)) & 1)
		}
		cell(startX+bit*(cellSize+cellGap), value)
	}
	return decodedFrame{width: width, height: height, yPlane: y, yStride: stride}
}

func TestDecodeProbeMarkerIntegrity(t *testing.T) {
	for _, marker := range []int{0, 1, 0x1234, 0xffff} {
		if got := decodeProbeMarker(markerFrame(marker)); got != marker {
			t.Fatalf("decode marker %04x = %04x", marker, got)
		}
	}
}

func TestDecodeProbeMarkerRejectsCorruption(t *testing.T) {
	frame := markerFrame(0x1234)
	for row := 40; row < 58; row++ {
		for col := 120 + 2*25; col < 120+2*25+18; col++ {
			frame.yPlane[row*int(frame.yStride)+col] = 224
		}
	}
	if got := decodeProbeMarker(frame); got != 0 {
		t.Fatalf("corrupted marker decoded as %04x", got)
	}
}

func TestDecodeProbeMarkerWraparoundIsExplicit(t *testing.T) {
	if got := decodeProbeMarker(markerFrame(0)); got != 0 {
		t.Fatalf("wrapped marker 0 decoded as %d", got)
	}
	if got := decodeProbeMarker(markerFrame(0xffff)); got != 0xffff {
		t.Fatalf("maximum marker decoded as %04x", got)
	}
}
