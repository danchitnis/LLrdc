package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func startWayland(displayNum string) error {
	log.Println("Starting Wayland session (labwc)...")

	runDir := "/tmp/llrdc-run"
	_ = os.RemoveAll(runDir)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return fmt.Errorf("failed to create runDir: %v", err)
	}

	// Set Environment for Wayland and Xwayland
	os.Setenv("XDG_RUNTIME_DIR", runDir)
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	os.Setenv("DISPLAY", ":0")
	os.Setenv("WLR_NO_HARDWARE_CURSORS", "1")
	os.Setenv("WLR_RENDERER", "pixman")
	os.Setenv("WLR_BACKENDS", "headless")

	// Labwc config dir in the remote user's home
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/remote"
	}
	configDir := filepath.Join(home, ".config", "labwc")
	_ = os.MkdirAll(configDir, 0755)

	rc := `<?xml version="1.0"?>
<labwc_config>
  <core>
    <decoration>none</decoration>
  </core>
  <mouse>
    <showCursor>true</showCursor>
    <acceleration>flat</acceleration>
    <speed>0</speed>
  </mouse>
</labwc_config>`
	_ = os.WriteFile(filepath.Join(configDir, "rc.xml"), []byte(rc), 0644)

	// Outputs for headless
	outputs := "HEADLESS-1 1280x720\n"
	_ = os.WriteFile(filepath.Join(configDir, "outputs"), []byte(outputs), 0644)

	// Set global screen size to match
	initScreenSize(1280, 720)

	// Start labwc
	cmd := exec.Command("labwc")
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start labwc: %v", err)
	}

	cleanupTasks = append(cleanupTasks, func() {
		log.Println("Killing labwc...")
		_ = cmd.Process.Kill()
	})

	// Wait for Wayland socket to appear
	socketPath := filepath.Join(runDir, "wayland-0")
	socketReady := false
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			socketReady = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !socketReady {
		return fmt.Errorf("timeout waiting for Wayland socket at %s", socketPath)
	}

	log.Println("Wayland socket is ready.")

	// Start native wayland input helper
	startWaylandInputHelper()

	// Create a dark green gradient background using ffmpeg
	bgPath := "/tmp/bg.png"
	_ = exec.Command("ffmpeg", "-f", "lavfi", "-i", "color=s=1280x720:c=0x001100", "-vf", "vignette=PI/4", "-frames:v", "1", bgPath).Run()

	// Start background
	bg := exec.Command("swaybg", "-i", bgPath, "-m", "fill")
	bg.Env = os.Environ()
	_ = bg.Start()

	// Launch a terminal so there's something to interact with
	term := exec.Command("xfce4-terminal")
	term.Env = os.Environ()
	_ = term.Start()

	// Damage loop is removed as it causes cursor flicker/drift and isn't needed with active background

	// Wait a moment for Xwayland and UI to initialize
	time.Sleep(3 * time.Second)

	return nil
}
