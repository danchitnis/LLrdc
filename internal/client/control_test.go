package client

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestControlServerStateEndpoints(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	server := NewControlServer("127.0.0.1:0", session, nil)

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
	server := NewControlServer("127.0.0.1:0", session, nil)

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
	server := NewControlServer("127.0.0.1:0", session, nil)

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

func TestControlServerMenuAndOverlayEndpoints(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	server := NewControlServer("127.0.0.1:0", session, &ControlHooks{
		GetMenuState: func() any {
			return MenuStateSnapshot{Visible: true, Title: "TEST"}
		},
		GetOverlayState: func() any {
			return OverlayState{MenuVisible: true, MenuTitle: "TEST"}
		},
	})

	menuReq := httptest.NewRequest(http.MethodGet, "/menuz", nil)
	menuRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(menuRec, menuReq)
	if menuRec.Code != http.StatusOK {
		t.Fatalf("unexpected /menuz status: got %d want %d", menuRec.Code, http.StatusOK)
	}

	var menu MenuStateSnapshot
	if err := json.Unmarshal(menuRec.Body.Bytes(), &menu); err != nil {
		t.Fatalf("unmarshal /menuz response: %v", err)
	}
	if !menu.Visible || menu.Title != "TEST" {
		t.Fatalf("unexpected menu payload: %+v", menu)
	}

	overlayReq := httptest.NewRequest(http.MethodGet, "/overlayz", nil)
	overlayRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(overlayRec, overlayReq)
	if overlayRec.Code != http.StatusOK {
		t.Fatalf("unexpected /overlayz status: got %d want %d", overlayRec.Code, http.StatusOK)
	}
}

func TestControlServerCommandEndpoint(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	var got string
	server := NewControlServer("127.0.0.1:0", session, &ControlHooks{
		ExecuteCommand: func(id string) error {
			got = id
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/command", bytes.NewBufferString(`{"id":"menu.toggle"}`))
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected /command status: got %d want %d", rec.Code, http.StatusOK)
	}
	if got != "menu.toggle" {
		t.Fatalf("unexpected command id: %q", got)
	}
}

func TestControlServerConnectUsesHook(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	var got string
	server := NewControlServer("127.0.0.1:0", session, &ControlHooks{
		Connect: func(serverURL string) error {
			got = serverURL
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/connect", bytes.NewBufferString(`{"server_url":"http://example.test:8080"}`))
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unexpected /connect status: got %d want %d", rec.Code, http.StatusAccepted)
	}
	if got != "http://example.test:8080" {
		t.Fatalf("unexpected connect server url: %q", got)
	}
}

func TestControlServerSnapshotEndpoint(t *testing.T) {
	t.Parallel()

	session := NewSession(nil)
	server := NewControlServer("127.0.0.1:0", session, &ControlHooks{
		CaptureSnapshot: func() ([]byte, error) {
			return []byte("png-bytes"), nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/snapshotz", nil)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected /snapshotz status: got %d want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if rec.Body.String() != "png-bytes" {
		t.Fatalf("unexpected snapshot body: %q", rec.Body.String())
	}
}
