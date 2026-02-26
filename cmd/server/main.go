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
	initScreenSize(3840, 2160)

	// Setup signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		shutdown()
	}()

	// 1. Start X11 unless TEST_PATTERN is set
	if os.Getenv("TEST_PATTERN") == "" {
		if err := startX11(DisplayNum); err != nil {
			log.Fatalf("Failed to initialize X11: %v", err)
		}
	} else {
		log.Println("TEST_PATTERN mode: skipping X11 setup.")
	}

	// 2. Initialize WebRTC and RTP Listener
	initWebRTC()

	// 3. Start ffmpeg streaming
	startStreaming(broadcastIVFFrame)

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
