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

func initScreenSize(width, height int) {
	maxScreenWidth.Store(int64(width))
	maxScreenHeight.Store(int64(height))
	setScreenSize(width, height)
}

func setScreenSize(width, height int) {
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
