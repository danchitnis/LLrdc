package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Disable logging to stdout to keep standard output dedicated entirely to raw bitstream bytes
	log.SetOutput(os.Stderr)
	log.Println("Starting Native NVIDIA Direct Capture helper...")

	// 1. Parse CLI Configuration
	config, err := parseConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// 2. Validate Wayland Session
	wlSession, err := NewWaylandCaptureSession()
	if err != nil {
		log.Fatalf("Wayland session error: %v", err)
	}
	if err := wlSession.ValidateConnection(); err != nil {
		log.Fatalf("Wayland connection validation failed: %v", err)
	}
	log.Println("Wayland connection validated successfully.")

	// 3. Validate DMA-BUF / Render Node accessibility
	_, err = NewDMABufImporter(config.RenderNode)
	if err != nil {
		log.Fatalf("DMA-BUF import validation error: %v", err)
	}
	log.Printf("Using DMA-BUF render node: %s", config.RenderNode)

	// 4. Initialize NVENC Hardware Encoder
	encoder := NewNVENCEncoder(config)
	args, err := encoder.BuildCommand()
	if err != nil {
		log.Fatalf("Failed to build encoder command: %v", err)
	}

	log.Printf("Executing native encoder: %v", args)

	// 5. Start Hardware Encoding Pipeline
	dataChan, err := encoder.Start(args)
	if err != nil {
		log.Fatalf("Failed to start NVENC encoder: %v", err)
	}
	log.Println("Zero-copy hardware capture pipeline is active.")

	// 6. Monitor Signals for Clean Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received termination signal, shutting down gracefully...")
		encoder.Kill()
		os.Exit(0)
	}()

	// 7. Stream Bitstream to Standard Output
	output := NewOutputHandler()
	if err := output.Stream(dataChan); err != nil {
		log.Printf("Streaming output halted: %v", err)
		encoder.Kill()
		os.Exit(1)
	}

	log.Println("NVIDIA direct capture helper finished successfully.")
}
