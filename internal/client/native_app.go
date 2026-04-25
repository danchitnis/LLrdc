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
	FPS   *int    `yaml:"fps"`
	Codec *string `yaml:"codec"`
	DPI   *int    `yaml:"dpi"`
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
	Label string
	Value string
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

func (a *NativeApp) filterCodecOptions(options []codecOption) []codecOption {
	if a.renderer == nil {
		return options
	}
	provider, ok := a.renderer.(SupportedVideoCodecsProvider)
	if !ok {
		return options
	}
	supported := provider.SupportedVideoCodecs()
	if len(supported) == 0 {
		return options
	}

	filtered := make([]codecOption, 0, len(options))
	for _, opt := range options {
		val := strings.ToLower(opt.Value)
		for _, s := range supported {
			if val == strings.ToLower(s) {
				filtered = append(filtered, opt)
				break
			}
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
		resolutionOpts:    defaultResolutionOptions(),
		hdpiOptions:       defaultHDPIOptions(),
		reconnectRequests: make(chan struct{}, 1),
		stopConnect:       make(chan struct{}),
		shutdownCh:        make(chan struct{}),
	}

	app.codecOptions = app.filterCodecOptions(app.codecOptions)
	app.codecIndex = app.codecIndexForConfig()
	app.framerateIndex = app.framerateIndexForConfig()
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
		{Label: "H.264 (NVIDIA NVENC)", Value: "h264_nvenc"},
		{Label: "H.264 (Intel QSV)", Value: "h264_qsv"},
		{Label: "H.265 (CPU)", Value: "h265"},
		{Label: "H.265 (NVIDIA NVENC)", Value: "h265_nvenc"},
		{Label: "H.265 (Intel QSV)", Value: "h265_qsv"},
		{Label: "AV1 (CPU)", Value: "av1"},
		{Label: "AV1 (NVIDIA NVENC)", Value: "av1_nvenc"},
		{Label: "AV1 (Intel QSV)", Value: "av1_qsv"},
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
	if a.opts.Config.Codec != nil {
		value := strings.TrimSpace(strings.ToLower(*a.opts.Config.Codec))
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
