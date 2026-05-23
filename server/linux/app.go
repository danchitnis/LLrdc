package linux

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
	log.Printf("Args: %v", os.Args)

	InitConfig()
	log.Printf("Parsed CaptureMode: %v", CaptureMode)
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

	InitWebRTC()
	startStreaming(broadcastVideoFrame)
	if CaptureMode == CaptureModeAgent {
		go startAgentControl()
	} else {
		startAudioStreaming()
	}
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
