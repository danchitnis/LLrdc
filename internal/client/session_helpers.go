package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

func (s *Session) consumeAudioTrack(track *webrtc.TrackRemote) {
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			s.setError(err)
			return
		}

		s.mu.Lock()
		s.stats.AudioPackets++
		s.stats.AudioBytes += uint64(packet.MarshalSize())
		s.mu.Unlock()
	}
}

func (s *Session) emit(event Event, data map[string]any) {
	s.hooks.Emit(event, EventPayload{
		Type: string(event),
		At:   time.Now(),
		Data: data,
	})
}

func (s *Session) sendJSON(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.sendRaw(body)
}

func (s *Session) sendRaw(body []byte) error {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return errors.New("session is not connected")
	}

	return conn.WriteMessage(websocket.TextMessage, body)
}

func (s *Session) setError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.state.LastError = err.Error()
	s.mu.Unlock()
	s.emit(EventError, map[string]any{
		"error": err.Error(),
	})
}

func httpToWebsocketURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneMapString(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
