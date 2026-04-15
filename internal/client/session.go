package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

type Renderer interface {
	HandleVideoFrame(codec string, frame []byte, packetTimestamp uint32) error
	RequestKeyframe()
	Close() error
}

type NullRenderer struct{}

func (NullRenderer) HandleVideoFrame(_ string, _ []byte, _ uint32) error { return nil }
func (NullRenderer) RequestKeyframe()                                   {}
func (NullRenderer) Close() error                                        { return nil }

type Event string

const (
	EventStateChanged Event = "state_changed"
	EventConfig       Event = "config"
	EventStats        Event = "stats"
	EventInputSent    Event = "input_sent"
	EventFrame        Event = "frame"
	EventError        Event = "error"
)

type EventPayload struct {
	Type string         `json:"type"`
	At   time.Time      `json:"at"`
	Data map[string]any `json:"data,omitempty"`
}

type HookBus struct {
	mu        sync.RWMutex
	listeners map[Event][]func(EventPayload)
}

func NewHookBus() *HookBus {
	return &HookBus{
		listeners: make(map[Event][]func(EventPayload)),
	}
}

func (b *HookBus) On(event Event, fn func(EventPayload)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[event] = append(b.listeners[event], fn)
	idx := len(b.listeners[event]) - 1
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		current := b.listeners[event]
		if idx < 0 || idx >= len(current) || current[idx] == nil {
			return
		}
		current[idx] = nil
	}
}

func (b *HookBus) Emit(event Event, payload EventPayload) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, fn := range b.listeners[event] {
		if fn != nil {
			fn(payload)
		}
	}
}

type SessionState struct {
	ServerURL               string            `json:"serverUrl"`
	BuildID                 string            `json:"buildId,omitempty"`
	Connected               bool              `json:"connected"`
	WebRTCConnected         bool              `json:"webrtcConnected"`
	InputChannelOpen        bool              `json:"inputChannelOpen"`
	RenderLoopStarted       bool              `json:"renderLoopStarted"`
	ShutdownRequested       bool              `json:"shutdownRequested"`
	ShutdownReason          string            `json:"shutdownReason,omitempty"`
	WindowBackend           string            `json:"windowBackend,omitempty"`
	WindowID                uint64            `json:"windowId,omitempty"`
	WindowCreated           bool              `json:"windowCreated"`
	WindowShown             bool              `json:"windowShown"`
	WindowMapped            bool              `json:"windowMapped"`
	WindowVisible           bool              `json:"windowVisible"`
	WindowEvent             string            `json:"windowEvent,omitempty"`
	WindowFlags             uint32            `json:"windowFlags,omitempty"`
	WindowHasFocus          bool              `json:"windowHasFocus"`
	WindowHasSurface        bool              `json:"windowHasSurface"`
	WindowDesktop           int               `json:"windowDesktop"`
	Presenting              bool              `json:"presenting"`
	DecoderAwaitingKeyframe bool              `json:"decoderAwaitingKeyframe"`
	VideoCodec              string            `json:"videoCodec"`
	LastConfig              map[string]any    `json:"lastConfig,omitempty"`
	LastStats               map[string]any    `json:"lastStats,omitempty"`
	LastError               string            `json:"lastError,omitempty"`
	LastMessageAt           time.Time         `json:"lastMessageAt,omitempty"`
	LastVideoPacketAt       time.Time         `json:"lastVideoPacketAt,omitempty"`
	LastVideoFrameAt        time.Time         `json:"lastVideoFrameAt,omitempty"`
	LastPresentAt           time.Time         `json:"lastPresentAt,omitempty"`
	FirstFramePresentedAt   time.Time            `json:"firstFramePresentedAt,omitempty"`
	LastLatencySample       map[string]any       `json:"lastLatencySample,omitempty"`
	RecentLatencySamples    []LatencyBreakdown   `json:"recentLatencySamples,omitempty"`
	CurrentTrackCodecs      map[string]string    `json:"currentTrackCodecs,omitempty"`
}

