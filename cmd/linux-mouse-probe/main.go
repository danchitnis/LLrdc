//go:build native && linux && cgo

package main

import (
	"fmt"
	"math"
	"os"
	"runtime"

	"github.com/danchitnis/llrdc/internal/client"
)

type probeCase struct {
	name    string
	windowW int32
	windowH int32
	videoW  int32
	videoH  int32
	mouseX  int32
	mouseY  int32
	wantX   float64
	wantY   float64
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cases := []probeCase{
		{
			name:    "matching_aspect_center",
			windowW: 1280,
			windowH: 720,
			videoW:  1920,
			videoH:  1080,
			mouseX:  640,
			mouseY:  360,
			wantX:   0.5,
			wantY:   0.5,
		},
		{
			name:    "letterboxed_top_of_video",
			windowW: 1280,
			windowH: 800, // 16:10 window for 16:9 video -> letterboxed
			videoW:  1920,
			videoH:  1080,
			mouseX:  640,
			mouseY:  40, // 800 - 1280*(1080/1920) = 800 - 720 = 80. bars are 40px each.
			wantX:   0.5,
			wantY:   0.0,
		},
		{
			name:    "letterboxed_out_of_bounds_top",
			windowW: 1280,
			windowH: 800,
			videoW:  1920,
			videoH:  1080,
			mouseX:  640,
			mouseY:  10, // in the top bar
			wantX:   0.5,
			wantY:   0.0,
		},
		{
			name:    "pillarboxed_left_of_video",
			windowW: 1600, // wider than 16:9
			windowH: 720,
			videoW:  1920,
			videoH:  1080,
			mouseX:  160, // 1600 - 720*(1920/1080) = 1600 - 1280 = 320. bars are 160px each.
			mouseY:  360,
			wantX:   0.0,
			wantY:   0.5,
		},
		{
			name:    "pillarboxed_right_edge",
			windowW: 1600,
			windowH: 720,
			videoW:  1920,
			videoH:  1080,
			mouseX:  1440, // 160 + 1280
			mouseY:  360,
			wantX:   1.0,
			wantY:   0.5,
		},
	}

	failed := false
	for _, tc := range cases {
		gotX, gotY := client.RunLinuxMousePayloadProbe(tc.windowW, tc.windowH, tc.videoW, tc.videoH, tc.mouseX, tc.mouseY)
		pass := almostEqual(gotX, tc.wantX, 1e-6) && almostEqual(gotY, tc.wantY, 1e-6)
		fmt.Printf("%s: pass=%t got=(%.6f,%.6f) want=(%.6f,%.6f)\n",
			tc.name, pass, gotX, gotY, tc.wantX, tc.wantY)
		if !pass {
			failed = true
		}
	}

	if failed {
		os.Exit(1)
	}
	fmt.Println("All Linux mouse probe tests passed!")
}

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}
