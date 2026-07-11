package common

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type WebSocketSession struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	IsNative bool
	ClientID string
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

	isNative := r.URL.Query().Get("client") == "native"
	clientID := r.URL.Query().Get("client_id")
	wsSession := &WebSocketSession{
		conn:     conn,
		IsNative: isNative,
		ClientID: clientID,
	}

	// Close only this client's stale transports before registering the new one.
	CloseClientSessions(clientID)

	wsSessionsMutex.Lock()
	wsSessions[conn] = wsSession
	wsSessionsMutex.Unlock()
	atomic.AddInt64(&ActiveClientCount, 1)

	log.Printf("WebSocket session established: %v", conn.RemoteAddr())
	if OnClientConnected != nil {
		OnClientConnected()
	}
	HandleClientConnectionChange()

	defer func() {
		wsSessionsMutex.Lock()
		delete(wsSessions, conn)
		wsSessionsMutex.Unlock()
		atomic.AddInt64(&ActiveClientCount, -1)
		conn.Close()
		log.Printf("WebSocket session closed: %v", conn.RemoteAddr())
		SafeClientDisconnected()
		if wsSession.ClientID != "" {
			CloseWebTransportSessionsByClientID(wsSession.ClientID)
		}
		HandleClientConnectionChange()
	}()

	writeJSON := func(v interface{}) error {
		wsSession.mu.Lock()
		defer wsSession.mu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		err := conn.WriteJSON(v)
		_ = conn.SetWriteDeadline(time.Time{})
		return err
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

	log.Printf("[Server-Handshake] [%s] BroadcastWebSocketJSON: starting config broadcast to %d sessions", time.Now().Format("15:04:05.000"), len(wsSessions))
	for conn, s := range wsSessions {
		s.mu.Lock()
		_ = s.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		err := s.conn.WriteJSON(v)
		_ = s.conn.SetWriteDeadline(time.Time{})
		s.mu.Unlock()
		log.Printf("[Server-Handshake] [%s] BroadcastWebSocketJSON: wrote config to session %v (err: %v)", time.Now().Format("15:04:05.000"), conn.RemoteAddr(), err)
	}
}

func CloseAllWebSocketSessions() {
	wsSessionsMutex.Lock()
	sessions := make([]*WebSocketSession, 0, len(wsSessions))
	for conn, s := range wsSessions {
		sessions = append(sessions, s)
		delete(wsSessions, conn)
	}
	wsSessionsMutex.Unlock()

	for _, s := range sessions {
		conn := s.conn
		s.mu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server restart"))
		conn.Close()
		s.mu.Unlock()
	}
}

func CloseWebSocketSessionsByClientID(clientID string) {
	if clientID == "" {
		return
	}

	wsSessionsMutex.Lock()
	sessions := make([]*WebSocketSession, 0)
	for conn, s := range wsSessions {
		if s.ClientID == clientID {
			sessions = append(sessions, s)
			delete(wsSessions, conn)
		}
	}
	wsSessionsMutex.Unlock()

	for _, s := range sessions {
		s.mu.Lock()
		_ = s.conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client reconnect"))
		_ = s.conn.Close()
		s.mu.Unlock()
	}
}

func WriteWebSocketFrame(frame []byte, streamID uint32, timestampMs int64) {
	wsSessionsMutex.RLock()
	defer wsSessionsMutex.RUnlock()

	if len(wsSessions) == 0 {
		return
	}

	timestamp := float64(timestampMs)
	packetLen := uint32(9 + len(frame))
	header := make([]byte, 13) // 4 (length) + 1 (type) + 8 (timestamp)
	binary.BigEndian.PutUint32(header[0:], packetLen)
	header[4] = 1 // Video Type
	binary.BigEndian.PutUint64(header[5:], math.Float64bits(timestamp))

	packet := append(header, frame...)

	for _, s := range wsSessions {
		if s.IsNative {
			continue // Skip sending binary video frames to native signaling-only sessions!
		}
		s.mu.Lock()
		_ = s.conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		_ = s.conn.WriteMessage(websocket.BinaryMessage, packet)
		_ = s.conn.SetWriteDeadline(time.Time{})
		s.mu.Unlock()
	}
}
