package common

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WebSocketSession struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

var (
	wsSessions      = make(map[*websocket.Conn]*WebSocketSession)
	wsSessionsMutex sync.RWMutex

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	wsSession := &WebSocketSession{
		conn: conn,
	}

	wsSessionsMutex.Lock()
	wsSessions[conn] = wsSession
	wsSessionsMutex.Unlock()

	log.Printf("WebSocket session established: %v", conn.RemoteAddr())
	if OnPeerConnected != nil {
		OnPeerConnected()
	}

	defer func() {
		wsSessionsMutex.Lock()
		delete(wsSessions, conn)
		wsSessionsMutex.Unlock()
		conn.Close()
		log.Printf("WebSocket session closed: %v", conn.RemoteAddr())
	}()

	writeJSON := func(v interface{}) error {
		wsSession.mu.Lock()
		defer wsSession.mu.Unlock()
		return conn.WriteJSON(v)
	}

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if messageType == websocket.TextMessage {
			var msg map[string]interface{}
			if err := json.Unmarshal(p, &msg); err != nil {
				continue
			}
			if MessageHandler != nil {
				MessageHandler(msg, writeJSON)
			}
		}
		// Binary messages could be handled here if needed, but for now client sends JSON
	}
}

func BroadcastWebSocketJSON(v interface{}) {
	wsSessionsMutex.RLock()
	defer wsSessionsMutex.RUnlock()

	for _, s := range wsSessions {
		s.mu.Lock()
		_ = s.conn.WriteJSON(v)
		s.mu.Unlock()
	}
}

func CloseAllWebSocketSessions() {
	wsSessionsMutex.Lock()
	defer wsSessionsMutex.Unlock()

	for conn := range wsSessions {
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server restart"))
		conn.Close()
	}
	wsSessions = make(map[*websocket.Conn]*WebSocketSession)
}

func WriteWebSocketFrame(frame []byte, streamID uint32, captureTime time.Time) {
	wsSessionsMutex.RLock()
	defer wsSessionsMutex.RUnlock()

	if len(wsSessions) == 0 {
		return
	}

	timestamp := float64(captureTime.UnixNano()) / float64(time.Millisecond)
	packetLen := uint32(9 + len(frame))
	header := make([]byte, 13) // 4 (length) + 1 (type) + 8 (timestamp)
	binary.BigEndian.PutUint32(header[0:], packetLen)
	header[4] = 1 // Video Type
	binary.BigEndian.PutUint64(header[5:], math.Float64bits(timestamp))

	packet := append(header, frame...)

	for _, s := range wsSessions {
		s.mu.Lock()
		_ = s.conn.WriteMessage(websocket.BinaryMessage, packet)
		s.mu.Unlock()
	}
}
