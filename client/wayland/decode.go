//go:build linux && native && cgo

package wayland

/*
#cgo pkg-config: sdl2 libavcodec libavutil libswscale
#include <SDL2/SDL.h>
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libswscale/swscale.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    AVCodecContext* ctx;
    AVFrame* frame;
    AVFrame* rgb_frame;
    struct SwsContext* sws_ctx;
    AVPacket* packet;
    int initialized;
    int is_444;
} llrdc_av_decoder;

static int llrdc_av_init(llrdc_av_decoder* decoder, const char* codec_name) {
    if (decoder->initialized) {
        return 0;
    }
    enum AVCodecID codec_id = AV_CODEC_ID_NONE;
    if (strstr(codec_name, "h264") || strstr(codec_name, "H264")) {
        codec_id = AV_CODEC_ID_H264;
    } else if (strstr(codec_name, "vp8") || strstr(codec_name, "VP8")) {
        codec_id = AV_CODEC_ID_VP8;
    } else if (strstr(codec_name, "hevc") || strstr(codec_name, "HEVC") || strstr(codec_name, "h265") || strstr(codec_name, "H265")) {
        codec_id = AV_CODEC_ID_HEVC;
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

    // Enable multi-threaded software decoding (0 = auto)
    // Use FF_THREAD_SLICE to avoid frame-delay latency in synchronous calls
    decoder->ctx->thread_count = 0;
    decoder->ctx->thread_type = FF_THREAD_SLICE;

    if (avcodec_open2(decoder->ctx, codec, NULL) < 0) {
        avcodec_free_context(&decoder->ctx);
        return -4;
    }

    decoder->frame = av_frame_alloc();
    decoder->rgb_frame = NULL;
    decoder->sws_ctx = NULL;
    decoder->is_444 = 0;
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

    if (ret >= 0) {
        if (decoder->frame->format == AV_PIX_FMT_YUV444P || decoder->frame->format == AV_PIX_FMT_YUVJ444P) {
            if (!decoder->is_444) {
                printf("DEBUG: Native decoder detected Chroma 4:4:4 (YUV444P) stream\n");
            }
            decoder->is_444 = 1;
            if (!decoder->sws_ctx || !decoder->rgb_frame || decoder->rgb_frame->width != decoder->frame->width || decoder->rgb_frame->height != decoder->frame->height) {
                if (decoder->sws_ctx) sws_freeContext(decoder->sws_ctx);
                if (decoder->rgb_frame) {
                    av_freep(&decoder->rgb_frame->data[0]);
                    av_frame_free(&decoder->rgb_frame);
                }

                decoder->sws_ctx = sws_getContext(decoder->frame->width, decoder->frame->height, decoder->frame->format,
                                                  decoder->frame->width, decoder->frame->height, AV_PIX_FMT_RGB24,
                                                  SWS_BILINEAR, NULL, NULL, NULL);
                decoder->rgb_frame = av_frame_alloc();
                decoder->rgb_frame->width = decoder->frame->width;
                decoder->rgb_frame->height = decoder->frame->height;
                decoder->rgb_frame->format = AV_PIX_FMT_RGB24;
                av_image_alloc(decoder->rgb_frame->data, decoder->rgb_frame->linesize, decoder->rgb_frame->width, decoder->rgb_frame->height, AV_PIX_FMT_RGB24, 1);
            }
            sws_scale(decoder->sws_ctx, (const uint8_t * const *)decoder->frame->data, decoder->frame->linesize, 0, decoder->frame->height, decoder->rgb_frame->data, decoder->rgb_frame->linesize);
        } else {
            decoder->is_444 = 0;
        }
    }

    return ret;
}

static void llrdc_av_close(llrdc_av_decoder* decoder) {
    if (!decoder->initialized) {
        return;
    }
    if (decoder->sws_ctx) sws_freeContext(decoder->sws_ctx);
    if (decoder->rgb_frame) {
        av_freep(&decoder->rgb_frame->data[0]);
        av_frame_free(&decoder->rgb_frame);
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
	"strings"
	"unsafe"
)

type avDecoder struct {
	raw           C.llrdc_av_decoder
	codec         string
	h264ParamSets []byte
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
	is444   bool
}

func decodeProbeMarker(frame decodedFrame) int {
	if frame.width <= 0 || frame.height <= 0 || frame.yStride <= 0 || len(frame.yPlane) == 0 {
		return 0
	}

	// Weston may expose a scaled logical surface. Try the unscaled layout and
	// the common integer buffer scales without relaxing the inverse checksum.
	for scale := 1; scale <= 4; scale++ {
		if marker := decodeProbeMarkerAtScale(frame, scale); marker >= 0 {
			return marker
		}
	}
	return 0
}

func decodeProbeMarkerAtScale(frame decodedFrame, scale int) int {
	const markerBits = 32
	refDarkX, refBrightX, startX, startY := 40*scale, 72*scale, 120*scale, 40*scale
	cellSize, cellGap := 18*scale, 7*scale
	refDark := sampleMarkerCellAverage(frame, refDarkX, startY, cellSize)
	refBright := sampleMarkerCellAverage(frame, refBrightX, startY, cellSize)
	if refDark < 0 || refBright < 0 || absInt(refDark-refBright) < 24 {
		return -1
	}
	threshold := (refDark + refBright) / 2

	bits := make([]int, markerBits)
	for bit := 0; bit < markerBits; bit++ {
		cellX := startX + bit*(cellSize+cellGap)
		cellAvg := sampleMarkerCellAverage(frame, cellX, startY, cellSize)
		if cellAvg < 0 {
			return -1
		}
		if cellAvg < threshold {
			bits[bit] = 1
		}
	}
	marker := 0
	inverse := 0
	for bit := 0; bit < 16; bit++ {
		marker |= bits[bit] << bit
		inverse |= bits[bit+16] << bit
	}
	if inverse != (^marker & 0xffff) {
		return -1
	}
	return marker
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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

func (d *avDecoder) Init(codec string) error {
	cStr := C.CString(codec)
	defer C.free(unsafe.Pointer(cStr))
	if rc := C.llrdc_av_init(&d.raw, cStr); rc != 0 {
		return fmt.Errorf("init av decoder (%s): %d", codec, int(rc))
	}
	d.codec = codec
	d.h264ParamSets = nil
	return nil
}

func (d *avDecoder) Decode(data []byte) (decodedFrame, error) {
	if len(data) == 0 {
		return decodedFrame{}, nil
	}
	// NVENC can emit a forced IDR without the parameter sets in the same
	// access unit. Cache SPS/PPS from any preceding unit and prepend them to
	// later H.264 samples so a decoder that has just joined/reconnected can
	// establish the stream instead of remaining stuck at "non-existing PPS".
	if strings.Contains(strings.ToLower(d.codec), "h264") {
		if sets := h264ParameterSets(data); len(sets) > 0 {
			d.h264ParamSets = sets
		}
		if len(d.h264ParamSets) > 0 && !h264HasParameterSets(data) {
			joined := make([]byte, 0, len(d.h264ParamSets)+len(data))
			joined = append(joined, d.h264ParamSets...)
			joined = append(joined, data...)
			data = joined
		}
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

	is444 := d.raw.is_444 == 1

	if is444 {
		rgb := d.raw.rgb_frame
		rgbStride := int32(rgb.linesize[0])
		return decodedFrame{
			width:   width,
			height:  height,
			yPlane:  C.GoBytes(unsafe.Pointer(rgb.data[0]), C.int(rgbStride*height)),
			uPlane:  nil,
			vPlane:  nil,
			yStride: rgbStride,
			uStride: 0,
			vStride: 0,
			is444:   true,
		}, nil
	}

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
		is444:   false,
	}, nil
}

func (d *avDecoder) Close() {
	C.llrdc_av_close(&d.raw)
}
