package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func getSessionDbusAddress() string {
	out, err := exec.Command("pgrep", "-x", "xfconfd").Output()
	if err != nil {
		return ""
	}
	pids := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(pids) == 0 || pids[0] == "" {
		return ""
	}
	pid := pids[0]
	environBytes, err := os.ReadFile(fmt.Sprintf("/proc/%s/environ", pid))
	if err != nil {
		return ""
	}
	envVars := strings.Split(string(environBytes), "\x00")
	for _, e := range envVars {
		if strings.HasPrefix(e, "DBUS_SESSION_BUS_ADDRESS=") {
			return strings.TrimPrefix(e, "DBUS_SESSION_BUS_ADDRESS=")
		}
	}
	return ""
}

func startX11(displayNum string) error {
	display := ":" + displayNum
	log.Printf("Starting Xvfb on %s...", display)

	// Clean up stale locks
	lockFile := fmt.Sprintf("/tmp/.X%s-lock", displayNum)
	os.Remove(lockFile)
	socketPath := fmt.Sprintf("/tmp/.X11-unix/X%s", displayNum)
	os.Remove(socketPath)

	// Start Xvfb
	xvfb := exec.Command("Xvfb", display, "-screen", "0", "3840x2160x24", "-nolisten", "tcp", "-ac", "+extension", "RANDR", "+extension", "XFIXES")
	if UseDebugX11 {
		xvfb.Stdout = os.Stdout
		xvfb.Stderr = os.Stderr
	}
	if err := xvfb.Start(); err != nil {
		return fmt.Errorf("failed to start Xvfb: %v", err)
	}

	cleanupTasks = append(cleanupTasks, func() {
		log.Println("Killing Xvfb...")
		xvfb.Process.Kill()
	})

	if err := waitForXServer(socketPath, 10*time.Second); err != nil {
		return err
	}
	log.Println("Xvfb is ready.")

	// Configure X11
	env := append(os.Environ(), "DISPLAY="+display)
	runWithEnv("xset", []string{"s", "off"}, env)
	runWithEnv("xset", []string{"-dpms"}, env)
	runWithEnv("xset", []string{"s", "noblank"}, env)

	// In tests, we sometimes want a *truly static* screen so the encoder can drop
	// identical frames. XFCE introduces periodic repaints (clock/panel/etc) which
	// can prevent the stream from ever going idle.
	if TestMinimalX11 {
		log.Println("TEST_MINIMAL_X11 mode: skipping xfce4-session.")
		// Best-effort: set a solid root background if xsetroot exists.
		_ = runWithEnv("xsetroot", []string{"-solid", "#000000"}, env)
		return nil
	}

	// Start PulseAudio
	log.Println("Starting pulseaudio...")
	paCmd := exec.Command("pulseaudio", "-D", "--exit-idle-time=-1")
	paCmd.Env = env
	if UseDebugX11 {
		paCmd.Stdout = os.Stdout
		paCmd.Stderr = os.Stderr
	}
	if err := paCmd.Run(); err != nil {
		log.Printf("Warning: pulseaudio failed to start: %v", err)
	}

	// Start XFCE
	log.Println("Starting xfce4-session...")
	session := exec.Command("dbus-run-session", "xfce4-session")
	session.Env = env
	if UseDebugX11 {
		session.Stdout = os.Stdout
		session.Stderr = os.Stderr
	}
	if err := session.Start(); err != nil {
		return fmt.Errorf("failed to start xfce4-session: %v", err)
	}

	cleanupTasks = append(cleanupTasks, func() {
		log.Println("Killing xfce4-session...")
		session.Process.Kill()
	})

	time.Sleep(3 * time.Second)

	// Post configure
	runWithEnv("xset", []string{"s", "off"}, env)
	runWithEnv("xset", []string{"-dpms"}, env)
	runWithEnv("xset", []string{"s", "noblank"}, env)
	runWithEnv("xfconf-query", []string{"-c", "xfwm4", "-p", "/general/use_compositing", "-s", "false"}, env)

	// Set wallpaper
	setWallpaper(env, displayNum)

	// Apply HDPI settings if enabled
	applyHdpiSettings(env)

	return nil
}

