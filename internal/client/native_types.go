package client

type WindowRenderer interface {
	Renderer
	Run() error
	Stop()
	SetInputSink(func(map[string]any) error)
	SetLifecycleSink(func(NativeWindowLifecycle))
	SetPresentSink(func(NativeFramePresented))
	Size() (int, int)
}

type VideoStreamResetter interface {
	ResetVideoStream(codec string)
}

type NativeRendererOptions struct {
        Title     string
        Width     int
        Height    int
        AutoStart bool
}
type NativeWindowLifecycle struct {
	Backend                 string
	WindowID                uint64
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
}
