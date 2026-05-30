package linux

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

func outputWithEnv(name string, args []string, env []string) (string, error) {
	var out []byte
	var err error
	for i := 0; i < 20; i++ {
		cmd := exec.Command(name, args...)
		cmd.Env = env
		out, err = cmd.CombinedOutput()
		if err == nil {
			return string(out), nil
		}
		// If it's a connection error or no outputs, retry.
		outStr := string(out)
		if strings.Contains(outStr, "failed to connect to display") || strings.Contains(outStr, "no outputs available") {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return outStr, err
	}
	return string(out), err
}

func runWithEnv(name string, args []string, env []string) error {
	_, err := outputWithEnv(name, args, env)
	return err
}

func applyHdpiSettings(env []string) {
	if HDPI <= 0 {
		return
	}

	dpi := int(float64(HDPI) * 96.0 / 100.0)
	log.Printf("Applying HDPI desktop scaling: %d%% (DPI: %d)", HDPI, dpi)

	waylandScale := 1.0
	if HDPI > 100 {
		waylandScale = float64(HDPI) / 100.0
	}

	waylandEnv := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0")

	// 1. Set XFCE/GTK scaling properties via xfconf
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

	// Standard mode string (without FPS)
	modeStr := fmt.Sprintf("%dx%d", width, height)
	// Custom mode string (with FPS)
	customModeStr := fmt.Sprintf("%dx%d@%d", width, height, FPS)
	scaleStr := fmt.Sprintf("%.6f", waylandScale)

	log.Printf("Resizing Wayland display (HEADLESS-1) to %dx%d @ %d FPS with scale %s", width, height, FPS, scaleStr)
	env := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run", "WAYLAND_DISPLAY=wayland-0")

	// Try standard --mode first.
	args := []string{"--output", "HEADLESS-1", "--mode", modeStr, "--scale", scaleStr}
	if err := runWithEnv("wlr-randr", args, env); err != nil {
		log.Printf("Warning: wlr-randr --mode %s failed: %v. Trying --custom-mode %s.", modeStr, err, customModeStr)

		// Fallback to --custom-mode
		args = []string{"--output", "HEADLESS-1", "--custom-mode", customModeStr, "--scale", scaleStr}
		if err := runWithEnv("wlr-randr", args, env); err != nil {
			log.Printf("Error: wlr-randr --custom-mode also failed: %v. Output might be unstable.", err)
			return err
		}
	}

	// Give the compositor a moment to process the mode change
	time.Sleep(200 * time.Millisecond)
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
