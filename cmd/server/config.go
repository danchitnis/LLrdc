package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var (
	Port        int
	FPS         int
	VideoCodec  string
	Chroma      string
	UseNVIDIA   bool
	UseIntel    bool
	CaptureMode string

	AV1NVENCAvailable       bool
	H264NVENC444Available   bool
	H265NVENC444Available   bool
	QSVAvailable            bool
	H265QSVAvailable        bool
	AV1QSVAvailable         bool
	UseDebugFFmpeg          bool
	UseDebugInput           bool
	TestPattern             bool
	EnableAudio             bool
	AudioBitrate            string
	Wallpaper               string
	WebRTCPublicIP          string
	WebRTCInterfaces        string
	WebRTCExcludeInterfaces string
	WebRTCBufferSize        int
	ActivityPulseHz         int
	ActivityTimeout         int
	NVENCLatencyMode        bool
	HDPI                    int
	SettleTime              int
	TileSize                int
	WebRTCLowLatency        bool
	InitialRes              int
)

func initConfig() {
	// Fallback from environment variables
	defaultPort := 8080
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil {
		defaultPort = p
	}

	defaultFPS := 30
	if f, err := strconv.Atoi(os.Getenv("FPS")); err == nil {
		defaultFPS = f
	}

	defaultBandwidth := targetBandwidthMbps
	if bw, err := strconv.Atoi(os.Getenv("BANDWIDTH")); err == nil {
		defaultBandwidth = bw
	}

	defaultVideoCodec := os.Getenv("VIDEO_CODEC")
	if defaultVideoCodec == "" {
		defaultVideoCodec = "vp8"
	}

	defaultChroma := os.Getenv("CHROMA")
	if defaultChroma == "" {
		defaultChroma = "420"
	}

	defaultUseNVIDIA := os.Getenv("USE_NVIDIA") == "true"
	defaultUseIntel := os.Getenv("USE_INTEL") == "true"
	defaultCaptureMode := os.Getenv("CAPTURE_MODE")
	if defaultCaptureMode == "" {
		defaultCaptureMode = CaptureModeCompat
	}
	defaultUseDebugFFmpeg := os.Getenv("USE_DEBUG_FFMPEG") == "true"
	defaultUseDebugInput := os.Getenv("USE_DEBUG_INPUT") == "true"
	defaultTestPattern := os.Getenv("TEST_PATTERN") != ""
	defaultEnableAudio := os.Getenv("ENABLE_AUDIO") != "false"
	defaultAudioBitrate := os.Getenv("AUDIO_BITRATE")
	if defaultAudioBitrate == "" {
		defaultAudioBitrate = "128k"
	}

	defaultWallpaper := os.Getenv("WALLPAPER")
	defaultWebRTCPublicIP := os.Getenv("WEBRTC_PUBLIC_IP")
	defaultWebRTCInterfaces := os.Getenv("WEBRTC_INTERFACES")
	defaultWebRTCExcludeInterfaces := os.Getenv("WEBRTC_EXCLUDE_INTERFACES")

	defaultWebRTCBufferSize := 2
	if bs, err := strconv.Atoi(os.Getenv("WEBRTC_BUFFER_SIZE")); err == nil {
		defaultWebRTCBufferSize = bs
	}

	defaultActivityPulseHz := 30
	if ap, err := strconv.Atoi(os.Getenv("ACTIVITY_PULSE_HZ")); err == nil {
		defaultActivityPulseHz = ap
	}

	defaultActivityTimeout := 1500
	if at, err := strconv.Atoi(os.Getenv("ACTIVITY_TIMEOUT")); err == nil {
		defaultActivityTimeout = at
	}

	defaultCpuEffort := 6
	if ce, err := strconv.Atoi(os.Getenv("CPU_EFFORT")); err == nil {
		defaultCpuEffort = ce
	}

	defaultNVENCLatencyMode := os.Getenv("NVENC_LATENCY_MODE") != "false"

	defaultHDPI := 0
	if hdpi, err := strconv.Atoi(os.Getenv("HDPI")); err == nil {
		defaultHDPI = hdpi
	}

	defaultSettleTime := 500
	if st, err := strconv.Atoi(os.Getenv("SETTLE_TIME")); err == nil {
		defaultSettleTime = st
	}

	defaultTileSize := 128
	if ts, err := strconv.Atoi(os.Getenv("TILE_SIZE")); err == nil {
		defaultTileSize = ts
	}

	defaultWebRTCLowLatency := os.Getenv("WEBRTC_LOW_LATENCY") == "true"

	defaultVBR := false
	if vbr, err := strconv.ParseBool(os.Getenv("VBR")); err == nil {
		defaultVBR = vbr
	}

	defaultVBRThreshold := 0
	if vt, err := strconv.Atoi(os.Getenv("VBR_THRESHOLD")); err == nil {
		defaultVBRThreshold = vt
	}

	defaultDamageTracking := false
	if dt, err := strconv.ParseBool(os.Getenv("DAMAGE_TRACKING")); err == nil {
		defaultDamageTracking = dt
	}

	resStr := strings.ToLower(os.Getenv("RESOLUTION"))
	defaultInitialRes := 0
	if strings.Contains(resStr, "720") {
		defaultInitialRes = 720
	} else if strings.Contains(resStr, "1080") {
		defaultInitialRes = 1080
	} else if strings.Contains(resStr, "1440") || strings.Contains(resStr, "2k") {
		defaultInitialRes = 1440
	} else if strings.Contains(resStr, "2160") || strings.Contains(resStr, "4k") {
		defaultInitialRes = 2160
	}

	// Custom Usage format
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of llrdc:\n")
		fmt.Fprintf(os.Stderr, "  llrdc [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Note: --port configures both the HTTP and WebRTC UDP port.\n\n")

		fmt.Fprintf(os.Stderr, "User Flags:\n")
		printFlag(os.Stderr, "port", "Port for HTTP and WebRTC UDP", Port)
		printFlag(os.Stderr, "fps", "Target framerate", FPS)
		printFlag(os.Stderr, "bandwidth", "Target bandwidth in Mbps", targetBandwidthMbps)
		printFlag(os.Stderr, "video-codec", "Video codec (vp8, h264, h264_nvenc, h264_qsv, h265, h265_nvenc, h265_qsv, av1, av1_nvenc, av1_qsv)", VideoCodec)
		printFlag(os.Stderr, "chroma", "Chroma subsampling format (420 or 444)", Chroma)
		printFlag(os.Stderr, "use-nvidia", "Enable NVIDIA acceleration if available", UseNVIDIA)
		printFlag(os.Stderr, "use-intel", "Enable Intel QSV acceleration if available", UseIntel)
		printFlag(os.Stderr, "capture-mode", "Capture mode (compat or direct)", CaptureMode)
		printFlag(os.Stderr, "use-debug-ffmpeg", "Enable FFmpeg debugging", UseDebugFFmpeg)
		printFlag(os.Stderr, "wallpaper", "Path to wallpaper image", Wallpaper)
		printFlag(os.Stderr, "webrtc-public-ip", "Public IP for WebRTC", WebRTCPublicIP)
		printFlag(os.Stderr, "webrtc-interfaces", "Comma-separated allowed network interfaces for WebRTC", WebRTCInterfaces)
		printFlag(os.Stderr, "webrtc-exclude-interfaces", "Comma-separated excluded network interfaces for WebRTC", WebRTCExcludeInterfaces)
		printFlag(os.Stderr, "enable-audio", "Enable audio streaming", EnableAudio)
		printFlag(os.Stderr, "audio-bitrate", "Audio bitrate (e.g. 64k, 128k)", AudioBitrate)
		printFlag(os.Stderr, "hdpi", "Set high DPI scaling percentage (e.g., 150, 200)", HDPI)
		printFlag(os.Stderr, "res", "Fixed initial resolution height (720, 1080, 1440, 2160). 0 for adaptive.", InitialRes)

		fmt.Fprintf(os.Stderr, "\nLatency & Smoothness Flags:\n")
		printFlag(os.Stderr, "webrtc-buffer", "WebRTC frame channel size (default 30)", WebRTCBufferSize)
		printFlag(os.Stderr, "activity-hz", "Input heartbeat frequency in Hz (default 30)", ActivityPulseHz)
		printFlag(os.Stderr, "activity-timeout", "Inactivity timeout in ms before stopping heartbeat (default 1500)", ActivityTimeout)
		printFlag(os.Stderr, "vbr", "Enable variable bitrate (encoder rate control) (default false)", defaultVBR)
		printFlag(os.Stderr, "vbr-threshold", "VBR threshold for static content (default 100)", defaultVBRThreshold)
		printFlag(os.Stderr, "damage-tracking", "Enable Wayland damage tracking (frame skipping) (default false)", defaultDamageTracking)
		printFlag(os.Stderr, "nvenc-latency", "Enable ultra-low latency NVENC optimizations (default true)", NVENCLatencyMode)
		printFlag(os.Stderr, "webrtc-low-latency", "Enable ultra-low latency WebRTC transport optimizations (ICE Lite, disabled replay protection) (default false)", WebRTCLowLatency)

		fmt.Fprintf(os.Stderr, "\nTesting Flags:\n")
		printFlag(os.Stderr, "test-pattern", "Run with test pattern instead of Wayland session", TestPattern)
		printFlag(os.Stderr, "settle-time", "Hybrid sharpness settle time (ms)", SettleTime)
		printFlag(os.Stderr, "tile-size", "Hybrid sharpness tile size (px)", TileSize)
	}

	// Define flags
	flag.IntVar(&Port, "port", defaultPort, "Port for HTTP and WebRTC UDP")
	flag.IntVar(&FPS, "fps", defaultFPS, "Target framerate")
	flag.IntVar(&targetBandwidthMbps, "bandwidth", defaultBandwidth, "Target bandwidth in Mbps")
	flag.StringVar(&VideoCodec, "video-codec", defaultVideoCodec, "Video codec (vp8, h264, h264_nvenc, h264_qsv, h265, h265_nvenc, h265_qsv, av1, av1_nvenc, av1_qsv)")
	flag.StringVar(&Chroma, "chroma", defaultChroma, "Chroma subsampling format (420 or 444)")
	flag.BoolVar(&UseNVIDIA, "use-nvidia", defaultUseNVIDIA, "Enable NVIDIA acceleration if available")
	flag.BoolVar(&UseIntel, "use-intel", defaultUseIntel, "Enable Intel QSV acceleration if available")
	flag.StringVar(&CaptureMode, "capture-mode", defaultCaptureMode, "Capture mode (compat or direct)")
	flag.BoolVar(&UseDebugFFmpeg, "use-debug-ffmpeg", defaultUseDebugFFmpeg, "Enable FFmpeg debugging")
	flag.BoolVar(&UseDebugInput, "use-debug-input", defaultUseDebugInput, "Enable Input debugging")
	flag.BoolVar(&TestPattern, "test-pattern", defaultTestPattern, "Run with test pattern instead of Wayland session")
	flag.StringVar(&Wallpaper, "wallpaper", defaultWallpaper, "Path to wallpaper image")
	flag.StringVar(&WebRTCPublicIP, "webrtc-public-ip", defaultWebRTCPublicIP, "Public IP for WebRTC")
	flag.StringVar(&WebRTCInterfaces, "webrtc-interfaces", defaultWebRTCInterfaces, "Comma-separated allowed network interfaces for WebRTC")
	flag.StringVar(&WebRTCExcludeInterfaces, "webrtc-exclude-interfaces", defaultWebRTCExcludeInterfaces, "Comma-separated excluded network interfaces for WebRTC")
	EnableAudio = true
	flag.BoolVar(&EnableAudio, "enable-audio", defaultEnableAudio, "Enable audio streaming")
	flag.StringVar(&AudioBitrate, "audio-bitrate", defaultAudioBitrate, "Audio bitrate (e.g. 64k, 128k)")
	flag.IntVar(&HDPI, "hdpi", defaultHDPI, "Set high DPI scaling percentage (e.g., 150, 200)")
	flag.IntVar(&InitialRes, "res", defaultInitialRes, "Fixed initial resolution height (720, 1080, 1440, 2160). 0 for adaptive.")
	flag.IntVar(&WebRTCBufferSize, "webrtc-buffer", defaultWebRTCBufferSize, "WebRTC frame channel size (default 30)")
	flag.IntVar(&ActivityPulseHz, "activity-hz", defaultActivityPulseHz, "Input heartbeat frequency in Hz (default 30)")
	flag.IntVar(&ActivityTimeout, "activity-timeout", defaultActivityTimeout, "Inactivity timeout in ms before stopping heartbeat (default 1500)")
	flag.BoolVar(&NVENCLatencyMode, "nvenc-latency", defaultNVENCLatencyMode, "Enable ultra-low latency NVENC optimizations (default true)")
	flag.BoolVar(&WebRTCLowLatency, "webrtc-low-latency", defaultWebRTCLowLatency, "Enable ultra-low latency WebRTC transport optimizations (default false)")
	flag.IntVar(&SettleTime, "settle-time", defaultSettleTime, "Hybrid sharpness settle time (ms)")
	flag.IntVar(&TileSize, "tile-size", defaultTileSize, "Hybrid sharpness tile size (px)")
	flag.BoolVar(&targetVBR, "vbr", defaultVBR, "Enable variable bitrate (encoder rate control)")
	flag.IntVar(&targetVBRThreshold, "vbr-threshold", defaultVBRThreshold, "VBR threshold for static content")
	flag.BoolVar(&targetDamageTracking, "damage-tracking", defaultDamageTracking, "Enable Wayland damage tracking (frame skipping)")
	flag.IntVar(&targetCpuEffort, "cpu-effort", defaultCpuEffort, "FFmpeg CPU effort/used (default 6)")

	flag.Parse()
	if UseNVIDIA && UseIntel {
		log.Fatalf("Invalid accelerator configuration: choose only one of --use-nvidia or --use-intel")
	}
	initDirectBufferState()
	if UseNVIDIA {
		log.Printf("Checking NVIDIA GPU capabilities...")

		// Check basic AV1 support via encoders list
		outAV1, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q av1_nvenc && echo true || echo false").Output()
		AV1NVENCAvailable = strings.TrimSpace(string(outAV1)) == "true"

		if AV1NVENCAvailable {
			log.Printf("AV1 NVENC support detected")
			// Note: AV1 NVENC does NOT support 4:4:4 chroma on any current NVIDIA GPU.
		}

		log.Printf("Checking H.264 NVENC 4:4:4 support...")
		outH264, _ := exec.Command("bash", "-c", "ffmpeg -y -f lavfi -i testsrc=size=256x256:rate=1 -t 1 -pix_fmt yuv444p -c:v h264_nvenc -profile:v high444p -f null - > /dev/null 2>&1 && echo true || echo false").Output()
		H264NVENC444Available = strings.TrimSpace(string(outH264)) == "true"
		if H264NVENC444Available {
			log.Printf("H.264 NVENC 4:4:4 support detected")
		} else {
			log.Printf("H.264 NVENC 4:4:4 support NOT detected")
		}

		log.Printf("Checking H.265 NVENC 4:4:4 support...")
		outH265, _ := exec.Command("bash", "-c", "ffmpeg -y -f lavfi -i testsrc=size=256x256:rate=1 -t 1 -pix_fmt yuv444p -c:v hevc_nvenc -profile:v rext -f null - > /dev/null 2>&1 && echo true || echo false").Output()
		H265NVENC444Available = strings.TrimSpace(string(outH265)) == "true"
		if H265NVENC444Available {
			log.Printf("H.265 NVENC 4:4:4 support detected")
		} else {
			log.Printf("H.265 NVENC 4:4:4 support NOT detected")
		}
	}

	if UseIntel {
		log.Printf("Checking Intel QSV capabilities...")
		outQSV, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q h264_qsv && echo true || echo false").Output()
		QSVAvailable = strings.TrimSpace(string(outQSV)) == "true"
		if QSVAvailable {
			log.Printf("Intel QSV hardware acceleration detected")
			renderNode := resolveIntelRenderNode()
			checkCmd := fmt.Sprintf("TMP=$(mktemp /tmp/llrdc-hevc-qsv-XXXX.hevc) && ffmpeg -hide_banner -y -f lavfi -i testsrc=size=1280x720:rate=30 -t 2 -vf format=nv12,hwupload -vaapi_device %s -c:v hevc_vaapi -bf 0 -rc_mode CBR -b:v 5M -maxrate 5M -bufsize 10M -g 60 -profile:v main -aud 1 -f hevc \"$TMP\" >/dev/null 2>&1 && [ \"$(ffprobe -hide_banner -show_frames -select_streams v -print_format compact=nk=1:p=0 \"$TMP\" 2>/dev/null | wc -l)\" -gt 10 ] && echo true || echo false", renderNode)
			outH265QSV, _ := exec.Command("bash", "-c", checkCmd).Output()
			H265QSVAvailable = strings.TrimSpace(string(outH265QSV)) == "true"
			if H265QSVAvailable {
				log.Printf("Intel H.265 hardware encode support detected")
			} else {
				log.Printf("Intel H.265 hardware encode support NOT detected; disabling h265_qsv")
			}
			outAV1QSV, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q av1_qsv && echo true || echo false").Output()
			AV1QSVAvailable = strings.TrimSpace(string(outAV1QSV)) == "true"
			if AV1QSVAvailable {
				log.Printf("AV1 QSV support detected")
			}
		} else {
			log.Printf("Intel QSV hardware acceleration NOT detected")
		}
	}

	if UseIntel && VideoCodec == "h265_qsv" && !H265QSVAvailable {
		if CaptureMode == CaptureModeDirect {
			log.Fatalf("Invalid direct-buffer configuration: Intel H.265 hardware encode is not supported on this FFmpeg/driver stack; use h264_qsv or av1_qsv for direct mode")
		}
		log.Printf("Intel H.265 hardware encode is not supported on this FFmpeg/driver stack; falling back to CPU h265")
		VideoCodec = "h265"
	}

	if err := validateCaptureModeConfig(); err != nil {
		log.Fatalf("Invalid direct-buffer configuration: %v", err)
	}

}

func printFlag(w *os.File, name, usage string, def any) {
	fmt.Fprintf(w, "  -%s\n    \t%s (default %v)\n", name, usage, def)
}

func SetWebRTCLowLatency(lowLatency bool) {
	if WebRTCLowLatency == lowLatency {
		return
	}
	log.Printf("WebRTC low-latency mode changed to %v", lowLatency)
	WebRTCLowLatency = lowLatency
}
