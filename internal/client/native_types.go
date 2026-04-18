package client

import "time"

type WindowRenderer interface {
	Renderer
	Run() error
	Stop()
	SetInputSink(func(map[string]any) error)
	SetLifecycleSink(func(NativeWindowLifecycle))
	UpdateMouse(x, y float64)
	SetPresentSink(func(NativeFramePresented))
	Size() (int, int)
}

type VideoStreamResetter interface {
	ResetVideoStream(codec string)
}

type WebSocketVideoFallbackProvider interface {
	SupportsWebSocketVideoFallback() bool
}

type NativeRendererOptions struct {
	Title        string
	Width        int
	Height       int
	AutoStart    bool
	ProbeLatency bool
	DebugCursor  bool
}
type NativeWindowLifecycle struct {
	Backend                 string
	WindowID                uint64
	Width                   int
	Height                  int
	Created                 bool
	Shown                   bool
	Mapped                  bool
	Visible                 bool
	Event                   string
	Flags                   uint32
	HasFocus                bool
	HasSurface              bool
	Desktop                 int
	RenderLoopStarted       bool
	DecoderStateChanged     bool
	DecoderAwaitingKeyframe bool
	DecodeError             bool
	Error                   string
}

type NativeFramePresented struct {
	Width           int
	Height          int
	PacketTimestamp uint32
	Brightness      int
	ReceiveAt       time.Time
	DecodeReadyAt   time.Time
	PresentationAt  time.Time
}

type LatencyBreakdown struct {
	PacketTimestamp uint32 `json:"packetTimestamp"`
	Brightness      int    `json:"brightness"`
	ReceiveAt       int64  `json:"receiveAt"`
	DecodeReadyAt   int64  `json:"decodeReadyAt"`
	PresentationAt  int64  `json:"presentationAt"`
}
