package main

import (
	"log"
	"net/http"
	"os"

	"github.com/danchitnis/llrdc/cmd/macos-server/encoder"
	"github.com/danchitnis/llrdc/internal/server"
)

var globalVTEncoder *encoder.VTEncoder

func main() {
	log.SetOutput(os.Stdout)

	// 1. Initialize server config and flags
	// This will parse command line flags and environment variables.
	server.InitConfig()

	// If screen size wasn't set by flags, use defaults
	width, height := server.GetScreenSize()
	if width == 320 && height == 240 { // Default minimums from GetScreenSize
		width, height = 1920, 1080
	}

	fps := server.FPS
	if fps <= 0 {
		fps = 60
	}

	bitrateKbps := 8000 // Default bitrate

	log.Printf("LLrdc macOS Native Server starting (Resolution: %dx%d@%d FPS)", width, height, fps)

	// 2. Initialize VideoToolbox Encoder
	globalVTEncoder = encoder.NewVTEncoder(width, height, fps, bitrateKbps, func(data []byte, isKeyframe bool) {
		broadcastVideoFrame(data, isKeyframe)
	})
	if globalVTEncoder == nil {
		log.Fatal("Failed to create VideoToolbox encoder")
	}
	defer globalVTEncoder.Close()

	// 3. Set up input forwarding
	server.SetInputWriter(globalInputWriter)
	go startInputForwarder()
	go startInputProcessor()

	// 4. Start Video Receiver (from Docker)
	go startVideoReceiver(width, height, globalVTEncoder)

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
