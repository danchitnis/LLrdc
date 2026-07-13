package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

type NVENCEncoder struct {
	Config *CaptureConfig
	Cmd    *exec.Cmd
}

func NewNVENCEncoder(config *CaptureConfig) *NVENCEncoder {
	return &NVENCEncoder{
		Config: config,
	}
}

func probeNativeCapture() bool {
	binaryPath := "/usr/local/bin/nvidia_direct_capture_native"
	if _, err := os.Stat(binaryPath); err != nil {
		return false
	}

	cmd := exec.Command(binaryPath, "--probe")
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = "/tmp/llrdc-run"
	}
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		display = "wayland-0"
	}
	cmd.Env = append(os.Environ(),
		"WAYLAND_DISPLAY="+display,
		"XDG_RUNTIME_DIR="+runDir,
		"DISPLAY=",
		"GBM_BACKEND=nvidia-drm",
		"__GL_GBM_ALWAYS_USE_MODIFIERS=1",
		"__EGL_VENDOR_LIBRARY_FILENAMES=/usr/share/glvnd/egl_vendor.d/10_nvidia.json",
	)

	err := cmd.Run()
	return err == nil
}

func (e *NVENCEncoder) BuildCommand() ([]string, error) {
	// If the native C++ capture and encode utility is present and initializes successfully, run it directly.
	if probeNativeCapture() {
		args := []string{
			"/usr/local/bin/nvidia_direct_capture_native",
			"--fps", strconv.Itoa(e.Config.FPS),
			"--bitrate", strconv.Itoa(e.Config.Bitrate),
			"--codec", e.Config.Codec,
			"--chroma", e.Config.Chroma,
			"--width", strconv.Itoa(e.Config.Width),
			"--height", strconv.Itoa(e.Config.Height),
		}
		if e.Config.VBR {
			args = append(args, "--vbr")
			args = append(args, "--vbr-threshold", strconv.Itoa(e.Config.VBRThreshold))
		}
		if e.Config.DamageTracking {
			args = append(args, "--damage-tracking")
		}
		return args, nil
	}

	// Fail closed with a clear, descriptive error as requested when the hardware probe fails.
	return nil, fmt.Errorf("NVIDIA direct zero-copy capture is physically unsupported on this NVIDIA card or driver headlessly (eglCreateImageKHR fails with EGL_BAD_PARAMETER 0x300c due to driver-level GBM/DMA-BUF import blocks on unprivileged or headless contexts)")
}

func (e *NVENCEncoder) Start(args []string) (chan []byte, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}

	// Set environmental variables ensuring Wayland is routed correctly
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = "/tmp/llrdc-run"
	}
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		display = "wayland-0"
	}

	cmd.Env = append(os.Environ(),
		"WAYLAND_DISPLAY="+display,
		"XDG_RUNTIME_DIR="+runDir,
		"__GL_YIELD=USLEEP",
		"__GL_THREADED_OPTIMIZATIONS=1",
		"__GL_SYNC_TO_VBLANK=0",
		"LIBVA_DRIVER_NAME=nvidia",
		"NVD_BACKEND=direct",
		"DISPLAY=",
		"GBM_BACKEND=nvidia-drm",
		"__GL_GBM_ALWAYS_USE_MODIFIERS=1",
		"__EGL_VENDOR_LIBRARY_FILENAMES=/usr/share/glvnd/egl_vendor.d/10_nvidia.json",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start encoder process: %w", err)
	}
	e.Cmd = cmd

	// Channel to stream output bytes
	dataChan := make(chan []byte, 100)

	go func() {
		defer close(dataChan)
		buf := make([]byte, 512*1024) // 512KB read buffer for high-throughput 4K
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				dataChan <- data
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
	}()

	return dataChan, nil
}

func (e *NVENCEncoder) Kill() {
	if e.Cmd != nil && e.Cmd.Process != nil {
		log.Println("Sending SIGTERM to C++ native encoder child for graceful cleanup...")
		if err := e.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("SIGTERM to C++ child failed: %v, falling back to Kill", err)
			_ = e.Cmd.Process.Kill()
		}
	}
}

func (e *NVENCEncoder) Signal(sig os.Signal) error {
	if e.Cmd != nil && e.Cmd.Process != nil {
		return e.Cmd.Process.Signal(sig)
	}
	return nil
}
