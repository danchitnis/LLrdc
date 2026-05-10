package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/danchitnis/llrdc/internal/server"
)

var (
	encMgr          *EncoderManager
	currentGeneration uint64
	generationMu    sync.Mutex
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
	server.InitConfig()
	server.CaptureMode = server.CaptureModeAgent
	server.HDPI = 100
	server.VideoCodec = "h264"

	// Set initial macOS split mode dimensions (1280x720 is a safe default)
	server.SetScreenSize(1280, 720)
	width, height := server.GetScreenSize()

	// 2. Initialize VideoToolbox Encoder
	gen := nextGeneration()
	encMgr.Recreate(width, height, server.FPS, gen)
	if enc, _ := encMgr.Get(); enc == nil {
		log.Fatal("Failed to create initial VideoToolbox encoder")
	}
	defer encMgr.Close()

	server.OnForceKeyframe = func() {
		log.Printf("Forcing VideoToolbox IDR frame")
		if enc, _ := encMgr.Get(); enc != nil {
			enc.ForceKeyframe()
		}
	}

	// 3. Set up input forwarding and control client
	agentHost := "127.0.0.1"
	if server.AgentAddress != "" {
		host, _, err := net.SplitHostPort(server.AgentAddress)
		if err == nil {
			agentHost = host
		}
	}
	startAgentControlClient(agentHost)

	server.SetInputWriter(globalInputWriter)
	go startInputForwarder()
	go startInputProcessor()

	// 4. Start Video Receiver (from Docker)
	go startVideoReceiver()

	// 5. Start HTTP & Signaling Server
	fs := http.FileServer(http.Dir("public"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			handleSignaling(w, r)
			return
		}
		fs.ServeHTTP(w, r)
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
				"input":   inputConnected,
				"control": controlConnected,
				"genMatch": readyGen == getGeneration(),
			},
		})
	})

	log.Printf("macOS Native Server listening on :%d", server.Port)
	ln, err := net.Listen("tcp4", "0.0.0.0:8080")
	if err != nil {
		log.Fatalf("Failed to listen on 0.0.0.0:8080: %v", err)
	}
	if err := http.Serve(ln, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
