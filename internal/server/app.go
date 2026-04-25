package server

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var cleanupTasks []func()

func Run() error {
	log.Println("Starting llrdc (Go)...")

	initConfig()
	initScreenSize(3840, 2160)
	initReadiness()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		shutdown()
	}()

	if !TestPattern {
		if err := startWayland(); err != nil {
			return fmt.Errorf("failed to initialize Wayland: %w", err)
		}
	} else {
		log.Println("TEST_PATTERN mode: skipping display server setup.")
	}

	initWebRTC()
	startStreaming(broadcastVideoFrame)
	startAudioStreaming()
	startHTTPServer()
	return nil
}

func shutdown() {
	log.Println("Shutting down...")
	for i := len(cleanupTasks) - 1; i >= 0; i-- {
		cleanupTasks[i]()
	}
	os.Exit(0)
}
