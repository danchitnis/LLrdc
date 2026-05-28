//go:build linux && native && cgo

package wayland

func RunLinuxMousePayloadProbe(windowW, windowH, videoW, videoH, mouseX, mouseY int32) (float64, float64) {
	r := &NativeRenderer{}
	return r.TestMouseMapping(windowW, windowH, videoW, videoH, mouseX, mouseY)
}
