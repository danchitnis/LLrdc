package main

import (
	"log"
	"os"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

var (
	videoTrack      *webrtc.TrackLocalStaticSample
	webrtcFrameChan = make(chan []byte, 300)
	lastSampleTime  time.Time
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
		for frame := range webrtcFrameChan {
			if videoTrack != nil {
				now := time.Now()
				nominalDur := time.Second / time.Duration(FPS)

				if lastSampleTime.IsZero() {
					lastSampleTime = now.Add(-nominalDur)
				}

				// Target time is last + nominal
				targetTime := lastSampleTime.Add(nominalDur)
				
				// Drift compensation: if we are more than 2 frames behind, jump ahead
				if now.Sub(targetTime) > nominalDur*2 {
					targetTime = now.Add(-nominalDur)
				}
				
				duration := targetTime.Sub(lastSampleTime)
				if duration <= 0 {
					duration = 1 * time.Microsecond // Ensure positive
				}

				_ = videoTrack.WriteSample(media.Sample{
					Data:     frame,
					Duration: duration,
				})
				lastSampleTime = targetTime
			}
		}
	}()
}

func ResetWebRTCSampleTime() {
	lastSampleTime = time.Time{}
}

func WriteWebRTCFrame(frame []byte) {
	select {
	case webrtcFrameChan <- frame:
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
