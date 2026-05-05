package main

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/danchitnis/llrdc/cmd/macos-server/encoder"
	"github.com/danchitnis/llrdc/internal/server"
)

var (
	encoderMu       sync.RWMutex
	globalVTEncoder *encoder.VTEncoder
)

func getEncoder() *encoder.VTEncoder {
	encoderMu.RLock()
	defer encoderMu.RUnlock()
	return globalVTEncoder
}

func recreateEncoder(width, height int) {
	encoderMu.Lock()
	defer encoderMu.Unlock()

	if globalVTEncoder != nil {
		log.Printf("Closing old VTEncoder (%dx%d)", globalVTEncoder.Width, globalVTEncoder.Height)
		globalVTEncoder.Close()
	}

	fps := server.FPS
	if fps <= 0 {
		fps = 60
	}
	bitrateKbps := 8000

	log.Printf("Creating new VTEncoder: %dx%d@%d FPS", width, height, fps)
	globalVTEncoder = encoder.NewVTEncoder(width, height, fps, bitrateKbps, func(data []byte, isKeyframe bool) {
		broadcastVideoFrame(data, isKeyframe)
	})
	if globalVTEncoder == nil {
		log.Printf("ERROR: Failed to create VideoToolbox encoder for %dx%d", width, height)
	}
}

func main() {
	log.SetOutput(os.Stdout)

	// 1. Initialize server config and flags
	server.InitConfig()

	// If screen size wasn't set by flags, use defaults
	width, height := server.GetScreenSize()
	if width == 320 && height == 240 {
		width, height = 1920, 1080
	}

	// 2. Initialize VideoToolbox Encoder
	recreateEncoder(width, height)
	if getEncoder() == nil {
		log.Fatal("Failed to create initial VideoToolbox encoder")
	}
	defer func() {
		encoderMu.Lock()
		if globalVTEncoder != nil {
			globalVTEncoder.Close()
		}
		encoderMu.Unlock()
	}()

	// 3. Set up input forwarding
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

	log.Printf("macOS Native Server listening on :%d", server.Port)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
