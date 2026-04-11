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
	// static buffer size is large, but we limit it dynamically in WriteWebRTCFrame
	webrtcFrameChan = make(chan WebRTCFrame, 1000)
	currentStreamID uint32
)

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

func initWebRTCTrack() {
	videoTrackMutex.Lock()
	defer videoTrackMutex.Unlock()

	var err error
	mimeType := webrtc.MimeTypeVP8
	if VideoCodec == "h264" || VideoCodec == "h264_nvenc" || VideoCodec == "h264_qsv" {
		mimeType = webrtc.MimeTypeH264
	} else if VideoCodec == "h265" || VideoCodec == "h265_nvenc" || VideoCodec == "h265_qsv" {
		mimeType = "video/H265"
	} else if VideoCodec == "av1" || VideoCodec == "av1_nvenc" || VideoCodec == "av1_qsv" {
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
		log.Printf("Initializing audio track (Opus)")
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
		var lastTrack *webrtc.TrackLocalStaticSample
		var lastCaptureTime time.Time

		framesWritten := 0
		lastLogTime := time.Now()

		for frame := range webrtcFrameChan {
			videoTrackMutex.RLock()
			vt := videoTrack
			videoTrackMutex.RUnlock()

			if vt == nil {
				continue
			}

			// If track changed, reset state
			if vt != lastTrack {
				lastTrack = vt
				lastCaptureTime = time.Time{}
			}

			// Drop frames from a different codec family to prevent decoder freezes across reconfigurations.
			if normalizeCodecFamily(frame.Codec) != normalizeCodecFamily(VideoCodec) {
				continue
			}

			// If stream ID changed (e.g. FFmpeg restart), treat as new stream
			if frame.StreamID != currentStreamID {
				currentStreamID = frame.StreamID
				lastCaptureTime = time.Time{}
			}

			duration := time.Second / time.Duration(FPS)
			if !lastCaptureTime.IsZero() {
				if delta := frame.CaptureTime.Sub(lastCaptureTime); delta > 0 {
					duration = delta
				}
			}
			if duration < time.Millisecond {
				duration = time.Millisecond
			}
			// Clamp pathological gaps so reconnects or dropped bursts do not cause visible jumps.
			maxDuration := 2 * time.Second / time.Duration(FPS)
			if maxDuration < time.Millisecond {
				maxDuration = time.Millisecond
			}
			if duration > maxDuration {
				duration = maxDuration
			}
			lastCaptureTime = frame.CaptureTime

			// Send the current frame immediately
			err := vt.WriteSample(media.Sample{
				Data:     frame.Data,
				Duration: duration,
			})
			if err == nil {
				framesWritten++
			} else {
				errMsg := err.Error()
				if errMsg != "io: read/write on closed pipe" && errMsg != "Track not bound" {
					log.Printf("WebRTC WriteSample error: %v", err)
				}
			}

			if time.Since(lastLogTime) >= time.Second {
				if UseDebugFFmpeg {
					log.Printf("WebRTC wrote %d frames to track in the last second", framesWritten)
				}
				framesWritten = 0
				lastLogTime = time.Now()
			}
		}
	}()
}

func WriteWebRTCFrame(frame []byte, streamID uint32, captureTime time.Time, codec string) {
	// If the channel is already fuller than our configured limit, flush it.
	limit := WebRTCBufferSize
	if limit < 1 {
		limit = 1
	}

	for len(webrtcFrameChan) >= limit {
		select {
		case <-webrtcFrameChan:
			continue
		default:
		}
		break
	}

	select {
	case webrtcFrameChan <- WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime, Codec: codec}:
	default:
		// If still full (unlikely), just drop.
	}
}

func extractHostOnly(hostport string) string {
	if hostport == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(host, "[]")
	}
	if strings.Count(hostport, ":") == 1 {
		if host, _, err := net.SplitHostPort(hostport); err == nil {
			return strings.Trim(host, "[]")
		}
		if idx := strings.LastIndex(hostport, ":"); idx != -1 {
			return hostport[:idx]
		}
	}
	return strings.Trim(hostport, "[]")
}

func resolveAdvertisedIP(requestHost string) string {
	if publicIP := strings.TrimSpace(WebRTCPublicIP); publicIP != "" {
		if net.ParseIP(publicIP) != nil {
			return publicIP
		}
		log.Printf("Warning: WEBRTC_PUBLIC_IP '%s' is not a valid IP. Ignoring.", publicIP)
	}

	host := strings.TrimSpace(extractHostOnly(requestHost))
	if host == "" {
		return ""
	}
	if strings.EqualFold(host, "localhost") {
		return "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		log.Printf("Warning: failed to resolve request host '%s' for WebRTC advertisement: %v", host, err)
		return ""
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	if len(ips) > 0 {
		return ips[0].String()
	}
	return ""
}

func createPeerConnection(requestHost string) (*webrtc.PeerConnection, error) {
	s := webrtc.SettingEngine{}
	s.SetEphemeralUDPPortRange(uint16(Port), uint16(Port))

	if WebRTCLowLatency {
		s.DisableSRTPReplayProtection(true)
		s.DisableSRTCPReplayProtection(true)
		s.SetLite(true)
	}

	// Prefer an explicit WEBRTC_PUBLIC_IP; otherwise derive from the browser request host.
	publicIP := resolveAdvertisedIP(requestHost)
	if publicIP != "" {
		s.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)
		log.Printf("WebRTC Setting NAT1To1IPs to %s", publicIP)
	}

	webrtcInterfaces := strings.TrimSpace(WebRTCInterfaces)
	webrtcExcludeInterfaces := strings.TrimSpace(WebRTCExcludeInterfaces)
	if webrtcInterfaces != "" || webrtcExcludeInterfaces != "" {
		interfaces := splitAndTrimCSV(webrtcInterfaces)
		excludeInterfaces := splitAndTrimCSV(webrtcExcludeInterfaces)

		// Get local interfaces to see if the requested ones exist
		localIfaces, _ := net.Interfaces()
		ifaceMap := make(map[string]bool)
		for _, iface := range localIfaces {
			ifaceMap[iface.Name] = true
		}

		s.SetInterfaceFilter(func(i string) bool {
			// Check exclusions first
			if webrtcExcludeInterfaces != "" {
				for _, excl := range excludeInterfaces {
					if i == excl || strings.HasPrefix(i, excl) {
						return false
					}
				}
			}
			// If allowed interfaces are specified, check those
			if len(interfaces) > 0 {
				foundAny := false
				for _, requested := range interfaces {
					if ifaceMap[requested] {
						foundAny = true
						if i == requested {
							return true
						}
					}
				}
				// If we found at least one of the requested interfaces in the container,
				// then we restrict to those. Otherwise, we allow all to prevent total lockout.
				return !foundAny
			}
			// Otherwise allow
			return true
		})
		log.Printf("WebRTC Setting InterfaceFilter: allow=%v, exclude=%v", interfaces, excludeInterfaces)
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(s))

	var iceServers []webrtc.ICEServer
	if !strings.HasPrefix(publicIP, "127.") && publicIP != "::1" && publicIP != "" {
		iceServers = []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}

	config := webrtc.Configuration{
		ICEServers: iceServers,
	}

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	videoTrackMutex.RLock()
	vt := videoTrack
	at := audioTrack
	videoTrackMutex.RUnlock()

	if vt != nil {
		log.Printf("Adding video track to PeerConnection: %s", vt.ID())
		if _, err = pc.AddTrack(vt); err != nil {
			return nil, err
		}
	}

	if at != nil {
		log.Printf("Adding audio track to PeerConnection: %s", at.ID())
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
