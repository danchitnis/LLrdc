package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

var cleanupTasks []func()

func main() {
	log.SetOutput(os.Stdout)
	log.Println("Starting llrdc (Go)...")

	// Initialize config
	initConfig()
	initScreenSize(1920, 1080)

	// Setup signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		shutdown()
	}()

	// 1. Start Wayland unless TEST_PATTERN is set
	if !TestPattern {
		if os.Getenv("USE_WAYLAND") == "true" {
			UseWayland = true
		}
		if err := startWayland(DisplayNum); err != nil {
			log.Fatalf("Failed to initialize Wayland: %v", err)
		}
	} else {
		log.Println("TEST_PATTERN mode: skipping Wayland setup.")
	}

	// 2. Initialize WebRTC and RTP Listener
	initWebRTC()

	// 3. Start ffmpeg streaming
	startStreaming(broadcastVideoFrame)
	startAudioStreaming()
	// 4. Start HTTP & WebSocket server (blocks)
	startHTTPServer()
}

func shutdown() {
	log.Println("Shutting down...")
	for i := len(cleanupTasks) - 1; i >= 0; i-- {
		cleanupTasks[i]()
	}
	os.Exit(0)
}
