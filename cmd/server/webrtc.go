package main

import (
	"log"
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
	videoTrack, err = webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion",
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

func createPeerConnection(hostIP string) (*webrtc.PeerConnection, error) {
	s := webrtc.SettingEngine{}
	s.SetEphemeralUDPPortRange(uint16(Port), uint16(Port))

	publicIP := os.Getenv("WEBRTC_PUBLIC_IP")
	if publicIP == "" {
		publicIP = hostIP
	}
	s.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)

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
