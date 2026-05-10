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
	server.FPS = 30
	server.VideoCodec = "h264"
	server.WebRTCLowLatency = true
	server.HDPI = 100

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

	var wsMu sync.Mutex
	safeWriteJSON := func(v interface{}) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Send initial config to browser to force H264 UI
	safeWriteJSON(map[string]interface{}{
		"type":               "config",
		"videoCodec":         "h264",
		"framerate":          server.FPS,
		"webrtc_low_latency": server.WebRTCLowLatency,
	})

	var configMu sync.Mutex
	var configTimer *time.Timer

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
			server.HandleWebRTCOffer(msg, r.Host, &pc, safeWriteJSON)
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

			log.Printf("Received %s message: %v", msg["type"], configMap)

			if w, ok1 := configMap["width"].(float64); ok1 {
				if h, ok2 := configMap["height"].(float64); ok2 {
					// macOS host enforces 32px alignment with exceptions for standard 720p/1080p heights
					width := (int(w) / 32) * 32
					height := int(h)
					if height != 720 && height != 1080 {
						height = (height / 32) * 32
					}
					server.SetScreenSize(width, height)
				}
			}
			if maxResFloat, ok := configMap["max_res"].(float64); ok {
				maxRes := int(maxResFloat)
				if server.InitialRes != maxRes {
					log.Printf("Received max resolution config: %dp", maxRes)
					server.InitialRes = maxRes
					if server.InitialRes > 0 {
						server.UpdateScreenSizeFromInitialRes()
					}
				}
			}
			if fps, ok := configMap["framerate"].(float64); ok {
				if fps > 0 && int(fps) != server.FPS {
					server.FPS = int(fps)
				}
			} else if fps, ok := configMap["fps"].(float64); ok {
				if fps > 0 && int(fps) != server.FPS {
					server.FPS = int(fps)
				}
			}
			if hdpi, ok := configMap["hdpi"].(float64); ok {
				if hdpi > 0 && int(hdpi) != server.HDPI {
					server.HDPI = int(hdpi)
				}
			}

			// Debounce applying the merged configuration to the agent
			configMu.Lock()
			if configTimer != nil {
				configTimer.Stop()
			}
			configTimer = time.AfterFunc(100*time.Millisecond, func() {
				width, height := server.GetScreenSize()
				gen := nextGeneration()
				log.Printf("Applying debounced config (gen %d): %dx%d @ %d FPS (HDPI %d%%)", gen, width, height, server.FPS, server.HDPI)
				encMgr.Recreate(width, height, server.FPS, gen)
				if globalControlClient != nil {
					globalControlClient.ApplyConfig(width, height, server.FPS, server.HDPI, gen)
				}
			})
			configMu.Unlock()

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
