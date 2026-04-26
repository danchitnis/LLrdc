package server

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

func runWithEnv(name string, args []string, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Command %s %v failed: %v\nOutput: %s", name, args, err, string(out))
		return err
	}
	return nil
}

func outputWithEnv(name string, args []string, env []string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Command %s %v failed: %v\nOutput: %s", name, args, err, string(out))
		return "", err
	}
	return string(out), nil
}

func applyHdpiSettings(env []string) {
	if HDPI <= 0 {
		return
	}

	dpi := int(float64(HDPI) * 96.0 / 100.0)
	log.Printf("Applying HDPI scaling: %d%% (DPI: %d)", HDPI, dpi)

	waylandScale := 1.0
	if HDPI > 100 {
		waylandScale = float64(HDPI) / 100.0
	}

	waylandEnv := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0")

	// 1. Set Wayland compositor scale
	log.Printf("Applying Wayland compositor scale: %f", waylandScale)
	if err := runWithEnv("wlr-randr", []string{"--output", "HEADLESS-1", "--scale", fmt.Sprintf("%f", waylandScale)}, waylandEnv); err != nil {
		log.Printf("Warning: wlr-randr --scale failed: %v", err)
	}

	// 2. Set XFCE/GTK scaling properties via xfconf
	// Set GDK scale (integer)
	gdkScale := int(waylandScale)
	if gdkScale < 1 {
		gdkScale = 1
	}
	_ = runWithEnv("xfconf-query", []string{"-c", "xsettings", "-p", "/Gdk/WindowScalingFactor", "-n", "-t", "int", "-s", fmt.Sprintf("%d", gdkScale), "--create"}, waylandEnv)

	// Set Xft DPI (fractional)
	_ = runWithEnv("xfconf-query", []string{"-c", "xsettings", "-p", "/Xft/DPI", "-n", "-t", "int", "-s", fmt.Sprintf("%d", dpi), "--create"}, waylandEnv)
}

func resizeDisplay(width, height int) error {
	waylandScale := 1.0
	if HDPI > 100 {
		waylandScale = float64(HDPI) / 100.0
	}

	modeStr := fmt.Sprintf("%dx%d", width, height)
	scaleStr := fmt.Sprintf("%.6f", waylandScale)
	log.Printf("Resizing Wayland display (HEADLESS-1) to %s with scale %s", modeStr, scaleStr)
	env := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0")

	args := []string{"--output", "HEADLESS-1", "--mode", modeStr, "--scale", scaleStr}
	if err := runWithEnv("wlr-randr", args, env); err != nil {
		log.Printf("Warning: wlr-randr --mode failed: %v. Trying --custom-mode.", err)
		args = []string{"--output", "HEADLESS-1", "--custom-mode", modeStr + "@60", "--scale", scaleStr}
		if err := runWithEnv("wlr-randr", args, env); err != nil {
			log.Printf("Error: wlr-randr --custom-mode also failed: %v", err)
			return err
		}
	}

	time.Sleep(100 * time.Millisecond)
	return nil
}

func waitForDisplayState(width, height int, timeout time.Duration) error {
	scale := 1.0
	if HDPI > 100 {
		scale = float64(HDPI) / 100.0
	}

	expectedMode := fmt.Sprintf("%dx%d", width, height)
	expectedScale := fmt.Sprintf("Scale: %.6f", scale)
	env := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0")

	return waitForPredicate("Wayland display state", timeout, 100*time.Millisecond, func() (bool, error) {
		out, err := outputWithEnv("wlr-randr", []string{"--output", "HEADLESS-1"}, env)
		if err != nil {
			return false, nil
		}
		return strings.Contains(out, expectedMode) && strings.Contains(out, expectedScale), nil
	})
}
