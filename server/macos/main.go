package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/server/common"
)

var (
	encMgr                 *EncoderManager
	currentGeneration      uint64
	generationMu           sync.Mutex
	displayChangeMu        sync.Mutex
	resizeTimer            *time.Timer
	resizeTimerMu          sync.Mutex
	pendingResizeW         int
	pendingResizeH         int
	macosStreamingPaused   bool
	macosStreamingPausedMu sync.Mutex
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

func isMacOSStreamingPaused() bool {
	macosStreamingPausedMu.Lock()
	defer macosStreamingPausedMu.Unlock()
	return macosStreamingPaused
}

func configPayload() map[string]interface{} {
	width, height := common.GetScreenSize()
	return map[string]interface{}{
		"type":                    "config",
		"videoCodec":              common.VideoCodec,
		"chroma":                  common.Chroma,
		"captureMode":             common.CaptureMode,
		"webtransportPort":        common.Port + 10,
		"webtransportFingerprint": common.WebTransportFingerprint,
		"screenWidth":             width,
		"screenHeight":            height,
		"framerate":               common.FPS,
		"bandwidth":               common.TargetBandwidthMbps,
		"hdpi":                    common.HDPI,
		"max_res":                 common.InitialRes,
		"enableClipboard":         common.EnableClipboard,
		"vtAvailable":             true,
		"capabilities": map[string]interface{}{
			"valid_combinations": common.GetValidCombinations(),
		},
	}
}

func main() {
	log.SetOutput(os.Stdout)

	encMgr = NewEncoderManager()

	// 1. Initialize server config and flags
	common.InitConfig()
	common.CaptureMode = common.CaptureModeAgent
	common.VideoCodec = "h264"
	common.FPS = 30
	common.TargetBandwidthMbps = 5

	// Set initial macOS split mode dimensions (1920x1080 to match container default)
	common.SetScreenSize(1920, 1080)

	// 2. Setup Force Keyframe and Connection Callbacks
	common.OnForceKeyframe = func() {
		log.Printf("Forcing VideoToolbox IDR frame")
		if enc, _ := encMgr.Get(); enc != nil {
			enc.ForceKeyframe()
		}
	}

	common.OnClientConnected = func() {
		log.Printf("Client connected, forcing VideoToolbox IDR frame and sending config")
		if enc, _ := encMgr.Get(); enc != nil {
			enc.ForceKeyframe()
		}
		// Send initial config to the new client
		common.BroadcastJSON(configPayload())
	}

	common.OnPauseStreaming = func() {
		log.Println("macOS Server: Pausing streaming encoding...")
		macosStreamingPausedMu.Lock()
		macosStreamingPaused = true
		macosStreamingPausedMu.Unlock()
		if globalControlClient != nil {
			globalControlClient.ApplyCurrentConfig()
		}
	}

	common.OnResumeStreaming = func() {
		log.Println("macOS Server: Resuming streaming encoding...")
		macosStreamingPausedMu.Lock()
		macosStreamingPaused = false
		macosStreamingPausedMu.Unlock()
		if globalControlClient != nil {
			globalControlClient.ApplyCurrentConfig()
		}
		if enc, _ := encMgr.Get(); enc != nil {
			enc.ForceKeyframe()
		}
	}
	defer encMgr.Close()

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
	webTransportAddr := fmt.Sprintf("0.0.0.0:%d", common.Port+10)
	common.InitWebTransport(webTransportAddr)

	// 6. Start HTTP Server
	fs := http.FileServer(http.Dir("public"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			r.URL.Path = "/viewer.html"
		}
		fs.ServeHTTP(w, r)
	})

	http.HandleFunc("/ws", common.HandleWebSocket)

	http.HandleFunc("/timez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"serverTimeMs": common.BenchmarkClockNowMs(),
		})
	})

	http.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configPayload())
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

	common.StartClientTimeoutTracker()
	log.Printf("macOS Native Server listening on :%d (WebTransport :%d)", common.Port, common.Port+10)
	ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", common.Port))
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", fmt.Sprintf("0.0.0.0:%d", common.Port), err)
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
	case "clipboard_set":
		text, _ := msg["text"].(string)
		paste, _ := msg["paste"].(bool)
		b64 := base64.StdEncoding.EncodeToString([]byte(text))
		pasteVal := "0"
		if paste {
			pasteVal = "1"
		}
		line := fmt.Sprintf("clipboard %s %s\n", b64, pasteVal)
		_, _ = globalInputWriter.Write([]byte(line))
	case "config":
		fps, okF := common.NumberToInt(msg["framerate"])
		if !okF || fps <= 0 {
			fps = common.FPS
		}
		bandwidth, okB := common.NumberToInt(msg["bandwidth"])
		if !okB || bandwidth <= 0 {
			bandwidth = common.TargetBandwidthMbps
		}
		codec, _ := msg["videoCodec"].(string)
		if codec == "" {
			codec = common.VideoCodec
		}
		chroma, _ := msg["chroma"].(string)
		if chroma == "" {
			chroma = common.Chroma
		}
		hdpi, okH := common.NumberToInt(msg["hdpi"])
		if !okH {
			hdpi = common.HDPI
		}
		maxRes, okR := common.NumberToInt(msg["max_res"])
		if !okR {
			maxRes = common.InitialRes
		}

		// Only update if something changed
		if common.FPS != fps || common.TargetBandwidthMbps != bandwidth || common.VideoCodec != codec || common.Chroma != chroma || common.HDPI != hdpi || common.InitialRes != maxRes {
			displayChangeMu.Lock()
			gen := nextGeneration()
			pixFmt := 0
			if chroma == "444" {
				pixFmt = 1
			}

			log.Printf("Client requested config update: %s (%s) %d FPS, %d Mbps, HDPI %d%%, MaxRes %dp (Gen %d)",
				codec, chroma, fps, bandwidth, hdpi, maxRes, gen)

			common.FPS = fps
			common.TargetBandwidthMbps = bandwidth
			common.VideoCodec = codec
			common.Chroma = chroma
			common.HDPI = hdpi

			if common.InitialRes != maxRes {
				common.InitialRes = maxRes
				if maxRes > 0 {
					common.UpdateScreenSizeFromInitialRes()
				}
			}

			width, height := common.GetScreenSize()
			encMgr.Recreate(codec, width, height, fps, bandwidth*1000, pixFmt, gen)

			if globalControlClient != nil {
				globalControlClient.ApplyConfig(width, height, fps, common.HDPI, bandwidth, gen, chroma)
			}
			displayChangeMu.Unlock()
		}

		// Update all clients with new config (including the requesting one to confirm)
		common.BroadcastJSON(configPayload())
	case "resize":
		widthFloat, wOk := msg["width"].(float64)
		heightFloat, hOk := msg["height"].(float64)
		if wOk && hOk {
			w, h := int(widthFloat), int(heightFloat)
			currentW, currentH := common.GetScreenSize()
			if w == currentW && h == currentH {
				log.Printf("Ignoring redundant resize request: %dx%d (already at target size)", w, h)
				return
			}

			resizeTimerMu.Lock()
			pendingResizeW = w
			pendingResizeH = h
			if resizeTimer != nil {
				resizeTimer.Stop()
			}
			resizeTimer = time.AfterFunc(50*time.Millisecond, func() {
				resizeTimerMu.Lock()
				w, h := pendingResizeW, pendingResizeH
				resizeTimerMu.Unlock()

				if common.SetScreenSize(w, h) {
					clampedW, clampedH := common.GetScreenSize()
					log.Printf("Debounced resize triggered: %dx%d (clamped %dx%d)", w, h, clampedW, clampedH)
					displayChangeMu.Lock()
					defer displayChangeMu.Unlock()
					if globalControlClient != nil {
						gen := nextGeneration()
						globalControlClient.ApplyConfig(clampedW, clampedH, common.FPS, common.HDPI, common.TargetBandwidthMbps, gen, common.Chroma)
					}
					// Update clients
					common.BroadcastJSON(configPayload())
				}
			})
			resizeTimerMu.Unlock()
		}
	case "ping":
		ts, ok := msg["ts"].(float64)
		if !ok {
			ts, ok = msg["timestamp"].(float64)
		}
		if ok {
			resp := map[string]interface{}{
				"type":     "pong",
				"ts":       ts,
				"serverTs": common.BenchmarkClockNowMs(),
			}
			writeJSON(resp)
		}
	}
}

func broadcastVideoFrame(data []byte, isKeyframe bool, codec string) {
	// For WebTransport we don't have stream IDs for individual frames, just use 0
	common.WriteFrame(data, 0, common.BenchmarkClockNowMs(), nil)
}
