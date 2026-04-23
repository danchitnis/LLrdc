package client

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type ControlServer struct {
	session *Session
	server  *http.Server
	hooks   *ControlHooks
}

type ControlHooks struct {
	GetMenuState    func() any
	Connect         func(serverURL string) error
	ExecuteCommand  func(id string) error
	CaptureSnapshot func() ([]byte, error)
	GetOverlayState func() any
}

func NewControlServer(addr string, session *Session, hooks *ControlHooks) *ControlServer {
	mux := http.NewServeMux()
	cs := &ControlServer{
		session: session,
		hooks:   hooks,
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}

	mux.HandleFunc("/readyz", cs.handleReady)
	mux.HandleFunc("/statez", cs.handleState)
	mux.HandleFunc("/statsz", cs.handleStats)
	mux.HandleFunc("/latencyz/latest", cs.handleLatency)
	mux.HandleFunc("/connect", cs.handleConnect)
	mux.HandleFunc("/resize", cs.handleResize)
	mux.HandleFunc("/config", cs.handleConfig)
	mux.HandleFunc("/input/mousemove", cs.handleMouseMove)
	mux.HandleFunc("/input/mousebtn", cs.handleMouseButton)
	mux.HandleFunc("/input/key", cs.handleKey)
	mux.HandleFunc("/input/wheel", cs.handleWheel)
	mux.HandleFunc("/menuz", cs.handleMenu)
	mux.HandleFunc("/command", cs.handleCommand)
	mux.HandleFunc("/snapshotz", cs.handleSnapshot)
	mux.HandleFunc("/overlayz", cs.handleOverlay)

	return cs
}

func (s *ControlServer) ListenAndServe() error {
	return s.server.ListenAndServe()
}

func (s *ControlServer) Close() error {
	return s.server.Close()
}

func (s *ControlServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	state := s.session.State()
	writeJSON(w, http.StatusOK, map[string]any{
		"buildId":                 state.BuildID,
		"windowWidth":             state.WindowWidth,
		"windowHeight":            state.WindowHeight,
		"lastResizeWidth":         state.LastResizeWidth,
		"lastResizeHeight":        state.LastResizeHeight,
		"lastResizeAt":            state.LastResizeAt,
		"lastPresentedWidth":      state.LastPresentedWidth,
		"lastPresentedHeight":     state.LastPresentedHeight,
		"serverScreenWidth":       state.ServerScreenWidth,
		"serverScreenHeight":      state.ServerScreenHeight,
		"ready":                   state.Connected,
		"connected":               state.Connected,
		"webrtcConnected":         state.WebRTCConnected,
		"peerConnectionState":     state.PeerConnectionState,
		"iceConnectionState":      state.ICEConnectionState,
		"inputChannelOpen":        state.InputChannelOpen,
		"renderLoopStarted":       state.RenderLoopStarted,
		"shutdownRequested":       state.ShutdownRequested,
		"shutdownReason":          state.ShutdownReason,
		"windowBackend":           state.WindowBackend,
		"windowId":                state.WindowID,
		"windowCreated":           state.WindowCreated,
		"windowShown":             state.WindowShown,
		"windowMapped":            state.WindowMapped,
		"windowVisible":           state.WindowVisible,
		"windowEvent":             state.WindowEvent,
		"windowFlags":             state.WindowFlags,
		"windowHasFocus":          state.WindowHasFocus,
		"windowPointerInside":     state.WindowPointerInside,
		"windowHasSurface":        state.WindowHasSurface,
		"windowDesktop":           state.WindowDesktop,
		"presenting":              state.Presenting,
		"decoderAwaitingKeyframe": state.DecoderAwaitingKeyframe,
		"firstFramePresentedAt":   state.FirstFramePresentedAt,
		"lastPresentAt":           state.LastPresentAt,
		"lastError":               state.LastError,
	})
}

func (s *ControlServer) handleState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.session.State())
}

func (s *ControlServer) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.session.Stats())
}

func (s *ControlServer) handleLatency(w http.ResponseWriter, _ *http.Request) {
	state := s.session.State()
	if state.LastLatencySample == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    "native decode/present latency sampling is not attached yet",
		})
		return
	}
	writeJSON(w, http.StatusOK, state.LastLatencySample)
}

func (s *ControlServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ServerURL string `json:"server_url"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}
	if s.hooks != nil && s.hooks.Connect != nil {
		if err := s.hooks.Connect(payload.ServerURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
		return
	}
	if err := s.session.Connect(payload.ServerURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *ControlServer) handleResize(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.session.SendResize(payload.Width, payload.Height); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.session.SendConfig(payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleMouseMove(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	err := s.session.SendInput(map[string]any{
		"type": "mousemove",
		"x":    payload.X,
		"y":    payload.Y,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleMouseButton(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Button int    `json:"button"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	err := s.session.SendInput(map[string]any{
		"type":   "mousebtn",
		"button": payload.Button,
		"action": payload.Action,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleKey(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Key    string `json:"key"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	msgType := "keydown"
	if payload.Action == "up" {
		msgType = "keyup"
	}
	err := s.session.SendInput(map[string]any{
		"type": msgType,
		"key":  payload.Key,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleWheel(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeltaX float64 `json:"deltaX"`
		DeltaY float64 `json:"deltaY"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	err := s.session.SendInput(map[string]any{
		"type":   "wheel",
		"deltaX": payload.DeltaX,
		"deltaY": payload.DeltaY,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleMenu(w http.ResponseWriter, _ *http.Request) {
	if s.hooks == nil || s.hooks.GetMenuState == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "menu_state_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, s.hooks.GetMenuState())
}

func (s *ControlServer) handleCommand(w http.ResponseWriter, r *http.Request) {
	if s.hooks == nil || s.hooks.ExecuteCommand == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "commands_unavailable"})
		return
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.hooks.ExecuteCommand(payload.ID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ControlServer) handleSnapshot(w http.ResponseWriter, _ *http.Request) {
	if s.hooks == nil || s.hooks.CaptureSnapshot == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "snapshot_unavailable"})
		return
	}
	body, err := s.hooks.CaptureSnapshot()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *ControlServer) handleOverlay(w http.ResponseWriter, _ *http.Request) {
	if s.hooks == nil || s.hooks.GetOverlayState == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "overlay_state_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, s.hooks.GetOverlayState())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(mustJSON(payload))))
	w.WriteHeader(status)
	_, _ = w.Write(mustJSON(payload))
}

func mustJSON(v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"marshal_failed"}`)
	}
	return body
}
