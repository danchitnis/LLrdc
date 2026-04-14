package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestControlServerStateEndpoints(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	server := NewControlServer("127.0.0.1:0", session)

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("unexpected /readyz status: got %d want %d", readyRec.Code, http.StatusOK)
	}

	var ready map[string]any
	if err := json.Unmarshal(readyRec.Body.Bytes(), &ready); err != nil {
		t.Fatalf("unmarshal /readyz response: %v", err)
	}
	if connected, _ := ready["connected"].(bool); connected {
		t.Fatalf("expected disconnected ready state, got %#v", ready)
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/statez", nil)
	stateRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("unexpected /statez status: got %d want %d", stateRec.Code, http.StatusOK)
	}

	var state SessionState
	if err := json.Unmarshal(stateRec.Body.Bytes(), &state); err != nil {
		t.Fatalf("unmarshal /statez response: %v", err)
	}
	if state.Connected {
		t.Fatalf("expected disconnected session state, got %+v", state)
	}
}

func TestControlServerLatencyEndpointWithoutSamples(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	server := NewControlServer("127.0.0.1:0", session)

	req := httptest.NewRequest(http.MethodGet, "/latencyz/latest", nil)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected /latencyz/latest status: got %d want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal /latencyz/latest response: %v", err)
	}
	if available, _ := payload["available"].(bool); available {
		t.Fatalf("expected unavailable latency sample, got %#v", payload)
	}
}

func TestControlServerReadyIncludesWindowLifecycleState(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	session.SetBuildID("test-build")
	session.UpdateWindowState(NativeWindowLifecycle{
		Backend:    "x11",
		WindowID:   42,
		Created:    true,
		Shown:      true,
		Mapped:     true,
		Visible:    true,
		HasFocus:   true,
		HasSurface: true,
		Desktop:    0,
	})
	session.RecordPresentedFrame(NativeFramePresented{Width: 1280, Height: 720})
	server := NewControlServer("127.0.0.1:0", session)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected /readyz status: got %d want %d", rec.Code, http.StatusOK)
	}

	var ready map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ready); err != nil {
		t.Fatalf("unmarshal /readyz response: %v", err)
	}
	if ready["buildId"] != "test-build" {
		t.Fatalf("unexpected build id: %#v", ready["buildId"])
	}
	if windowID, _ := ready["windowId"].(float64); windowID != 42 {
		t.Fatalf("unexpected window id: %#v", ready["windowId"])
	}
	if mapped, _ := ready["windowMapped"].(bool); !mapped {
		t.Fatalf("expected mapped window state, got %#v", ready)
	}
	if visible, _ := ready["windowVisible"].(bool); !visible {
		t.Fatalf("expected visible window state, got %#v", ready)
	}
	if presenting, _ := ready["presenting"].(bool); !presenting {
		t.Fatalf("expected presenting state, got %#v", ready)
	}
}
