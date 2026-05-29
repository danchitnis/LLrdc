package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/server/common"
)

var (
	encMgr            *EncoderManager
	currentGeneration uint64
	generationMu      sync.Mutex
)

func nextGeneration() uint64 {
	generationMu.Lock()
	defer generationMu.Unlock()
	currentGeneration++
	return currentGeneration
}

func getGeneration() uint64 {
	generationMu.Lock()
	defer generationMu.Unlock()
	return currentGeneration
}

func main() {
	log.SetOutput(os.Stdout)

	encMgr = NewEncoderManager()

	// 1. Initialize server config and flags
	common.InitConfig()
	common.CaptureMode = common.CaptureModeAgent
	common.VideoCodec = "h264"

	// Set initial macOS split mode dimensions (1280x720 is a safe default)
	common.SetScreenSize(1280, 720)
	width, height := common.GetScreenSize()

	// 2. Initialize VideoToolbox Encoder
	gen := nextGeneration()
	pixFmt := 0
	if common.Chroma == "444" {
		pixFmt = 1
	}
	encMgr.Recreate(common.VideoCodec, width, height, common.FPS, common.TargetBandwidthMbps*1000, pixFmt, gen)
	if enc, _ := encMgr.Get(); enc == nil {
		log.Fatal("Failed to create initial VideoToolbox encoder")
	}
	defer encMgr.Close()

	common.OnForceKeyframe = func() {
		log.Printf("Forcing VideoToolbox IDR frame")
		if enc, _ := encMgr.Get(); enc != nil {
			enc.ForceKeyframe()
		}
	}

	// 3. Set up input forwarding and control client
	agentHost := "127.0.0.1"
	if common.AgentAddress != "" {
		host, _, err := net.SplitHostPort(common.AgentAddress)
		if err == nil {
			agentHost = host
		}
	}
	startAgentControlClient(agentHost)

	go startInputForwarder()
	go startInputProcessor()

	// 4. Start Video Receiver (from Docker)
	go startVideoReceiver()

	// 5. Start WebTransport and WebSockets
	common.MessageHandler = HandleControlMessage
	common.InitWebTransport("0.0.0.0:8090")

	// 6. Start HTTP Server
	fs := http.FileServer(http.Dir("public"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	})

	http.HandleFunc("/ws", common.HandleWebSocket)

	http.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		config := map[string]interface{}{
			"type":             "config",
			"videoCodec":       common.VideoCodec,
			"chroma":           common.Chroma,
			"captureMode":      common.CaptureMode,
			"webtransportPort": 8090,
			"webtransportFingerprint": common.WebTransportFingerprint,
			"screenWidth":      encMgr.width,
			"screenHeight":     encMgr.height,
		}
		json.NewEncoder(w).Encode(config)
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		inputConnected := globalInputWriter.IsConnected()
		controlConnected := false
		readyGen := uint64(0)
		if globalControlClient != nil {
			controlConnected = globalControlClient.conn != nil
			if globalControlClient.IsReady(getGeneration()) {
				readyGen = getGeneration()
			}
		}

		ready := inputConnected && controlConnected && readyGen == getGeneration()

		w.Header().Set("Content-Type", "application/json")
		if ready {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"ready": ready,
			"conditions": map[string]bool{
				"input":    inputConnected,
				"control":  controlConnected,
				"genMatch": readyGen == getGeneration(),
			},
		})
	})

	log.Printf("macOS Native Server listening on :%d", common.Port)
	ln, err := net.Listen("tcp4", "0.0.0.0:8080")
	if err != nil {
		log.Fatalf("Failed to listen on 0.0.0.0:8080: %v", err)
	}
	if err := http.Serve(ln, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func HandleControlMessage(msg map[string]interface{}, writeJSON func(interface{}) error) {
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "keydown", "keyup", "key", "mousemove", "mousebtn", "wheel", "spawn":
		common.HandleInputMessage(msg)
	case "config":
		// Handle basic config updates if needed
	case "resize":
		widthFloat, wOk := msg["width"].(float64)
		heightFloat, hOk := msg["height"].(float64)
		if wOk && hOk {
			width := int(widthFloat)
			height := int(heightFloat)
			if common.SetScreenSize(width, height) {
				clampedW, clampedH := common.GetScreenSize()
				log.Printf("Client requested resize to %dx%d (clamped %dx%d)", width, height, clampedW, clampedH)
				go func() {
					if globalControlClient != nil {
						gen := nextGeneration()
						globalControlClient.ApplyConfig(clampedW, clampedH, common.FPS, common.HDPI, common.TargetBandwidthMbps, gen, common.Chroma)
					}
					// Update clients
					config := map[string]interface{}{
						"type":             "config",
						"videoCodec":       common.VideoCodec,
						"chroma":           common.Chroma,
						"captureMode":      common.CaptureMode,
						"webtransportPort": 8090,
						"webtransportFingerprint": common.WebTransportFingerprint,
						"screenWidth":      clampedW,
						"screenHeight":     clampedH,
					}
					common.BroadcastJSON(config)
				}()
			}
		}
	case "ping":
		if ts, ok := msg["timestamp"].(float64); ok {
			resp := map[string]interface{}{"type": "pong", "timestamp": ts}
			writeJSON(resp)
		}
	}
}

func broadcastVideoFrame(data []byte, isKeyframe bool, codec string) {
	// For WebTransport we don't have stream IDs for individual frames, just use 0
	common.WriteFrame(data, 0, time.Now())
}

