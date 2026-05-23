package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/server/linux"
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
	linux.Port = 8080
	linux.FPS = 30
	linux.VideoCodec = "h264"
	linux.WebRTCLowLatency = true

	// Initialize WebRTC track and mux
	linux.InitWebRTC()
}

func handleSignaling(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("New signaling connection from %s", r.RemoteAddr)

	var wsMu sync.Mutex
	safeWriteJSON := func(v interface{}) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	reportedCodec := linux.VideoCodec
	if linux.Chroma == "444" {
		if linux.VideoCodec == "h264" {
			reportedCodec = "h264-444"
		} else if linux.VideoCodec == "h265" || linux.VideoCodec == "hevc" {
			reportedCodec = "h265-444"
		}
	}
	safeWriteJSON(map[string]interface{}{
		"type":               "config",
		"videoCodec":         reportedCodec,
		"chroma":             linux.Chroma,
		"framerate":          linux.FPS,
		"hdpi":               linux.HDPI,
		"bandwidth":          linux.TargetBandwidthMbps,
		"webrtc_low_latency": linux.WebRTCLowLatency,
	})

	var configMu sync.Mutex
	var configTimer *time.Timer
	lastAppliedHDPI := -1

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
			linux.HandleWebRTCOffer(msg, r.Host, &pc, safeWriteJSON)
			pcMu.Unlock()

		case "webrtc_ice":
			pcMu.Lock()
			linux.HandleWebRTCICE(msg, pc)
			pcMu.Unlock()

		case "config", "resize":
			configMap := msg
			if msg["type"] == "config" {
				if c, ok := msg["config"].(map[string]interface{}); ok {
					configMap = c
				}
			}

			log.Printf("Received %s message: %v", msg["type"], configMap)

			reconnectNeeded := false

			if w, ok1 := configMap["width"].(float64); ok1 {
				if h, ok2 := configMap["height"].(float64); ok2 {
					linux.SetScreenSize(int(w), int(h))
				}
			}
			if fps, ok := configMap["framerate"].(float64); ok {
				if int(fps) > 0 && int(fps) != linux.FPS {
					linux.FPS = int(fps)
				}
			} else if fps, ok := configMap["fps"].(float64); ok {
				if int(fps) > 0 && int(fps) != linux.FPS {
					linux.FPS = int(fps)
				}
			}
			if hdpi, ok := configMap["hdpi"].(float64); ok {
				if hdpi > 0 && int(hdpi) != linux.HDPI {
					linux.HDPI = int(hdpi)
					reconnectNeeded = true
				}
			}
			if bw, ok := configMap["bandwidth"].(float64); ok {
				if bw > 0 && int(bw) != linux.TargetBandwidthMbps {
					linux.TargetBandwidthMbps = int(bw)
				}
			}

			if codec, ok := configMap["videoCodec"].(string); ok {
				oldCodec := linux.VideoCodec
				oldChroma := linux.Chroma

				if codec == "h264-444" {
					linux.Chroma = "444"
					linux.VideoCodec = "h264"
				} else if codec == "h265-444" {
					linux.Chroma = "444"
					linux.VideoCodec = "h265"
				} else {
					linux.VideoCodec = codec
					if chroma, ok := configMap["chroma"].(string); ok {
						linux.Chroma = chroma
					} else {
						linux.Chroma = "420"
					}
				}

				if linux.VideoCodec != oldCodec || linux.Chroma != oldChroma {
					reconnectNeeded = true
				}
			}

			configMu.Lock()
			if configTimer != nil {
				configTimer.Stop()
			}

			configTimer = time.AfterFunc(100*time.Millisecond, func() {
				if reconnectNeeded {
					pcMu.Lock()
					if pc != nil {
						pc.Close()
						pc = nil
					}
					pcMu.Unlock()
				}

				width, height := linux.GetScreenSize()
				pixFmt := 0
				if linux.Chroma == "444" {
					pixFmt = 1
				}

				// Only increment generation if something actually changed on the server side
				// that requires an agent restart or encoder recreation.
				gen := getGeneration()
				enc, encGen := encMgr.Get()
				if enc == nil || enc.Width != width || enc.Height != height || encGen != gen || enc.PixFmt != pixFmt || linux.TargetBandwidthMbps*1000 != enc.BitrateKbps() || linux.FPS != enc.FPS || encMgr.Codec() != linux.VideoCodec || lastAppliedHDPI != linux.HDPI {
					gen = nextGeneration()
					lastAppliedHDPI = linux.HDPI
					log.Printf("Applying debounced config (gen %d): %s %dx%d@%d FPS (fmt %d), %d Mbps, %d%% HDPI", gen, linux.VideoCodec, width, height, linux.FPS, pixFmt, linux.TargetBandwidthMbps, linux.HDPI)
					encMgr.Recreate(linux.VideoCodec, width, height, linux.FPS, linux.TargetBandwidthMbps*1000, pixFmt, gen)
					if globalControlClient != nil {
						globalControlClient.ApplyConfig(width, height, linux.FPS, linux.HDPI, linux.TargetBandwidthMbps, gen, linux.Chroma)
					}
				} else {
					log.Printf("Debounced config received but no functional changes detected (gen %d).", gen)
				}
			})
			configMu.Unlock()

			// Report current effective config back to client
			// This confirms the choice and triggers client-side re-negotiation if needed
			reportedCodec = linux.VideoCodec
			if linux.Chroma == "444" {
				if linux.VideoCodec == "h264" {
					reportedCodec = "h264-444"
				} else if linux.VideoCodec == "h265" || linux.VideoCodec == "hevc" {
					reportedCodec = "h265-444"
				}
			}

			width, height := linux.GetScreenSize()
			gen := getGeneration()
			safeWriteJSON(map[string]interface{}{
				"type":               "config",
				"videoCodec":         reportedCodec,
				"width":              width,
				"height":             height,
				"fps":                linux.FPS,
				"hdpi":               linux.HDPI,
				"bandwidth":          linux.TargetBandwidthMbps,
				"generation":         gen,
				"chroma":             linux.Chroma,
				"webrtc_low_latency": linux.WebRTCLowLatency,
			})
		}
	}
}

func broadcastVideoFrame(data []byte, isKeyframe bool, codec string) {
	// Convert AVCC (4-byte length) to Annex-B (00 00 00 01)
	annexB := avccToAnnexB(data)
	linux.WriteWebRTCFrame(annexB, 0, time.Now(), codec, nil)
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
