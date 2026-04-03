package main

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn        *websocket.Conn
	mu          sync.Mutex
	sendChan    chan []byte
	webrtcReady bool
}

var clientsMutex sync.Mutex
var clients = make(map[*websocket.Conn]*Client)

func configPayload(restarted bool) map[string]interface{} {
	directState := snapshotDirectBufferState()
	return map[string]interface{}{
		"type":                   "config",
		"videoCodec":             VideoCodec,
		"chroma":                 Chroma,
		"gpuAvailable":           UseGPU,
		"captureMode":            CaptureMode,
		"directBufferRequested":  directState.Requested,
		"directBufferSupported":  directState.Supported,
		"directBufferActive":     directState.Active,
		"directBufferReason":     directState.Reason,
		"directBufferRenderNode": directState.RenderNode,
		"directBufferRenderer":   directState.Renderer,
		"av1NvencAvailable":      AV1NVENCAvailable,
		"h264Nvenc444Available":  H264NVENC444Available,
		"h265Nvenc444Available":  H265NVENC444Available,
		"framerate":              FPS,
		"bandwidth":              targetBandwidthMbps,
		"quality":                targetQuality,
		"vbr":                    targetVBR,
		"mpdecimate":             targetMpdecimate,
		"keyframe_interval":      targetKeyframeInterval,
		"settle_time":            SettleTime,
		"tile_size":              TileSize,
		"enable_audio":           EnableAudio,
		"audio_bitrate":          AudioBitrate,
		"hdpi":                   HDPI,
		"webrtc_buffer":          WebRTCBufferSize,
		"activity_hz":            ActivityPulseHz,
		"activity_timeout":       ActivityTimeout,
		"nvenc_latency":          NVENCLatencyMode,
		"webrtc_low_latency":     WebRTCLowLatency,
		"restarted":              restarted,
	}
}

