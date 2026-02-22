package main

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
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

var clientsMutex sync.Mutex
var clients = make(map[*websocket.Conn]bool)

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

func broadcastIVFFrame(frame []byte) {
	WriteWebRTCFrame(frame)

	timestamp := float64(time.Now().UnixNano()) / float64(time.Millisecond)
	header := make([]byte, 9)
	header[0] = 1 // Video Type
	binary.BigEndian.PutUint64(header[1:], math.Float64bits(timestamp))

	packet := append(header, frame...)

	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	for client := range clients {
		_ = client.WriteMessage(websocket.BinaryMessage, packet)
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

	clientsMutex.Lock()
	clients[conn] = true
	clientsMutex.Unlock()

	defer func() {
		clientsMutex.Lock()
		delete(clients, conn)
		clientsMutex.Unlock()
	}()

	var connMutex sync.Mutex
	writeJSON := func(v interface{}) error {
		connMutex.Lock()
		defer connMutex.Unlock()
		return conn.WriteJSON(v)
	}

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
			if bwFloat, ok := msg["bandwidth"].(float64); ok {
				bw := int(bwFloat)
				log.Printf("Received bandwidth config: %d Mbps", bw)
				SetBandwidth(bw)
			}
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
				pc, err = createPeerConnection()
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
