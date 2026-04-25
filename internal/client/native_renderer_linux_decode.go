//go:build native && linux && cgo

package client

/*
#cgo pkg-config: vpx sdl2 libavcodec libavutil
#include <vpx/vpx_decoder.h>
#include <vpx/vp8dx.h>
#include <vpx/vpx_image.h>
#include <SDL2/SDL.h>
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct {
        vpx_codec_ctx_t ctx;
        int initialized;
} llrdc_vpx_decoder;

static int llrdc_vpx_init(llrdc_vpx_decoder* decoder) {
        if (decoder->initialized) {
                return 0;
        }
        if (vpx_codec_dec_init(&decoder->ctx, vpx_codec_vp8_dx(), NULL, 0) != VPX_CODEC_OK) {
                return -1;
        }
        decoder->initialized = 1;
        return 0;
}

static int llrdc_vpx_decode(llrdc_vpx_decoder* decoder, const unsigned char* data, unsigned int size) {
        if (!decoder->initialized) {
                return -1;
        }
        return vpx_codec_decode(&decoder->ctx, data, size, NULL, 0);
}

static vpx_image_t* llrdc_vpx_get_frame(llrdc_vpx_decoder* decoder, vpx_codec_iter_t* iter) {
        if (!decoder->initialized) {
                return NULL;
        }
        return vpx_codec_get_frame(&decoder->ctx, iter);
}

static const char* llrdc_vpx_error(llrdc_vpx_decoder* decoder) {
        if (!decoder->initialized) {
                return "decoder not initialized";
        }
        return vpx_codec_error(&decoder->ctx);
}

static void llrdc_vpx_close(llrdc_vpx_decoder* decoder) {
        if (!decoder->initialized) {
                return;
        }
        vpx_codec_destroy(&decoder->ctx);
        decoder->initialized = 0;
}

typedef struct {
    AVCodecContext* ctx;
    AVFrame* frame;
    AVPacket* packet;
    int initialized;
} llrdc_av_decoder;

static int llrdc_av_init(llrdc_av_decoder* decoder, const char* codec_name) {
    if (decoder->initialized) {
        return 0;
    }
    enum AVCodecID codec_id = AV_CODEC_ID_NONE;
    if (strstr(codec_name, "h264") || strstr(codec_name, "H264")) {
        codec_id = AV_CODEC_ID_H264;
    }

    if (codec_id == AV_CODEC_ID_NONE) {
        return -1;
    }

    const AVCodec* codec = avcodec_find_decoder(codec_id);
    if (!codec) {
        return -2;
    }

    decoder->ctx = avcodec_alloc_context3(codec);
    if (!decoder->ctx) {
        return -3;
    }

    if (avcodec_open2(decoder->ctx, codec, NULL) < 0) {
        avcodec_free_context(&decoder->ctx);
        return -4;
    }

    decoder->frame = av_frame_alloc();
    decoder->packet = av_packet_alloc();
    decoder->initialized = 1;
    return 0;
}

static int llrdc_av_decode(llrdc_av_decoder* decoder, const unsigned char* data, unsigned int size) {
    if (!decoder->initialized) {
        return -1;
    }
    decoder->packet->data = (uint8_t*)data;
    decoder->packet->size = size;

    int ret = avcodec_send_packet(decoder->ctx, decoder->packet);
    if (ret < 0) {
        return ret;
    }

    ret = avcodec_receive_frame(decoder->ctx, decoder->frame);
    if (ret == AVERROR(EAGAIN)) {
        return 1; // Need more data
    }
    if (ret == AVERROR_EOF) {
        return 2; // EOF
    }
    return ret;
}

static void llrdc_av_close(llrdc_av_decoder* decoder) {
    if (!decoder->initialized) {
        return;
    }
    avcodec_free_context(&decoder->ctx);
    av_frame_free(&decoder->frame);
    av_packet_free(&decoder->packet);
    decoder->initialized = 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type vp8Decoder struct {
	raw C.llrdc_vpx_decoder
}

type avDecoder struct {
	raw C.llrdc_av_decoder
}

type decodedFrame struct {
	width   int32
	height  int32
	yPlane  []byte
	uPlane  []byte
	vPlane  []byte
	yStride int32
	uStride int32
	vStride int32
}

func decodeProbeMarker(frame decodedFrame) int {
	if frame.width <= 0 || frame.height <= 0 || frame.yStride <= 0 || len(frame.yPlane) == 0 {
		return 0
	}

	const (
		markerBits = 16
		refDarkX   = 80
		refBrightX = 144
		startX     = 240
		startY     = 80
		cellSize   = 40
		cellGap    = 20
	)

	refDark := sampleMarkerCellAverage(frame, refDarkX, startY, cellSize)
	refBright := sampleMarkerCellAverage(frame, refBrightX, startY, cellSize)
	if refDark < 0 || refBright < 0 {
		return 0
	}
	threshold := (refDark + refBright) / 2

	marker := 0
	for bit := 0; bit < markerBits; bit++ {
		cellX := startX + bit*(cellSize+cellGap)
		cellAvg := sampleMarkerCellAverage(frame, cellX, startY, cellSize)
		if cellAvg < 0 {
			return 0
		}
		if cellAvg < threshold {
			marker++
			continue
		}
		break
	}

	return marker
}

func sampleMarkerCellAverage(frame decodedFrame, x, y, size int) int {
	if size <= 4 || frame.yStride <= 0 || len(frame.yPlane) == 0 {
		return -1
	}
	margin := 4
	sum := 0
	count := 0
	for yy := y + margin; yy < y+size-margin; yy++ {
		if yy < 0 || yy >= int(frame.height) {
			return -1
		}
		for xx := x + margin; xx < x+size-margin; xx++ {
			if xx < 0 || xx >= int(frame.width) {
				return -1
			}
			offset := yy*int(frame.yStride) + xx
			if offset < 0 || offset >= len(frame.yPlane) {
				return -1
			}
			sum += int(frame.yPlane[offset])
			count++
		}
	}
	if count == 0 {
		return -1
	}
	return sum / count
}

func (d *vp8Decoder) Init() error {
	if rc := C.llrdc_vpx_init(&d.raw); rc != 0 {
		return fmt.Errorf("init vp8 decoder: %d", int(rc))
	}
	return nil
}

func (d *vp8Decoder) Decode(data []byte) (decodedFrame, error) {
	if len(data) == 0 {
		return decodedFrame{}, nil
	}
	if rc := C.llrdc_vpx_decode(&d.raw, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))); rc != 0 {
		return decodedFrame{}, fmt.Errorf("decode vp8 frame: %s", C.GoString(C.llrdc_vpx_error(&d.raw)))
	}
	var iter C.vpx_codec_iter_t
	img := C.llrdc_vpx_get_frame(&d.raw, &iter)
	if img == nil {
		return decodedFrame{}, nil
	}

	width := int32(img.d_w)
	height := int32(img.d_h)
	yStride := int32(img.stride[0])
	uStride := int32(img.stride[1])
	vStride := int32(img.stride[2])

	return decodedFrame{
		width:   width,
		height:  height,
		yPlane:  C.GoBytes(unsafe.Pointer(img.planes[0]), C.int(yStride*height)),
		uPlane:  C.GoBytes(unsafe.Pointer(img.planes[1]), C.int(uStride*((height+1)/2))),
		vPlane:  C.GoBytes(unsafe.Pointer(img.planes[2]), C.int(vStride*((height+1)/2))),
		yStride: yStride,
		uStride: uStride,
		vStride: vStride,
	}, nil
}

func (d *vp8Decoder) Close() {
	C.llrdc_vpx_close(&d.raw)
}

func (d *avDecoder) Init(codec string) error {
	cStr := C.CString(codec)
	defer C.free(unsafe.Pointer(cStr))
	if rc := C.llrdc_av_init(&d.raw, cStr); rc != 0 {
		return fmt.Errorf("init av decoder (%s): %d", codec, int(rc))
	}
	return nil
}

func (d *avDecoder) Decode(data []byte) (decodedFrame, error) {
	if len(data) == 0 {
		return decodedFrame{}, nil
	}
	rc := C.llrdc_av_decode(&d.raw, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data)))
	if rc != 0 {
		if int(rc) == 1 { // Need more data
			return decodedFrame{}, nil
		}
		if int(rc) == 2 { // EOF
			return decodedFrame{}, nil
		}
		return decodedFrame{}, fmt.Errorf("decode av frame: %d", int(rc))
	}

	f := d.raw.frame
	width := int32(f.width)
	height := int32(f.height)

	yStride := int32(f.linesize[0])
	uStride := int32(f.linesize[1])
	vStride := int32(f.linesize[2])

	return decodedFrame{
		width:   width,
		height:  height,
		yPlane:  C.GoBytes(unsafe.Pointer(f.data[0]), C.int(yStride*height)),
		uPlane:  C.GoBytes(unsafe.Pointer(f.data[1]), C.int(uStride*((height+1)/2))),
		vPlane:  C.GoBytes(unsafe.Pointer(f.data[2]), C.int(vStride*((height+1)/2))),
		yStride: yStride,
		uStride: uStride,
		vStride: vStride,
	}, nil
}

func (d *avDecoder) Close() {
	C.llrdc_av_close(&d.raw)
}
