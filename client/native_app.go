package client

import (
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	statsHUDRefreshInterval = time.Second
	statsMetricWindow       = statsHUDRefreshInterval
	statsMetricWindowMs     = int64(statsMetricWindow / time.Millisecond)
)

type ClientConfig struct {
	Resolution *struct {
		Width  int `yaml:"width"`
		Height int `yaml:"height"`
	} `yaml:"resolution"`
	FPS        *int    `yaml:"fps"`
	Codec      *string `yaml:"codec"`
	VideoCodec *string `yaml:"videoCodec"`
	DPI        *int    `yaml:"dpi"`
	Bitrate    *int    `yaml:"bitrate"`
}

func LoadClientConfig(path string) (ClientConfig, error) {
	var cfg ClientConfig
	path = strings.TrimSpace(path)
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

type NativeAppOptions struct {
	Renderer     WindowRenderer
	ControlAddr  string
	ServerURL    string
	ConfigPath   string
	Config       ClientConfig
	Paths        AppPaths
	BuildID      string
	ShowStats    bool
	ExitAfter    time.Duration
	LatencyProbe bool
	DebugCursor  bool
}

type codecOption struct {
	Label  string
	Value  string
	Chroma string
}

type resolutionOption struct {
	Label string
	Value int
}

type hdpiOption struct {
	Label string
	Value int
}

type framerateOption struct {
	Label string
	Value int
}

type bitrateOption struct {
	Label string
	Value int
}

type MenuItemSnapshot struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Value      string `json:"value,omitempty"`
	Enabled    bool   `json:"enabled"`
	Selected   bool   `json:"selected"`
	Current    bool   `json:"current,omitempty"`
	Depth      int    `json:"depth,omitempty"`
	Expandable bool   `json:"expandable,omitempty"`
	Expanded   bool   `json:"expanded,omitempty"`
	ParentID   string `json:"parentId,omitempty"`
}

type MenuStateSnapshot struct {
	Visible       bool               `json:"visible"`
	Title         string             `json:"title,omitempty"`
	Hint          string             `json:"hint,omitempty"`
	SelectedIndex int                `json:"selectedIndex"`
	Items         []MenuItemSnapshot `json:"items,omitempty"`
}

type NativeApp struct {
	opts     NativeAppOptions
	renderer WindowRenderer
	session  *Session
	control  *ControlServer

	mu               sync.RWMutex
	desiredServerURL string
	showStats        bool
	latencyProbe     bool
	debugCursor      bool
	menuVisible      bool
	menuSelected     int
	currentSubmenu   string
	modifiers        map[string]bool
	codecOptions     []codecOption
	codecIndex       int
	framerateOptions []framerateOption
	framerateIndex   int
	bitrateOptions   []bitrateOption
	bitrateIndex     int
	resolutionOpts   []resolutionOption
	resolutionIndex  int
	hdpiOptions      []hdpiOption
	hdpiIndex        int
	lastHUDText      string
	lastHUDColor     OverlayColor
	lastHUDUpdateAt  time.Time
	lastMenuMouseX   float64
	lastMenuMouseY   float64

	reconnectRequests chan struct{}
	stopConnect       chan struct{}
	shutdownCh        chan struct{}
	shutdownOnce      sync.Once
	reconnecting      atomic.Bool
}

