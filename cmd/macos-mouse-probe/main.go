//go:build native && darwin && cgo

package main

import (
	"fmt"
	"math"
	"os"
	"runtime"

	"github.com/danchitnis/llrdc/internal/client"
)

type probeCase struct {
	name          string
	contentW      float64
	contentH      float64
	videoW        float64
	videoH        float64
	pointX        float64
	pointYFromTop float64
	wantX         float64
	wantY         float64
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cases := []probeCase{
		{
			name:          "matching_aspect_center",
			contentW:      1280,
			contentH:      720,
			videoW:        1920,
			videoH:        1080,
			pointX:        640,
			pointYFromTop: 360,
			wantX:         0.5,
			wantY:         0.5,
		},
		{
			name:          "matching_aspect_top_left",
			contentW:      1280,
			contentH:      720,
			videoW:        1920,
			videoH:        1080,
			pointX:        64,
			pointYFromTop: 72,
			wantX:         64.0 / 1280.0,
			wantY:         72.0 / 720.0,
		},
		{
			name:          "letterboxed_center",
			contentW:      1280,
			contentH:      800,
			videoW:        1920,
			videoH:        1080,
			pointX:        640,
			pointYFromTop: 400,
			wantX:         0.5,
			wantY:         0.5,
		},
		{
			name:          "letterboxed_top_of_video",
			contentW:      1280,
			contentH:      800,
			videoW:        1920,
			videoH:        1080,
			pointX:        640,
			pointYFromTop: 40,
			wantX:         0.5,
			wantY:         0.0,
		},
	}

	failed := false
	for _, tc := range cases {
		result := client.RunDarwinMousePayloadProbe(tc.contentW, tc.contentH, tc.videoW, tc.videoH, tc.pointX, tc.pointYFromTop)
		titleBarH := result.FrameH - tc.contentH
		pass := result.OK &&
			titleBarH > 0 &&
			almostEqual(result.X, tc.wantX, 1e-6) &&
			almostEqual(result.Y, tc.wantY, 1e-6)
		fmt.Printf("%s ok=%t frame_h=%.3f titlebar_h=%.3f got=(%.6f,%.6f) want=(%.6f,%.6f)\n",
			tc.name,
			pass,
			result.FrameH,
			titleBarH,
			result.X,
			result.Y,
			tc.wantX,
			tc.wantY,
		)
		if !pass {
			failed = true
		}
	}

	if failed {
		os.Exit(1)
	}
}

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}