func startHTTPServer() {
	go func() {
		for {
			time.Sleep(2 * time.Second)

			ffmpegMutex.Lock()
			cmd := ffmpegCmd
			ffmpegMutex.Unlock()

			var cpuUsage float64 = 0

			if cmd != nil && cmd.Process != nil {
				pid := cmd.Process.Pid
				out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=").Output()
				if err == nil {
					valStr := strings.TrimSpace(string(out))
					if val, err := strconv.ParseFloat(valStr, 64); err == nil {
						// Report raw percentage (100.0 = 1 core)
						cpuUsage = val
						if cpuUsage == 0 {
							cpuUsage = 0.1
						}
					}
				}
			}

			statsMsg := map[string]interface{}{
				"type":      "stats",
				"ffmpegCpu": cpuUsage,
			}

			clientsMutex.Lock()
			for _, client := range clients {
				client.mu.Lock()
				_ = client.conn.WriteJSON(statsMsg)
				client.mu.Unlock()
			}
			clientsMutex.Unlock()
		}
	}()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	http.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		payload, err := marshalReadinessStatus()
		if err != nil {
			http.Error(w, "failed to marshal readiness state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !readiness.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = w.Write(payload)
	})

	http.HandleFunc("/latencyz", func(w http.ResponseWriter, r *http.Request) {
		record, ok := snapshotLatencyTrace(r.URL.Query().Get("marker"))
		if !ok {
			http.Error(w, "latency trace not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(record); err != nil {
			http.Error(w, "failed to encode latency trace", http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if websocket.IsWebSocketUpgrade(r) {
			wsHandler(w, r)
			return
		}

		log.Printf("HTTP %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		if r.Method == http.MethodGet {
			urlPath := r.URL.Path
			if urlPath == "/" {
				urlPath = "/viewer.html"
			}

			wd, _ := os.Getwd()
			publicDir := filepath.Join(wd, "public")
			filePath := filepath.Join(publicDir, urlPath)

			// Basic path traversal prevention
			if filepath.Clean(filePath)[:len(publicDir)] != publicDir {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			if filepath.Ext(filePath) == ".html" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			}

			http.ServeFile(w, r, filePath)
			return
		}
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	addr := ":" + strconv.Itoa(Port)
	log.Printf("Server listening on http://0.0.0.0%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func broadcastJSON(msg interface{}) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	for _, client := range clients {
		client.mu.Lock()
		_ = client.conn.WriteJSON(msg)
		client.mu.Unlock()
	}
}

func broadcastVideoFrame(frame []byte, streamID uint32, codec string) {
	captureTime := time.Now()
	recordLatencyProbeFrame(captureTime)
	// Copy frame for WebRTC delivery so we don't share memory with IVF reader
	webrtcCopy := make([]byte, len(frame))
	copy(webrtcCopy, frame)
	WriteWebRTCFrame(webrtcCopy, streamID, captureTime, codec)

	timestamp := float64(captureTime.UnixNano()) / float64(time.Millisecond)
	header := make([]byte, 9)
	header[0] = 1 // Video Type
	binary.BigEndian.PutUint64(header[1:], math.Float64bits(timestamp))

	packet := append(header, frame...)

	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	for _, client := range clients {
		if client.webrtcReady {
			continue // Skip sending heavy binary frames if WebRTC is handling it
		}
		select {
		case client.sendChan <- packet:
		default:
			// Drop frame if client websocket buffer is full to prevent blocking ffmpeg
		}
	}
}

func broadcastConfig(restarted bool) {
	broadcastJSON(configPayload(restarted))
}

func handleInputMessage(msg map[string]interface{}) {
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "keydown", "keyup", "key":
		if key, ok := msg["key"].(string); ok {
			injectKey(key, msgType)
		}
	case "mousemove":
		if x, ok1 := msg["x"].(float64); ok1 {
			if y, ok2 := msg["y"].(float64); ok2 {
				injectMouseMove(x, y)
			}
		}
	case "mousebtn":
		if btn, ok := msg["button"].(float64); ok {
			if action, ok2 := msg["action"].(string); ok2 {
				injectMouseButton(int(btn), action)
			}
		}
	case "wheel":
		if dx, ok1 := msg["deltaX"].(float64); ok1 {
			if dy, ok2 := msg["deltaY"].(float64); ok2 {
				injectMouseWheel(dx, dy)
			}
		}
	case "spawn":
		if cmd, ok := msg["command"].(string); ok {
			allowed := map[string]bool{
				"gnome-calculator": true, "weston-terminal": true, "gedit": true,
				"mousepad": true, "xclock": true, "xeyes": true, "xfce4-terminal": true,
			}
			parts := strings.Fields(cmd)
			if len(parts) > 0 && allowed[parts[0]] {
				spawnApp(cmd)
			}
		}
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("Client connected from %s", r.RemoteAddr)

	client := &Client{
		conn:     conn,
		sendChan: make(chan []byte, 300),
	}

	clientsMutex.Lock()
	clients[conn] = client
	clientsMutex.Unlock()

	defer func() {
		clientsMutex.Lock()
		delete(clients, conn)
		clientsMutex.Unlock()
	}()

	// Background worker for non-blocking websocket writes
	go func() {
		for packet := range client.sendChan {
			client.mu.Lock()
			_ = client.conn.WriteMessage(websocket.BinaryMessage, packet)
			client.mu.Unlock()
		}
	}()

	writeJSON := func(v interface{}) error {
		client.mu.Lock()
		defer client.mu.Unlock()
		return client.conn.WriteJSON(v)
	}

	// Send initial codec and config to client
	initialConfig := configPayload(false)
	_ = writeJSON(initialConfig)

	var pc *webrtc.PeerConnection

	defer func() {
		if pc != nil {
			pc.Close()
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		msgType, _ := msg["type"].(string)

		switch msgType {
		case "keydown", "keyup", "key", "mousemove", "mousebtn", "wheel", "spawn":
			handleInputMessage(msg)
		case "config":
			go func(configMsg map[string]interface{}) {
				log.Printf("Received config message: %v", configMsg)
				restartRequested := false
				displayChanged := false
				previousStreamID := getCurrentFFmpegStreamID()

				if hdpiFloat, ok := configMsg["hdpi"].(float64); ok {
					hdpi := int(hdpiFloat)
					log.Printf("Received HDPI config: %d%%", hdpi)
					if HDPI != hdpi {
						HDPI = hdpi
						applyHdpiSettings(os.Environ())
						displayChanged = true
					}
				}
				if vCodec, ok := configMsg["videoCodec"].(string); ok {
					if VideoCodec != vCodec {
						restartRequested = true
					}
					SetVideoCodec(vCodec)
				}
				if chromaStr, ok := configMsg["chroma"].(string); ok {
					if Chroma != chromaStr {
						restartRequested = true
					}
					SetChroma(chromaStr)
				}
				if vbrBool, ok := configMsg["vbr"].(bool); ok {
					if targetVBR != vbrBool {
						restartRequested = true
					}
					SetVBR(vbrBool)
				}
				if mpdecimateBool, ok := configMsg["mpdecimate"].(bool); ok {
					if targetMpdecimate != mpdecimateBool {
						restartRequested = true
					}
					SetMpdecimate(mpdecimateBool)
				}
				if keyframeFloat, ok := configMsg["keyframe_interval"].(float64); ok {
					keyframe := int(keyframeFloat)
					if targetKeyframeInterval != keyframe {
						restartRequested = true
					}
					SetKeyframeInterval(keyframe)
				}
				if effortFloat, ok := configMsg["cpu_effort"].(float64); ok {
					effort := int(effortFloat)
					if targetCpuEffort != effort {
						restartRequested = true
					}
					SetCpuEffort(effort)
				}
				if threadsFloat, ok := configMsg["cpu_threads"].(float64); ok {
					threads := int(threadsFloat)
					if targetCpuThreads != threads {
						restartRequested = true
					}
					SetCpuThreads(threads)
				}
				if mouseBool, ok := configMsg["enable_desktop_mouse"].(bool); ok {
					if targetDrawMouse != mouseBool {
						restartRequested = true
					}
					SetDrawMouse(mouseBool)
				}
				if settleTime, ok := configMsg["settle_time"].(float64); ok {
					log.Printf("Received Settle Time config: %vms", settleTime)
					SettleTime = int(settleTime)
				}
				if tileSize, ok := configMsg["tile_size"].(float64); ok {
					log.Printf("Received Tile Size config: %vpx", tileSize)
					TileSize = int(tileSize)
				}
				if enableAudioBool, ok := configMsg["enable_audio"].(bool); ok {
					SetEnableAudio(enableAudioBool)
				}
				if audioBitrateStr, ok := configMsg["audio_bitrate"].(string); ok {
					SetAudioBitrate(audioBitrateStr)
				}
				if webrtcBufferFloat, ok := configMsg["webrtc_buffer"].(float64); ok {
					webrtcBuffer := int(webrtcBufferFloat)
					if WebRTCBufferSize != webrtcBuffer {
						log.Printf("WebRTC buffer changed to %d", webrtcBuffer)
						WebRTCBufferSize = webrtcBuffer
						// This takes effect on the next WriteWebRTCFrame
					}
				}
				if activityHzFloat, ok := configMsg["activity_hz"].(float64); ok {
					activityHz := int(activityHzFloat)
					if ActivityPulseHz != activityHz {
						log.Printf("Activity heartbeat frequency changed to %d Hz", activityHz)
						ActivityPulseHz = activityHz
					}
				}
				if activityTimeoutFloat, ok := configMsg["activity_timeout"].(float64); ok {
					activityTimeout := int(activityTimeoutFloat)
					if ActivityTimeout != activityTimeout {
						log.Printf("Activity heartbeat timeout changed to %d ms", activityTimeout)
						ActivityTimeout = activityTimeout
					}
				}
				if nvencLatencyBool, ok := configMsg["nvenc_latency"].(bool); ok {
					if NVENCLatencyMode != nvencLatencyBool {
						log.Printf("NVENC latency mode changed to %v", nvencLatencyBool)
						NVENCLatencyMode = nvencLatencyBool
						restartRequested = true
					}
				}
				if webrtcLowLatencyBool, ok := configMsg["webrtc_low_latency"].(bool); ok {
					if WebRTCLowLatency != webrtcLowLatencyBool {
						SetWebRTCLowLatency(webrtcLowLatencyBool)
						restartRequested = true
					}
				}

				if bwFloat, ok := configMsg["bandwidth"].(float64); ok {
					bandwidth := int(bwFloat)
					if targetMode != "bandwidth" || targetBandwidthMbps != bandwidth {
						restartRequested = true
					}
					SetBandwidth(bandwidth)
				} else if qFloat, ok := configMsg["quality"].(float64); ok {
					quality := int(qFloat)
					if targetMode != "quality" || targetQuality != quality {
						restartRequested = true
					}
					SetQuality(quality)
				}

				if fpsFloat, ok := configMsg["framerate"].(float64); ok {
					fps := int(fpsFloat)
					if FPS != fps {
						restartRequested = true
					}
					SetFramerate(fps)
				}

				if displayChanged {
					width, height := GetScreenSize()
					if err := waitForDisplayState(width, height, 5*time.Second); err != nil {
						log.Printf("Timed out waiting for HDPI display update: %v", err)
					}
					TriggerPing()
				}

				if restartRequested {
					log.Println("Config updated, waiting for restarted stream to become ready...")
					PrimeFrameGeneration(0, 5, 100*time.Millisecond)
					if err := waitForStreamReadyAfter(previousStreamID, 8*time.Second); err != nil {
						log.Printf("Restarted stream did not become ready in time: %v", err)
						PrimeFrameGeneration(0, 10, 100*time.Millisecond)
					}
				}

				broadcastConfig(true)
			}(msg)
		case "resize":
			widthFloat, wOk := msg["width"].(float64)
			heightFloat, hOk := msg["height"].(float64)
			if wOk && hOk {
				width := int(widthFloat)
				height := int(heightFloat)
				if SetScreenSize(width, height) {
					// Get the actual clamped size
					clampedW, clampedH := GetScreenSize()
					log.Printf("Received resize: %dx%d (clamped to %dx%d)", width, height, clampedW, clampedH)
					if !TestPattern {
						previousStreamID := getCurrentFFmpegStreamID()
						go func() {
							PauseStreaming()
							if err := resizeDisplay(clampedW, clampedH); err != nil {
								log.Printf("Resize failed: %v", err)
							}

							if err := waitForDisplayState(clampedW, clampedH, 5*time.Second); err != nil {
								log.Printf("Resize did not reach requested display state: %v", err)
							}

							ResumeStreaming()
							PrimeFrameGeneration(0, 5, 100*time.Millisecond)
							if err := waitForStreamReadyAfter(previousStreamID, 8*time.Second); err != nil {
								log.Printf("Resized stream did not become ready in time: %v", err)
								PrimeFrameGeneration(0, 10, 100*time.Millisecond)
							}
							broadcastConfig(true)
						}()
					} else {
						RestartForResize()
						broadcastConfig(true)
					}
				}
			}
		case "webrtc_ready":
			log.Printf("Client WebRTC ready, stopping fallback websocket video transmission")
			clientsMutex.Lock()
			if c, ok := clients[conn]; ok {
				c.webrtcReady = true
			}
			clientsMutex.Unlock()
			// Trigger a ping to push the first frame in VBR mode
			TriggerPing()
		case "ping":
			if ts, ok := msg["timestamp"].(float64); ok {
				resp := map[string]interface{}{"type": "pong", "timestamp": ts}
				writeJSON(resp)
			}
		case "webrtc_offer":
			handleWebRTCOffer(msg, r.Host, &pc, writeJSON)
		case "webrtc_ice":
			handleWebRTCICE(msg, pc)
		}
	}
}
