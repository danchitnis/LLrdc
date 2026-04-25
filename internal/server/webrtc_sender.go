package server

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	webrtcVideoOutboundMTU = 1200
	vp8ClockRate           = 90000
)

type videoFrameWriter interface {
	TrackLocal() webrtc.TrackLocal
	WriteFrame(frame WebRTCFrame) error
}

var (
	videoWriter videoFrameWriter
	audioTrack  *webrtc.TrackLocalStaticSample

	videoWriteStatsMu sync.Mutex
	framesWritten     int
	lastLogTime       time.Time
)

func initWebRTCTrack() {
	videoTrackMutex.Lock()
	defer videoTrackMutex.Unlock()

	writer, err := newVideoFrameWriter(VideoCodec, WebRTCLowLatency)
	if err != nil {
		log.Fatalf("Failed to create video track: %v", err)
	}
	videoWriter = writer

	if audioTrack == nil {
		log.Printf("Initializing audio track (Opus)")
		audioTrack, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion",
		)
		if err != nil {
			log.Fatalf("Failed to create audio track: %v", err)
		}
	}
}

func newVideoFrameWriter(codec string, lowLatency bool) (videoFrameWriter, error) {
	capability, codecFamily := videoTrackCapability(codec)
	if lowLatency {
		if codecFamily == "vp8" {
			log.Printf("Initializing isolated WebRTC VP8 ULL RTP sender")
			return newVP8ULLVideoWriter(capability, codecFamily)
		} else if codecFamily == "h264" {
			log.Printf("Initializing isolated WebRTC H264 ULL RTP sender")
			return newH264ULLVideoWriter(capability, codecFamily)
		}
	}
	log.Printf("Initializing WebRTC sample-track sender for %s", capability.MimeType)
	return newSampleVideoWriter(capability, codecFamily)
}

func videoTrackCapability(codec string) (webrtc.RTPCodecCapability, string) {
	codecFamily := normalizeCodecFamily(codec)
	capability := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}
	switch codecFamily {
	case "h264":
		capability = webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42E034",
		}
	case "h265":
		capability = webrtc.RTPCodecCapability{MimeType: "video/H265"}
	case "av1":
		capability = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1}
	}
	return capability, codecFamily
}

func normalizeCodecFamily(codec string) string {
	switch codec {
	case "h264", "h264_nvenc", "h264_qsv":
		return "h264"
	case "h265", "h265_nvenc", "h265_qsv":
		return "h265"
	case "av1", "av1_nvenc", "av1_qsv":
		return "av1"
	default:
		return codec
	}
}

func frameDuration() time.Duration {
	if FPS <= 0 {
		return time.Second / 60
	}
	duration := time.Second / time.Duration(FPS)
	if duration < time.Millisecond {
		return time.Millisecond
	}
	return duration
}

func frameSamples() uint32 {
	duration := frameDuration()
	samples := uint32((duration * vp8ClockRate) / time.Second)
	if samples == 0 {
		return 1
	}
	return samples
}

func recordVideoFrameWrite() {
	videoWriteStatsMu.Lock()
	defer videoWriteStatsMu.Unlock()

	if lastLogTime.IsZero() {
		lastLogTime = time.Now()
	}
	framesWritten++
	if time.Since(lastLogTime) >= time.Second {
		framesWritten = 0
		lastLogTime = time.Now()
	}
}

func cryptoRandomUint32() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return binary.BigEndian.Uint32(buf[:])
	}
	return uint32(time.Now().UnixNano())
}

func cryptoRandomUint16() uint16 {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return binary.BigEndian.Uint16(buf[:])
	}
	return uint16(time.Now().UnixNano())
}

func validateFrameCodec(frame WebRTCFrame, wantCodecFamily string) error {
	if normalizeCodecFamily(frame.Codec) != wantCodecFamily {
		return fmt.Errorf("frame codec family %q does not match writer codec family %q", normalizeCodecFamily(frame.Codec), wantCodecFamily)
	}
	return nil
}
