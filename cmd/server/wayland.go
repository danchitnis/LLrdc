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
	log.Println("Starting Wayland session (labwc + XFCE 4.20 native)...")

	runDir := "/tmp/llrdc-run"
	_ = os.RemoveAll(runDir)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return fmt.Errorf("failed to create runDir: %v", err)
	}

	// Set Environment for Wayland and Native GDK
	os.Setenv("XDG_RUNTIME_DIR", runDir)
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	os.Setenv("DISPLAY", ":0")
	os.Setenv("WLR_NO_HARDWARE_CURSORS", "1")
	os.Setenv("WLR_RENDERER", "pixman")
	os.Setenv("WLR_BACKENDS", "headless")
	
	// Force Native Wayland for GDK/GTK applications (XFCE 4.20)
	os.Setenv("GDK_BACKEND", "wayland")
	os.Setenv("QT_QPA_PLATFORM", "wayland")
	
	// Reduce warnings and improve theming
	os.Setenv("NO_AT_BRIDGE", "1")
	os.Setenv("XDG_MENU_PREFIX", "xfce-")
	os.Setenv("XDG_CURRENT_DESKTOP", "XFCE")
	
	// Ensure data dirs are set for icons/themes
	if os.Getenv("XDG_DATA_DIRS") == "" {
		os.Setenv("XDG_DATA_DIRS", "/usr/local/share:/usr/share")
	}
	if os.Getenv("XDG_CONFIG_DIRS") == "" {
		os.Setenv("XDG_CONFIG_DIRS", "/etc/xdg")
	}

	// Labwc config dir
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

	// Autostart script for XFCE components (Native Wayland)
	// We use xfconf-query to force high-res icons and standard background
	autostart := `#!/bin/sh
(
  sleep 4
  
  # Ensure xfconfd is running
  /usr/lib/x86_64-linux-gnu/xfce4/xfconf/xfconfd &
  sleep 1

  # Set Icon Theme (Elementary is very high quality for XFCE)
  xfconf-query -c xsettings -p /Net/IconThemeName -s "elementary-Xfce-darker" --create
  xfconf-query -c xsettings -p /Gdk/WindowScalingFactor -n -t int -s 1 --create
  
  # Set Theme
  xfconf-query -c xsettings -p /Net/ThemeName -s "Greybird" --create
  
  # Set Background (The XFCE Mouse image)
  # We try multiple property paths to ensure it sticks across versions
  BG_FILE="/usr/share/backgrounds/xfce/xfce-blue.jpg"
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitorHEADLESS-1/workspace0/last-image -s "$BG_FILE" --create
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitor0/workspace0/last-image -s "$BG_FILE" --create
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitorHEADLESS-1/workspace0/image-style -n -t int -s 5 --create
  
  # Disable session management warnings
  xfconf-query -c xfce4-session -p /general/SaveOnExit -n -t bool -s false --create
  
  # Ensure components use Wayland where possible
  export GDK_BACKEND=wayland
  
  xfsettingsd &
  xfce4-panel &
  xfdesktop &
  xfce4-terminal &
) &
`
	_ = os.WriteFile(filepath.Join(configDir, "autostart"), []byte(autostart), 0755)

	// Outputs for headless
	outputs := "HEADLESS-1 1920x1080\n"
	_ = os.WriteFile(filepath.Join(configDir, "outputs"), []byte(outputs), 0644)

	// Set global screen size
	initScreenSize(1920, 1080)

	// Start labwc inside dbus-run-session
	cmd := exec.Command("dbus-run-session", "labwc")
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start labwc: %v", err)
	}

	cleanupTasks = append(cleanupTasks, func() {
		log.Println("Killing labwc session...")
		_ = cmd.Process.Kill()
	})

	// Wait for Wayland socket to appear
	socketPath := filepath.Join(runDir, "wayland-0")
	socketReady := false
	for i := 0; i < 100; i++ {
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

	// Wait a moment for UI components to stabilize
	time.Sleep(15 * time.Second)

	return nil
}
