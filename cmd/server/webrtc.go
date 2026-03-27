package main

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type WebRTCFrame struct {
	Data        []byte
	StreamID    uint32
	CaptureTime time.Time
	Codec       string
}

var (
	videoTrack      *webrtc.TrackLocalStaticSample
	audioTrack      *webrtc.TrackLocalStaticSample
	videoTrackMutex sync.RWMutex
	webrtcFrameChan = make(chan WebRTCFrame, 300)
	lastSampleTime  time.Time
	currentStreamID uint32
)

func initWebRTCTrack() {
	videoTrackMutex.Lock()
	defer videoTrackMutex.Unlock()

	var err error
	mimeType := webrtc.MimeTypeVP8
	if VideoCodec == "h264" || VideoCodec == "h264_nvenc" {
		mimeType = webrtc.MimeTypeH264
	} else if VideoCodec == "h265" || VideoCodec == "h265_nvenc" {
		mimeType = "video/H265"
	} else if VideoCodec == "av1" || VideoCodec == "av1_nvenc" {
		mimeType = webrtc.MimeTypeAV1
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

	if audioTrack == nil {
		audioTrack, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion",
		)
		if err != nil {
			log.Fatalf("Failed to create audio track: %v", err)
		}
	}
}

func initWebRTC() {
	initWebRTCTrack()

	go func() {
		var bufferedFrame *WebRTCFrame
		var lastTrack *webrtc.TrackLocalStaticSample

		framesWritten := 0
		lastLogTime := time.Now()

		for frame := range webrtcFrameChan {
			videoTrackMutex.RLock()
			vt := videoTrack
			videoTrackMutex.RUnlock()

			if vt == nil {
				continue
			}

			// If track changed, flush/discard old buffer and reset
			if vt != lastTrack {
				bufferedFrame = nil
				lastTrack = vt
			}

			// DROP frames from a different codec to prevent browser decoder from freezing
			if frame.Codec != VideoCodec {
				continue
			}

			if bufferedFrame == nil {
				// First frame for this track
				f := frame // Copy
				bufferedFrame = &f
				currentStreamID = frame.StreamID
				continue
			}

			// If stream ID changed (e.g. FFmpeg restart), flush old buffer
			if frame.StreamID != currentStreamID {
				_ = vt.WriteSample(media.Sample{
					Data:     bufferedFrame.Data,
					Duration: time.Second / time.Duration(FPS),
				})
				framesWritten++

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
			err := vt.WriteSample(media.Sample{
				Data:     bufferedFrame.Data,
				Duration: duration,
			})
			if err == nil {
				framesWritten++
			}

			if time.Since(lastLogTime) >= time.Second {
				if UseDebugFFmpeg {
					log.Printf("WebRTC wrote %d frames to track in the last second", framesWritten)
				}
				framesWritten = 0
				lastLogTime = time.Now()
			}

			// Buffer the new frame
			f := frame
			bufferedFrame = &f
		}
	}()
}

func WriteWebRTCFrame(frame []byte, streamID uint32, captureTime time.Time) {
	select {
	case webrtcFrameChan <- WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime, Codec: VideoCodec}:
	default:
		log.Println("WARNING: webrtcFrameChan is full, dropping frame!")
	}
}

func createPeerConnection() (*webrtc.PeerConnection, error) {
	s := webrtc.SettingEngine{}
	s.SetEphemeralUDPPortRange(uint16(Port), uint16(Port))

	// Optionally allow overriding the public IP (e.g., if behind a strict NAT)
	publicIP := WebRTCPublicIP
	if publicIP != "" {
		if net.ParseIP(publicIP) != nil {
			s.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)
			log.Printf("WebRTC Setting NAT1To1IPs to %s", publicIP)
		} else {
			log.Printf("Warning: WEBRTC_PUBLIC_IP '%s' is not a valid IP. Ignoring.", publicIP)
		}
	}

	webrtcInterfaces := strings.TrimSpace(WebRTCInterfaces)
	webrtcExcludeInterfaces := strings.TrimSpace(WebRTCExcludeInterfaces)
	if webrtcInterfaces != "" || webrtcExcludeInterfaces != "" {
		interfaces := splitAndTrimCSV(webrtcInterfaces)
		excludeInterfaces := splitAndTrimCSV(webrtcExcludeInterfaces)
		s.SetInterfaceFilter(func(i string) bool {
			for _, excl := range excludeInterfaces {
				if i == excl || strings.HasPrefix(i, excl) {
					return false
				}
			}
			if len(interfaces) == 0 {
				return true
			}
			for _, iface := range interfaces {
				if i == iface {
					return true
				}
			}
			return false
		})
		log.Printf("WebRTC Setting InterfaceFilter: allow=%v, exclude=%v", interfaces, excludeInterfaces)
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

	videoTrackMutex.RLock()
	vt := videoTrack
	at := audioTrack
	videoTrackMutex.RUnlock()

	if _, err = pc.AddTrack(vt); err != nil {
		return nil, err
	}

	if at != nil {
		if _, err = pc.AddTrack(at); err != nil {
			return nil, err
		}
	}

	return pc, nil
}

func splitAndTrimCSV(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}
