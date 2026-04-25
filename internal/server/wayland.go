package server

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
		os.Unsetenv("WLR_RENDERER")
		os.Setenv("WLR_RENDER_DRM_DEVICE", renderNode)
		log.Printf("Direct capture mode requested; using render node %s", renderNode)
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
# Launch XFCE Components
xfsettingsd &
xfce4-panel &
xfdesktop &

wait_for_cmd 100 pgrep -x xfsettingsd
wait_for_cmd 100 pgrep -x xfce4-panel
wait_for_cmd 100 pgrep -x xfdesktop

# Ensure the PulseAudio panel plugin exists in the default XFCE panel.
python3 - <<'PY'
import re
import subprocess

def run(cmd):
    return subprocess.run(cmd, shell=True, text=True, capture_output=True)

def query(path):
    res = run(f"xfconf-query -c xfce4-panel -p {path}")
    return res.stdout.strip() if res.returncode == 0 else ""

def query_ids(path):
    raw = query(path)
    return [int(line.strip()) for line in raw.splitlines() if line.strip().isdigit()]

panel1_ids = query_ids("/panels/panel-1/plugin-ids")
panel2_ids = query_ids("/panels/panel-2/plugin-ids")

all_props = query("/").splitlines()
used_ids = set(panel1_ids + panel2_ids)
for prop in all_props:
    m = re.search(r"/plugins/plugin-(\d+)$", prop.strip())
    if m:
        used_ids.add(int(m.group(1)))

existing_pulseaudio = None
for pid in sorted(used_ids):
    plugin_name = query(f"/plugins/plugin-{pid}")
    if plugin_name == "pulseaudio":
        existing_pulseaudio = pid
        break

if existing_pulseaudio is None:
    plugin_id = max([19] + sorted(used_ids)) + 1
    run(f"xfconf-query -c xfce4-panel -p /plugins/plugin-{plugin_id} -n -t string -s pulseaudio --create")
else:
    plugin_id = existing_pulseaudio

# Remove the pulseaudio plugin from panel-2 if a previous bad config put it there.
if plugin_id in panel2_ids:
    new_panel2_ids = [pid for pid in panel2_ids if pid != plugin_id]
    if new_panel2_ids:
        args = " ".join([f"-t int -s {pid}" for pid in new_panel2_ids])
        run(f"xfconf-query -c xfce4-panel -p /panels/panel-2/plugin-ids {args}")

# Append to the top panel if it's not already there.
if plugin_id not in panel1_ids:
    new_panel1_ids = list(panel1_ids)
    insert_after = 6 if 6 in new_panel1_ids else None
    if insert_after is not None:
        insert_idx = new_panel1_ids.index(insert_after) + 1
        new_panel1_ids.insert(insert_idx, plugin_id)
    else:
        new_panel1_ids.append(plugin_id)
    args = " ".join([f"-t int -s {pid}" for pid in new_panel1_ids])
    run(f"xfconf-query -c xfce4-panel -p /panels/panel-1/plugin-ids {args}")

run("xfce4-panel -r")
PY

xfconf-query -c xsettings -p /Net/IconThemeName -n -t string -s "elementary-Xfce-darker" --create
xfconf-query -c xsettings -p /Net/ThemeName -n -t string -s "Greybird" --create
xfconf-query -c xsettings -p /Gdk/WindowScalingFactor -n -t int -s %d --create

BG_FILE="%s"
for m in monitor0 monitorHEADLESS-1 HEADLESS-1 default; do
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/last-image -n -t string -s "$BG_FILE" --create
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/image-style -n -t int -s 5 --create
  xfconf-query -c xfce4-desktop -p /backdrop/screen0/$m/workspace0/color-style -n -t int -s 0 --create
done

xfconf-query -c xfce4-session -p /general/SaveOnExit -n -t bool -s false --create

xfdesktop --reload

# swaybg is more reliable for Wayland backgrounds on labwc
swaybg -o HEADLESS-1 -i "$BG_FILE" -m stretch &
`, gdkScale, bgFile)
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

# NOTE: randr and scaling are handled by Go server using native wlr-randr --scale

# Set Wayland native backend for GTK/XFCE
export GDK_BACKEND=wayland

%s

touch "$READY_FILE"
`, desktopReadyMarker, xfceAutostart)
	_ = os.WriteFile(filepath.Join(configDir, "autostart"), []byte(autostart), 0755)

	// Start labwc standalone (it will use the global DBUS session)
	cmd := exec.Command("labwc")
	cmd.Env = append(os.Environ(),
		"XDG_RUNTIME_DIR="+runDir,
		"WLR_BACKENDS=headless",
		"WLR_HEADLESS_OUTPUTS=1",
		"DISPLAY=:99",
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
	w, h = GetScreenSize()
	log.Printf("Setting initial Wayland resolution to %dx%d", w, h)
	_ = resizeDisplay(w, h)
	applyHdpiSettings(waylandEnv)
	if err := waitForDisplayState(w, h, 10*time.Second); err != nil {
		return err
	}

	// Start PulseAudio
	log.Println("Starting pulseaudio with null-sink for desktop audio capture...")
	paCmd := exec.Command("pulseaudio", "-D", "--exit-idle-time=-1")
	paCmd.Env = waylandEnv
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
			_ = runWithEnv("pactl", []string{"load-module", "module-null-sink", "sink_name=remote", "sink_properties=device.description=Desktop-Audio"}, waylandEnv)
			_ = runWithEnv("pactl", []string{"set-default-sink", "remote"}, waylandEnv)
			// Prevent the sink from suspending to keep audio stream active
			_ = runWithEnv("pactl", []string{"unload-module", "module-suspend-on-idle"}, waylandEnv)
			log.Println("PulseAudio virtual sink 'remote' initialized.")
			readiness.Set(readinessPulseAudio, true)
		} else {
			log.Printf("Warning: PulseAudio daemon started but pactl timed out.")
		}
	} else {
		log.Printf("Warning: pulseaudio failed to start: %v", err)
	}
	if !EnableAudio {
		readiness.Set(readinessPulseAudio, true)
	}

	if !minimal {
		if err := waitForFile(desktopReadyMarker, 30*time.Second, 100*time.Millisecond); err != nil {
			log.Printf("Warning: desktop session readiness failed: %v", err)
		}
	}
	readiness.Set(readinessDesktopSession, true)
	log.Println("Desktop session is fully ready.")
	PrimeFrameGeneration(0, 10, 100*time.Millisecond)

	// We consider it "ready enough" to start streaming once the socket is there and input is ready
	return nil
}
