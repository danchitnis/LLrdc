package common

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	webrtcVideoOutboundMTU = 1200
	vp8ClockRate           = 90000

	// Payload types matching webrtc.go registrations
	PayloadTypeVP8  = 96
	PayloadTypeH264 = 120
	PayloadTypeH265 = 121
	PayloadTypeAV1  = 45
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

func InitWebRTCTrack() {
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
			return newVP8ULLVideoWriter(capability, codecFamily, PayloadTypeVP8)
		} else if codecFamily == "h264" {
			log.Printf("Initializing isolated WebRTC H264 ULL RTP sender")
			return newH264ULLVideoWriter(capability, codecFamily, PayloadTypeH264)
		}
	}
	if codecFamily == "h265" {
		log.Printf("Initializing isolated WebRTC H265 ULL RTP sender")
		return newH265ULLVideoWriter(capability, codecFamily, PayloadTypeH265)
	}
	log.Printf("Initializing WebRTC sample-track sender for %s", capability.MimeType)
	return newSampleVideoWriter(capability, codecFamily)
}

func videoTrackCapability(codec string) (webrtc.RTPCodecCapability, string) {
	codecFamily := NormalizeCodecFamily(codec)
	capability := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}
	switch codecFamily {
	case "h264":
		profileLevelID := "42E034" // Constrained Baseline
		if Chroma == "444" {
			profileLevelID = "f40032" // High 4:4:4 Predictive Level 5.0
		}
		capability = webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: fmt.Sprintf("level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=%s", profileLevelID),
		}
	case "h265":
		sdpFmtp := "profile-id=1;tier-flag=0;level-id=120" // Main profile
		if Chroma == "444" {
			sdpFmtp = "profile-id=4;tier-flag=0;level-id=123" // Main 4:4:4, Main tier, Level 4.1
		}
		capability = webrtc.RTPCodecCapability{
			MimeType:    "video/H265",
			ClockRate:   90000,
			SDPFmtpLine: sdpFmtp,
		}
	case "av1":
		capability = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1, ClockRate: 90000}
	}
	return capability, codecFamily
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
	if NormalizeCodecFamily(frame.Codec) != wantCodecFamily {
		return fmt.Errorf("frame codec family %q does not match writer codec family %q", NormalizeCodecFamily(frame.Codec), wantCodecFamily)
	}
	return nil
}

func FindAnnexBStartCode(data []byte, from int) (int, int, bool) {
	if from < 0 {
		from = 0
	}
	for i := from; i+3 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}
		if data[i+2] == 1 {
			return i, 3, true
		}
		if i+4 < len(data) && data[i+2] == 0 && data[i+3] == 1 {
			return i, 4, true
		}
	}
	return -1, 0, false
}

func SplitAnnexB(data []byte) [][]byte {
	var nalus [][]byte
	start := 0
	for {
		sIdx, prefixLen, ok := FindAnnexBStartCode(data, start)
		if !ok {
			break
		}

		nextStart := sIdx + prefixLen
		eIdx, _, ok := FindAnnexBStartCode(data, nextStart)

		var nalu []byte
		if ok {
			nalu = data[sIdx+prefixLen : eIdx]
			start = eIdx
		} else {
			nalu = data[sIdx+prefixLen:]
			start = len(data)
		}

		for len(nalu) > 0 && nalu[len(nalu)-1] == 0 {
			nalu = nalu[:len(nalu)-1]
		}

		if len(nalu) > 0 {
			nalus = append(nalus, nalu)
		}

		if start >= len(data) {
			break
		}
	}
	return nalus
}

func WriteAudioSample(data []byte, duration time.Duration) error {
	videoTrackMutex.RLock()
	at := audioTrack
	videoTrackMutex.RUnlock()

	if at != nil {
		return at.WriteSample(media.Sample{
			Data:     data,
			Duration: duration,
		})
	}
	return nil
}
