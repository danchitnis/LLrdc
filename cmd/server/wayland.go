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

	// Labwc config dir in a reliable location
	configDir := "/tmp/labwc"
	_ = os.RemoveAll(configDir)
	_ = os.MkdirAll(configDir, 0755)
	os.Setenv("XDG_CONFIG_HOME", "/tmp") // labwc looks for XDG_CONFIG_HOME/labwc

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
	autostart := fmt.Sprintf(`#!/bin/sh
(
  set -x
  sleep 5
  
  # Force resolution for headless output
  XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0 wlr-randr --output HEADLESS-1 --mode 1920x1080
  
  sleep 2
  
  # Start background and panel
  xfsettingsd &
  xfce4-panel &
  xfdesktop &
  
  sleep 3
  
  # Set properties for XFCE components
  BG_FILE="%s"
  if [ -z "$BG_FILE" ]; then
    BG_FILE="/usr/share/backgrounds/xfce/xfce-blue.jpg"
  fi
  
  for m in monitor0 monitorHEADLESS-1 monitor1 monitorHDMI-A-1 default; do
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/last-image -n -t string -s "$BG_FILE" --create
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/image-style -n -t int -s 5 --create
  done
  
  # Set Icon Theme (Elementary is very high quality for XFCE)
  xfconf-query -c xsettings -p /Net/IconThemeName -n -t string -s "elementary-Xfce-darker" --create
  xfconf-query -c xsettings -p /Gdk/WindowScalingFactor -n -t int -s 1 --create
  
  # Set Theme
  xfconf-query -c xsettings -p /Net/ThemeName -n -t string -s "Greybird" --create
  
  # Disable session management warnings
  xfconf-query -c xfce4-session -p /general/SaveOnExit -n -t bool -s false --create

  # Trigger background update
  xfdesktop --next
) &
`, Wallpaper)
	_ = os.WriteFile(filepath.Join(configDir, "autostart"), []byte(autostart), 0755)

	// Set global screen size
	initScreenSize(1920, 1080)

	// Start labwc inside dbus-run-session
	cmd := exec.Command("dbus-run-session", "labwc", "-c", configDir)
	cmd.Env = append(os.Environ(), 
		"XDG_RUNTIME_DIR="+runDir,
		"WLR_BACKENDS=headless",
		"WLR_HEADLESS_OUTPUTS=1920x1080",
	)
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
