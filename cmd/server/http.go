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
						cpuUsage = val
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
	configMsg := map[string]interface{}{
		"type":                  "config",
		"videoCodec":            VideoCodec,
		"chroma":                Chroma,
		"gpuAvailable":          UseGPU,
		"av1NvencAvailable":     AV1NVENCAvailable,
		"h264Nvenc444Available": H264NVENC444Available,
		"h265Nvenc444Available": H265NVENC444Available,
		"framerate":             FPS,
		"bandwidth":             targetBandwidthMbps,
		"quality":               targetQuality,
		"vbr":                   targetVBR,
		"mpdecimate":            targetMpdecimate,
		"keyframe_interval":     targetKeyframeInterval,
		"settle_time":           SettleTime,
		"tile_size":             TileSize,
		"enable_audio":          EnableAudio,
		"audio_bitrate":         AudioBitrate,
		"hdpi":                  HDPI,
		"restarted":             restarted,
	}
	broadcastJSON(configMsg)
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
	initialConfig := map[string]interface{}{
		"type":                  "config",
		"videoCodec":            VideoCodec,
		"chroma":                Chroma,
		"gpuAvailable":          UseGPU,
		"av1NvencAvailable":     AV1NVENCAvailable,
		"h264Nvenc444Available": H264NVENC444Available,
		"h265Nvenc444Available": H265NVENC444Available,
		"framerate":             FPS,
		"bandwidth":             targetBandwidthMbps,
		"quality":               targetQuality,
		"vbr":                   targetVBR,
		"mpdecimate":            targetMpdecimate,
		"keyframe_interval":     targetKeyframeInterval,
		"settle_time":           SettleTime,
		"tile_size":             TileSize,
		"enable_audio":          EnableAudio,
		"audio_bitrate":         AudioBitrate,
		"hdpi":                  HDPI,
	}
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
			// Allow multiple rapid WebSocket messages to settle
			time.Sleep(100 * time.Millisecond)

			go func() {
				if hdpiFloat, ok := msg["hdpi"].(float64); ok {
					hdpi := int(hdpiFloat)
					log.Printf("Received HDPI config: %d%%", hdpi)
					if HDPI != hdpi {
						HDPI = hdpi
						applyHdpiSettings(os.Environ())
						injectMouseMove(0.5, 0.5)
						injectMouseMove(0.501, 0.501)
					}
				}
				if vCodec, ok := msg["videoCodec"].(string); ok {
					SetVideoCodec(vCodec)
				}
				if chromaStr, ok := msg["chroma"].(string); ok {
					SetChroma(chromaStr)
				}
				if vbrBool, ok := msg["vbr"].(bool); ok {
					SetVBR(vbrBool)
				}
				if mpdecimateBool, ok := msg["mpdecimate"].(bool); ok {
					SetMpdecimate(mpdecimateBool)
				}
				if keyframeFloat, ok := msg["keyframe_interval"].(float64); ok {
					SetKeyframeInterval(int(keyframeFloat))
				}
				if effortFloat, ok := msg["cpu_effort"].(float64); ok {
					SetCpuEffort(int(effortFloat))
				}
				if threadsFloat, ok := msg["cpu_threads"].(float64); ok {
					SetCpuThreads(int(threadsFloat))
				}
				if mouseBool, ok := msg["enable_desktop_mouse"].(bool); ok {
					SetDrawMouse(mouseBool)
				}
				if settleTime, ok := msg["settle_time"].(float64); ok {
					log.Printf("Received Settle Time config: %vms", settleTime)
					SettleTime = int(settleTime)
				}
				if tileSize, ok := msg["tile_size"].(float64); ok {
					log.Printf("Received Tile Size config: %vpx", tileSize)
					TileSize = int(tileSize)
				}
				if enableAudioBool, ok := msg["enable_audio"].(bool); ok {
					SetEnableAudio(enableAudioBool)
				}
				if audioBitrateStr, ok := msg["audio_bitrate"].(string); ok {
					SetAudioBitrate(audioBitrateStr)
				}

				if bwFloat, ok := msg["bandwidth"].(float64); ok {
					SetBandwidth(int(bwFloat))
				} else if qFloat, ok := msg["quality"].(float64); ok {
					SetQuality(int(qFloat))
				}

				if fpsFloat, ok := msg["framerate"].(float64); ok {
					SetFramerate(int(fpsFloat))
				}

					log.Println("Config updated, sending Kill() to restart stream...")
					PrimeFrameGeneration(1250*time.Millisecond, 10, 100*time.Millisecond)
					broadcastConfig(true)
				}()
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
						go func() {
							PauseStreaming()
							if err := resizeDisplay(clampedW, clampedH); err != nil {
								log.Printf("Resize failed: %v", err)
							}

							// Force Wayland compositor damage to allocate the new sized buffer
							injectMouseMove(0.5, 0.5)
							injectMouseMove(0.501, 0.501)

							time.Sleep(1000 * time.Millisecond)
							ResumeStreaming()
							PrimeFrameGeneration(250*time.Millisecond, 10, 100*time.Millisecond)
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
