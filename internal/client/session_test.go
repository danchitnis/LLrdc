package client

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestHTTPToWebsocketURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "http://localhost:8080", want: "ws://localhost:8080/"},
		{in: "https://example.com/path", want: "wss://example.com/path"},
		{in: "ws://127.0.0.1:9000", want: "ws://127.0.0.1:9000/"},
	}

	for _, tt := range tests {
		got, err := httpToWebsocketURL(tt.in)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("unexpected conversion for %q: got %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestSendConfigForcesType(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	err := session.SendConfig(map[string]any{
		"framerate": 60,
	})
	if err == nil {
		t.Fatalf("expected an error when sending config without a connection")
	}
}

func TestShouldStopInitialKeyframeRequests(t *testing.T) {
	t.Parallel()

	if shouldStopInitialKeyframeRequests("video/VP8", []byte{0x01, 0x00}) {
		t.Fatalf("unexpected stop for non-keyframe VP8 payload")
	}
	if !shouldStopInitialKeyframeRequests("video/VP8", []byte{0x00, 0x00}) {
		t.Fatalf("expected stop for keyframe VP8 payload")
	}
	if !shouldStopInitialKeyframeRequests("audio/opus", []byte{0x01}) {
		t.Fatalf("expected non-VP8 payloads to stop after first data frame")
	}
}

func TestBuildH264AccessUnitFromAnnexB(t *testing.T) {
	t.Parallel()

	frame := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x1f,
		0x00, 0x00, 0x00, 0x01, 0x68, 0xeb, 0xec, 0xb2,
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84,
	}

	unit, err := buildH264AccessUnit(frame)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unit.AVCC) == 0 {
		t.Fatalf("expected AVCC output")
	}
	if len(unit.SPS) == 0 || unit.SPS[0]&0x1F != 7 {
		t.Fatalf("expected SPS to be captured")
	}
	if len(unit.PPS) == 0 || unit.PPS[0]&0x1F != 8 {
		t.Fatalf("expected PPS to be captured")
	}
}

func TestShouldStopInitialKeyframeRequestsForAnnexBH264Keyframe(t *testing.T) {
	t.Parallel()

	frame := []byte{
		0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x1f,
		0x00, 0x00, 0x01, 0x68, 0xeb, 0xec, 0xb2,
		0x00, 0x00, 0x01, 0x65, 0x88, 0x84,
	}

	if !shouldStopInitialKeyframeRequests("video/H264", frame) {
		t.Fatalf("expected annex-b keyframe payload to stop keyframe polling")
	}
}

func TestShouldNotStopInitialKeyframeRequestsForAnnexBH264PFrame(t *testing.T) {
	t.Parallel()

	frame := []byte{
		0x00, 0x00, 0x01, 0x41, 0x9a, 0x22,
	}

	if shouldStopInitialKeyframeRequests("video/H264", frame) {
		t.Fatalf("expected annex-b p-frame payload to keep keyframe polling active")
	}
}

func TestParseBinaryVideoPacket(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 9+3)
	raw[0] = 1
	binary.BigEndian.PutUint64(raw[1:9], math.Float64bits(1000))
	copy(raw[9:], []byte{0x01, 0x02, 0x03})

	packet, ok := parseBinaryVideoPacket(raw)
	if !ok {
		t.Fatalf("expected packet to parse")
	}
	if packet.packetTimestamp != 90000 {
		t.Fatalf("unexpected packet timestamp: got %d want 90000", packet.packetTimestamp)
	}
	if len(packet.chunkData) != 3 {
		t.Fatalf("unexpected payload length: %d", len(packet.chunkData))
	}
}