func (a *NativeApp) buildCodecOptions() []codecOption {
	baseOptions := defaultCodecOptions()
	if a.renderer == nil {
		return baseOptions
	}

	provider, ok := a.renderer.(SupportedVideoCodecsProvider)
	if !ok {
		return baseOptions
	}
	supported := provider.SupportedVideoCodecs()
	if len(supported) == 0 {
		return baseOptions
	}

	a.session.mu.RLock()
	lastConfig := a.session.state.LastConfig
	a.session.mu.RUnlock()

	var qsv, nvenc bool
	if lastConfig != nil {
		qsv, _ = lastConfig["qsvAvailable"].(bool)
		nvenc, _ = lastConfig["nvidiaAvailable"].(bool)
	} else {
		// If server config is not available, assume all hardware codecs are possible
		// to allow initial configuration from YAML to be respected.
		qsv = true
		nvenc = true
	}

	filtered := make([]codecOption, 0, len(baseOptions))
	for _, opt := range baseOptions {
		val := strings.ToLower(opt.Value)

		// Check renderer support by stripping hardware suffixes
		baseCodec := val
		if idx := strings.Index(baseCodec, "_"); idx > 0 {
			baseCodec = baseCodec[:idx]
		}

		rendererSupports := false
		for _, s := range supported {
			if baseCodec == strings.ToLower(s) {
				rendererSupports = true
				break
			}
		}
		if !rendererSupports {
			continue
		}

		// Check server hardware availability
		isNVENC := strings.HasSuffix(val, "_nvenc")
		isQSV := strings.HasSuffix(val, "_qsv")
		isVAAPI := strings.HasSuffix(val, "_vaapi")

		shouldShow := false
		if !isNVENC && !isQSV && !isVAAPI {
			shouldShow = true // CPU fallback is always an option if renderer supports it
		} else if isNVENC {
			shouldShow = nvenc
		} else if isQSV || isVAAPI {
			shouldShow = qsv
		}

		if shouldShow {
			filtered = append(filtered, opt)
		}
	}
	return filtered
}

func NewNativeApp(opts NativeAppOptions) *NativeApp {
	if opts.Paths.ExecutablePath == "" {
		opts.Paths = ResolveAppPaths("")
	}

	app := &NativeApp{
		opts:              opts,
		renderer:          opts.Renderer,
		session:           NewSession(opts.Renderer),
		desiredServerURL:  strings.TrimSpace(opts.ServerURL),
		showStats:         opts.ShowStats,
		latencyProbe:      opts.LatencyProbe,
		debugCursor:       opts.DebugCursor,
		modifiers:         make(map[string]bool),
		codecOptions:      defaultCodecOptions(),
		framerateOptions:  defaultFramerateOptions(),
		bitrateOptions:    defaultBitrateOptions(),
		resolutionOpts:    defaultResolutionOptions(),
		hdpiOptions:       defaultHDPIOptions(),
		reconnectRequests: make(chan struct{}, 1),
		stopConnect:       make(chan struct{}),
		shutdownCh:        make(chan struct{}),
	}

	app.codecOptions = app.buildCodecOptions()
	app.codecIndex = app.codecIndexForConfig()
	app.framerateIndex = app.framerateIndexForConfig()
	app.bitrateIndex = app.bitrateIndexForConfig()
	app.resolutionIndex = app.resolutionIndexForRenderer()
	app.hdpiIndex = app.hdpiIndexForConfig()
	app.lastHUDColor = OverlayColor{R: 68, G: 255, B: 68, A: 255}

	app.session.SetBuildID(opts.BuildID)
	app.session.ClearShutdown()

	if app.renderer != nil {
		app.renderer.SetLatencyProbe(app.latencyProbe)
		app.renderer.SetDebugCursor(app.debugCursor)
	}

	app.control = NewControlServer(opts.ControlAddr, app.session, &ControlHooks{
		GetMenuState:    app.MenuSnapshot,
		Connect:         app.Connect,
		ExecuteCommand:  app.ExecuteCommand,
		CaptureSnapshot: app.CaptureSnapshotPNG,
		GetOverlayState: app.overlayState,
	})

	return app
}

