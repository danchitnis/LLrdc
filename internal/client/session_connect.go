package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

func (s *Session) Connect(serverURL string) error {
	if !s.connecting.CompareAndSwap(false, true) {
		return nil
	}
	defer s.connecting.Store(false)

	s.connectMu.Lock()
	defer s.connectMu.Unlock()

	if strings.TrimSpace(serverURL) == "" {
		return errors.New("server URL is required")
	}

	// Ensure previous connection is fully cleaned up
	if err := s.disconnectLocked(); err != nil {
		return err
	}
	// Small pause to allow OS to release UDP ports
	time.Sleep(100 * time.Millisecond)

	wsURL, err := httpToWebsocketURL(serverURL)
	if err != nil {
		return err
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed: %w (status %s)", err, resp.Status)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	// Read initial config message synchronously
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, raw, err := conn.ReadMessage()
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read initial config: %w", err)
	}
	if messageType != websocket.TextMessage {
		_ = conn.Close()
		return fmt.Errorf("expected text message for initial config, got %d", messageType)
	}

	var initMsg map[string]any
	if err := json.Unmarshal(raw, &initMsg); err != nil {
		_ = conn.Close()
		return fmt.Errorf("parse initial config: %w", err)
	}
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("register default codecs: %w", err)
	}

	lowLatency, _ := initMsg["webrtc_low_latency"].(bool)
	if toggler, ok := s.renderer.(LowLatencyRenderer); ok {
		toggler.SetLowLatency(lowLatency)
	}

	i := &interceptor.Registry{}
	if !lowLatency {
		log.Printf("WebRTC using default interceptors (NACK, Jitter Buffer)")
		if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
			_ = conn.Close()
			return fmt.Errorf("register default interceptors: %w", err)
		}
	} else {
		log.Printf("WebRTC skipping interceptors for low-latency mode")
		i.Add(newRemotePacketTimestampInterceptorFactory(benchmarkClockNowMs, s.recordRemotePacketAt))
	}

	se := webrtc.SettingEngine{}
	se.DisableSRTPReplayProtection(true)
	se.DisableSRTCPReplayProtection(true)
	if lowLatency {
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
		se.BufferFactory = newLatencyBufferFactory(benchmarkClockNowMs, s.recordDecryptedPacketAt)
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithSettingEngine(se),
		webrtc.WithInterceptorRegistry(i),
	)

	pc, err := api.NewPeerConnection(webrtc.Configuration{
		BundlePolicy: webrtc.BundlePolicyMaxBundle,
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create peer connection: %w", err)
	}
	// Wait for PeerConnection to reach a stable state
	ready := make(chan bool, 1)
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			select {
			case ready <- true:
			default:
			}
		}
	})

	ordered := false
	maxRetransmits := uint16(0)
	dc, err := pc.CreateDataChannel("input", &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: &maxRetransmits,
	})
	if err != nil {
		_ = pc.Close()
		_ = conn.Close()
		return fmt.Errorf("create input data channel: %w", err)
	}

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		_ = pc.Close()
		_ = conn.Close()
		return fmt.Errorf("add video transceiver: %w", err)
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		_ = pc.Close()
		_ = conn.Close()
		return fmt.Errorf("add audio transceiver: %w", err)
	}

	var connectionID uint64
	s.mu.Lock()
	s.connectionID++
	connectionID = s.connectionID
	s.conn = conn
	s.pc = pc
	s.input = dc
	s.state.ServerURL = serverURL
	s.state.Connected = true
	s.state.WebRTCConnected = false
	s.state.PeerConnectionState = webrtc.PeerConnectionStateNew.String()
	s.state.ICEConnectionState = webrtc.ICEConnectionStateNew.String()
	s.state.InputChannelOpen = false

	s.state.VideoCodec = ""
	s.state.LastConfig = nil
	if msgType, _ := initMsg["type"].(string); msgType == "config" {
		s.state.LastConfig = cloneMap(initMsg)
		if codec, ok := initMsg["videoCodec"].(string); ok {
			s.state.VideoCodec = codec
		}
		if width, ok := numberToInt(initMsg["screenWidth"]); ok {
			s.state.ServerScreenWidth = width
		}
		if height, ok := numberToInt(initMsg["screenHeight"]); ok {
			s.state.ServerScreenHeight = height
		}
	}

	s.state.LastStats = nil
	s.state.LastResizeWidth = 0
	s.state.LastResizeHeight = 0
	s.state.LastResizeAt = time.Time{}
	s.state.LastPresentedWidth = 0
	s.state.LastPresentedHeight = 0
	s.state.LastMessageAt = time.Time{}
	s.state.LastVideoPacketAt = time.Time{}
	s.state.LastVideoFrameAt = time.Time{}
	s.state.LastPresentAt = time.Time{}
	s.state.FirstFramePresentedAt = time.Time{}
	s.state.LastLatencySample = nil
	s.state.RecentVideoByteSamples = nil
	s.state.DecoderAwaitingKeyframe = true
	s.state.Presenting = false
	s.state.CurrentTrackCodecs = make(map[string]string)
	s.stats = SessionStats{
		ConnectedAt: time.Now(),
	}
	s.mu.Unlock()
	s.remotePacketMu.Lock()
	s.remotePacketTimes = make(map[packetTimingKey]packetTiming)
	s.remotePacketMu.Unlock()
	s.emit(EventStateChanged, map[string]any{
		"connected": true,
		"serverUrl": serverURL,
	})
	if msgType, _ := initMsg["type"].(string); msgType == "config" {
		s.emit(EventConfig, cloneMap(initMsg))
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		_ = s.sendJSON(map[string]any{
			"type":      "webrtc_ice",
			"candidate": candidate.ToJSON(),
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		shouldSendReady, shouldReconnect := s.handlePeerConnectionStateChange(connectionID, state)
		if shouldReconnect {
			s.emit(EventReconnectRequest, nil)
		}
		if shouldSendReady {
			_ = s.sendMessage(map[string]any{"type": "webrtc_ready"})
		}
		s.emit(EventStateChanged, map[string]any{
			"peerConnectionState": state.String(),
		})
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		s.mu.Lock()
		if s.connectionID != connectionID {
			s.mu.Unlock()
			return
		}
		s.state.ICEConnectionState = state.String()
		s.mu.Unlock()
		s.emit(EventStateChanged, map[string]any{
			"iceConnectionState": state.String(),
		})
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		codec := track.Codec().MimeType
		s.mu.Lock()
		if s.connectionID != connectionID {
			s.mu.Unlock()
			return
		}
		s.state.CurrentTrackCodecs[track.Kind().String()] = codec
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			s.state.VideoCodec = codec
		}
		s.mu.Unlock()
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			if resetter, ok := s.renderer.(VideoStreamResetter); ok {
				resetter.ResetVideoStream(codec)
			}
			go s.consumeVideoTrack(pc, track, lowLatency)
			return
		}
		go s.consumeAudioTrack(track)
	})

	dc.OnOpen(func() {
		if s.setInputChannelOpen(connectionID, true) {
			s.emit(EventStateChanged, map[string]any{
				"inputChannelOpen": true,
			})
		}
	})

	dc.OnClose(func() {
		if s.setInputChannelOpen(connectionID, false) {
			s.emit(EventStateChanged, map[string]any{
				"inputChannelOpen": false,
			})
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		_ = s.Disconnect()
		return fmt.Errorf("create offer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		_ = s.Disconnect()
		return fmt.Errorf("set local description: %w", err)
	}
	if err := s.sendJSON(map[string]any{
		"type": "webrtc_offer",
		"sdp":  pc.LocalDescription(),
	}); err != nil {
		_ = s.Disconnect()
		return fmt.Errorf("send webrtc offer: %w", err)
	}

	go s.readLoop(connectionID, conn, pc)

	// Wait for PeerConnection to connect or timeout
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
	}

	return nil
}
func numberToInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func (s *Session) Disconnect() error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	return s.disconnectLocked()
}

