package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func (s *Session) Connect(serverURL string) error {
	if !s.connecting.CompareAndSwap(false, true) {
		return nil
	}
	defer s.connecting.Store(false)

	s.connectMu.Lock()
	defer s.connectMu.Unlock()

	if strings.TrimSpace(serverURL) == "" {
		return errors.New("server URL is required")
	}

	// Ensure previous connection is fully cleaned up
	if err := s.disconnectLocked(); err != nil {
		return err
	}
	// Small pause to allow OS to release UDP ports
	time.Sleep(100 * time.Millisecond)

	wsURL, err := httpToWebsocketURL(serverURL)
	if err != nil {
		return err
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed: %w (status %s)", err, resp.Status)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	// Read messages until we get the initial config
	var initMsg map[string]any
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		messageType, raw, err := conn.ReadMessage()
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("read initial config: %w", err)
		}

		if messageType == websocket.TextMessage {
			if err := json.Unmarshal(raw, &initMsg); err == nil {
				if msgType, _ := initMsg["type"].(string); msgType == "config" {
					break // Found it
				}
			}
		}
		// Ignore binary or non-config messages during handshake
	}

	lowLatency, _ := initMsg["low_latency"].(bool)
	if toggler, ok := s.renderer.(LowLatencyRenderer); ok {
		toggler.SetLowLatency(lowLatency)
	}

	var connectionID uint64
	s.mu.Lock()
	s.connectionID++
	connectionID = s.connectionID
	s.conn = conn
	s.state.ServerURL = serverURL
	s.state.Connected = true

	s.state.VideoCodec = ""
	s.state.LastConfig = nil
	if msgType, _ := initMsg["type"].(string); msgType == "config" {
		s.state.LastConfig = cloneMap(initMsg)
		if codec, ok := initMsg["videoCodec"].(string); ok {
			s.state.VideoCodec = codec
		}
		if width, ok := numberToInt(initMsg["screenWidth"]); ok {
			s.state.ServerScreenWidth = width
		}
		if height, ok := numberToInt(initMsg["screenHeight"]); ok {
			s.state.ServerScreenHeight = height
		}
	}

	s.state.LastStats = nil
	s.state.LastResizeWidth = 0
	s.state.LastResizeHeight = 0
	s.state.LastResizeAt = time.Time{}
	s.state.LastPresentedWidth = 0
	s.state.LastPresentedHeight = 0
	s.state.LastMessageAt = time.Time{}
	s.state.LastVideoPacketAt = time.Time{}
	s.state.LastVideoFrameAt = time.Time{}
	s.state.LastPresentAt = time.Time{}
	s.state.FirstFramePresentedAt = time.Time{}
	s.state.LastLatencySample = nil
	s.state.RecentVideoByteSamples = nil
	s.state.DecoderAwaitingKeyframe = true
	s.state.Presenting = false
	s.state.CurrentTrackCodecs = make(map[string]string)
	s.stats = SessionStats{
		ConnectedAt: time.Now(),
	}
	s.mu.Unlock()

	wtFingerprint, ok1 := initMsg["webtransportFingerprint"].(string)
	wtPort, ok2 := numberToInt(initMsg["webtransportPort"])

	if ok1 && ok2 && wtFingerprint != "" && wtPort > 0 {
		go func(cid uint64) {
			if err := s.connectWebTransport(cid, serverURL, wtPort, wtFingerprint); err != nil {
				log.Printf("WebTransport connection failed: %v", err)
			}
		}(connectionID)
	}

	s.emit(EventStateChanged, map[string]any{
		"connected": true,
		"serverUrl": serverURL,
	})
	if msgType, _ := initMsg["type"].(string); msgType == "config" {
		s.emit(EventConfig, cloneMap(initMsg))
	}

	go s.readLoop(connectionID, conn)

	return nil
}

func numberToInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func (s *Session) Disconnect() error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	return s.disconnectLocked()
}

func (s *Session) disconnectLocked() error {
	return s.disconnectIfCurrentLocked(s.connectionID)
}

func (s *Session) disconnectIfCurrent(connectionID uint64) error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	return s.disconnectIfCurrentLocked(connectionID)
}

func (s *Session) disconnectIfCurrentLocked(connectionID uint64) error {
	s.mu.Lock()
	if s.connectionID != connectionID {
		s.mu.Unlock()
		return nil
	}
	conn := s.conn
	wtSession := s.wtSession
	wtControl := s.wtControl
	udpConn := s.udpConn
	s.connectionID++
	s.conn = nil
	s.wtSession = nil
	s.wtControl = nil
	s.udpConn = nil
	s.state.Connected = false
	s.state.WebTransportConnected = false
	s.state.Presenting = false
	s.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if wtControl != nil {
		_ = wtControl.Close()
	}
	if wtSession != nil {
		_ = wtSession.CloseWithError(0, "disconnect")
	}
	if udpConn != nil {
		_ = udpConn.Close()
	}

	s.emit(EventStateChanged, map[string]any{
		"connected": false,
	})
	return nil
}
