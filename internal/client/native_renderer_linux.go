//go:build native && linux && cgo

package client

import (
	"fmt"
	"strings"
	"sync"
)

type NativeRenderer struct {
	title        string
	width        int
	height       int
	autoStart    bool
	fullscreen   bool
	probeLatency bool
	debugCursor  bool
	lowLatency   bool

	mu                      sync.RWMutex
	runStarted              bool
	decoderAwaitingKeyframe bool
	inputSink               func(map[string]any) error
	lifecycle               func(NativeWindowLifecycle)
	present                 func(NativeFramePresented)
	overlay                 OverlayState
	samples                 chan nativeVideoSample
	streamResets            chan string
	resizeRequests          chan nativeResizeRequest
	snapshotRequests        chan chan nativeSnapshotResult
	vsyncRequests           chan bool
	stopCh                  chan struct{}
	doneCh                  chan struct{}

	mouseX int32
	mouseY int32

	videoWidth  int32
	videoHeight int32
}

type nativeVideoSample struct {
	codec                        string
	data                         []byte
	packetTimestamp              uint32
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	receiveAt                    int64
}

type nativeDecodedSample struct {
	frame                        decodedFrame
	packetTimestamp              uint32
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	receiveAt                    int64
	decodeReadyAt                int64
}

type nativeResizeRequest struct {
	width  int
	height int
	result chan error
}

type nativeSnapshotResult struct {
	body []byte
	err  error
}

func NewNativeRenderer(opts NativeRendererOptions) (WindowRenderer, error) {
	width := opts.Width
	if width <= 0 {
		width = 1280
	}
	height := opts.Height
	if height <= 0 {
		height = 720
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "LLrdc Native Client"
	}
	return &NativeRenderer{
		title:                   title,
		width:                   width,
		height:                  height,
		autoStart:               opts.AutoStart,
		fullscreen:              opts.Fullscreen,
		probeLatency:            opts.ProbeLatency,
		debugCursor:             opts.DebugCursor,
		decoderAwaitingKeyframe: true,
		samples:                 make(chan nativeVideoSample, 8),
		streamResets:            make(chan string, 1),
		resizeRequests:          make(chan nativeResizeRequest, 4),
		snapshotRequests:        make(chan chan nativeSnapshotResult, 2),
		vsyncRequests:           make(chan bool, 2),
		stopCh:                  make(chan struct{}),
		doneCh:                  make(chan struct{}),
	}, nil
}

func (r *NativeRenderer) SetInputSink(fn func(map[string]any) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputSink = fn
}

func (r *NativeRenderer) SetLifecycleSink(fn func(NativeWindowLifecycle)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lifecycle = fn
}

func (r *NativeRenderer) SetPresentSink(fn func(NativeFramePresented)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.present = fn
}

func (r *NativeRenderer) SetOverlayState(state OverlayState) {
	r.mu.Lock()
	r.overlay = cloneOverlayState(state)
	r.mu.Unlock()
}

func (r *NativeRenderer) SetLatencyProbe(enabled bool) {
	r.mu.Lock()
	r.probeLatency = enabled
	r.mu.Unlock()
}

func (r *NativeRenderer) SetDebugCursor(enabled bool) {
	r.mu.Lock()
	r.debugCursor = enabled
	r.mu.Unlock()
}

func (r *NativeRenderer) SetLowLatency(enabled bool) {
	r.mu.Lock()
	changed := r.lowLatency != enabled
	r.lowLatency = enabled
	r.mu.Unlock()
	if !changed {
		return
	}

	select {
	case r.vsyncRequests <- !enabled:
	default:
		select {
		case <-r.vsyncRequests:
		default:
		}
		select {
		case r.vsyncRequests <- !enabled:
		default:
		}
	}
}

func (r *NativeRenderer) SetWindowSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid window size %dx%d", width, height)
	}
	r.mu.Lock()
	r.width = width
	r.height = height
	runStarted := r.runStarted
	r.mu.Unlock()
	if !runStarted {
		return nil
	}
	result := make(chan error, 1)
	req := nativeResizeRequest{width: width, height: height, result: result}
	select {
	case r.resizeRequests <- req:
	case <-r.stopCh:
		return fmt.Errorf("renderer has stopped")
	}
	return <-result
}

