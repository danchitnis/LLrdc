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

	// Labwc config dir in a standard location
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/remote"
	}
	configDir := filepath.Join(home, ".config", "labwc")
	_ = os.MkdirAll(configDir, 0755)

	rc := `<labwc_config>
  <core>
    <decoration>none</decoration>
  </core>
  <keyboard>
    <default />
  </keyboard>
  <mouse>
    <default />
    <!-- Disable the default root menu on right click -->
    <action name="ShowMenu" menu="root-menu" button="Right" context="Root" clear="true" />
  </mouse>
</labwc_config>`
	_ = os.WriteFile(filepath.Join(configDir, "rc.xml"), []byte(rc), 0644)

	// Outputs file to map the headless output name
	outputs := "HEADLESS-1 1920x1080\n"
	_ = os.WriteFile(filepath.Join(configDir, "outputs"), []byte(outputs), 0644)

	bgFile := Wallpaper
	if bgFile == "" {
		bgFile = "/usr/share/backgrounds/xfce/xfce-blue.jpg"
	}

	// Native labwc autostart script
	autostart := fmt.Sprintf(`#!/bin/sh
# Wait for the compositor to settle
sleep 1

# Force headless output resolution
wlr-randr --output HEADLESS-1 --mode 1920x1080

# Set Wayland native backend for GTK/XFCE
export GDK_BACKEND=wayland

# Launch XFCE Components
xfsettingsd &
xfce4-panel &
xfdesktop &

# Apply Theme & Background Properties after components start
(
  sleep 3
  xfconf-query -c xsettings -p /Net/IconThemeName -n -t string -s "elementary-Xfce-darker" --create
  xfconf-query -c xsettings -p /Gdk/WindowScalingFactor -n -t int -s 1 --create
  xfconf-query -c xsettings -p /Net/ThemeName -n -t string -s "Greybird" --create
  
  for m in monitor0 monitorHEADLESS-1 HEADLESS-1 default; do
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/last-image -n -t string -s "%s" --create
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/image-style -n -t int -s 5 --create
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/color-style -n -t int -s 0 --create
  done
  
  xfconf-query -c xfce4-session -p /general/SaveOnExit -n -t bool -s false --create
  xfdesktop --reload
  
  # swaybg is more reliable for Wayland backgrounds on labwc
  # It will draw below xfdesktop if xfdesktop decides to work, or visible if xfdesktop is transparent
  swaybg -o HEADLESS-1 -i "%s" -m stretch &
) &
`, bgFile, bgFile)
	_ = os.WriteFile(filepath.Join(configDir, "autostart"), []byte(autostart), 0755)

	// Set global screen size
	initScreenSize(1920, 1080)

	// Start labwc inside dbus-run-session
	cmd := exec.Command("dbus-run-session", "sh", "-c", "labwc")
	cmd.Env = append(os.Environ(), 
		"XDG_RUNTIME_DIR="+runDir,
		"WLR_BACKENDS=headless",
		"WLR_HEADLESS_OUTPUTS=1",
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
	time.Sleep(5 * time.Second)

	return nil
}
