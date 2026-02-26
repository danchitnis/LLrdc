package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	xvfb := exec.Command("Xvfb", display, "-screen", "0", "1920x1080x24", "-nolisten", "tcp", "-ac", "+extension", "RANDR")
	xvfb.Stdout = os.Stdout
	xvfb.Stderr = os.Stderr
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

	// Start XFCE
	log.Println("Starting xfce4-session...")
	session := exec.Command("dbus-run-session", "xfce4-session")
	session.Env = env
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
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

	return nil
}

func resizeDisplay(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid resize: %dx%d", width, height)
	}
	mode := fmt.Sprintf("%dx%d", width, height)
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
	wallpaper := os.Getenv("WALLPAPER")
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
	return c.Run()
}
