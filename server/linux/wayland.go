package linux

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func configureWaylandRuntime(runDir string) (string, error) {
	os.Setenv("XDG_RUNTIME_DIR", runDir)
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	os.Setenv("DISPLAY", ":99")
	os.Setenv("WLR_NO_HARDWARE_CURSORS", "1")
	os.Setenv("WLR_BACKENDS", "headless")

	renderNode := ""
	if CaptureMode == CaptureModeDirect {
		var err error
		renderNode, err = detectRenderNode()
		if err != nil {
			markDirectBufferProbeResult("", false, err.Error(), directBufferProbeResult{})
			return "", err
		}
		if UseNVIDIA {
			os.Setenv("__EGL_VENDOR_LIBRARY_FILENAMES", "/usr/share/glvnd/egl_vendor.d/10_nvidia.json")
			os.Setenv("GBM_BACKEND", "nvidia-drm")
			os.Setenv("__GL_GBM_ALWAYS_USE_MODIFIERS", "1")
			os.Setenv("WLR_RENDERER", "gles2")
			os.Setenv("WLR_DRM_DEVICES", renderNode)
		} else {
			os.Unsetenv("WLR_RENDERER")
		}
		os.Setenv("WLR_RENDER_DRM_DEVICE", renderNode)
		log.Printf("Direct capture mode requested; using render node %s", renderNode)
	} else if currentAcceleratorMode() == acceleratorIntel {
		renderNode = resolveIntelRenderNode()
		os.Unsetenv("WLR_RENDERER")
		os.Setenv("WLR_RENDER_DRM_DEVICE", renderNode)
		markDirectBufferProbeResult("", false, "Direct buffer disabled in compat mode", directBufferProbeResult{})
		log.Printf("Intel compat mode requested; using render node %s", renderNode)
	} else if currentAcceleratorMode() == acceleratorNVIDIA {
		var err error
		renderNode, err = detectRenderNode()
		if err == nil {
			os.Setenv("__EGL_VENDOR_LIBRARY_FILENAMES", "/usr/share/glvnd/egl_vendor.d/10_nvidia.json")
			os.Setenv("GBM_BACKEND", "nvidia-drm")
			os.Setenv("__GL_GBM_ALWAYS_USE_MODIFIERS", "1")
			os.Setenv("WLR_RENDERER", "gles2")
			os.Setenv("WLR_RENDER_DRM_DEVICE", renderNode)
			os.Setenv("WLR_DRM_DEVICES", renderNode)
			log.Printf("NVIDIA compat mode; using render node %s for hardware-accelerated rendering", renderNode)
		} else {
			log.Printf("NVIDIA compat mode; fallback to software rendering (pixman): %v", err)
			os.Setenv("WLR_RENDERER", "pixman")
			os.Unsetenv("WLR_RENDER_DRM_DEVICE")
		}
		markDirectBufferProbeResult("", false, "Direct buffer disabled in compat mode", directBufferProbeResult{})
	} else {
		os.Setenv("WLR_RENDERER", "pixman")
		os.Unsetenv("WLR_RENDER_DRM_DEVICE")
		markDirectBufferProbeResult("", false, "Direct buffer disabled in compat mode", directBufferProbeResult{})
	}

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

	return renderNode, nil
}

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

