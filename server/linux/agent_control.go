package linux

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

	splitState struct {
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

	displayChangeRequested := false
	captureChangeRequested := false

	splitStateMu.Lock()
	if gen, ok := config["generation"].(float64); ok {
		if splitState.generation != uint64(gen) {
			log.Printf("Agent received generation config: %d", uint64(gen))
			splitState.generation = uint64(gen)
			captureChangeRequested = true
		}
	}
	if width, ok := config["width"].(float64); ok {
		if splitState.width != int(width) {
			splitState.width = int(width)
			displayChangeRequested = true
		}
	}
	if height, ok := config["height"].(float64); ok {
		if splitState.height != int(height) {
			splitState.height = int(height)
			displayChangeRequested = true
		}
	}
	if fps, ok := config["fps"].(float64); ok {
		if int(fps) != splitState.fps {
			log.Printf("Agent received FPS config: %d", int(fps))
			splitState.fps = int(fps)
			displayChangeRequested = true
		}
	}
	if pixFmt, ok := config["pixfmt"].(float64); ok {
		if int(pixFmt) != splitState.pixFmt {
			log.Printf("Agent received PixFmt config: %d", int(pixFmt))
			splitState.pixFmt = int(pixFmt)
			captureChangeRequested = true
		}
	}
	if bw, ok := config["bandwidth"].(float64); ok {
		SetBandwidth(int(bw))
	}
	w, h := splitState.width, splitState.height
	fps := splitState.fps
	splitStateMu.Unlock()

	// Apply Wayland changes
	if hdpi, ok := config["hdpi"].(float64); ok {
		if int(hdpi) != HDPI {
			log.Printf("Agent received HDPI config: %d%%", int(hdpi))
			HDPI = int(hdpi)
			// Apply HDPI changes to Wayland
			waylandEnv := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:99")
			applyHdpiSettings(waylandEnv)
			displayChangeRequested = true
		}
	}

	if SetScreenSize(w, h) {
		displayChangeRequested = true
	}
	if FPS != fps {
		FPS = fps
		displayChangeRequested = true
	}

	if displayChangeRequested {
		actualW, actualH := GetScreenSize()
		_ = resizeDisplay(actualW, actualH)
		captureChangeRequested = true
	}

	if captureChangeRequested {
		// Kill wf-recorder to trigger restart with new dimensions/format
		KillFFmpegWithTimestamp()

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
