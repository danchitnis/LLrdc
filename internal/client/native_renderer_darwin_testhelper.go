//go:build native && darwin && cgo

package client

/*
#include <stdint.h>

int llrdc_test_mouse_payload(double contentW, double contentH, double videoW, double videoH, double pointX, double pointYFromTop, double* outX, double* outY, double* outFrameH);
*/
import "C"

type DarwinMousePayloadProbeResult struct {
	X      float64
	Y      float64
	FrameH float64
	OK     bool
}

func RunDarwinMousePayloadProbe(contentW, contentH, videoW, videoH, pointX, pointYFromTop float64) DarwinMousePayloadProbeResult {
	var outX, outY, outFrameH C.double
	result := C.llrdc_test_mouse_payload(
		C.double(contentW),
		C.double(contentH),
		C.double(videoW),
		C.double(videoH),
		C.double(pointX),
		C.double(pointYFromTop),
		&outX,
		&outY,
		&outFrameH,
	)
	return DarwinMousePayloadProbeResult{
		X:      float64(outX),
		Y:      float64(outY),
		FrameH: float64(outFrameH),
		OK:     result != 0,
	}
}
