package linux

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/danchitnis/llrdc/server/common"
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

	AV1NVENCAvailable     bool
	H264NVENC444Available bool
	H265NVENC444Available bool
	VAAPIAvailable        bool
	H265VAAPIAvailable    bool
	AV1VAAPIAvailable     bool
	UseDebugFFmpeg        bool
	UseDebugInput         bool
	TestPattern           bool
	EnableAudio           bool
	AudioBitrate          string
	Wallpaper             string
	ActivityPulseHz       int
	ActivityTimeout       int
	NVENCLatencyMode      bool
	HDPI                  int
	SettleTime            int
	TileSize              int
	InitialRes            int
	AgentAddress          string
	ClientTimeout         int
	EnableClipboard       bool

	displayChangeMu sync.Mutex

	// Globally shared settings
	TargetBandwidthMbps  int  = 5
	TargetVBR            bool = false
	TargetVBRThreshold   int  = 0
	TargetDamageTracking bool = false
	TargetCpuEffort      int  = 6
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
	defaultEnableClipboard := os.Getenv("ENABLE_CLIPBOARD") != "false"
	defaultAudioBitrate := os.Getenv("AUDIO_BITRATE")
	if defaultAudioBitrate == "" {
		defaultAudioBitrate = "128k"
	}

	defaultWallpaper := os.Getenv("WALLPAPER")

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

	defaultClientTimeout := 0
	if ct, err := strconv.Atoi(os.Getenv("CLIENT_TIMEOUT")); err == nil {
		defaultClientTimeout = ct
	}

	// Custom Usage format
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of llrdc:\n")
		fmt.Fprintf(os.Stderr, "  llrdc [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Note: --port configures the HTTP server port. WebTransport uses Port + 10.\n\n")

		fmt.Fprintf(os.Stderr, "User Flags:\n")
		printFlag(os.Stderr, "port", "Port for HTTP server", Port)
		printFlag(os.Stderr, "fps", "Target framerate", FPS)
		printFlag(os.Stderr, "bandwidth", "Target bandwidth in Mbps", TargetBandwidthMbps)
		printFlag(os.Stderr, "video-codec", "Video codec (vp8, h264, h264_nvenc, h264_vaapi, h265, h265_nvenc, h265_vaapi, hevc_vaapi, av1, av1_nvenc, av1_vaapi)", VideoCodec)
		printFlag(os.Stderr, "chroma", "Chroma subsampling format (420 or 444)", Chroma)
		printFlag(os.Stderr, "use-nvidia", "Enable NVIDIA acceleration if available", UseNVIDIA)
		printFlag(os.Stderr, "use-intel", "Enable Intel VAAPI acceleration if available", UseIntel)
		printFlag(os.Stderr, "intel-render-node", "Path to Intel render node (e.g. /dev/dri/renderD128)", IntelRenderNode)
		printFlag(os.Stderr, "capture-mode", "Capture mode (compat, direct, or agent)", CaptureMode)
		printFlag(os.Stderr, "agent-address", "TCP address for remote agent streaming (e.g. host.docker.internal:12345)", AgentAddress)
		printFlag(os.Stderr, "use-debug-ffmpeg", "Enable FFmpeg debugging", UseDebugFFmpeg)
		printFlag(os.Stderr, "wallpaper", "Path to wallpaper image", Wallpaper)
		printFlag(os.Stderr, "enable-audio", "Enable audio streaming", EnableAudio)
		printFlag(os.Stderr, "audio-bitrate", "Audio bitrate (e.g. 64k, 128k)", AudioBitrate)
		printFlag(os.Stderr, "hdpi", "Set high DPI scaling percentage (e.g., 150, 200)", HDPI)
		printFlag(os.Stderr, "res", "Fixed initial resolution height (720, 1080, 1440, 2160). 0 for adaptive.", InitialRes)
		printFlag(os.Stderr, "client-timeout", "Timeout in seconds to pause streaming when no clients are connected (0 to disable)", ClientTimeout)

		fmt.Fprintf(os.Stderr, "\nLatency & Smoothness Flags:\n")
		printFlag(os.Stderr, "activity-hz", "Input heartbeat frequency in Hz (default 30)", ActivityPulseHz)
		printFlag(os.Stderr, "activity-timeout", "Inactivity timeout in ms before stopping heartbeat (default 1500)", ActivityTimeout)
		printFlag(os.Stderr, "vbr", "Enable variable bitrate (encoder rate control) (default false)", defaultVBR)
		printFlag(os.Stderr, "vbr-threshold", "VBR threshold for static content (default 100)", defaultVBRThreshold)
		printFlag(os.Stderr, "damage-tracking", "Enable Wayland damage tracking (frame skipping) (default false)", defaultDamageTracking)
		printFlag(os.Stderr, "nvenc-latency", "Enable ultra-low latency NVENC optimizations (default true)", NVENCLatencyMode)

		fmt.Fprintf(os.Stderr, "\nTesting Flags:\n")
		printFlag(os.Stderr, "test-pattern", "Run with test pattern instead of Wayland session", TestPattern)
		printFlag(os.Stderr, "settle-time", "Hybrid sharpness settle time (ms)", SettleTime)
		printFlag(os.Stderr, "tile-size", "Hybrid sharpness tile size (px)", TileSize)
	}

	// Define flags
	flag.IntVar(&Port, "port", defaultPort, "Port for HTTP server")
	flag.IntVar(&FPS, "fps", defaultFPS, "Target framerate")
	flag.IntVar(&TargetBandwidthMbps, "bandwidth", defaultBandwidth, "Target bandwidth in Mbps")
	flag.StringVar(&VideoCodec, "video-codec", defaultVideoCodec, "Video codec (vp8, h264, h264_nvenc, h264_vaapi, h265, h265_nvenc, h265_vaapi, hevc_vaapi, av1, av1_nvenc, av1_vaapi)")
	flag.StringVar(&Chroma, "chroma", defaultChroma, "Chroma subsampling format (420 or 444)")
	flag.BoolVar(&UseNVIDIA, "use-nvidia", defaultUseNVIDIA, "Enable NVIDIA acceleration if available")
	flag.BoolVar(&UseIntel, "use-intel", defaultUseIntel, "Enable Intel VAAPI acceleration if available")
	flag.StringVar(&IntelRenderNode, "intel-render-node", defaultIntelRenderNode, "Path to Intel render node (e.g. /dev/dri/renderD128)")
	flag.StringVar(&CaptureMode, "capture-mode", defaultCaptureMode, "Capture mode (compat, direct, or agent)")
	flag.StringVar(&AgentAddress, "agent-address", defaultAgentAddress, "TCP address for remote agent streaming (e.g. host.docker.internal:12345)")
	flag.BoolVar(&UseDebugFFmpeg, "use-debug-ffmpeg", defaultUseDebugFFmpeg, "Enable FFmpeg debugging")
	flag.BoolVar(&UseDebugInput, "use-debug-input", defaultUseDebugInput, "Enable Input debugging")
	flag.BoolVar(&TestPattern, "test-pattern", defaultTestPattern, "Run with test pattern instead of Wayland session")
	flag.StringVar(&Wallpaper, "wallpaper", defaultWallpaper, "Path to wallpaper image")
	EnableAudio = true
	flag.BoolVar(&EnableAudio, "enable-audio", defaultEnableAudio, "Enable audio streaming")
	flag.BoolVar(&EnableClipboard, "enable-clipboard", defaultEnableClipboard, "Enable clipboard synchronization")
	flag.StringVar(&AudioBitrate, "audio-bitrate", defaultAudioBitrate, "Audio bitrate (e.g. 64k, 128k)")
	flag.IntVar(&HDPI, "hdpi", defaultHDPI, "Set high DPI scaling percentage (e.g., 150, 200)")
	flag.IntVar(&InitialRes, "res", defaultInitialRes, "Fixed initial resolution height (720, 1080, 1440, 2160). 0 for adaptive.")
	flag.IntVar(&ClientTimeout, "client-timeout", defaultClientTimeout, "Timeout in seconds to pause streaming when no clients are connected (0 to disable)")
	flag.IntVar(&ActivityPulseHz, "activity-hz", defaultActivityPulseHz, "Input heartbeat frequency in Hz (default 30)")
	flag.IntVar(&ActivityTimeout, "activity-timeout", defaultActivityTimeout, "Inactivity timeout in ms before stopping heartbeat (default 1500)")
	flag.BoolVar(&NVENCLatencyMode, "nvenc-latency", defaultNVENCLatencyMode, "Enable ultra-low latency NVENC optimizations (default true)")
	flag.IntVar(&SettleTime, "settle-time", defaultSettleTime, "Hybrid sharpness settle time (ms)")
	flag.IntVar(&TileSize, "tile-size", defaultTileSize, "Hybrid sharpness tile size (px)")
	flag.BoolVar(&TargetVBR, "vbr", defaultVBR, "Enable variable bitrate (encoder rate control)")
	flag.IntVar(&TargetVBRThreshold, "vbr-threshold", defaultVBRThreshold, "VBR threshold for static content")
	flag.BoolVar(&TargetDamageTracking, "damage-tracking", defaultDamageTracking, "Enable Wayland damage tracking (frame skipping)")
	flag.IntVar(&TargetCpuEffort, "cpu-effort", defaultCpuEffort, "FFmpeg CPU effort/used (default 6)")

	if flag.Parsed() {
		return
	}
	flag.Parse()

	if err := validateCaptureModeConfig(); err != nil {
		log.Fatalf("Invalid direct-buffer configuration: %v", err)
	}

	if UseNVIDIA {
		log.Printf("Checking NVIDIA GPU capabilities...")
		outAV1, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q av1_nvenc && echo true || echo false").Output()
		AV1NVENCAvailable = strings.TrimSpace(string(outAV1)) == "true"
		if AV1NVENCAvailable {
			log.Printf("AV1 NVENC support detected")
		}

		log.Printf("Checking H.264 NVENC 4:4:4 support...")
		outH264, _ := exec.Command("bash", "-c", "ffmpeg -y -f lavfi -i testsrc=size=256x256:rate=1 -t 1 -pix_fmt yuv444p -c:v h264_nvenc -profile:v high444p -tune lossless -f null - > /dev/null 2>&1 && echo true || echo false").Output()
		H264NVENC444Available = strings.TrimSpace(string(outH264)) == "true"
		if H264NVENC444Available {
			log.Printf("H.264 NVENC 4:4:4 support detected")
		}

		log.Printf("Checking H.265 NVENC 4:4:4 support...")
		outH265, _ := exec.Command("bash", "-c", "ffmpeg -y -f lavfi -i testsrc=size=256x256:rate=1 -t 1 -pix_fmt yuv444p -c:v hevc_nvenc -profile:v rext -tune lossless -f null - > /dev/null 2>&1 && echo true || echo false").Output()
		H265NVENC444Available = strings.TrimSpace(string(outH265)) == "true"
		if H265NVENC444Available {
			log.Printf("H.265 NVENC 4:4:4 support detected")
		}
	}

	if UseIntel {
		log.Printf("Checking Intel VAAPI capabilities...")
		outVAAPI, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q h264_vaapi && echo true || echo false").Output()
		VAAPIAvailable = strings.TrimSpace(string(outVAAPI)) == "true"
		if VAAPIAvailable {
			log.Printf("Intel VAAPI hardware acceleration detected")
			renderNode := resolveIntelRenderNode()
			checkCmd := fmt.Sprintf("TMP=$(mktemp /tmp/llrdc-hevc-vaapi-XXXX.hevc) && ffmpeg -hide_banner -y -f lavfi -i testsrc=size=1280x720:rate=30 -t 2 -init_hw_device vaapi=hw:%s -filter_hw_device hw -vf format=nv12,hwupload -c:v hevc_vaapi -bf 0 -b:v 5M -maxrate 5M -bufsize 10M -g 60 -profile:v main -f hevc \"$TMP\" >/dev/null 2>&1 && echo true || echo false", renderNode)
			cmd := exec.Command("bash", "-c", checkCmd)
			cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run")
			outH265VAAPI, _ := cmd.Output()
			H265VAAPIAvailable = strings.TrimSpace(string(outH265VAAPI)) == "true"
			if H265VAAPIAvailable {
				log.Printf("Intel H.265 hardware encode support detected")
			}
			outAV1VAAPI, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q av1_vaapi && echo true || echo false").Output()
			AV1VAAPIAvailable = strings.TrimSpace(string(outAV1VAAPI)) == "true"
			if AV1VAAPIAvailable {
				log.Printf("AV1 VAAPI support detected")
			}
		}
	}

	if UseIntel && (VideoCodec == "h265_vaapi" || VideoCodec == "hevc_vaapi") && !H265VAAPIAvailable {
		if CaptureMode == CaptureModeDirect {
			log.Fatalf("Invalid direct-buffer configuration: Intel H.265 hardware encode is not supported on this FFmpeg/driver stack")
		}
		log.Printf("Intel H.265 hardware encode is not supported; falling back to CPU h265")
		VideoCodec = "h265"
	}

	VideoCodec = ResolveRequestedVideoCodec(VideoCodec)
	SyncConfigToCommon()
}

