package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type WaylandCaptureSession struct {
	SocketPath string
}

func NewWaylandCaptureSession() (*WaylandCaptureSession, error) {
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = "/tmp/llrdc-run"
	}
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		display = "wayland-0"
	}

	socketPath := filepath.Join(runDir, display)

	// Validate Wayland socket exists
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("wayland socket not found at %s: %w", socketPath, err)
	}

	return &WaylandCaptureSession{
		SocketPath: socketPath,
	}, nil
}

func (s *WaylandCaptureSession) ValidateConnection() error {
	// Try to dial the Wayland Unix socket to ensure it's responsive
	conn, err := net.DialTimeout("unix", s.SocketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to Wayland Unix socket: %w", err)
	}
	_ = conn.Close()
	return nil
}