type SessionStats struct {
	SignalingMessages uint64    `json:"signalingMessages"`
	InputMessagesSent uint64    `json:"inputMessagesSent"`
	VideoPackets      uint64    `json:"videoPackets"`
	VideoFrames       uint64    `json:"videoFrames"`
	PresentedFrames   uint64    `json:"presentedFrames"`
	DecodeErrors      uint64    `json:"decodeErrors"`
	VideoBytes        uint64    `json:"videoBytes"`
	AudioPackets      uint64    `json:"audioPackets"`
	AudioBytes        uint64    `json:"audioBytes"`
	ConnectedAt       time.Time `json:"connectedAt,omitempty"`
}

type Session struct {
	renderer Renderer
	hooks    *HookBus

	mu     sync.RWMutex
	wsMu   sync.Mutex
	conn   *websocket.Conn
	pc     *webrtc.PeerConnection
	input  *webrtc.DataChannel
	state  SessionState
	stats  SessionStats
	closed chan struct{}

	keyframeRequests chan struct{}
}

func NewSession(renderer Renderer) *Session {
	if renderer == nil {
		renderer = NullRenderer{}
	}
	return &Session{
		renderer: renderer,
		hooks:    NewHookBus(),
		state: SessionState{
			WindowDesktop:      -1,
			CurrentTrackCodecs: make(map[string]string),
		},
		closed:           make(chan struct{}),
		keyframeRequests: make(chan struct{}, 1),
	}
}

func (s *Session) Hooks() *HookBus {
	return s.hooks
}

func (s *Session) State() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copyState := s.state
	copyState.LastConfig = cloneMap(s.state.LastConfig)
	copyState.LastStats = cloneMap(s.state.LastStats)
	copyState.LastLatencySample = cloneMap(s.state.LastLatencySample)
	if s.state.RecentLatencySamples != nil {
		copyState.RecentLatencySamples = make([]LatencyBreakdown, len(s.state.RecentLatencySamples))
		copy(copyState.RecentLatencySamples, s.state.RecentLatencySamples)
	}
	copyState.CurrentTrackCodecs = cloneMapString(s.state.CurrentTrackCodecs)
	return copyState
}

func (s *Session) Stats() SessionStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *Session) UpdateWindowState(state NativeWindowLifecycle) {
	s.mu.Lock()
	if state.Backend != "" {
		s.state.WindowBackend = state.Backend
	}
	if state.WindowID != 0 {
		s.state.WindowID = state.WindowID
	}
	if state.RenderLoopStarted {
		s.state.RenderLoopStarted = true
	}
	if state.Created {
		s.state.WindowCreated = true
	}
	s.state.WindowShown = state.Shown
	s.state.WindowMapped = state.Mapped
	s.state.WindowVisible = state.Visible
	if state.Event != "" {
		s.state.WindowEvent = state.Event
	}
	if state.Flags != 0 {
		s.state.WindowFlags = state.Flags
	}
	s.state.WindowHasFocus = state.HasFocus
	s.state.WindowHasSurface = state.HasSurface
	if state.Desktop >= 0 {
		s.state.WindowDesktop = state.Desktop
	}
	if state.DecoderStateChanged {
		s.state.DecoderAwaitingKeyframe = state.DecoderAwaitingKeyframe
	}
	if state.DecodeError {
		s.stats.DecodeErrors++
		select {
		case s.keyframeRequests <- struct{}{}:
		default:
		}
	}
	if state.Error != "" {
		s.state.LastError = state.Error
	}
	s.mu.Unlock()
	current := s.State()
	s.emit(EventStateChanged, map[string]any{
		"windowBackend":           current.WindowBackend,
		"windowId":                current.WindowID,
		"windowCreated":           current.WindowCreated,
		"windowShown":             current.WindowShown,
		"windowMapped":            current.WindowMapped,
		"windowVisible":           current.WindowVisible,
		"windowEvent":             current.WindowEvent,
		"windowFlags":             current.WindowFlags,
		"windowHasFocus":          current.WindowHasFocus,
		"windowHasSurface":        current.WindowHasSurface,
		"windowDesktop":           current.WindowDesktop,
		"presenting":              current.Presenting,
		"renderLoopStarted":       current.RenderLoopStarted,
		"decoderAwaitingKeyframe": current.DecoderAwaitingKeyframe,
		"windowError":             state.Error,
	})
}

