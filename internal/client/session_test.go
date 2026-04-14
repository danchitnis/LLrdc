package client

import "testing"

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
