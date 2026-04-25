package client

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

func (s *Session) readLoop(connectionID uint64, conn *websocket.Conn, pc *webrtc.PeerConnection) {
	for {
		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			s.setError(err)
			go func() {
				_ = s.disconnectIfCurrent(connectionID)
			}()
			return
		}
		if messageType == websocket.BinaryMessage {
			s.consumeBinaryVideoMessage(raw)
			continue
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
		case "webrtc_answer":
			answerBytes, _ := json.Marshal(msg["sdp"])
			var answer webrtc.SessionDescription
			if err := json.Unmarshal(answerBytes, &answer); err != nil {
				s.setError(err)
				continue
			}
			if err := pc.SetRemoteDescription(answer); err != nil {
				s.setError(err)
			}
		case "webrtc_ice":
			candidateBytes, _ := json.Marshal(msg["candidate"])
			var candidate webrtc.ICECandidateInit
			if err := json.Unmarshal(candidateBytes, &candidate); err != nil {
				s.setError(err)
				continue
			}
			if err := pc.AddICECandidate(candidate); err != nil {
				s.setError(err)
			}
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

func (s *Session) consumeBinaryVideoMessage(raw []byte) {
	if provider, ok := s.renderer.(WebSocketVideoFallbackProvider); !ok || !provider.SupportsWebSocketVideoFallback() {
		return
	}

	packet, ok := parseBinaryVideoPacket(raw)
	if !ok {
		return
	}

	s.mu.RLock()
	codec := s.state.VideoCodec
	s.mu.RUnlock()
	if strings.TrimSpace(codec) == "" {
		codec = "video/VP8"
	}

	s.mu.Lock()
	now := time.Now()
	s.stats.VideoPackets++
	s.stats.VideoFrames++
	s.stats.VideoBytes += uint64(len(packet.chunkData))
	s.state.LastVideoPacketAt = now
	s.state.LastVideoFrameAt = s.state.LastVideoPacketAt
	s.recordVideoByteSampleLocked(now, len(packet.chunkData))
	s.mu.Unlock()

	if err := s.renderer.HandleVideoFrame(codec, packet.chunkData, packet.packetTimestamp); err != nil {
		s.setError(err)
		return
	}

	s.emit(EventFrame, map[string]any{
		"codec":           codec,
		"packetTimestamp": packet.packetTimestamp,
		"size":            len(packet.chunkData),
		"transport":       "websocket",
	})
}

func isH264KeyframePayload(data []byte) bool {
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
	// WebRTC often sends H.264 in AVCC format.
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

func requestInitialKeyframes(pc *webrtc.PeerConnection, mediaSSRC uint32, stop <-chan struct{}) {
	requestKeyframe := func() bool {
		if pc == nil || pc.RemoteDescription() == nil {
			return false
		}
		if err := pc.WriteRTCP([]rtcp.Packet{
			&rtcp.PictureLossIndication{MediaSSRC: mediaSSRC},
		}); err != nil {
			return false
		}
		return true
	}

	if !requestKeyframe() {
		return
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()

	for {
		select {
		case <-stop:
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			if !requestKeyframe() {
				return
			}
		}
	}
}

type binaryVideoPacket struct {
	packetTimestamp uint32
	chunkData       []byte
}

func parseBinaryVideoPacket(raw []byte) (binaryVideoPacket, bool) {
	if len(raw) < 9 || raw[0] != 1 {
		return binaryVideoPacket{}, false
	}

	timestampMs := math.Float64frombits(binary.BigEndian.Uint64(raw[1:9]))
	packetTimestamp := uint32(timestampMs * 90)
	return binaryVideoPacket{
		packetTimestamp: packetTimestamp,
		chunkData:       append([]byte(nil), raw[9:]...),
	}, true
}
