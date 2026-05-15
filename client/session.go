package client

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type Renderer interface {
	HandleVideoFrame(codec string, frame []byte, packetTimestamp uint32) error
	RequestKeyframe()
	Close() error
}

type PreferredVideoCodecProvider interface {
	PreferredVideoCodec() string
}

type SupportedVideoCodecsProvider interface {
	SupportedVideoCodecs() []string
}

type NullRenderer struct{}

func (NullRenderer) HandleVideoFrame(_ string, _ []byte, _ uint32) error { return nil }
func (NullRenderer) RequestKeyframe()                                    {}
func (NullRenderer) Close() error                                        { return nil }

type Event string

const (
	EventStateChanged     Event = "state_changed"
	EventConfig           Event = "config"
	EventStats            Event = "stats"
	EventInputSent        Event = "input_sent"
	EventFrame            Event = "frame"
	EventError            Event = "error"
	EventReconnectRequest Event = "reconnect_request"
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
	ServerURL               string             `json:"serverUrl"`
	BuildID                 string             `json:"buildId,omitempty"`
	WindowWidth             int                `json:"windowWidth,omitempty"`
	WindowHeight            int                `json:"windowHeight,omitempty"`
	LastResizeWidth         int                `json:"lastResizeWidth,omitempty"`
	LastResizeHeight        int                `json:"lastResizeHeight,omitempty"`
	LastResizeAt            time.Time          `json:"lastResizeAt,omitempty"`
	LastPresentedWidth      int                `json:"lastPresentedWidth,omitempty"`
	LastPresentedHeight     int                `json:"lastPresentedHeight,omitempty"`
	ServerScreenWidth       int                `json:"serverScreenWidth,omitempty"`
	ServerScreenHeight      int                `json:"serverScreenHeight,omitempty"`
	Connected               bool               `json:"connected"`
	WebRTCConnected         bool               `json:"webrtcConnected"`
	PeerConnectionState     string             `json:"peerConnectionState,omitempty"`
	ICEConnectionState      string             `json:"iceConnectionState,omitempty"`
	InputChannelOpen        bool               `json:"inputChannelOpen"`
	RenderLoopStarted       bool               `json:"renderLoopStarted"`
	ShutdownRequested       bool               `json:"shutdownRequested"`
	ShutdownReason          string             `json:"shutdownReason,omitempty"`
	WindowBackend           string             `json:"windowBackend,omitempty"`
	WindowID                uint64             `json:"windowId,omitempty"`
	WindowCreated           bool               `json:"windowCreated"`
	WindowShown             bool               `json:"windowShown"`
	WindowMapped            bool               `json:"windowMapped"`
	WindowVisible           bool               `json:"windowVisible"`
	WindowEvent             string             `json:"windowEvent,omitempty"`
	WindowFlags             uint32             `json:"windowFlags,omitempty"`
	WindowHasFocus          bool               `json:"windowHasFocus"`
	WindowPointerInside     bool               `json:"windowPointerInside"`
	WindowHasSurface        bool               `json:"windowHasSurface"`
	WindowDesktop           int                `json:"windowDesktop"`
	Presenting              bool               `json:"presenting"`
	DecoderAwaitingKeyframe bool               `json:"decoderAwaitingKeyframe"`
	VideoCodec              string             `json:"videoCodec"`
	LastConfig              map[string]any     `json:"lastConfig,omitempty"`
	LastStats               map[string]any     `json:"lastStats,omitempty"`
	LastError               string             `json:"lastError,omitempty"`
	LastMessageAt           time.Time          `json:"lastMessageAt,omitempty"`
	LastVideoPacketAt       time.Time          `json:"lastVideoPacketAt,omitempty"`
	LastVideoFrameAt        time.Time          `json:"lastVideoFrameAt,omitempty"`
	LastPresentAt           time.Time          `json:"lastPresentAt,omitempty"`
	FirstFramePresentedAt   time.Time          `json:"firstFramePresentedAt,omitempty"`
	LastLatencySample       map[string]any     `json:"lastLatencySample,omitempty"`
	RecentLatencySamples    []LatencyBreakdown `json:"recentLatencySamples,omitempty"`
	RecentLocalInputSamples []LocalInputSample `json:"recentLocalInputSamples,omitempty"`
	RecentVideoByteSamples  []TimedByteSample  `json:"recentVideoByteSamples,omitempty"`
	CurrentTrackCodecs      map[string]string  `json:"currentTrackCodecs,omitempty"`
}

type TimedByteSample struct {
	AtMs  int64 `json:"atMs"`
	Bytes int   `json:"bytes"`
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

	mu           sync.RWMutex
	wsMu         sync.Mutex
	connectMu    sync.Mutex
	connecting   atomic.Bool
	connectionID uint64
	conn         *websocket.Conn
	pc           *webrtc.PeerConnection
	input        *webrtc.DataChannel
	udpConn      *net.UDPConn
	state        SessionState
	stats        SessionStats
	closed       chan struct{}

	keyframeRequests chan struct{}

	remotePacketMu    sync.Mutex
	remotePacketTimes map[packetTimingKey]packetTiming
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
		closed:            make(chan struct{}),
		keyframeRequests:  make(chan struct{}, 1),
		remotePacketTimes: make(map[packetTimingKey]packetTiming),
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
	if s.state.RecentLocalInputSamples != nil {
		copyState.RecentLocalInputSamples = make([]LocalInputSample, len(s.state.RecentLocalInputSamples))
		copy(copyState.RecentLocalInputSamples, s.state.RecentLocalInputSamples)
	}
	if s.state.RecentVideoByteSamples != nil {
		copyState.RecentVideoByteSamples = make([]TimedByteSample, len(s.state.RecentVideoByteSamples))
		copy(copyState.RecentVideoByteSamples, s.state.RecentVideoByteSamples)
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
	if state.Width > 0 {
		s.state.WindowWidth = state.Width
	}
	if state.Height > 0 {
		s.state.WindowHeight = state.Height
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
	s.state.WindowPointerInside = state.PointerInside
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
		"windowWidth":             current.WindowWidth,
		"windowHeight":            current.WindowHeight,
		"windowCreated":           current.WindowCreated,
		"windowShown":             current.WindowShown,
		"windowMapped":            current.WindowMapped,
		"windowVisible":           current.WindowVisible,
		"windowEvent":             current.WindowEvent,
		"windowFlags":             current.WindowFlags,
		"windowHasFocus":          current.WindowHasFocus,
		"windowPointerInside":     current.WindowPointerInside,
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
