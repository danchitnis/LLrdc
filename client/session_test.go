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
		{in: "http://localhost:8080", want: "ws://localhost:8080/ws"},
		{in: "https://example.com/path", want: "wss://example.com/path"},
		{in: "ws://127.0.0.1:9000", want: "ws://127.0.0.1:9000/ws"},
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
	if packet.packetTimestamp != 1000 {
		t.Fatalf("unexpected packet timestamp: got %d want 1000", packet.packetTimestamp)
	}
	if len(packet.chunkData) != 3 {
		t.Fatalf("unexpected payload length: %d", len(packet.chunkData))
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
