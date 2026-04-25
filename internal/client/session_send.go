package client

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/pion/webrtc/v4"
)

func (s *Session) SendResize(width, height int) error {
	s.mu.Lock()
	s.state.LastResizeWidth = width
	s.state.LastResizeHeight = height
	s.state.LastResizeAt = time.Now()
	s.mu.Unlock()
	return s.sendJSON(map[string]any{
		"type":   "resize",
		"width":  width,
		"height": height,
	})
}

func (s *Session) SendConfig(config map[string]any) error {
	msg := cloneMap(config)
	msg["type"] = "config"

	// Upgrade codec if it's a generic one and hardware is available
	s.mu.RLock()
	lastConfig := s.state.LastConfig
	s.mu.RUnlock()

	if vCodec, ok := msg["videoCodec"].(string); ok && lastConfig != nil {
		if vCodec == "h264" {
			if qsv, _ := lastConfig["qsvAvailable"].(bool); qsv {
				msg["videoCodec"] = "h264_qsv"
			} else if nvenc, _ := lastConfig["nvidiaAvailable"].(bool); nvenc {
				msg["videoCodec"] = "h264_nvenc"
			}
		} else if vCodec == "h265" {
			if qsv, _ := lastConfig["h265QsvAvailable"].(bool); qsv {
				msg["videoCodec"] = "h265_qsv"
			} else if nvenc, _ := lastConfig["h265Nvenc444Available"].(bool); nvenc {
				msg["videoCodec"] = "h265_nvenc"
			}
		} else if vCodec == "av1" {
			if qsv, _ := lastConfig["av1QsvAvailable"].(bool); qsv {
				msg["videoCodec"] = "av1_qsv"
			} else if nvenc, _ := lastConfig["av1NvencAvailable"].(bool); nvenc {
				msg["videoCodec"] = "av1_nvenc"
			}
		}
	}

	return s.sendJSON(msg)
}

func (s *Session) SendInput(msg map[string]any) error {
	s.emit(EventInputSent, cloneMap(msg))
	return s.sendMessage(msg)
}

func (s *Session) sendMessage(msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if msgType, _ := msg["type"].(string); msgType == "mousemove" || msgType == "mousebtn" || msgType == "keydown" || msgType == "keyup" || msgType == "wheel" {
		s.stats.InputMessagesSent++
	}
	input := s.input
	conn := s.conn
	s.mu.Unlock()

	if input != nil && input.ReadyState() == webrtc.DataChannelStateOpen {
		return input.SendText(string(body))
	}

	if conn == nil {
		return errors.New("session is not connected")
	}
	return s.sendRaw(body)
}
