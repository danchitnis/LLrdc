package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/server/common"
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
	common.Port = 8080
	common.FPS = 30
	common.VideoCodec = "h264"
	common.WebRTCLowLatency = true

	// Initialize WebRTC track and mux
	common.InitWebRTC()
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

	reportedCodec := common.VideoCodec
	if common.Chroma == "444" {
		if common.VideoCodec == "h264" {
			reportedCodec = "h264-444"
		} else if common.VideoCodec == "h265" || common.VideoCodec == "hevc" {
			reportedCodec = "h265-444"
		}
	}
	safeWriteJSON(map[string]interface{}{
		"type":               "config",
		"videoCodec":         reportedCodec,
		"chroma":             common.Chroma,
		"framerate":          common.FPS,
		"hdpi":               common.HDPI,
		"bandwidth":          common.TargetBandwidthMbps,
		"vtAvailable":        true,
		"webrtc_low_latency": common.WebRTCLowLatency,
		"max_res":            common.InitialRes,
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
			common.HandleWebRTCOffer(msg, r.Host, &pc, safeWriteJSON)
			pcMu.Unlock()

		case "webrtc_ice":
			pcMu.Lock()
			common.HandleWebRTCICE(msg, pc)
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

			sizeChanged := false
			if w, ok1 := configMap["width"].(float64); ok1 {
				if h, ok2 := configMap["height"].(float64); ok2 {
					sizeChanged = common.SetScreenSize(int(w), int(h))
				}
			}
			if fps, ok := configMap["framerate"].(float64); ok {
				if int(fps) > 0 && int(fps) != common.FPS {
					common.FPS = int(fps)
				}
			} else if fps, ok := configMap["fps"].(float64); ok {
				if int(fps) > 0 && int(fps) != common.FPS {
					common.FPS = int(fps)
				}
			}
			if hdpi, ok := configMap["hdpi"].(float64); ok {
				if hdpi > 0 && int(hdpi) != common.HDPI {
					common.HDPI = int(hdpi)
					reconnectNeeded = true
				}
			}
			if bw, ok := configMap["bandwidth"].(float64); ok {
				if bw > 0 && int(bw) != common.TargetBandwidthMbps {
					common.TargetBandwidthMbps = int(bw)
				}
			}
			var maxResVal int
			if maxRes, ok := configMap["max_res"].(float64); ok {
				maxResVal = int(maxRes)
			} else if maxResStr, ok := configMap["max_res"].(string); ok {
				if val, err := strconv.Atoi(maxResStr); err == nil {
					maxResVal = val
				}
			}

			if maxResVal != common.InitialRes {
				common.InitialRes = maxResVal
				if common.InitialRes > 0 {
					common.UpdateScreenSizeFromInitialRes()
				}
			}

			if codec, ok := configMap["videoCodec"].(string); ok {
				oldCodec := common.VideoCodec
				oldChroma := common.Chroma

				if codec == "h264-444" {
					common.Chroma = "444"
					common.VideoCodec = "h264"
				} else if codec == "h265-444" {
					common.Chroma = "444"
					common.VideoCodec = "h265"
				} else {
					common.VideoCodec = codec
					if chroma, ok := configMap["chroma"].(string); ok {
						common.Chroma = chroma
					} else {
						common.Chroma = "420"
					}
				}

				if common.VideoCodec != oldCodec || common.Chroma != oldChroma {
					common.InitWebRTCTrack()
					reconnectNeeded = true
				}
			}

			configMu.Lock()
			if configTimer != nil {
				configTimer.Stop()
			}

			configTimer = time.AfterFunc(100*time.Millisecond, func() {
				if reconnectNeeded {
					log.Printf("Sending reconnect hint to client due to config change requiring WebRTC reconnect")
					_ = safeWriteJSON(map[string]interface{}{"type": "reconnect_hint"})

					pcMu.Lock()
					if pc != nil {
						pc.Close()
						pc = nil
					}
					pcMu.Unlock()
				}

				width, height := common.GetScreenSize()
				pixFmt := 0
				if common.Chroma == "444" {
					pixFmt = 1
				}

				// Only increment generation if something actually changed on the server side
				// that requires an agent restart or encoder recreation.
				gen := getGeneration()
				enc, encGen := encMgr.Get()
				if enc == nil || enc.Width != width || enc.Height != height || encGen != gen || enc.PixFmt != pixFmt || common.TargetBandwidthMbps*1000 != enc.BitrateKbps() || common.FPS != enc.FPS || encMgr.Codec() != common.VideoCodec || lastAppliedHDPI != common.HDPI {
					gen = nextGeneration()
					lastAppliedHDPI = common.HDPI
					log.Printf("Applying debounced config (gen %d): %s %dx%d@%d FPS (fmt %d), %d Mbps, %d%% HDPI", gen, common.VideoCodec, width, height, common.FPS, pixFmt, common.TargetBandwidthMbps, common.HDPI)
					encMgr.Recreate(common.VideoCodec, width, height, common.FPS, common.TargetBandwidthMbps*1000, pixFmt, gen)
					if globalControlClient != nil {
						globalControlClient.ApplyConfig(width, height, common.FPS, common.HDPI, common.TargetBandwidthMbps, gen, common.Chroma)
					}
				} else {
					log.Printf("Debounced config received but no functional changes detected (gen %d).", gen)
				}
			})
			configMu.Unlock()

			// Report current effective config back to client
			// This confirms the choice and triggers client-side re-negotiation if needed
			reportedCodec = common.VideoCodec
			if common.Chroma == "444" {
				if common.VideoCodec == "h264" {
					reportedCodec = "h264-444"
				} else if common.VideoCodec == "h265" || common.VideoCodec == "hevc" {
					reportedCodec = "h265-444"
				}
			}

			if msg["type"] == "config" || (msg["type"] == "resize" && sizeChanged) {
				width, height := common.GetScreenSize()
				gen := getGeneration()
				safeWriteJSON(map[string]interface{}{
					"type":               "config",
					"videoCodec":         reportedCodec,
					"width":              width,
					"height":             height,
					"fps":                common.FPS,
					"hdpi":               common.HDPI,
					"bandwidth":          common.TargetBandwidthMbps,
					"generation":         gen,
					"chroma":             common.Chroma,
					"vtAvailable":        true,
					"webrtc_low_latency": common.WebRTCLowLatency,
					"max_res":            common.InitialRes,
				})
			}
		}
	}
}

func broadcastVideoFrame(data []byte, isKeyframe bool, codec string) {
	// Convert AVCC (4-byte length) to Annex-B (00 00 00 01)
	annexB := avccToAnnexB(data)
	common.WriteWebRTCFrame(annexB, 0, time.Now(), codec, nil)
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