func TestHandlePeerConnectionStateChangeDoesNotEmitWhileHoldingSessionLock(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	session.mu.Lock()
	session.connectionID = 1
	session.mu.Unlock()

	reconnectSeen := make(chan struct{}, 1)
	session.Hooks().On(EventReconnectRequest, func(_ EventPayload) {
		_ = session.State()
		select {
		case reconnectSeen <- struct{}{}:
		default:
		}
	})

	done := make(chan struct{})
	go func() {
		_, shouldReconnect := session.handlePeerConnectionStateChange(1, webrtc.PeerConnectionStateClosed)
		if shouldReconnect {
			session.emit(EventReconnectRequest, nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("peer connection state change deadlocked")
	}

	select {
	case <-reconnectSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect hook was not delivered")
	}
}

func TestHandlePeerConnectionStateChangeIgnoresStaleConnection(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	session.mu.Lock()
	session.connectionID = 2
	session.state.PeerConnectionState = webrtc.PeerConnectionStateConnected.String()
	session.mu.Unlock()

	shouldSendReady, shouldReconnect := session.handlePeerConnectionStateChange(1, webrtc.PeerConnectionStateClosed)
	if shouldSendReady || shouldReconnect {
		t.Fatal("stale connection state change should be ignored")
	}
	if got := session.State().PeerConnectionState; got != webrtc.PeerConnectionStateConnected.String() {
		t.Fatalf("stale connection mutated state: got %q", got)
	}
}

func TestDisconnectIfCurrentIgnoresStaleConnection(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	session.mu.Lock()
	session.connectionID = 2
	session.state.Connected = true
	session.state.ServerURL = "http://current.example"
	session.mu.Unlock()

	if err := session.disconnectIfCurrent(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := session.State()
	if !state.Connected {
		t.Fatal("stale disconnect should not clear current connection")
	}
	if state.ServerURL != "http://current.example" {
		t.Fatalf("stale disconnect mutated server URL: %q", state.ServerURL)
	}
}

func TestVideoSampleBuilderConfig(t *testing.T) {
	t.Parallel()

	maxLate, maxDelay := videoSampleBuilderConfig(false)
	if maxLate != 256 {
		t.Fatalf("unexpected default maxLate: got %d want 256", maxLate)
	}
	if maxDelay != 0 {
		t.Fatalf("unexpected default maxDelay: got %v want 0", maxDelay)
	}

	maxLate, maxDelay = videoSampleBuilderConfig(true)
	if maxLate != 8 {
		t.Fatalf("unexpected ULL maxLate: got %d want 8", maxLate)
	}
	if maxDelay != 12*time.Millisecond {
		t.Fatalf("unexpected ULL maxDelay: got %v want %v", maxDelay, 12*time.Millisecond)
	}
}

func TestMinPositiveTime(t *testing.T) {
	t.Parallel()

	if got := minPositiveTime(0, 25); got != 25 {
		t.Fatalf("minPositiveTime(0, 25) = %d, want 25", got)
	}
	if got := minPositiveTime(40, 25); got != 25 {
		t.Fatalf("minPositiveTime(40, 25) = %d, want 25", got)
	}
	if got := minPositiveTime(25, 40); got != 25 {
		t.Fatalf("minPositiveTime(25, 40) = %d, want 25", got)
	}
	if got := minPositiveTime(25, 0); got != 25 {
		t.Fatalf("minPositiveTime(25, 0) = %d, want 25", got)
	}
}

func testVP8Payload(start bool, payload ...byte) []byte {
	header := byte(0x00)
	if start {
		header = 0x10
	}
	return append([]byte{header}, payload...)
}

func TestVP8ULLAssemblerEmitsFrameOnMarker(t *testing.T) {
	t.Parallel()

	var assembler vp8ULLAssembler
	packet1 := &rtp.Packet{Header: rtp.Header{SequenceNumber: 1, Timestamp: 90, Marker: false}, Payload: testVP8Payload(true, 0x01)}
	packet2 := &rtp.Packet{Header: rtp.Header{SequenceNumber: 2, Timestamp: 90, Marker: true}, Payload: testVP8Payload(false, 0x02)}

	if _, ready, dropped, err := assembler.push(packet1, 1000); ready || dropped || err != nil {
		t.Fatalf("unexpected first push result: ready=%t dropped=%t err=%v", ready, dropped, err)
	}

	frame, ready, dropped, err := assembler.push(packet2, 1010)
	if err != nil {
		t.Fatalf("unexpected second push error: %v", err)
	}
	if !ready {
		t.Fatal("expected completed frame on marker packet")
	}
	if dropped {
		t.Fatal("did not expect frame drop on contiguous packets")
	}
	if frame.packetTimestamp != 90 {
		t.Fatalf("unexpected packet timestamp: got %d want 90", frame.packetTimestamp)
	}
	if frame.firstPacketReadAt != 1000 {
		t.Fatalf("unexpected firstPacketReadAt: got %d want 1000", frame.firstPacketReadAt)
	}
	if string(frame.data) != string([]byte{0x01, 0x02}) {
		t.Fatalf("unexpected frame payload: %#v", frame.data)
	}
}

func TestVP8ULLAssemblerDropsGapAndRecoversOnNextFrame(t *testing.T) {
	t.Parallel()

	var assembler vp8ULLAssembler
	packet1 := &rtp.Packet{Header: rtp.Header{SequenceNumber: 1, Timestamp: 90, Marker: false}, Payload: testVP8Payload(true, 0x01)}
	packetGap := &rtp.Packet{Header: rtp.Header{SequenceNumber: 3, Timestamp: 90, Marker: true}, Payload: testVP8Payload(false, 0x03)}
	packetNext := &rtp.Packet{Header: rtp.Header{SequenceNumber: 4, Timestamp: 180, Marker: true}, Payload: testVP8Payload(true, 0x04)}

	if _, ready, dropped, err := assembler.push(packet1, 1000); ready || dropped || err != nil {
		t.Fatalf("unexpected initial push result: ready=%t dropped=%t err=%v", ready, dropped, err)
	}

	if _, ready, dropped, err := assembler.push(packetGap, 1010); ready || !dropped || err == nil {
		t.Fatalf("expected gap push to drop partial frame and error, got ready=%t dropped=%t err=%v", ready, dropped, err)
	}

	frame, ready, dropped, err := assembler.push(packetNext, 1020)
	if err != nil {
		t.Fatalf("unexpected recovery error: %v", err)
	}
	if !ready {
		t.Fatal("expected next frame to be emitted after recovery")
	}
	if dropped {
		t.Fatal("did not expect recovery frame itself to be marked dropped")
	}
	if frame.packetTimestamp != 180 {
		t.Fatalf("unexpected recovered timestamp: got %d want 180", frame.packetTimestamp)
	}
	if frame.firstPacketReadAt != 1020 {
		t.Fatalf("unexpected recovered firstPacketReadAt: got %d want 1020", frame.firstPacketReadAt)
	}
	if string(frame.data) != string([]byte{0x04}) {
		t.Fatalf("unexpected recovered payload: %#v", frame.data)
	}
}

func TestVP8ULLAssemblerDropsPreviousFrameOnTimestampChange(t *testing.T) {
	t.Parallel()

	var assembler vp8ULLAssembler
	packet1 := &rtp.Packet{Header: rtp.Header{SequenceNumber: 10, Timestamp: 900, Marker: false}, Payload: testVP8Payload(true, 0x0a)}
	packet2 := &rtp.Packet{Header: rtp.Header{SequenceNumber: 11, Timestamp: 990, Marker: true}, Payload: testVP8Payload(true, 0x0b)}

	if _, ready, dropped, err := assembler.push(packet1, 2000); ready || dropped || err != nil {
		t.Fatalf("unexpected initial push result: ready=%t dropped=%t err=%v", ready, dropped, err)
	}

	frame, ready, dropped, err := assembler.push(packet2, 2010)
	if err != nil {
		t.Fatalf("unexpected timestamp change error: %v", err)
	}
	if !ready {
		t.Fatal("expected new timestamp packet to emit its own complete frame")
	}
	if !dropped {
		t.Fatal("expected previous incomplete frame to be dropped on timestamp change")
	}
	if frame.packetTimestamp != 990 {
		t.Fatalf("unexpected packet timestamp: got %d want 990", frame.packetTimestamp)
	}
	if frame.firstPacketReadAt != 2010 {
		t.Fatalf("unexpected firstPacketReadAt: got %d want 2010", frame.firstPacketReadAt)
	}
}
