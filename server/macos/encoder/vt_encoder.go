package encoder

/*
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework Foundation
#include <stdlib.h>
#include <stdint.h>

typedef struct VTEncoder VTEncoder;
VTEncoder* vt_encoder_create(const char* codec, int width, int height, int fps, int bitrate_kbps, int pix_fmt, uintptr_t handle);
int vt_encoder_encode(VTEncoder* encoder, uint8_t* yuv_data, int force_keyframe);
void vt_encoder_destroy(VTEncoder* encoder);
*/
import "C"
import (
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"
)

type VTEncoder struct {
	ptr           *C.VTEncoder
	handle        cgo.Handle
	onFrame       func(data []byte, isKeyframe bool)
	forceNextIDR  atomic.Bool
	Width, Height int
	FPS           int
	bitrateKbps   int
	PixFmt        int
	mu            sync.RWMutex
}

func (e *VTEncoder) BitrateKbps() int {
	return e.bitrateKbps
}

//export goEncodedFrameCallback
func goEncodedFrameCallback(handle C.uintptr_t, data unsafe.Pointer, length C.int, isKeyframe C.int) {
	h := cgo.Handle(handle)
	encoder, ok := h.Value().(*VTEncoder)
	if ok && encoder.onFrame != nil {
		buf := C.GoBytes(data, length)
		encoder.onFrame(buf, isKeyframe != 0)
	}
}

func NewVTEncoder(codec string, width, height, fps, bitrateKbps int, pixFmt int, onFrame func(data []byte, isKeyframe bool)) *VTEncoder {
	enc := &VTEncoder{
		onFrame:     onFrame,
		Width:       width,
		Height:      height,
		FPS:         fps,
		bitrateKbps: bitrateKbps,
		PixFmt:      pixFmt,
	}
	enc.handle = cgo.NewHandle(enc)
	cCodec := C.CString(codec)
	defer C.free(unsafe.Pointer(cCodec))
	enc.ptr = C.vt_encoder_create(cCodec, C.int(width), C.int(height), C.int(fps), C.int(bitrateKbps), C.int(pixFmt), C.uintptr_t(enc.handle))
	if enc.ptr == nil {
		enc.handle.Delete()
		return nil
	}
	return enc
}
func (e *VTEncoder) Encode(yuvData []byte) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.ptr == nil {
		return -1
	}

	// Safety check: ensure buffer is large enough for the encoder's pixel format
	expectedSize := e.Width * e.Height * 3 / 2 // 4:2:0
	if e.PixFmt == 1 {
		expectedSize = e.Width * e.Height * 3 // 4:4:4
	}
	if len(yuvData) < expectedSize {
		return -1
	}

	force := 0
	if e.forceNextIDR.CompareAndSwap(true, false) {
		force = 1
	}
	return int(C.vt_encoder_encode(e.ptr, (*C.uint8_t)(unsafe.Pointer(&yuvData[0])), C.int(force)))
}

func (e *VTEncoder) ForceKeyframe() {
	e.forceNextIDR.Store(true)
}

func (e *VTEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ptr != nil {
		C.vt_encoder_destroy(e.ptr)
		e.ptr = nil
		e.handle.Delete()
	}
}