func (s *Session) SetBuildID(buildID string) {
	s.mu.Lock()
	s.state.BuildID = strings.TrimSpace(buildID)
	s.mu.Unlock()
}

func (s *Session) RequestShutdown(reason string) {
	s.mu.Lock()
	s.state.ShutdownRequested = true
	s.state.ShutdownReason = strings.TrimSpace(reason)
	s.mu.Unlock()
}

func (s *Session) ClearShutdown() {
	s.mu.Lock()
	s.state.ShutdownRequested = false
	s.state.ShutdownReason = ""
	s.mu.Unlock()
}

func (s *Session) Connect(serverURL string) error {
	if strings.TrimSpace(serverURL) == "" {
		return errors.New("server URL is required")
	}

	if err := s.Disconnect(); err != nil {
		return err
	}

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

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return fmt.Errorf("register default codecs: %w", err)
	}

	se := webrtc.SettingEngine{}
	se.DisableSRTPReplayProtection(true)
	se.DisableSRTCPReplayProtection(true)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se))

	pc, err := api.NewPeerConnection(webrtc.Configuration{
		BundlePolicy: webrtc.BundlePolicyMaxBundle,
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create peer connection: %w", err)
	}

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

	s.mu.Lock()
	s.conn = conn
	s.pc = pc
	s.input = dc
	s.state.ServerURL = serverURL
	s.state.Connected = true
	s.state.WebRTCConnected = false
	s.state.InputChannelOpen = false
	s.state.VideoCodec = ""
	s.state.LastConfig = nil
	s.state.LastStats = nil
	s.state.LastMessageAt = time.Time{}
	s.state.LastVideoPacketAt = time.Time{}
	s.state.LastVideoFrameAt = time.Time{}
	s.state.LastPresentAt = time.Time{}
	s.state.FirstFramePresentedAt = time.Time{}
	s.state.LastLatencySample = nil
	s.state.DecoderAwaitingKeyframe = true
	s.state.Presenting = false
	s.state.CurrentTrackCodecs = make(map[string]string)
	s.stats = SessionStats{
		ConnectedAt: time.Now(),
	}
	s.mu.Unlock()
	s.emit(EventStateChanged, map[string]any{
		"connected": true,
		"serverUrl": serverURL,
	})

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
		s.mu.Lock()
		s.state.WebRTCConnected = state == webrtc.PeerConnectionStateConnected
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateDisconnected || state == webrtc.PeerConnectionStateClosed {
			s.state.InputChannelOpen = false
		}
		s.mu.Unlock()
		if state == webrtc.PeerConnectionStateConnected {
			_ = s.sendMessage(map[string]any{"type": "webrtc_ready"})
		}
		s.emit(EventStateChanged, map[string]any{
			"peerConnectionState": state.String(),
		})
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		codec := track.Codec().MimeType
		s.mu.Lock()
		s.state.CurrentTrackCodecs[track.Kind().String()] = codec
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			s.state.VideoCodec = codec
		}
		s.mu.Unlock()
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			if resetter, ok := s.renderer.(VideoStreamResetter); ok {
				resetter.ResetVideoStream(codec)
			}
			go s.consumeVideoTrack(pc, track)
			return
		}
		go s.consumeAudioTrack(track)
	})

	dc.OnOpen(func() {
		s.mu.Lock()
		s.state.InputChannelOpen = true
		s.mu.Unlock()
		s.emit(EventStateChanged, map[string]any{
			"inputChannelOpen": true,
		})
	})

	dc.OnClose(func() {
		s.mu.Lock()
		s.state.InputChannelOpen = false
		s.mu.Unlock()
		s.emit(EventStateChanged, map[string]any{
			"inputChannelOpen": false,
		})
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

	go s.readLoop(conn, pc)
	return nil
}

func (s *Session) Disconnect() error {
	s.mu.Lock()
	conn := s.conn
	pc := s.pc
	input := s.input
	s.conn = nil
	s.pc = nil
	s.input = nil
	s.state.Connected = false
	s.state.WebRTCConnected = false
	s.state.InputChannelOpen = false
	s.state.Presenting = false
	s.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if input != nil {
		_ = input.Close()
	}
	if pc != nil {
		done := make(chan struct{})
		go func() {
			_ = pc.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(1500 * time.Millisecond):
		}
	}

	s.emit(EventStateChanged, map[string]any{
		"connected": false,
	})
	return nil
}

func (s *Session) RecordPresentedFrame(event NativeFramePresented) {
	s.mu.Lock()
	s.stats.PresentedFrames++
	now := time.Now()
	s.state.LastPresentAt = now
	s.state.Presenting = true
	if s.state.FirstFramePresentedAt.IsZero() {
		s.state.FirstFramePresentedAt = now
	}

	sample := LatencyBreakdown{
		PacketTimestamp: event.PacketTimestamp,
		Brightness:      event.Brightness,
		ReceiveAt:       event.ReceiveAt.UnixMilli(),
		DecodeReadyAt:   event.DecodeReadyAt.UnixMilli(),
		PresentationAt:  event.PresentationAt.UnixMilli(),
	}
	s.state.RecentLatencySamples = append(s.state.RecentLatencySamples, sample)
	if len(s.state.RecentLatencySamples) > 300 {
		s.state.RecentLatencySamples = s.state.RecentLatencySamples[1:]
	}

	s.mu.Unlock()
	s.emit(EventFrame, map[string]any{
		"presented":       true,
		"width":           event.Width,
		"height":          event.Height,
		"packetTimestamp": event.PacketTimestamp,
		"brightness":      event.Brightness,
	})
}

func (s *Session) RecordDecodeAwaitingKeyframe(awaiting bool) {
	s.mu.Lock()
	s.state.DecoderAwaitingKeyframe = awaiting
	s.mu.Unlock()
	s.emit(EventStateChanged, map[string]any{
		"decoderAwaitingKeyframe": awaiting,
	})
}

func (s *Session) SendResize(width, height int) error {
	return s.sendMessage(map[string]any{
		"type":   "resize",
		"width":  width,
		"height": height,
	})
}

func (s *Session) SendConfig(config map[string]any) error {
	msg := cloneMap(config)
	msg["type"] = "config"
	return s.sendMessage(msg)
}

func (s *Session) SendInput(msg map[string]any) error {
	s.emit(EventInputSent, cloneMap(msg))
	return s.sendMessage(msg)
}

func (s *Session) sendMessage(msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if msgType, _ := msg["type"].(string); msgType == "mousemove" || msgType == "mousebtn" || msgType == "keydown" || msgType == "keyup" || msgType == "wheel" {
		s.stats.InputMessagesSent++
	}
	input := s.input
	conn := s.conn
	s.mu.Unlock()

	if input != nil && input.ReadyState() == webrtc.DataChannelStateOpen {
		return input.SendText(string(body))
	}

	if conn == nil {
		return errors.New("session is not connected")
	}
	return s.sendRaw(body)
}

func (s *Session) readLoop(conn *websocket.Conn, pc *webrtc.PeerConnection) {
	for {
		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			s.setError(err)
			_ = s.Disconnect()
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.setError(err)
			continue
		}

		s.mu.Lock()
		s.stats.SignalingMessages++
		s.state.LastMessageAt = time.Now()
		s.mu.Unlock()

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "webrtc_answer":
			answerBytes, _ := json.Marshal(msg["sdp"])
			var answer webrtc.SessionDescription
			if err := json.Unmarshal(answerBytes, &answer); err != nil {
				s.setError(err)
				continue
			}
			if err := pc.SetRemoteDescription(answer); err != nil {
				s.setError(err)
			}
		case "webrtc_ice":
			candidateBytes, _ := json.Marshal(msg["candidate"])
			var candidate webrtc.ICECandidateInit
			if err := json.Unmarshal(candidateBytes, &candidate); err != nil {
				s.setError(err)
				continue
			}
			if err := pc.AddICECandidate(candidate); err != nil {
				s.setError(err)
			}
		case "config":
			s.mu.Lock()
			s.state.LastConfig = cloneMap(msg)
			if codec, ok := msg["videoCodec"].(string); ok {
				s.state.VideoCodec = codec
			}
			s.mu.Unlock()
			s.emit(EventConfig, cloneMap(msg))
		case "stats":
			s.mu.Lock()
			s.state.LastStats = cloneMap(msg)
			s.mu.Unlock()
			s.emit(EventStats, cloneMap(msg))
		default:
			s.emit(EventStateChanged, cloneMap(msg))
		}
	}
}