func startWayland() error {
	log.Println("Starting Wayland session (labwc + XFCE 4.20 native)...")

	runDir := "/tmp/llrdc-run"
	_ = os.RemoveAll(runDir)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return fmt.Errorf("failed to create runDir: %v", err)
	}
	_ = os.Remove(desktopReadyMarker)

	// 0. Ensure a global DBus session exists for the server process
	if err := startDBus(); err != nil {
		log.Printf("Warning: failed to start global DBus: %v", err)
	}

	renderNode, err := configureWaylandRuntime(runDir)
	if err != nil {
		return fmt.Errorf("failed to configure Wayland runtime: %w", err)
	}

	w, h := GetScreenSize()

	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/remote"
	}

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
	outputs := fmt.Sprintf("HEADLESS-1 %dx%d@%d\n", w, h, FPS)
	_ = os.WriteFile(filepath.Join(configDir, "outputs"), []byte(outputs), 0644)

	bgFile := Wallpaper
	if bgFile == "" {
		bgFile = "/usr/share/backgrounds/xfce/xfce-x.svg"
	}

	scale := 1.0
	if HDPI > 100 {
		scale = float64(HDPI) / 100.0
	}

	gdkScale := int(scale)
	if gdkScale < 1 {
		gdkScale = 1
	}

	minimal := os.Getenv("LLRDC_MINIMAL_WAYLAND") == "1"
	xfceAutostart := ""
	if !minimal {
		xfceAutostart = fmt.Sprintf(`
# Initialize XFCE configuration
xfconf-query -c xsettings -p /Net/IconThemeName -n -t string -s "elementary-xfce-dark" --create || true
xfconf-query -c xsettings -p /Net/ThemeName -n -t string -s "Greybird" --create || true
xfconf-query -c xsettings -p /Gdk/WindowScalingFactor -n -t int -s %d --create || true

for m in monitor0 monitorHEADLESS-1 HEADLESS-1 default; do
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/last-image -n -t string -s "%s" --create || true
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/image-style -n -t int -s 5 --create || true
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/color-style -n -t int -s 0 --create || true
done

xfconf-query -c xfce4-session -p /general/SaveOnExit -n -t bool -s false --create || true

# Launch XFCE Components
xfsettingsd &
xfce4-panel &
xfdesktop &

wait_for_cmd 100 pgrep -x xfsettingsd
wait_for_cmd 100 pgrep -x xfce4-panel
wait_for_cmd 100 pgrep -x xfdesktop

xfdesktop --reload

# swaybg is more reliable for Wayland backgrounds on labwc
swaybg -o HEADLESS-1 -i "%s" -m stretch &
`, gdkScale, bgFile, bgFile)
	}

	autostart := fmt.Sprintf(`#!/bin/sh
set -eu

READY_FILE="%s"
rm -f "$READY_FILE"

wait_for_cmd() {
  attempts="$1"
  shift
  i=0
  while ! "$@" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -ge "$attempts" ]; then
      return 1
    fi
    sleep 0.2
  done
}

# Set Wayland native backend for GTK/XFCE
export GDK_BACKEND=wayland

%s

touch "$READY_FILE"
`, desktopReadyMarker, xfceAutostart)
	_ = os.WriteFile(filepath.Join(configDir, "autostart"), []byte(autostart), 0755)

	// Start labwc standalone (it will use the global DBUS session)
	cmd := exec.Command("labwc")
	env := append(os.Environ(),
		"XDG_RUNTIME_DIR="+runDir,
		"WLR_BACKENDS=headless",
		"WLR_HEADLESS_OUTPUTS=1",
		"DISPLAY=:99",
	)
	if !UseNVIDIA {
		env = append(env,
			"WLR_DRM_NO_MODIFIERS=1",
			"WLR_NO_MODIFIERS=1",
		)
	}
	cmd.Env = env
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
	if err := waitForFile(socketPath, 10*time.Second, 100*time.Millisecond); err != nil {
		return fmt.Errorf("timeout waiting for Wayland socket at %s: %w", socketPath, err)
	}
	readiness.Set(readinessWaylandSocket, true)

	log.Println("Wayland socket is ready.")

	if CaptureMode == CaptureModeDirect {
		waylandEnv := append(os.Environ(), "XDG_RUNTIME_DIR="+runDir, "WAYLAND_DISPLAY=wayland-0")
		probeResult, probeErr := runDirectBufferProbe(waylandEnv)
		if probeErr != nil {
			markDirectBufferProbeResult(renderNode, false, fmt.Sprintf("direct-buffer probe failed: %v", probeErr), directBufferProbeResult{})
			return fmt.Errorf("direct-buffer probe failed: %w", probeErr)
		}
		if !probeResult.ScreencopyAvailable || !probeResult.LinuxDMABUFAvailable {
			reason := "Wayland compositor does not advertise both screencopy and linux-dmabuf"
			markDirectBufferProbeResult(renderNode, false, reason, probeResult)
			return fmt.Errorf("%s", reason)
		}
		markDirectBufferProbeResult(renderNode, true, "Direct-buffer probe passed; waiting for hardware capture", probeResult)
		log.Printf("Direct-buffer probe passed (render node: %s, renderer: %s)", renderNode, compatibleRendererName())
	}

	// Start native wayland input helper
	startWaylandInputHelper()
	if err := waitForPredicate("Wayland input helper readiness", 10*time.Second, 100*time.Millisecond, func() (bool, error) {
		return readiness.Snapshot()[readinessInputHelper], nil
	}); err != nil {
		return err
	}

	waylandEnv := append(os.Environ(), "XDG_RUNTIME_DIR="+runDir, "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:99")

	// Set initial resolution and apply HDPI
	displayChangeMu.Lock()
	w, h = GetScreenSize()
	log.Printf("Setting initial Wayland resolution to %dx%d", w, h)

	_ = resizeDisplay(w, h)
	applyHdpiSettings(waylandEnv)
	displayChangeMu.Unlock()

	if err := waitForDisplayState(w, h, 10*time.Second); err != nil {
		return err
	}

	// XFCE Desktop session components (panels, etc.) are allowed to boot in the background.
	// We don't block video capture on them being fully ready.
	readiness.Set(readinessDesktopSession, true)
	log.Println("Wayland and Input ready. Desktop session booting in background.")
	PrimeFrameGeneration(0, 10, 100*time.Millisecond)

	return nil
}
