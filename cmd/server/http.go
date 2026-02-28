package main

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")

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

func broadcastIVFFrame(frame []byte, streamID uint32) {
	captureTime := time.Now()
	// Copy frame for WebRTC delivery so we don't share memory with IVF reader
	webrtcCopy := make([]byte, len(frame))
	copy(webrtcCopy, frame)
	WriteWebRTCFrame(webrtcCopy, streamID, captureTime)

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

	var pc *webrtc.PeerConnection

	// Extract the host IP from the request to use as the WebRTC advertised IP
	hostIP := r.Host
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		hostIP = host
	}

	// Resolve localhost or hostnames to actual IP because Pion WebRTC strictly requires an IP
	if ips, err := net.LookupIP(hostIP); err == nil && len(ips) > 0 {
		for _, ip := range ips {
			if ipv4 := ip.To4(); ipv4 != nil {
				hostIP = ipv4.String()
				break
			}
		}
	}

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
		case "keydown", "keyup":
			if key, ok := msg["key"].(string); ok {
				injectKey(key, msgType, Display)
			}
		case "mousemove":
			if x, ok1 := msg["x"].(float64); ok1 {
				if y, ok2 := msg["y"].(float64); ok2 {
					injectMouseMove(x, y, Display)
				}
			}
		case "mousedown", "mouseup":
			if btn, ok := msg["button"].(float64); ok {
				injectMouseButton(int(btn), msgType, Display)
			}
		case "spawn":
			if cmd, ok := msg["command"].(string); ok {
				allowed := map[string]bool{
					"gnome-calculator": true, "weston-terminal": true, "gedit": true,
					"mousepad": true, "xclock": true, "xeyes": true, "xfce4-terminal": true,
				}
				if allowed[cmd] {
					spawnApp(cmd, Display)
				}
			}
		case "config":
			hasBwOrQuality := false
			if vbrBool, ok := msg["vbr"].(bool); ok {
				log.Printf("Received VBR config: %v", vbrBool)
				SetVBR(vbrBool)
			}
			if effortFloat, ok := msg["cpu_effort"].(float64); ok {
				effort := int(effortFloat)
				log.Printf("Received CPU effort config: %d", effort)
				SetCpuEffort(effort)
			}
			if threadsFloat, ok := msg["cpu_threads"].(float64); ok {
				threads := int(threadsFloat)
				log.Printf("Received CPU threads config: %d", threads)
				SetCpuThreads(threads)
			}
			if mouseBool, ok := msg["enable_desktop_mouse"].(bool); ok {
				log.Printf("Received Enable Desktop Mouse config: %v", mouseBool)
				SetDrawMouse(mouseBool)
			}
			if bwFloat, ok := msg["bandwidth"].(float64); ok {
				hasBwOrQuality = true
				bw := int(bwFloat)
				log.Printf("Received bandwidth config: %d Mbps", bw)
				// If framerate is also changing, set FPS first (without kill) so the
				// restarted ffmpeg picks up the new fps immediately.
				if fpsFloat, ok2 := msg["framerate"].(float64); ok2 {
					fps := int(fpsFloat)
					log.Printf("Received framerate config: %d fps", fps)
					ffmpegMutex.Lock()
					FPS = fps
					log.Printf("Target framerate changed to %d fps, restarting ffmpeg...", fps)
					ffmpegMutex.Unlock()
				}
				SetBandwidth(bw)
			} else if qFloat, ok := msg["quality"].(float64); ok {
				hasBwOrQuality = true
				q := int(qFloat)
				log.Printf("Received quality config: %d", q)
				if fpsFloat, ok2 := msg["framerate"].(float64); ok2 {
					fps := int(fpsFloat)
					log.Printf("Received framerate config: %d fps", fps)
					ffmpegMutex.Lock()
					FPS = fps
					log.Printf("Target framerate changed to %d fps, restarting ffmpeg...", fps)
					ffmpegMutex.Unlock()
				}
				SetQuality(q)
			}
			if !hasBwOrQuality {
				if fpsFloat, ok := msg["framerate"].(float64); ok {
					fps := int(fpsFloat)
					log.Printf("Received framerate config: %d fps", fps)
					SetFramerate(fps)
				}
			}
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
					if os.Getenv("TEST_PATTERN") == "" {
						if err := resizeDisplay(clampedW, clampedH); err != nil {
							log.Printf("Resize failed: %v", err)
						}
					}
					RestartForResize()
				}
			}
		case "webrtc_ready":
			log.Printf("Client WebRTC ready, stopping fallback websocket video transmission")
			clientsMutex.Lock()
			if c, ok := clients[conn]; ok {
				c.webrtcReady = true
			}
			clientsMutex.Unlock()
		case "ping":
			if ts, ok := msg["timestamp"].(float64); ok {
				resp := map[string]interface{}{"type": "pong", "timestamp": ts}
				writeJSON(resp)
			}
		case "webrtc_offer":
			if sdpMap, ok := msg["sdp"].(map[string]interface{}); ok {
				b, _ := json.Marshal(sdpMap)
				var sdp webrtc.SessionDescription
				json.Unmarshal(b, &sdp)

				if pc != nil {
					pc.Close()
				}
				pc, err = createPeerConnection(hostIP)
				if err != nil {
					log.Printf("Failed to create PeerConnection: %v", err)
					continue
				}

				pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
					if candidate != nil {
						cJSON := candidate.ToJSON()
						writeJSON(map[string]interface{}{
							"type":      "webrtc_ice",
							"candidate": cJSON,
						})
					}
				})

				if err := pc.SetRemoteDescription(sdp); err != nil {
					log.Printf("SetRemoteDescription error: %v", err)
					continue
				}

				answer, err := pc.CreateAnswer(nil)
				if err != nil {
					log.Printf("CreateAnswer error: %v", err)
					continue
				}

				if err := pc.SetLocalDescription(answer); err != nil {
					log.Printf("SetLocalDescription error: %v", err)
					continue
				}

				writeJSON(map[string]interface{}{
					"type": "webrtc_answer",
					"sdp":  pc.LocalDescription(),
				})
			}
		case "webrtc_ice":
			if candidateMap, ok := msg["candidate"].(map[string]interface{}); ok {
				if pc != nil {
					b, _ := json.Marshal(candidateMap)
					var ice webrtc.ICECandidateInit
					json.Unmarshal(b, &ice)
					if err := pc.AddICECandidate(ice); err != nil {
						log.Printf("AddICECandidate error: %v", err)
					}
				}
			}
		}
	}
}
