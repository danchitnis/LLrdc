package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/internal/server"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	pc   *webrtc.PeerConnection
	pcMu sync.Mutex
)

func init() {
	// Initialize server config with some defaults for macOS
	server.Port = 8080
	server.FPS = 60
	server.VideoCodec = "h264"
	server.WebRTCLowLatency = true

	// Initialize WebRTC track and mux
	server.InitWebRTC()
}

func handleSignaling(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Send initial config to browser to force H264 UI
	conn.WriteJSON(map[string]interface{}{
		"type": "config",
		"config": map[string]interface{}{
			"videoCodec": "h264",
			"fps":        server.FPS,
		},
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Unmarshal error: %v", err)
			continue
		}

		switch msg["type"] {
		case "webrtc_offer":
			pcMu.Lock()
			server.HandleWebRTCOffer(msg, r.Host, &pc, func(v interface{}) error {
				return conn.WriteJSON(v)
			})
			pcMu.Unlock()

		case "webrtc_ice":
			pcMu.Lock()
			server.HandleWebRTCICE(msg, pc)
			pcMu.Unlock()

		case "config", "resize":
			configMap := msg
			if msg["type"] == "config" {
				if c, ok := msg["config"].(map[string]interface{}); ok {
					configMap = c
				}
			}
			if w, ok1 := configMap["width"].(float64); ok1 {
				if h, ok2 := configMap["height"].(float64); ok2 {
					server.SetScreenSize(int(w), int(h))
				}
			}

		default:
			// Route other messages (input) to HandleInputMessage
			server.HandleInputMessage(msg)
		}
	}
}

func broadcastVideoFrame(data []byte, isKeyframe bool) {
	// Convert AVCC (4-byte length) to Annex-B (00 00 00 01)
	annexB := avccToAnnexB(data)
	server.WriteWebRTCFrame(annexB, 0, time.Now(), "h264", nil)
}

func avccToAnnexB(data []byte) []byte {
	var annexB []byte
	pos := 0
	for pos < len(data) {
		if pos+4 > len(data) {
			break
		}
		naluLen := int(data[pos])<<24 | int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4
		if pos+naluLen > len(data) {
			break
		}

		annexB = append(annexB, []byte{0, 0, 0, 1}...)
		annexB = append(annexB, data[pos:pos+naluLen]...)
		pos += naluLen
	}
	return annexB
}