func defaultCodecOptions() []codecOption {
	return []codecOption{
		{Label: "VP8", Value: "vp8"},
		{Label: "H.264 (CPU)", Value: "h264"},
		{Label: "H.264 (4:4:4)", Value: "h264-444", Chroma: "444"},
		{Label: "H.264 (NVIDIA 4:4:4)", Value: "h264_nvenc", Chroma: "444"},
		{Label: "H.264 (Intel)", Value: "h264_qsv"},
		{Label: "H.265 (CPU)", Value: "h265"},
		{Label: "H.265 (4:4:4)", Value: "h265-444", Chroma: "444"},
		{Label: "H.265 (NVIDIA 4:4:4)", Value: "h265_nvenc", Chroma: "444"},
		{Label: "HEVC 4:4:4 (Intel)", Value: "h265_qsv-444", Chroma: "444"},
		{Label: "AV1 (CPU)", Value: "av1"},
		{Label: "AV1 (NVIDIA NVENC)", Value: "av1_nvenc"},
	}
}

func defaultResolutionOptions() []resolutionOption {
	return []resolutionOption{
		{Label: "Responsive", Value: 0},
		{Label: "720p", Value: 720},
		{Label: "1080p", Value: 1080},
		{Label: "2K", Value: 1440},
		{Label: "4K", Value: 2160},
	}
}

func defaultFramerateOptions() []framerateOption {
	return []framerateOption{
		{Label: "15 FPS", Value: 15},
		{Label: "30 FPS", Value: 30},
		{Label: "60 FPS", Value: 60},
		{Label: "90 FPS", Value: 90},
		{Label: "120 FPS", Value: 120},
	}
}

func defaultBitrateOptions() []bitrateOption {
	return []bitrateOption{
		{Label: "1 Mbps", Value: 1},
		{Label: "2 Mbps", Value: 2},
		{Label: "5 Mbps", Value: 5},
		{Label: "10 Mbps", Value: 10},
		{Label: "20 Mbps", Value: 20},
		{Label: "50 Mbps", Value: 50},
		{Label: "100 Mbps", Value: 100},
	}
}

func (a *NativeApp) bitrateIndexForConfig() int {
	value := 10
	if a.opts.Config.Bitrate != nil {
		value = *a.opts.Config.Bitrate
	}
	for i, opt := range a.bitrateOptions {
		if opt.Value == value {
			return i
		}
	}
	return 3 // Default to 10 Mbps (index 3)
}

func (a *NativeApp) currentBitrateValue() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.bitrateIndex >= 0 && a.bitrateIndex < len(a.bitrateOptions) {
		return a.bitrateOptions[a.bitrateIndex].Value
	}
	return 5
}

func defaultHDPIOptions() []hdpiOption {
	return []hdpiOption{
		{Label: "100%", Value: 100},
		{Label: "125%", Value: 125},
		{Label: "150%", Value: 150},
		{Label: "175%", Value: 175},
		{Label: "200%", Value: 200},
	}
}

func (a *NativeApp) codecIndexForConfig() int {
	cfgCodec := a.opts.Config.Codec
	if cfgCodec == nil {
		cfgCodec = a.opts.Config.VideoCodec
	}
	if cfgCodec != nil {
		value := strings.TrimSpace(strings.ToLower(*cfgCodec))
		for idx, option := range a.codecOptions {
			if option.Value == value {
				return idx
			}
		}
	}

	if provider, ok := a.session.renderer.(PreferredVideoCodecProvider); ok {
		preferred := strings.TrimSpace(strings.ToLower(provider.PreferredVideoCodec()))
		if preferred != "" {
			for idx, option := range a.codecOptions {
				if option.Value == preferred {
					return idx
				}
			}
		}
	}
	return 0
}

func (a *NativeApp) framerateIndexForConfig() int {
	value := 30
	if a.opts.Config.FPS != nil {
		value = *a.opts.Config.FPS
	}
	for idx, option := range a.framerateOptions {
		if option.Value == value {
			return idx
		}
	}
	return 1
}

func (a *NativeApp) resolutionIndexForRenderer() int {
	return 0
}

func (a *NativeApp) hdpiIndexForConfig() int {
	value := 200
	if a.opts.Config.DPI != nil {
		value = *a.opts.Config.DPI
	}
	for idx, option := range a.hdpiOptions {
		if option.Value == value {
			return idx
		}
	}
	return 0
}