func resizeDisplay(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid resize: %dx%d", width, height)
	}
	mode := fmt.Sprintf("%dx%d", width, height)

	if UseWayland {
		log.Printf("Resizing Wayland display (HEADLESS-1) to %s", mode)
		// Use wlr-randr for Wayland resizing
		env := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0")
		if err := runWithEnv("wlr-randr", []string{"--output", "HEADLESS-1", "--custom-mode", fmt.Sprintf("%s@60", mode)}, env); err != nil {
			return fmt.Errorf("wlr-randr failed: %v", err)
		}
		return nil
	}

	log.Printf("Resizing X11 display to %s", mode)
	env := append(os.Environ(), "DISPLAY="+Display)

	// Try multiple ways to resize
	// 1. try xrandr -s
	if err := runWithEnv("xrandr", []string{"-s", mode}, env); err == nil {
		return nil
	}

	// 2. try xrandr --fb
	if err := runWithEnv("xrandr", []string{"--fb", mode}, env); err != nil {
		log.Printf("xrandr --fb failed: %v", err)
	}

	return nil
}

func setWallpaper(baseEnv []string, displayNum string) {
	dbusAddr := getSessionDbusAddress()
	if dbusAddr == "" {
		log.Println("Warning: Could not find DBUS session bus address; wallpaper not set.")
		return
	}

	env := append(baseEnv, "DBUS_SESSION_BUS_ADDRESS="+dbusAddr)
	wallpaper := Wallpaper
	if wallpaper == "" {
		wallpaper = "/usr/share/backgrounds/xfce/xfce-shapes.svg"
	}

	out, _ := exec.Command("xfconf-query", "-c", "xfce4-desktop", "-l").Output()
	allProps := strings.Split(string(out), "\n")
	var imageProps []string
	for _, p := range allProps {
		p = strings.TrimSpace(p)
		if strings.HasSuffix(p, "/last-image") {
			imageProps = append(imageProps, p)
		}
	}

	for _, prop := range imageProps {
		runWithEnv("xfconf-query", []string{"-c", "xfce4-desktop", "-p", prop, "-s", wallpaper}, env)
		styleProp := strings.TrimSuffix(prop, "/last-image") + "/image-style"
		runWithEnv("xfconf-query", []string{"-c", "xfce4-desktop", "-p", styleProp, "-s", "5"}, env)
	}

	if len(imageProps) > 0 {
		cmd := exec.Command("xfdesktop", "--reload")
		cmd.Env = env
		cmd.Run()
		log.Printf("Wallpaper set to: %s [bus: %s]", wallpaper, dbusAddr)
	}
}

func waitForXServer(socketPath string, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timed out waiting for X server")
}

func runWithEnv(cmd string, args []string, env []string) error {
	c := exec.Command(cmd, args...)
	c.Env = env
	out, err := c.CombinedOutput()
	if err != nil {
		log.Printf("Command %s failed: %v, Output: %s", cmd, err, string(out))
		return fmt.Errorf("%s failed: %v", cmd, err)
	}
	return nil
}

func applyHdpiSettings(baseEnv []string) {
	if HDPI <= 0 {
		return
	}

	dbusAddr := getSessionDbusAddress()
	if dbusAddr == "" {
		log.Println("Warning: Could not find DBUS session bus address; HDPI settings not applied.")
		return
	}

	env := append(baseEnv, "DBUS_SESSION_BUS_ADDRESS="+dbusAddr)

	dpi := (96 * HDPI) / 100
	log.Printf("Applying HDPI scaling: %d%% (DPI: %d)", HDPI, dpi)

	// Set Xft DPI
	runWithEnv("xfconf-query", []string{"-c", "xsettings", "-p", "/Xft/DPI", "-n", "-t", "int", "-s", strconv.Itoa(dpi)}, env)

	// Set GDK Window Scaling Factor
	scale := 1
	if HDPI >= 200 {
		scale = HDPI / 100
	}
	runWithEnv("xfconf-query", []string{"-c", "xsettings", "-p", "/Gdk/WindowScalingFactor", "-n", "-t", "int", "-s", strconv.Itoa(scale)}, env)

	// Set Icon Size on Desktop
	iconSize := 48 * HDPI / 100
	runWithEnv("xfconf-query", []string{"-c", "xfce4-desktop", "-p", "/desktop-icons/icon-size", "-n", "-t", "int", "-s", strconv.Itoa(iconSize)}, env)

	// Set Cursor Size
	cursorSize := 24 * HDPI / 100
	runWithEnv("xfconf-query", []string{"-c", "xsettings", "-p", "/Gtk/CursorThemeSize", "-n", "-t", "int", "-s", strconv.Itoa(cursorSize)}, env)

	// Set Panel Size
	panelSize := 30 * HDPI / 100
	runWithEnv("xfconf-query", []string{"-c", "xfce4-panel", "-p", "/panels/panel-1/size", "-n", "-t", "int", "-s", strconv.Itoa(panelSize)}, env)
	
	// Restart panel to apply size changes effectively
	runWithEnv("xfce4-panel", []string{"-r"}, env)
}

