package client

import (
	"encoding/binary"
	"math"
	"testing"
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