func printFlag(w *os.File, name, usage string, def any) {
	fmt.Fprintf(w, "  -%s\n    \t%s (default %v)\n", name, usage, def)
}

func SyncConfigToCommon() {
	common.Port = Port
	common.FPS = FPS
	common.VideoCodec = VideoCodec
	common.Chroma = Chroma
	common.UseNVIDIA = UseNVIDIA
	common.UseIntel = UseIntel
	common.IntelRenderNode = IntelRenderNode
	common.CaptureMode = CaptureMode
	common.AV1NVENCAvailable = AV1NVENCAvailable
	common.H264NVENC444Available = H264NVENC444Available
	common.H265NVENC444Available = H265NVENC444Available
	common.VAAPIAvailable = VAAPIAvailable
	common.H265VAAPIAvailable = H265VAAPIAvailable
	common.AV1VAAPIAvailable = AV1VAAPIAvailable
	common.UseDebugFFmpeg = UseDebugFFmpeg
	common.UseDebugInput = UseDebugInput
	common.TestPattern = TestPattern
	common.EnableAudio = EnableAudio
	common.EnableClipboard = EnableClipboard
	common.AudioBitrate = AudioBitrate
	common.Wallpaper = Wallpaper
	common.ActivityPulseHz = ActivityPulseHz
	common.ActivityTimeout = ActivityTimeout
	common.NVENCLatencyMode = NVENCLatencyMode
	common.HDPI = HDPI
	common.SettleTime = SettleTime
	common.TileSize = TileSize
	common.InitialRes = InitialRes
	common.AgentAddress = AgentAddress
	common.ClientTimeout = ClientTimeout
	common.TargetBandwidthMbps = TargetBandwidthMbps
	common.TargetVBR = TargetVBR
	common.TargetVBRThreshold = TargetVBRThreshold
	common.TargetDamageTracking = TargetDamageTracking
	common.TargetCpuEffort = TargetCpuEffort
}