func (s *Session) handlePeerConnectionStateChange(connectionID uint64, state webrtc.PeerConnectionState) (shouldSendReady bool, shouldReconnect bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connectionID != connectionID {
		return false, false
	}

	s.state.WebRTCConnected = state == webrtc.PeerConnectionStateConnected
	s.state.PeerConnectionState = state.String()

	isDown := state == webrtc.PeerConnectionStateFailed ||
		state == webrtc.PeerConnectionStateDisconnected ||
		state == webrtc.PeerConnectionStateClosed

	if isDown {
		s.state.InputChannelOpen = false
		shouldReconnect = !s.state.ShutdownRequested
	}

	return state == webrtc.PeerConnectionStateConnected, shouldReconnect
}

func (s *Session) setInputChannelOpen(connectionID uint64, open bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connectionID != connectionID {
		return false
	}
	s.state.InputChannelOpen = open
	return true
}

func (s *Session) disconnectLocked() error {
	return s.disconnectIfCurrentLocked(s.connectionID)
}

func (s *Session) disconnectIfCurrent(connectionID uint64) error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	return s.disconnectIfCurrentLocked(connectionID)
}

func (s *Session) disconnectIfCurrentLocked(connectionID uint64) error {
	s.mu.Lock()
	if s.connectionID != connectionID {
		s.mu.Unlock()
		return nil
	}
	conn := s.conn
	pc := s.pc
	input := s.input
	udpConn := s.udpConn
	s.connectionID++
	s.conn = nil
	s.pc = nil
	s.input = nil
	s.udpConn = nil
	s.state.Connected = false
	s.state.WebRTCConnected = false
	s.state.PeerConnectionState = ""
	s.state.ICEConnectionState = ""
	s.state.InputChannelOpen = false
	s.state.Presenting = false
	s.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if input != nil {
		_ = input.Close()
	}
	if udpConn != nil {
		_ = udpConn.Close()
	}
	if pc != nil {
		done := make(chan struct{})
		go func() {
			_ = pc.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}
	}

	s.emit(EventStateChanged, map[string]any{
		"connected": false,
	})
	return nil
}
