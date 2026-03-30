package main

import "sync/atomic"

const (
	minScreenWidth  = 320
	minScreenHeight = 240
)

var screenWidth atomic.Int64
var screenHeight atomic.Int64

var maxScreenWidth atomic.Int64
var maxScreenHeight atomic.Int64

func initScreenSize(maxW, maxH int) {
	maxScreenWidth.Store(int64(maxW))
	maxScreenHeight.Store(int64(maxH))

	initialW := 1920
	initialH := 1080
	if initialW > maxW {
		initialW = maxW
	}
	if initialH > maxH {
		initialH = maxH
	}

	setScreenSize(initialW, initialH)
}

func setScreenSize(width, height int) {
	// Ensure 16-pixel alignment for maximum encoder compatibility
	width = (width / 16) * 16
	height = (height / 16) * 16

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
	screenWidth.Store(int64(width))
	screenHeight.Store(int64(height))
}

func SetScreenSize(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	// Ensure 16-pixel alignment for maximum encoder compatibility
	width = (width / 16) * 16
	height = (height / 16) * 16

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

func GetScreenSize() (int, int) {
	width := screenWidth.Load()
	height := screenHeight.Load()
	if width == 0 || height == 0 {
		return minScreenWidth, minScreenHeight
	}
	return int(width), int(height)
}
