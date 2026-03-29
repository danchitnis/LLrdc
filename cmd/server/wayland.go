package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func startDBus() error {
	// If already set, don't restart
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		return nil
	}
	out, err := exec.Command("dbus-launch", "--sh-syntax").Output()
	if err != nil {
		return fmt.Errorf("dbus-launch failed: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DBUS_SESSION_BUS_ADDRESS=") {
			parts := strings.Split(strings.TrimPrefix(line, "DBUS_SESSION_BUS_ADDRESS="), ";")
			addr := strings.Trim(parts[0], "'")
			os.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
			log.Printf("Global DBUS session started: %s", addr)
		}
	}
	return nil
}

func startWayland(displayNum string) error {
	log.Println("Starting Wayland session (labwc + XFCE 4.20 native)...")

	runDir := "/tmp/llrdc-run"
	_ = os.RemoveAll(runDir)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return fmt.Errorf("failed to create runDir: %v", err)
	}

	// 0. Ensure a global DBus session exists for the server process
	if err := startDBus(); err != nil {
		log.Printf("Warning: failed to start global DBus: %v", err)
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

	w, h := GetScreenSize()

	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/remote"
	}

	// 1. Setup .asoundrc for the remote user to bridge ALSA to PulseAudio
	asoundrc := `pcm.!default {
    type pulse
    fallback "sysdefault"
}
ctl.!default {
    type pulse
    fallback "sysdefault"
}
`
	_ = os.WriteFile(filepath.Join(home, ".asoundrc"), []byte(asoundrc), 0644)

	// Labwc config dir in a standard location
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
	outputs := fmt.Sprintf("HEADLESS-1 %dx%d\n", w, h)
	_ = os.WriteFile(filepath.Join(configDir, "outputs"), []byte(outputs), 0644)

	bgFile := Wallpaper
	if bgFile == "" {
		bgFile = "/usr/share/backgrounds/xfce/xfce-blue.jpg"
	}

	// Native labwc autostart script
	autostart := fmt.Sprintf(`#!/bin/sh
# Wait for the compositor to settle
sleep 1

# NOTE: randr and scaling are handled by Go server using native wlr-randr --scale

# Set Wayland native backend for GTK/XFCE
export GDK_BACKEND=wayland

# Enforce standard icon sizes; compositor scale handles high DPI natively
xfconf-query -c xfce4-desktop -p /desktop-icons/icon-size -n -t int -s 48 --create

# Launch XFCE Components
xfsettingsd &
xfce4-panel &
xfdesktop &

# Apply Theme & Background Properties after components start
(
  sleep 5
  
  # 2. Add PulseAudio plugin safely (Append to existing plugins)
  python3 -c '
import os, subprocess
def run(cmd):
    try: return subprocess.check_output(cmd, shell=True).decode().strip()
    except: return ""
try:
    ids_str = run("xfconf-query -c xfce4-panel -p /panels/panel-1/plugin-ids")
    ids = [i for i in ids_str.split("\n") if i.isdigit()] if ids_str else []
    if "20" not in ids:
        run("xfconf-query -c xfce4-panel -p /plugins/plugin-20 -n -t string -s pulseaudio --create")
        cmd = "xfconf-query -c xfce4-panel -p /panels/panel-1/plugin-ids " + " ".join(["-t int -s " + i for i in ids]) + " -t int -s 20"
        run(cmd)
        run("xfce4-panel -r")
except Exception as e: print(f"Error adding panel plugin: {e}")
' &

  xfconf-query -c xsettings -p /Net/IconThemeName -n -t string -s "elementary-Xfce-darker" --create
  xfconf-query -c xsettings -p /Net/ThemeName -n -t string -s "Greybird" --create
  
  for m in monitor0 monitorHEADLESS-1 HEADLESS-1 default; do
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/last-image -n -t string -s "%s" --create
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/image-style -n -t int -s 5 --create
    xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/color-style -n -t int -s 0 --create
  done
  
  xfconf-query -c xfce4-session -p /general/SaveOnExit -n -t bool -s false --create
  
  xfdesktop --reload
  
  # swaybg is more reliable for Wayland backgrounds on labwc
  swaybg -o HEADLESS-1 -i "%s" -m stretch &
) &
`, bgFile, bgFile)
	_ = os.WriteFile(filepath.Join(configDir, "autostart"), []byte(autostart), 0755)

	// Start labwc standalone (it will use the global DBUS session)
	cmd := exec.Command("labwc")
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
	time.Sleep(1 * time.Second) // Stabilize before RandR

	// Apply HDPI and resolution settings fully BEFORE returning
	env := append(os.Environ(), "XDG_RUNTIME_DIR="+runDir, "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0")
	applyHdpiSettings(env)

	// Start PulseAudio
	log.Println("Starting pulseaudio with null-sink for desktop audio capture...")
	paCmd := exec.Command("pulseaudio", "-D", "--exit-idle-time=-1")
	paCmd.Env = env
	if UseDebugX11 {
		paCmd.Stdout = os.Stdout
		paCmd.Stderr = os.Stderr
	}
	if err := paCmd.Run(); err == nil {
		// Wait for PA to be ready by polling pactl
		paReady := false
		for i := 0; i < 40; i++ {
			if err := exec.Command("pactl", "info").Run(); err == nil {
				paReady = true
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if paReady {
			// Create a virtual sink for "Desktop Audio"
			_ = runWithEnv("pactl", []string{"load-module", "module-null-sink", "sink_name=remote", "sink_properties=device.description=Desktop-Audio"}, env)
			_ = runWithEnv("pactl", []string{"set-default-sink", "remote"}, env)
			// Prevent the sink from suspending to keep audio stream active
			_ = runWithEnv("pactl", []string{"unload-module", "module-suspend-on-idle"}, env)
			log.Println("PulseAudio virtual sink 'remote' initialized.")
		} else {
			log.Printf("Warning: PulseAudio daemon started but pactl timed out.")
		}
	} else {
		log.Printf("Warning: pulseaudio failed to start: %v", err)
	}

	// Start native wayland input helper
	startWaylandInputHelper()

	// Wait a moment for UI components to stabilize
	time.Sleep(2 * time.Second)

	return nil
}
