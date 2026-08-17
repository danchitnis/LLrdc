//go:build linux && native && cgo

package wayland

import (
	"fmt"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/client"
)

type NativeRenderer struct {
	title               string
	width               int
	height              int
	autoStart           bool
	fullscreen          bool
	probeLatency        bool
	debugCursor         bool
	videoWidth          int32
	videoHeight         int32
	mouseX              int32
	mouseY              int32
	lowLatency          bool
	presentedFrameCount uint64

	mu                      sync.RWMutex
	runStarted              bool
	decoderAwaitingKeyframe bool
	inputSink               func(map[string]any) error
	lifecycle               func(client.NativeWindowLifecycle)
	present                 func(client.NativeFramePresented)
	overlay                 client.OverlayState
	samples                 chan nativeVideoSample
	decoderResets           chan string
	streamResets            chan string
	resizeRequests          chan nativeResizeRequest
	snapshotRequests        chan chan nativeSnapshotResult
	vsyncRequests           chan bool
	stopCh                  chan struct{}
	doneCh                  chan struct{}
}

func NewNativeRenderer(opts client.NativeRendererOptions) (*NativeRenderer, error) {
	if opts.Width <= 0 {
		opts.Width = 1280
	}
	if opts.Height <= 0 {
		opts.Height = 720
	}
	return &NativeRenderer{
		title:                   opts.Title,
		width:                   opts.Width,
		height:                  opts.Height,
		autoStart:               opts.AutoStart,
		fullscreen:              opts.Fullscreen,
		probeLatency:            opts.ProbeLatency,
		debugCursor:             opts.DebugCursor,
		decoderAwaitingKeyframe: true,
		samples:                 make(chan nativeVideoSample, 8),
		decoderResets:           make(chan string, 4),
		streamResets:            make(chan string, 4),
		resizeRequests:          make(chan nativeResizeRequest, 4),

		snapshotRequests: make(chan chan nativeSnapshotResult, 2),
		vsyncRequests:    make(chan bool, 2),
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
	}, nil
}

func (r *NativeRenderer) SetInputSink(sink func(map[string]any) error) {
	r.mu.Lock()
	r.inputSink = sink
	r.mu.Unlock()
}

func (r *NativeRenderer) SetLifecycleSink(lc func(client.NativeWindowLifecycle)) {
	r.mu.Lock()
	r.lifecycle = lc
	r.mu.Unlock()
}

func (r *NativeRenderer) SetPresentSink(p func(client.NativeFramePresented)) {
	r.mu.Lock()
	r.present = p
	r.mu.Unlock()
}

func (r *NativeRenderer) SetOverlayState(state client.OverlayState) {
	r.mu.Lock()
	r.overlay = state
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

func (r *NativeRenderer) SetWindowSize(width, height int) error {
	res := make(chan error, 1)
	select {
	case r.resizeRequests <- nativeResizeRequest{width: width, height: height, result: res}:
		return <-res
	case <-r.stopCh:
		return fmt.Errorf("renderer stopped")
	}
}

func (r *NativeRenderer) RequestKeyframe() {
	// Handled by session
}

func (r *NativeRenderer) CaptureSnapshotPNG() ([]byte, error) {
	ch := make(chan nativeSnapshotResult, 1)
	select {
	case r.snapshotRequests <- ch:
	case <-r.stopCh:
		return nil, fmt.Errorf("renderer stopped")
	}

	snapshot := <-ch
	return snapshot.body, snapshot.err
}

func (r *NativeRenderer) Size() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.width, r.height
}

func (r *NativeRenderer) PreferredVideoCodec() string {
	return "h265"
}

func (r *NativeRenderer) SupportedVideoCodecs() []string {
	return []string{"vp8", "h264", "h264_nvenc", "h264_vaapi", "h265", "h265_nvenc", "h265_vaapi-444", "hevc", "hevc_vaapi", "av1", "av1_nvenc"}
}

func (r *NativeRenderer) ResetVideoStream(codec string) {
	select {
	case r.streamResets <- "reset_texture":
	default:
	}
	select {
	case r.decoderResets <- codec:
	default:
	}
}

func (r *NativeRenderer) HandleVideoFrame(codec string, frame []byte, packetTimestamp int64) error {
	return r.HandleVideoFrameWithTiming(codec, frame, packetTimestamp, 0, 0, 0, 0, client.BenchmarkClockNowMs())
}

func (r *NativeRenderer) HandleVideoFrameWithTimingNs(codec string, frame []byte, packetTimestamp int64, receiveNs int64) error {
	return r.enqueueVideoSample(nativeVideoSample{codec: codec, data: frame, packetTimestamp: packetTimestamp, receiveAt: receiveNs / int64(time.Millisecond), receiveNs: receiveNs})
}

func (r *NativeRenderer) HandleVideoFrameWithTiming(
	codec string,
	frame []byte,
	packetTimestamp int64,
	firstPacketSequenceNumber uint16,
	firstDecryptedPacketQueuedAt int64,
	firstRemotePacketAt int64,
	firstPacketReadAt int64,
	receiveAt int64,
) error {
	sample := nativeVideoSample{
		codec:                        codec,
		data:                         frame,
		packetTimestamp:              packetTimestamp,
		firstPacketSequenceNumber:    firstPacketSequenceNumber,
		firstDecryptedPacketQueuedAt: firstDecryptedPacketQueuedAt,
		firstRemotePacketAt:          firstRemotePacketAt,
		firstPacketReadAt:            firstPacketReadAt,
		receiveAt:                    receiveAt,
	}
	return r.enqueueVideoSample(sample)
}

func (r *NativeRenderer) enqueueVideoSample(sample nativeVideoSample) error {

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
		return nil // Drop frame if full
	}
}

func (r *NativeRenderer) SetLowLatency(lowLatency bool) {
	r.mu.Lock()
	r.lowLatency = lowLatency
	r.mu.Unlock()
}

func (r *NativeRenderer) SetVSync(vsync bool) {
	select {
	case r.vsyncRequests <- vsync:
	default:
	}
}

func (r *NativeRenderer) Stop() {
	select {
	case <-r.stopCh:
		// Already stopped
	default:
		close(r.stopCh)
	}
}

func (r *NativeRenderer) Close() error {
	r.Stop()
	r.mu.RLock()
	runStarted := r.runStarted
	r.mu.RUnlock()
	if runStarted {
		<-r.doneCh
	}
	return nil
}

type nativeVideoSample struct {
	codec                        string
	data                         []byte
	packetTimestamp              int64
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	receiveAt                    int64
	receiveNs                    int64
}

type nativeDecodedSample struct {
	frame                        *decodedFrame
	packetTimestamp              int64
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	receiveAt                    int64
	receiveNs                    int64
	decodeReadyAt                int64
	decodeReadyNs                int64
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

func (r *NativeRenderer) MenuItemIndexAt(x, y float64, itemCount int) int {
	r.mu.RLock()
	w, h := r.width, r.height
	r.mu.RUnlock()
	return menuItemIndexAt(w, h, x, y, itemCount)
}
