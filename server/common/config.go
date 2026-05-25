package common

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	CaptureModeCompat = "compat"
	CaptureModeDirect = "direct"
	CaptureModeAgent  = "agent"
)

var (
	Port            int
	FPS             int
	VideoCodec      string
	Chroma          string
	UseNVIDIA       bool
	UseIntel        bool
	IntelRenderNode string
	CaptureMode     string

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
	AgentAddress            string

	// Globally shared settings (historically in ffmpeg.go or others)
	TargetBandwidthMbps int = 5
	TargetVBR           bool = false
	TargetVBRThreshold  int = 0
	TargetDamageTracking bool = false
	TargetCpuEffort     int = 6
)

func InitConfig() {
	// Fallback from environment variables
	defaultPort := 8080
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil {
		defaultPort = p
	}

	defaultFPS := 30
	if f, err := strconv.Atoi(os.Getenv("FPS")); err == nil {
		defaultFPS = f
	}

	defaultBandwidth := TargetBandwidthMbps
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
	defaultIntelRenderNode := os.Getenv("INTEL_RENDER_NODE")
	if defaultIntelRenderNode == "" {
		defaultIntelRenderNode = "/dev/dri/renderD128"
	}
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

	defaultWebRTCBufferSize := 1
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

	defaultHDPI := 100
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

	defaultAgentAddress := os.Getenv("AGENT_ADDRESS")

	// Custom Usage format
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of llrdc:\n")
		fmt.Fprintf(os.Stderr, "  llrdc [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Note: --port configures both the HTTP and WebRTC UDP port.\n\n")

		fmt.Fprintf(os.Stderr, "User Flags:\n")
		printFlag(os.Stderr, "port", "Port for HTTP and WebRTC UDP", Port)
		printFlag(os.Stderr, "fps", "Target framerate", FPS)
		printFlag(os.Stderr, "bandwidth", "Target bandwidth in Mbps", TargetBandwidthMbps)
		printFlag(os.Stderr, "video-codec", "Video codec (vp8, h264, h264_nvenc, h264_qsv, h265, h265_nvenc, h265_qsv, hevc_vaapi, av1, av1_nvenc, av1_qsv)", VideoCodec)
		printFlag(os.Stderr, "chroma", "Chroma subsampling format (420 or 444)", Chroma)
		printFlag(os.Stderr, "use-nvidia", "Enable NVIDIA acceleration if available", UseNVIDIA)
		printFlag(os.Stderr, "use-intel", "Enable Intel QSV acceleration if available", UseIntel)
		printFlag(os.Stderr, "intel-render-node", "Path to Intel render node (e.g. /dev/dri/renderD128)", IntelRenderNode)
		printFlag(os.Stderr, "capture-mode", "Capture mode (compat, direct, or agent)", CaptureMode)
		printFlag(os.Stderr, "agent-address", "TCP address for remote agent streaming (e.g. host.docker.internal:12345)", AgentAddress)
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
	flag.IntVar(&TargetBandwidthMbps, "bandwidth", defaultBandwidth, "Target bandwidth in Mbps")
	flag.StringVar(&VideoCodec, "video-codec", defaultVideoCodec, "Video codec (vp8, h264, h264_nvenc, h264_qsv, h264_vaapi, h265, h265_nvenc, h265_qsv, hevc_vaapi, av1, av1_nvenc, av1_qsv)")
	flag.StringVar(&Chroma, "chroma", defaultChroma, "Chroma subsampling format (420 or 444)")
	flag.BoolVar(&UseNVIDIA, "use-nvidia", defaultUseNVIDIA, "Enable NVIDIA acceleration if available")
	flag.BoolVar(&UseIntel, "use-intel", defaultUseIntel, "Enable Intel QSV acceleration if available")
	flag.StringVar(&IntelRenderNode, "intel-render-node", defaultIntelRenderNode, "Path to Intel render node (e.g. /dev/dri/renderD128)")
	flag.StringVar(&CaptureMode, "capture-mode", defaultCaptureMode, "Capture mode (compat, direct, or agent)")
	flag.StringVar(&AgentAddress, "agent-address", defaultAgentAddress, "TCP address for remote agent streaming (e.g. host.docker.internal:12345)")
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
	flag.BoolVar(&TargetVBR, "vbr", defaultVBR, "Enable variable bitrate (encoder rate control)")
	flag.IntVar(&TargetVBRThreshold, "vbr-threshold", defaultVBRThreshold, "VBR threshold for static content")
	flag.BoolVar(&TargetDamageTracking, "damage-tracking", defaultDamageTracking, "Enable Wayland damage tracking (frame skipping)")
	flag.IntVar(&TargetCpuEffort, "cpu-effort", defaultCpuEffort, "FFmpeg CPU effort/used (default 6)")

	// In Go, package flag.Parse can only be called once, typically in main.
	// But both linux.Run and macOS main can call InitConfig.
	if flag.Parsed() {
		return
	}
	flag.Parse()
}

func printFlag(w *os.File, name, usage string, def any) {
	fmt.Fprintf(w, "  -%s\n    \t%s (default %v)\n", name, usage, def)
}

func ResolveRequestedVideoCodec(codec string) string {
	if UseIntel {
		if codec == "h265" || codec == "hevc" || codec == "h265_vaapi" || codec == "hevc_vaapi" || codec == "h265_qsv" {
			if H265QSVAvailable {
				return "h265_qsv"
			}
			return "h265_vaapi"
		}
		if codec == "h264" || codec == "h264_vaapi" || codec == "h264_qsv" {
			if QSVAvailable {
				return "h264_qsv"
			}
			return "h264_vaapi"
		}
		if codec == "av1" || codec == "av1_qsv" || codec == "av1_vaapi" {
			if AV1QSVAvailable {
				return "av1_qsv"
			}
			return "av1_vaapi"
		}
	}
	return codec
}

func NormalizeCodecFamily(codec string) string {
	switch codec {
	case "h264", "h264_nvenc", "h264_qsv", "h264_vaapi":
		return "h264"
	case "h265", "h265_nvenc", "h265_qsv", "h265_vaapi", "hevc_vaapi":
		return "h265"
	case "av1", "av1_nvenc", "av1_qsv", "av1_vaapi":
		return "av1"
	default:
		return codec
	}
}
