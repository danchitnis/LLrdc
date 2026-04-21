//go:build native && linux && cgo

package client

func RunLinuxMousePayloadProbe(windowW, windowH, videoW, videoH, mouseX, mouseY int32) (float64, float64) {
	r := &NativeRenderer{}
	return r.TestMouseMapping(windowW, windowH, videoW, videoH, mouseX, mouseY)
}