func (s *Session) consumeVideoTrack(pc *webrtc.PeerConnection, track *webrtc.TrackRemote) {
	codecName := strings.ToLower(track.Codec().MimeType)
	var builder *samplebuilder.SampleBuilder
	if strings.Contains(codecName, "vp8") {
		builder = samplebuilder.New(256, &codecs.VP8Packet{}, 90000)
	}
	stopKeyframeRequests := make(chan struct{})
	var stopKeyframeOnce sync.Once
	stopKeyframe := func() {
		stopKeyframeOnce.Do(func() {
			close(stopKeyframeRequests)
		})
	}
	if builder != nil && pc != nil {
		go requestInitialKeyframes(pc, uint32(track.SSRC()), stopKeyframeRequests)

		go func() {
			for {
				select {
				case <-stopKeyframeRequests:
					return
				case <-s.keyframeRequests:
					fmt.Printf("DEBUG: Sending PLI on video track (SSRC: %d) due to decode error or packet loss\n", track.SSRC())
					_ = pc.WriteRTCP([]rtcp.Packet{
						&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())},
					})
				}
			}
		}()
	}
	defer stopKeyframe()

	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			s.setError(err)
			return
		}

		s.mu.Lock()
		s.stats.VideoPackets++
		s.stats.VideoBytes += uint64(packet.MarshalSize())
		s.state.LastVideoPacketAt = time.Now()
		s.mu.Unlock()

		if builder == nil {
			continue
		}

		builder.Push(packet)
		for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
			s.mu.Lock()
			s.stats.VideoFrames++
			s.state.LastVideoFrameAt = time.Now()
			s.mu.Unlock()
			if shouldStopInitialKeyframeRequests(track.Codec().MimeType, sample.Data) {
				stopKeyframe()
			}

			if err := s.renderer.HandleVideoFrame(track.Codec().MimeType, sample.Data, sample.PacketTimestamp); err != nil {
				s.setError(err)
			}

			if sample.PrevDroppedPackets > 0 {
				s.renderer.RequestKeyframe()
				select {
				case s.keyframeRequests <- struct{}{}:
				default:
				}
			}

			s.emit(EventFrame, map[string]any{
				"codec":           track.Codec().MimeType,
				"packetTimestamp": sample.PacketTimestamp,
				"size":            len(sample.Data),
				"droppedPackets":  sample.PrevDroppedPackets,
			})
		}
	}
}

