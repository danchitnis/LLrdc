package main

import (
	"log"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

var videoTrack *webrtc.TrackLocalStaticSample

func initWebRTC() {
	var err error
	videoTrack, err = webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion",
	)
	if err != nil {
		log.Fatalf("Failed to create video track: %v", err)
	}
}

func WriteWebRTCFrame(frame []byte) {
	if videoTrack != nil {
		err := videoTrack.WriteSample(media.Sample{
			Data:     frame,
			Duration: time.Second / time.Duration(FPS),
		})
		if err != nil {
			// Avoid spamming log on err
		}
	}
}

func createPeerConnection() (*webrtc.PeerConnection, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	if _, err = pc.AddTrack(videoTrack); err != nil {
		return nil, err
	}

	return pc, nil
}
