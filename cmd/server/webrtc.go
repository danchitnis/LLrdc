package main

import (
	"log"
	"net"
	"os"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type WebRTCFrame struct {
	Data        []byte
	StreamID    uint32
	CaptureTime time.Time
}

var (
	videoTrack      *webrtc.TrackLocalStaticSample
	webrtcFrameChan = make(chan WebRTCFrame, 300)
	lastSampleTime  time.Time
	currentStreamID uint32
)

func initWebRTC() {
	var err error
	mimeType := webrtc.MimeTypeVP8
	if VideoCodec == "h264" || VideoCodec == "h264_nvenc" {
		mimeType = webrtc.MimeTypeH264
	}
	log.Printf("Initializing WebRTC with %s track", mimeType)

	capability := webrtc.RTPCodecCapability{MimeType: mimeType}
	if mimeType == webrtc.MimeTypeH264 {
		capability.SDPFmtpLine = "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42E034"
	}

	videoTrack, err = webrtc.NewTrackLocalStaticSample(
		capability, "video", "pion",
	)
	if err != nil {
		log.Fatalf("Failed to create video track: %v", err)
	}

	go func() {
		var bufferedFrame *WebRTCFrame

		for frame := range webrtcFrameChan {
			if videoTrack == nil {
				continue
			}

			if bufferedFrame == nil {
				// First frame
				f := frame // Copy
				bufferedFrame = &f
				currentStreamID = frame.StreamID
				continue
			}

			// If stream ID changed, flush old buffer with a small duration, start new buffer
			if frame.StreamID != currentStreamID {
				_ = videoTrack.WriteSample(media.Sample{
					Data:     bufferedFrame.Data,
					Duration: time.Second / time.Duration(FPS),
				})

				f := frame
				bufferedFrame = &f
				currentStreamID = frame.StreamID
				continue
			}

			// Calculate exact duration between the buffered frame and the new frame
			duration := frame.CaptureTime.Sub(bufferedFrame.CaptureTime)
			if duration <= 0 {
				duration = 1 * time.Microsecond
			}

			// Send the buffered frame with the exact time elapsed until the next frame
			_ = videoTrack.WriteSample(media.Sample{
				Data:     bufferedFrame.Data,
				Duration: duration,
			})

			// Buffer the new frame
			f := frame
			bufferedFrame = &f
		}
	}()
}

func WriteWebRTCFrame(frame []byte, streamID uint32, captureTime time.Time) {
	select {
	case webrtcFrameChan <- WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime}:
	default:
		log.Println("WARNING: webrtcFrameChan is full, dropping frame!")
	}
}

func createPeerConnection() (*webrtc.PeerConnection, error) {
	s := webrtc.SettingEngine{}
	s.SetEphemeralUDPPortRange(uint16(Port), uint16(Port))

	// Optionally allow overriding the public IP (e.g., if behind a strict NAT)
	publicIP := os.Getenv("WEBRTC_PUBLIC_IP")
	if publicIP != "" {
		if net.ParseIP(publicIP) != nil {
			s.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)
			log.Printf("WebRTC Setting NAT1To1IPs to %s", publicIP)
		} else {
			log.Printf("Warning: WEBRTC_PUBLIC_IP '%s' is not a valid IP. Ignoring.", publicIP)
		}
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(s))

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	if _, err = pc.AddTrack(videoTrack); err != nil {
		return nil, err
	}

	return pc, nil
}
