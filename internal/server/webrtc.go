package server

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

type WebRTCFrame struct {
	Data         []byte
	StreamID     uint32
	CaptureTime  time.Time
	Codec        string
	LatencyTrace *latencyProbeSendTrace
}

var (
	webrtcUDPMux    ice.UDPMux
	videoTrackMutex sync.RWMutex
	webrtcFrameChan = make(chan WebRTCFrame, 1000)
)

func initWebRTCMux() {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: Port})
	if err != nil {
		log.Printf("Warning: Failed to bind WebRTC UDP Mux to port %d: %v. Falling back to ephemeral ports.", Port, err)
		return
	}

	const bufferSize = 4 * 1024 * 1024
	if err := udpConn.SetReadBuffer(bufferSize); err != nil {
		log.Printf("Warning: Failed to set UDP read buffer: %v", err)
	}
	if err := udpConn.SetWriteBuffer(bufferSize); err != nil {
		log.Printf("Warning: Failed to set UDP write buffer: %v", err)
	}

	mux := ice.NewUDPMuxDefault(ice.UDPMuxParams{
		UDPConn: newLatencyProbePacketConn(udpConn, benchmarkClockNowMs),
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

func writeFrameToTrack(frame WebRTCFrame) {
	videoTrackMutex.RLock()
	writer := videoWriter
	videoTrackMutex.RUnlock()
	if writer == nil {
		return
	}

	if err := writer.WriteFrame(frame); err == nil {
		recordVideoFrameWrite()
	} else {
		errMsg := err.Error()
		if errMsg != "io: read/write on closed pipe" && errMsg != "Track not bound" {
			log.Printf("WebRTC video write error: %v", err)
		}
	}
}

func WriteWebRTCFrame(frame []byte, streamID uint32, captureTime time.Time, codec string, trace *latencyProbeSendTrace) {
	if WebRTCBufferSize <= 0 && WebRTCLowLatency {
		writeFrameToTrack(WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime, Codec: codec, LatencyTrace: trace})
		return
	}

	limit := WebRTCBufferSize
	if limit < 1 {
		limit = 1
	}

	newFrame := WebRTCFrame{Data: frame, StreamID: streamID, CaptureTime: captureTime, Codec: codec, LatencyTrace: trace}

	select {
	case webrtcFrameChan <- newFrame:
		return
	default:
	}

	for {
		select {
		case <-webrtcFrameChan:
			select {
			case webrtcFrameChan <- newFrame:
				return
			default:
				continue
			}
		default:
			select {
			case webrtcFrameChan <- newFrame:
				return
			default:
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

		localIfaces, _ := net.Interfaces()
		ifaceMap := make(map[string]bool)
		for _, iface := range localIfaces {
			ifaceMap[iface.Name] = true
		}

		s.SetInterfaceFilter(func(i string) bool {
			if webrtcExcludeInterfaces != "" {
				for _, excl := range excludeInterfaces {
					if i == excl || strings.HasPrefix(i, excl) {
						return false
					}
				}
			}
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
				return !foundAny
			}
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
	writer := videoWriter
	at := audioTrack
	videoTrackMutex.RUnlock()

	if writer != nil {
		vt := writer.TrackLocal()
		log.Printf("Adding video track to PeerConnection: %s", vt.ID())
		rtpSender, err := pc.AddTrack(vt)
		if err != nil {
			return nil, err
		}
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
