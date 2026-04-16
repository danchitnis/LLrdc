package main

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/rtcp"
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
	webrtcUDPMux    ice.UDPMux
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

var (
	lastTrackMutex  sync.Mutex
	lastTrack       *webrtc.TrackLocalStaticSample
	lastCaptureTime time.Time
	framesWritten   int
	lastLogTime     time.Time
)

func writeFrameToTrack(frame WebRTCFrame) {
	videoTrackMutex.RLock()
	vt := videoTrack
	videoTrackMutex.RUnlock()

	if vt == nil {
		return
	}

	lastTrackMutex.Lock()
	defer lastTrackMutex.Unlock()

	if lastLogTime.IsZero() {
		lastLogTime = time.Now()
	}

	// If track changed, reset state
	if vt != lastTrack {
		lastTrack = vt
		lastCaptureTime = time.Time{}
	}

	// Drop frames from a different codec family to prevent decoder freezes across reconfigurations.
	if normalizeCodecFamily(frame.Codec) != normalizeCodecFamily(VideoCodec) {
		return
	}

	sid := frame.StreamID

	// If stream ID changed (e.g. FFmpeg restart), treat as new stream
	if sid != currentStreamID {
		currentStreamID = sid
		lastCaptureTime = time.Time{}
	}

	duration := time.Second / time.Duration(FPS)
	if duration < time.Millisecond {
		duration = time.Millisecond
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
		framesWritten = 0
		lastLogTime = time.Now()
	}
}

func initWebRTCMux() {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: Port})
	if err != nil {
		log.Printf("Warning: Failed to bind WebRTC UDP Mux to port %d: %v. Falling back to ephemeral ports.", Port, err)
		return
	}

	// Increase socket buffers to handle large video bursts
	const bufferSize = 4 * 1024 * 1024 // 4MB
	if err := udpConn.SetReadBuffer(bufferSize); err != nil {
		log.Printf("Warning: Failed to set UDP read buffer: %v", err)
	}
	if err := udpConn.SetWriteBuffer(bufferSize); err != nil {
		log.Printf("Warning: Failed to set UDP write buffer: %v", err)
	}

	mux := ice.NewUDPMuxDefault(ice.UDPMuxParams{
		UDPConn: udpConn,
	})

	webrtcUDPMux = mux
	log.Printf("WebRTC UDP Mux initialized on port %d with 4MB buffers", Port)
}

func initWebRTC() {
	initWebRTCMux()
	initWebRTCTrack()

	go func() {
		for frame := range webrtcFrameChan {
			writeFrameToTrack(frame)
		}
	}()
}

func WriteWebRTCFrame(frame []byte, streamID uint32, captureTime time.Time, codec string) {
	limit := WebRTCBufferSize
	if limit < 1 {
		limit = 1
	}

	// If zero buffering is requested and we are in low-latency mode, write directly.
	if limit <= 0 && WebRTCLowLatency {
		writeFrameToTrack(WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime, Codec: codec})
		return
	}

	newFrame := WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime, Codec: codec}

	// Try to send without blocking
	select {
	case webrtcFrameChan <- newFrame:
		return
	default:
	}

	// If we are here, the channel is full.
	// We drop the oldest frame and try again to ensure the NEWEST frame is always delivered.
	dropped := 0
	for {
		select {
		case <-webrtcFrameChan:
			dropped++
			select {
			case webrtcFrameChan <- newFrame:
				return
			default:
				// still full, keep dropping
				continue
			}
		default:
			// Channel became empty but we failed to write? (race)
			// Try one last time to write.
			select {
			case webrtcFrameChan <- newFrame:
				return
			default:
				// Hard fail - should not happen with cap=1000
				log.Printf("Warning: WriteWebRTCFrame failed to queue frame even after flushing channel")
				return
			}
		}
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

	if webrtcUDPMux != nil {
		s.SetICEUDPMux(webrtcUDPMux)
	} else {
		s.SetEphemeralUDPPortRange(uint16(Port), uint16(Port))
	}

	if WebRTCLowLatency {
		log.Printf("WebRTC creating peer connection in low-latency mode")
		s.DisableSRTPReplayProtection(true)
		s.DisableSRTCPReplayProtection(true)
		s.SetLite(true)
		s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
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
		rtpSender, err := pc.AddTrack(vt)
		if err != nil {
			return nil, err
		}
		// Read RTCP packets to handle PLIs (Picture Loss Indication)
		go func() {
			for {
				packets, _, rtcpErr := rtpSender.ReadRTCP()
				if rtcpErr != nil {
					return
				}
				for _, pk := range packets {
					if _, ok := pk.(*rtcp.PictureLossIndication); ok {
						log.Printf("Received PLI on video track, triggering keyframe...")
						TriggerPing()
						PrimeFrameGeneration(0, 5, 50*time.Millisecond)
					}
				}
			}
		}()
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
