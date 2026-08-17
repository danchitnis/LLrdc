package client

type WindowRenderer interface {
	Renderer
	Run() error
	Stop()
	SetInputSink(func(map[string]any) error)
	SetLifecycleSink(func(NativeWindowLifecycle))
	UpdateMouse(x, y float64)
	SetPresentSink(func(NativeFramePresented))
	SetOverlayState(OverlayState)
	SetLatencyProbe(enabled bool)
	SetDebugCursor(enabled bool)
	SetWindowSize(width, height int) error
	CaptureSnapshotPNG() ([]byte, error)
	Size() (int, int)
	MenuItemIndexAt(x, y float64, itemCount int) int
}
type VideoStreamResetter interface {
	ResetVideoStream(codec string)
}

type LowLatencyRenderer interface {
	SetLowLatency(enabled bool)
}

type NativeRendererOptions struct {
	Title        string
	Width        int
	Height       int
	AutoStart    bool
	Fullscreen   bool
	ProbeLatency bool
	DebugCursor  bool
	LowLatency   bool
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
	PointerInside           bool
	HasSurface              bool
	Desktop                 int
	RenderLoopStarted       bool
	PresentationClockID     uint32
	DecoderStateChanged     bool
	DecoderAwaitingKeyframe bool
	DecodeError             bool
	Error                   string
}

type NativeFramePresented struct {
	Width                        int
	Height                       int
	PacketTimestamp              int64
	FirstPacketSequenceNumber    uint16
	Brightness                   int
	ProbeMarker                  int
	FirstDecryptedPacketQueuedAt int64
	FirstRemotePacketAt          int64
	FirstPacketReadAt            int64
	ReceiveAt                    int64
	ReceiveNs                    int64
	DecodeReadyAt                int64
	DecodeReadyNs                int64
	PresentationAt               int64
	PresentationSource           string
	CompositorPresentedAt        int64
	CompositorPresentedNs        int64
	RenderSubmittedNs            int64
}

type LatencyBreakdown struct {
	PacketTimestamp              int64  `json:"packetTimestamp"`
	FirstPacketSequenceNumber    uint16 `json:"firstPacketSequenceNumber,omitempty"`
	Brightness                   int    `json:"brightness"`
	ProbeMarker                  int    `json:"probeMarker,omitempty"`
	FirstDecryptedPacketQueuedAt int64  `json:"firstDecryptedPacketQueuedAt,omitempty"`
	FirstRemotePacketAt          int64  `json:"firstRemotePacketAt,omitempty"`
	FirstPacketReadAt            int64  `json:"firstPacketReadAt,omitempty"`
	ReceiveAt                    int64  `json:"receiveAt"`
	DecodeReadyAt                int64  `json:"decodeReadyAt"`
	PresentationAt               int64  `json:"presentationAt"`
	CompositorPresentedAt        int64  `json:"compositorPresentedAt,omitempty"`
	PresentationSource           string `json:"presentationSource,omitempty"`
	CompositorPresentedNs        int64  `json:"compositorPresentedNs,omitempty"`
	RenderSubmittedNs            int64  `json:"renderSubmittedNs,omitempty"`
	ReceiveNs                    int64  `json:"receiveNs,omitempty"`
	DecodeReadyNs                int64  `json:"decodeReadyNs,omitempty"`
}

type LocalInputSample struct {
	Type   string  `json:"type"`
	Action string  `json:"action,omitempty"`
	Button int     `json:"button,omitempty"`
	Key    string  `json:"key,omitempty"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	AtMs   int64   `json:"atMs"`
}

type OverlayColor struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
	A uint8 `json:"a"`
}

type OverlayState struct {
	HUDLines      []string     `json:"hudLines,omitempty"`
	HUDColor      OverlayColor `json:"hudColor"`
	MenuVisible   bool         `json:"menuVisible"`
	MenuTitle     string       `json:"menuTitle,omitempty"`
	MenuHint      string       `json:"menuHint,omitempty"`
	MenuItems     []string     `json:"menuItems,omitempty"`
	SelectedIndex int          `json:"selectedIndex,omitempty"`
}

type TimedVideoFrameHandler interface {
	HandleVideoFrameWithTiming(
		codec string,
		frame []byte,
		packetTimestamp int64,
		firstPacketSequenceNumber uint16,
		firstDecryptedPacketQueuedAt int64,
		firstRemotePacketAt int64,
		firstPacketReadAt int64,
		receiveAt int64,
	) error
}

type TimedVideoFrameHandlerNs interface {
	HandleVideoFrameWithTimingNs(codec string, frame []byte, packetTimestamp int64, receiveNs int64) error
}
