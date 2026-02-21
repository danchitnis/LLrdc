package main

import (
	"log"
	"net"

	"github.com/pion/webrtc/v4"
)

var videoTrack *webrtc.TrackLocalStaticRTP

func initWebRTC() {
	var err error
	videoTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion",
	)
	if err != nil {
		log.Fatalf("Failed to create video track: %v", err)
	}

	go rtpListener()
}

func rtpListener() {
	addr := &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: RtpPort,
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on RTP port %d: %v", RtpPort, err)
	}
	defer conn.Close()

	if err := conn.SetReadBuffer(10 * 1024 * 1024); err != nil {
		log.Printf("Warning: Failed to set RTP UDP read buffer (%v)\n", err)
	}

	log.Printf("Listening for RTP packets on %s\n", addr.String())

	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("RTP Read error: %v", err)
			continue
		}

		if videoTrack != nil {
			if _, err := videoTrack.Write(buf[:n]); err != nil {
				// Avoid spamming log on err
			}
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
