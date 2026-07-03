package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func (e *NVENCEncoder) BuildCommand() ([]string, error) {
	// Map high-level codec names to FFmpeg NVENC equivalent
	var ffmpegCodec string
	var format string
	switch strings.ToLower(e.Config.Codec) {
	case "h264", "h264_nvenc":
		ffmpegCodec = "h264_nvenc"
		format = "h264"
	case "hevc", "h265", "h265_nvenc", "hevc_nvenc":
		ffmpegCodec = "hevc_nvenc"
		format = "hevc"
	case "av1", "av1_nvenc":
		ffmpegCodec = "av1_nvenc"
		format = "ivf"
	default:
		return nil, fmt.Errorf("unsupported codec for NVENC: %s", e.Config.Codec)
	}

	// Build high-performance direct-buffer wf-recorder arguments
	// Omit -d to bypass the NVIDIA proprietary driver's VAAPI RGB-import surface limitation
	// Always disable compositor damage tracking (-D) to prevent erratic/delayed mouse movement and ensure a butter-smooth constant frame rate
	args := []string{
		"wf-recorder",
		"-D",
		"-g", fmt.Sprintf("0,0 %dx%d", e.Config.Width, e.Config.Height),
	}

	args = append(args,
		"-c", ffmpegCodec,
		"-m", format,
		"-r", fmt.Sprintf("%d", e.Config.FPS),
		"-B", "2", // Low queue size to minimize lag under load
	)

	// Avoid CPU color space conversion: utilize GPU shader for color conversion
	if e.Config.Chroma == "444" {
		args = append(args, "-x", "bgr0")
	} else {
		args = append(args, "-x", "nv12")
	}

	// Inject low-latency preset optimizations
	tune := "ull"
	rgbMode := "yuv420"
	if e.Config.Chroma == "444" {
		rgbMode = "yuv444"
	}

	surfaces := "8"
	if e.Config.Chroma == "444" {
		surfaces = "16"
	}

	args = append(args,
		"-p", "preset=p1",
		"-p", "tune="+tune,
		"-p", "rc=cbr",
		"-p", "delay=0",
		"-p", "surfaces="+surfaces,
		"-p", "threads=1",
		"-p", "rgb_mode="+rgbMode,
		"-p", "bf=0",
		"-p", "spatial-aq=0",
		"-p", "temporal-aq=0",
		"-p", "strict_gop=1",
		"-p", "repeat_headers=1",
		"-p", fmt.Sprintf("b=%dM", e.Config.Bitrate),
		"-p", fmt.Sprintf("g=%d", 2*e.Config.FPS), // 2-second keyframe interval
	)

	if e.Config.Chroma == "444" {
		profile := "high444p"
		if ffmpegCodec == "hevc_nvenc" {
			profile = "rext"
		}
		args = append(args, "-p", "profile="+profile, "-p", "dpb_size=1")
		if ffmpegCodec == "h264_nvenc" {
			args = append(args, "-p", "coder=ac")
		}
	}

	// Always enforce ultra-low-latency mode
	args = append(args, "-p", "rc-lookahead=0", "-p", "no-scenecut=1", "-p", "b_ref_mode=0")

	if ffmpegCodec == "h264_nvenc" || ffmpegCodec == "hevc_nvenc" {
		args = append(args, "-p", "aud=1")
	}

	// Pipe the output to stdout
	args = append(args, "-f", "pipe:1")

	return args, nil
}

func (e *NVENCEncoder) Start(args []string) (chan []byte, error) {
	cmd := exec.Command(args[0], args[1:]...)

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
		_ = e.Cmd.Process.Kill()
	}
}