func (r *NativeRenderer) CaptureSnapshotPNG() ([]byte, error) {
	result := make(chan nativeSnapshotResult, 1)
	select {
	case r.snapshotRequests <- result:
	case <-r.stopCh:
		return nil, fmt.Errorf("renderer has stopped")
	}
	snapshot := <-result
	return snapshot.body, snapshot.err
}

func (r *NativeRenderer) Size() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.width, r.height
}

func (r *NativeRenderer) PreferredVideoCodec() string {
	return "vp8"
}

func (r *NativeRenderer) SupportedVideoCodecs() []string {
	return []string{"vp8", "h264"}
}

func (r *NativeRenderer) ResetVideoStream(codec string) {
	select {
	case r.streamResets <- codec:
	default:
		select {
		case <-r.streamResets:
		default:
		}
		r.streamResets <- codec
	}
}

func (r *NativeRenderer) HandleVideoFrame(codec string, frame []byte, packetTimestamp uint32) error {
	return r.handleVideoFrameWithTiming(codec, frame, packetTimestamp, 0, 0, 0, 0, benchmarkClockNowMs())
}

func (r *NativeRenderer) handleVideoFrameWithTiming(
	codec string,
	frame []byte,
	packetTimestamp uint32,
	firstPacketSequenceNumber uint16,
	firstDecryptedPacketQueuedAt int64,
	firstRemotePacketAt int64,
	firstPacketReadAt int64,
	receiveAt int64,
) error {
	if receiveAt <= 0 {
		receiveAt = benchmarkClockNowMs()
	}
	if firstDecryptedPacketQueuedAt <= 0 {
		firstDecryptedPacketQueuedAt = firstRemotePacketAt
	}
	if firstRemotePacketAt <= 0 {
		firstRemotePacketAt = firstPacketReadAt
	}
	if firstPacketReadAt <= 0 {
		firstPacketReadAt = receiveAt
	}
	if firstDecryptedPacketQueuedAt <= 0 {
		firstDecryptedPacketQueuedAt = firstPacketReadAt
	}
	if firstRemotePacketAt <= 0 {
		firstRemotePacketAt = receiveAt
	}
	sample := nativeVideoSample{
		codec:                        codec,
		data:                         append([]byte(nil), frame...),
		packetTimestamp:              packetTimestamp,
		firstPacketSequenceNumber:    firstPacketSequenceNumber,
		firstDecryptedPacketQueuedAt: firstDecryptedPacketQueuedAt,
		firstRemotePacketAt:          firstRemotePacketAt,
		firstPacketReadAt:            firstPacketReadAt,
		receiveAt:                    receiveAt,
	}
	r.mu.RLock()
	lowLatency := r.lowLatency
	r.mu.RUnlock()
	if lowLatency {
		for len(r.samples) >= 1 {
			select {
			case <-r.samples:
			default:
				goto enqueueSample
			}
		}
	}
enqueueSample:
	select {
	case r.samples <- sample:
		return nil
	default:
		select {
		case <-r.samples:
		default:
		}
		r.samples <- sample
		return nil
	}
}

func (r *NativeRenderer) RequestKeyframe() {
	r.mu.Lock()
	r.decoderAwaitingKeyframe = true
	r.mu.Unlock()
	r.emitLifecycle(NativeWindowLifecycle{DecoderStateChanged: true, DecoderAwaitingKeyframe: true})
}

func (r *NativeRenderer) Close() error {
	r.Stop()
	return nil
}

func (r *NativeRenderer) Stop() {
	select {
	case <-r.stopCh:
		r.mu.RLock()
		runStarted := r.runStarted
		r.mu.RUnlock()
		if runStarted {
			<-r.doneCh
		}
		return
	default:
		close(r.stopCh)
	}
	r.mu.RLock()
	runStarted := r.runStarted
	r.mu.RUnlock()
	if !runStarted {
		return
	}
	<-r.doneCh
}
