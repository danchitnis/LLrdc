package client

import (
	"encoding/binary"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
)

func (s *Session) readLoop(connectionID uint64, conn *websocket.Conn) {
	for {
		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			s.setError(err)
			go func() {
				_ = s.disconnectIfCurrent(connectionID)
			}()
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.setError(err)
			continue
		}

		s.mu.Lock()
		s.stats.SignalingMessages++
		s.state.LastMessageAt = time.Now()
		s.mu.Unlock()

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "config":
			s.mu.Lock()
			s.state.LastConfig = cloneMap(msg)
			if codec, ok := msg["videoCodec"].(string); ok {
				s.state.VideoCodec = codec
			}
			if width, ok := numberToInt(msg["screenWidth"]); ok {
				s.state.ServerScreenWidth = width
			}
			if height, ok := numberToInt(msg["screenHeight"]); ok {
				s.state.ServerScreenHeight = height
			}
			s.mu.Unlock()
			s.emit(EventConfig, cloneMap(msg))
		case "stats":
			s.mu.Lock()
			s.state.LastStats = cloneMap(msg)
			s.mu.Unlock()
			s.emit(EventStats, cloneMap(msg))
		case "reconnect_hint":
			s.emit(EventReconnectRequest, nil)
		default:
			s.emit(EventStateChanged, cloneMap(msg))
		}
	}
}

func IsH264KeyframePayload(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// 1. Try Annex-B (start codes)
	nalus := splitH264NALUs(data)
	if len(nalus) > 0 {
		for _, nalu := range nalus {
			if len(nalu) == 0 {
				continue
			}
			naluType := nalu[0] & 0x1F
			if naluType == 7 || naluType == 8 || naluType == 5 {
				return true
			}
		}
	}

	// 2. Try AVCC (length-prefixed, 4-byte headers)
	ptr := 0
	for ptr+4 <= len(data) {
		naluSize := int(binary.BigEndian.Uint32(data[ptr : ptr+4]))
		ptr += 4
		if ptr+naluSize > len(data) {
			break
		}
		if naluSize > 0 {
			naluType := data[ptr] & 0x1F
			if naluType == 7 || naluType == 8 || naluType == 5 {
				return true
			}
		}
		ptr += naluSize
	}

	// 3. Last resort: check first byte (some minimal encodings)
	naluType := data[0] & 0x1F
	if naluType == 7 || naluType == 8 || naluType == 5 {
		return true
	}

	return false
}
