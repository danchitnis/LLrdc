package client

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

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
