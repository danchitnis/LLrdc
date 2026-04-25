//go:build linux && cgo

package main

import (
	"fmt"
	"github.com/veandco/go-sdl2/sdl"
	"time"
)

func main() {
	fmt.Println("Starting autonomous SDL mouse test...")
	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		fmt.Printf("SDL Init failed: %v\n", err)
		return
	}
	defer sdl.Quit()

	window, err := sdl.CreateWindow("Autonomous Test", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, 1280, 720, sdl.WINDOW_SHOWN)
	if err != nil {
		fmt.Printf("Window creation failed: %v\n", err)
		return
	}
	defer window.Destroy()

	// Wait for window to appear
	time.Sleep(1 * time.Second)

	fmt.Println("Warping mouse to center (640, 360)")
	window.WarpMouseInWindow(640, 360)
	time.Sleep(500 * time.Millisecond)

	fmt.Println("Warping mouse to top-left (10, 10)")
	window.WarpMouseInWindow(10, 10)
	time.Sleep(500 * time.Millisecond)

	fmt.Println("Autonomous SDL mouse test complete.")
}
