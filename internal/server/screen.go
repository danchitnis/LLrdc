package server

import "sync/atomic"

const (
	minScreenWidth  = 320
	minScreenHeight = 240
)

var screenWidth atomic.Int64
var screenHeight atomic.Int64

var maxScreenWidth atomic.Int64
var maxScreenHeight atomic.Int64

func UpdateScreenSizeFromInitialRes() {
	initialW := 1920
	initialH := 1080

	if InitialRes > 0 {
		initialH = InitialRes
		// Maintain 16:9 for standard heights if they fit, otherwise just use 1.77 ratio
		if InitialRes == 720 {
			initialW = 1280
		} else if InitialRes == 1080 {
			initialW = 1920
		} else if InitialRes == 1440 {
			initialW = 2560
		} else if InitialRes == 2160 {
			initialW = 3840
		} else {
			initialW = (InitialRes * 16 / 9 / 8) * 8
		}
	}

	forceSetScreenSize(initialW, initialH)
}

func initScreenSize(maxW, maxH int) {
	maxScreenWidth.Store(int64(maxW))
	maxScreenHeight.Store(int64(maxH))
	UpdateScreenSizeFromInitialRes()
}

func forceSetScreenSize(width, height int) bool {
	// Ensure 8-pixel alignment for maximum encoder compatibility
	width = (width / 8) * 8
	height = (height / 8) * 8

	if width < minScreenWidth {
		width = minScreenWidth
	}
	if height < minScreenHeight {
		height = minScreenHeight
	}

	maxW := int(maxScreenWidth.Load())
	maxH := int(maxScreenHeight.Load())
	if maxW > 0 && width > maxW {
		width = maxW
	}
	if maxH > 0 && height > maxH {
		height = maxH
	}

	prevWidth := screenWidth.Load()
	prevHeight := screenHeight.Load()
	if int64(width) == prevWidth && int64(height) == prevHeight {
		return false
	}

	screenWidth.Store(int64(width))
	screenHeight.Store(int64(height))
	return true
}

func SetScreenSize(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}

	if InitialRes > 0 {
		return false // Ignore client resizes when a fixed resolution is active
	}

	return forceSetScreenSize(width, height)
}

func GetScreenSize() (int, int) {
	width := screenWidth.Load()
	height := screenHeight.Load()
	if width == 0 || height == 0 {
		return minScreenWidth, minScreenHeight
	}
	return int(width), int(height)
}