func shouldStopInitialKeyframeRequests(codec string, frame []byte) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	if strings.Contains(codec, "vp8") {
		return isVP8KeyframePayload(frame)
	}
	return len(frame) > 0
}

func isVP8KeyframePayload(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return data[0]&0x01 == 0
}

func requestInitialKeyframes(pc *webrtc.PeerConnection, mediaSSRC uint32, stop <-chan struct{}) {
	requestKeyframe := func() bool {
		if err := pc.WriteRTCP([]rtcp.Packet{
			&rtcp.PictureLossIndication{MediaSSRC: mediaSSRC},
		}); err != nil {
			return false
		}
		return true
	}

	if !requestKeyframe() {
		return
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()

	for {
		select {
		case <-stop:
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			if !requestKeyframe() {
				return
			}
		}
	}
}

func (s *Session) consumeAudioTrack(track *webrtc.TrackRemote) {
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			s.setError(err)
			return
		}

		s.mu.Lock()
		s.stats.AudioPackets++
		s.stats.AudioBytes += uint64(packet.MarshalSize())
		s.mu.Unlock()
	}
}

func (s *Session) emit(event Event, data map[string]any) {
	s.hooks.Emit(event, EventPayload{
		Type: string(event),
		At:   time.Now(),
		Data: data,
	})
}

func (s *Session) sendJSON(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.sendRaw(body)
}

func (s *Session) sendRaw(body []byte) error {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return errors.New("session is not connected")
	}

	return conn.WriteMessage(websocket.TextMessage, body)
}

func (s *Session) setError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.state.LastError = err.Error()
	s.mu.Unlock()
	s.emit(EventError, map[string]any{
		"error": err.Error(),
	})
}

func httpToWebsocketURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneMapString(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