func ResolveRequestedVideoCodec(codec string) string {
	// Strip -444 suffix if present for resolution logic, but we'll use Chroma to decide 4:4:4 later
	baseCodec := strings.Split(codec, "-")[0]

	if UseIntel {
		if baseCodec == "h265" || baseCodec == "hevc" || baseCodec == "h265_vaapi" || baseCodec == "hevc_vaapi" {
			if H265VAAPIAvailable || baseCodec == "h265_vaapi" || baseCodec == "hevc_vaapi" {
				return "h265_vaapi"
			}
			return "h265"
		}
		if baseCodec == "h264" || baseCodec == "h264_vaapi" {
			if VAAPIAvailable || baseCodec == "h264_vaapi" {
				return "h264_vaapi"
			}
			return "h264"
		}
		if baseCodec == "av1" || baseCodec == "av1_vaapi" {
			if AV1VAAPIAvailable || baseCodec == "av1_vaapi" {
				return "av1_vaapi"
			}
			return "av1"
		}
	}

	if UseNVIDIA {
		if baseCodec == "h265" || baseCodec == "hevc" || baseCodec == "hevc_nvenc" || baseCodec == "h265_nvenc" {
			return "h265_nvenc"
		}
		if baseCodec == "h264" || baseCodec == "h264_nvenc" {
			return "h264_nvenc"
		}
		if baseCodec == "av1" || baseCodec == "av1_nvenc" {
			if AV1NVENCAvailable {
				return "av1_nvenc"
			}
		}
	}

	return baseCodec
}

func NormalizeCodecFamily(codec string) string {
	switch codec {
	case "h264", "h264_nvenc", "h264_vaapi":
		return "h264"
	case "h265", "h265_nvenc", "hevc_nvenc", "h265_vaapi", "hevc_vaapi":
		return "h265"
	case "av1", "av1_nvenc", "av1_vaapi":
		return "av1"
	default:
		return codec
	}
}
