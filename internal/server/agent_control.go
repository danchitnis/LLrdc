package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/internal/splitproto"
)

var (
	controlConns   = make(map[net.Conn]struct{})
	controlConnsMu sync.Mutex
	splitStateMu   sync.RWMutex
	splitState     struct {
		generation uint64
		fps        int
		width      int
		height     int
		pixFmt     int
	}
)

func startAgentControl() {
	addr := fmt.Sprintf(":%d", splitproto.ControlPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Agent control failed to listen on %s: %v", addr, err)
		return
	}
	defer ln.Close()

	log.Printf("Agent control listening on %s", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Agent control accept error: %v", err)
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
		}
		go handleAgentControlConnection(conn)
	}
}

func handleAgentControlConnection(conn net.Conn) {
	controlConnsMu.Lock()
	controlConns[conn] = struct{}{}
	controlConnsMu.Unlock()

	defer func() {
		controlConnsMu.Lock()
		delete(controlConns, conn)
		controlConnsMu.Unlock()
		conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var msg splitproto.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Agent control failed to unmarshal: %v", err)
			continue
		}

		switch msg.Type {
		case splitproto.MsgApplyConfig:
			handleApplyConfig(conn, msg.Config)
		}
	}
}

func handleApplyConfig(conn net.Conn, config map[string]interface{}) {
	log.Printf("Agent control: applying config: %v", config)

	splitStateMu.Lock()
	if gen, ok := config["generation"].(float64); ok {
		splitState.generation = uint64(gen)
	}
	if width, ok := config["width"].(float64); ok {
		splitState.width = int(width)
	}
	if height, ok := config["height"].(float64); ok {
		splitState.height = int(height)
	}
	if fps, ok := config["fps"].(float64); ok {
		if int(fps) != splitState.fps {
			log.Printf("Agent received FPS config: %d", int(fps))
			splitState.fps = int(fps)
		}
	}
	if pixFmt, ok := config["pixfmt"].(float64); ok {
		splitState.pixFmt = int(pixFmt)
	}
	w, h := splitState.width, splitState.height
	fps := splitState.fps
	splitStateMu.Unlock()

	// Apply Wayland changes
	configChangeRequested := false
	if hdpi, ok := config["hdpi"].(float64); ok {
		if int(hdpi) != HDPI {
			log.Printf("Agent received HDPI config: %d%%", int(hdpi))
			HDPI = int(hdpi)
			// Apply HDPI changes to Wayland
			waylandEnv := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:99")
			applyHdpiSettings(waylandEnv)
			configChangeRequested = true
		}
	}

	if SetScreenSize(w, h) {
		configChangeRequested = true
	}
	if FPS != fps {
		FPS = fps
		configChangeRequested = true
	}

	// For split path, we don't use the shared broadcastConfig/applyDisplayChange
	// We handle it here explicitly.
	if configChangeRequested {
		actualW, actualH := GetScreenSize()
		_ = resizeDisplay(actualW, actualH)
		
		// Wait for display to be ready before restarting capture
		// Increased timeout to 5s for better stability in Docker/CI
		_ = waitForDisplayState(actualW, actualH, 5*time.Second)

		// Kill wf-recorder to trigger restart with new dimensions
		killFFmpegWithTimestamp()
		
		// Brief pause to allow wf-recorder to fully exit and Wayland to settle
		time.Sleep(200 * time.Millisecond)
	}

	// Send ConfigApplied
	resp := splitproto.Message{
		Type: splitproto.MsgConfigApplied,
		Config: map[string]interface{}{
			"generation": splitState.generation,
		},
	}
	data, _ := json.Marshal(resp)
	conn.Write(append(data, '\n'))
}

func getSplitHeader() splitproto.Header {
	splitStateMu.RLock()
	defer splitStateMu.RUnlock()
	
	w, h := GetScreenSize()

	return splitproto.Header{
		Magic:      [4]byte{'L', 'L', 'S', 'P'},
		Version:    splitproto.Version,
		Generation: splitState.generation,
		Width:      uint16(w),
		Height:     uint16(h),
		FPS:        uint16(splitState.fps),
		PixFmt:     uint16(splitState.pixFmt),
	}
}

func BroadcastMsg(msg splitproto.Message) {
	data, _ := json.Marshal(msg)
	data = append(data, '\n')

	controlConnsMu.Lock()
	defer controlConnsMu.Unlock()
	for conn := range controlConns {
		conn.Write(data)
	}
}

func NotifyFirstFrame(generation uint64) {
	log.Printf("Agent control: notifying first frame for generation %d", generation)
	BroadcastMsg(splitproto.Message{
		Type: splitproto.MsgFirstFrame,
		Config: map[string]interface{}{
			"generation": generation,
		},
	})
}
