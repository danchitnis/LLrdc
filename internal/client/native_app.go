package client

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

func (a *NativeApp) Run() error {
	a.attachSessionHooks()
	a.startAuxiliaryTasks()
	a.startConnectLoop()

	if a.renderer != nil {
		a.attachRendererHooks()
		a.refreshOverlay()
		if err := a.renderer.Run(); err != nil {
			log.Printf("native renderer stopped with error: %v", err)
			a.requestShutdown("renderer_error")
		}
		a.requestShutdown("renderer_stopped")
	} else {
		a.scheduleReconnect()
		<-a.shutdownCh
	}

	close(a.stopConnect)
	_ = a.control.Close()
	if a.renderer != nil {
		a.renderer.Stop()
	}

	disconnectDone := make(chan struct{})
	go func() {
		_ = a.session.Disconnect()
		close(disconnectDone)
	}()
	select {
	case <-disconnectDone:
	case <-time.After(2 * time.Second):
		log.Printf("session disconnect timed out during shutdown")
	}
	return nil
}

func (a *NativeApp) Connect(serverURL string) error {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		a.mu.RLock()
		serverURL = a.desiredServerURL
		a.mu.RUnlock()
	}
	if serverURL == "" {
		return fmt.Errorf("server url is required")
	}

	a.mu.Lock()
	a.desiredServerURL = serverURL
	a.mu.Unlock()

	state := a.session.State()
	if state.Connected {
		if strings.EqualFold(strings.TrimSpace(state.ServerURL), serverURL) {
			return nil
		}
		if err := a.session.Disconnect(); err != nil {
			return err
		}
	}
	a.scheduleReconnect()
	return nil
}

func (a *NativeApp) attachSessionHooks() {
	a.session.Hooks().On(EventError, func(event EventPayload) {
		if message, ok := event.Data["error"].(string); ok {
			log.Printf("client error: %s", message)
		}
		a.refreshOverlay()
	})
	a.session.Hooks().On(EventStateChanged, func(event EventPayload) {
		if connected, ok := event.Data["connected"].(bool); ok && !connected {
			if a.session.State().ShutdownRequested {
				return
			}
			a.scheduleReconnect()
		}
		a.refreshOverlay()
	})
	a.session.Hooks().On(EventConfig, func(_ EventPayload) {
		a.refreshOverlay()
	})
	a.session.Hooks().On(EventStats, func(_ EventPayload) {
		a.refreshOverlay()
	})
	a.session.Hooks().On(EventFrame, func(_ EventPayload) {
		a.refreshOverlay()
	})
}

func (a *NativeApp) attachRendererHooks() {
	var firstPresentLogged atomic.Bool
	a.session.Hooks().On(EventInputSent, func(payload EventPayload) {
		if msgType, ok := payload.Data["type"].(string); ok && msgType == "mousemove" {
			if x, ok1 := payload.Data["x"].(float64); ok1 {
				if y, ok2 := payload.Data["y"].(float64); ok2 {
					a.renderer.UpdateMouse(x, y)
				}
			}
		}
	})

	a.renderer.SetInputSink(func(msg map[string]any) error {
		return a.handleRendererInput(msg)
	})
	a.renderer.SetLifecycleSink(func(event NativeWindowLifecycle) {
		a.session.UpdateWindowState(event)
		if event.Event == "started" {
			a.scheduleReconnect()
		}
		if event.Event == "close" {
			a.requestShutdown("window_close")
		}
		if event.Created || event.Shown || event.Mapped || event.Visible || event.Event == "close" || event.Event == "hidden" || event.Event == "unshown" || event.RenderLoopStarted {
			log.Printf("native window lifecycle: backend=%s id=%d event=%s created=%t shown=%t mapped=%t visible=%t desktop=%d loop=%t flags=%d focus=%t surface=%t awaiting_keyframe=%t", event.Backend, event.WindowID, event.Event, event.Created, event.Shown, event.Mapped, event.Visible, event.Desktop, event.RenderLoopStarted, event.Flags, event.HasFocus, event.HasSurface, event.DecoderAwaitingKeyframe)
		}
		if event.Error != "" {
			log.Printf("native window error: %s", event.Error)
		}
		a.refreshOverlay()
	})
	a.renderer.SetPresentSink(func(event NativeFramePresented) {
		a.session.RecordPresentedFrame(event)
		if firstPresentLogged.CompareAndSwap(false, true) {
			log.Printf("native frame presented: %dx%d ts=%d", event.Width, event.Height, event.PacketTimestamp)
		}
	})
}

func (a *NativeApp) startAuxiliaryTasks() {
	go func() {
		log.Printf("client control API listening on http://%s", a.opts.ControlAddr)
		if err := a.control.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("control server failed: %v", err)
		}
	}()

	if a.opts.ExitAfter > 0 {
		go func() {
			timer := time.NewTimer(a.opts.ExitAfter)
			defer timer.Stop()
			<-timer.C
			a.requestShutdown("exit_after")
		}()
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-a.shutdownCh
		signal.Stop(sigs)
	}()
	go func() {
		select {
		case <-sigs:
			a.requestShutdown("signal")
		case <-a.shutdownCh:
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-a.shutdownCh:
				return
			case <-ticker.C:
				a.refreshOverlay()
			}
		}
	}()
}

func (a *NativeApp) startConnectLoop() {
	if strings.TrimSpace(a.desiredServerURL) == "" {
		return
	}
	go func() {
		for {
			select {
			case <-a.stopConnect:
				return
			case <-a.reconnectRequests:
			}

			if !a.reconnecting.CompareAndSwap(false, true) {
				continue
			}

			for {
				select {
				case <-a.stopConnect:
					a.reconnecting.Store(false)
					return
				default:
				}

				if err := a.session.Connect(a.desiredServerURL); err != nil {
					log.Printf("connect failed: %v", err)
					time.Sleep(2 * time.Second)
					continue
				}
				log.Printf("connected to %s", a.desiredServerURL)
				a.sendInitialConfig()
				a.reconnecting.Store(false)
				a.refreshOverlay()
				break
			}
		}
	}()
}

func (a *NativeApp) sendInitialConfig() {
	configMap := make(map[string]any)
	configMap["framerate"] = a.currentFramerateValue()
	configMap["max_res"] = a.currentResolution().Value
	if hdpi := a.currentHDPIValue(); hdpi >= 0 {
		configMap["hdpi"] = hdpi
	}
	if codec := a.currentCodecValue(); codec != "" {
		configMap["videoCodec"] = codec
	}
	if len(configMap) > 0 {
		if err := a.session.SendConfig(configMap); err != nil {
			log.Printf("failed to send initial config: %v", err)
		}
	}
	if a.renderer != nil {
		width, height := a.renderer.Size()
		width, height = a.targetStreamSize(width, height)
		if err := a.session.SendResize(width, height); err != nil {
			log.Printf("initial resize failed: %v", err)
		}
	}
}

func (a *NativeApp) scheduleReconnect() {
	if strings.TrimSpace(a.desiredServerURL) == "" {
		return
	}
	select {
	case <-a.stopConnect:
		return
	default:
	}
	if a.session.State().ShutdownRequested {
		return
	}
	if a.reconnecting.Load() {
		return
	}
	select {
	case a.reconnectRequests <- struct{}{}:
	default:
	}
}

func (a *NativeApp) requestShutdown(reason string) {
	a.shutdownOnce.Do(func() {
		a.session.RequestShutdown(reason)
		close(a.shutdownCh)
	})
}

func (a *NativeApp) currentCodecValue() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.codecIndex < 0 || a.codecIndex >= len(a.codecOptions) {
		return ""
	}
	return a.codecOptions[a.codecIndex].Value
}

func (a *NativeApp) currentFramerateValue() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.framerateIndex < 0 || a.framerateIndex >= len(a.framerateOptions) {
		return 30
	}
	return a.framerateOptions[a.framerateIndex].Value
}

func (a *NativeApp) currentResolution() resolutionOption {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.resolutionIndex < 0 || a.resolutionIndex >= len(a.resolutionOpts) {
		return a.resolutionOpts[0]
	}
	return a.resolutionOpts[a.resolutionIndex]
}

func (a *NativeApp) currentHDPIValue() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.hdpiIndex < 0 || a.hdpiIndex >= len(a.hdpiOptions) {
		return 0
	}
	return a.hdpiOptions[a.hdpiIndex].Value
}

func (a *NativeApp) handleRendererInput(msg map[string]any) error {
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "resize":
		width, height := intFromAny(msg["width"]), intFromAny(msg["height"])
		if width > 0 && height > 0 {
			a.updateResolutionIndex(width, height)
			a.refreshOverlay()
			width, height = a.targetStreamSize(width, height)
			return a.session.SendResize(width, height)
		}
		return nil
	case "keydown", "keyup":
		handled, err := a.handleKeyMessage(msgType, stringFromAny(msg["key"]))
		if handled {
			return err
		}
		if a.isMenuVisible() {
			return nil
		}
	case "mousemove", "mousebtn", "wheel":
		if a.isMenuVisible() {
			return a.handleMenuPointerInput(msg)
		}
	}
	return a.session.SendInput(msg)
}

func (a *NativeApp) handleMenuPointerInput(msg map[string]any) error {
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "mousemove":
		x, okX := floatFromAny(msg["x"])
		y, okY := floatFromAny(msg["y"])
		if !okX || !okY {
			return nil
		}
		a.updateMenuHover(x, y)
		return nil
	case "mousebtn":
		x, okX := floatFromAny(msg["x"])
		y, okY := floatFromAny(msg["y"])
		if !okX || !okY {
			a.mu.RLock()
			x = a.lastMenuMouseX
			y = a.lastMenuMouseY
			a.mu.RUnlock()
		} else {
			a.updateMenuHover(x, y)
		}
		button := intFromAny(msg["button"])
		action, _ := msg["action"].(string)
		if button == 0 && action == "mouseup" {
			return a.activateMenuPointer(x, y)
		}
		return nil
	default:
		return nil
	}
}

func (a *NativeApp) handleKeyMessage(msgType, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, nil
	}

	switch msgType {
	case "keydown":
		a.setModifier(key, true)
		if a.isMenuToggleKey(key) {
			a.toggleMenu()
			return true, nil
		}
		if a.isMenuVisible() {
			switch key {
			case "Escape":
				a.dismissMenuLevel()
			case "ArrowUp":
				a.moveMenuSelection(-1)
			case "ArrowDown", "Tab":
				a.moveMenuSelection(1)
			case "ArrowLeft":
				a.collapseCurrentSubmenu()
			case "ArrowRight":
				return true, a.executeSelectedMenuItem()
			case "Enter", "Space":
				return true, a.executeSelectedMenuItem()
			}
			return true, nil
		}
	case "keyup":
		a.setModifier(key, false)
		if key == "F1" {
			return true, nil
		}
		if key == "Comma" && a.menuShortcutModifier() {
			return true, nil
		}
	}
	return false, nil
}

func (a *NativeApp) isMenuToggleKey(key string) bool {
	if key == "F1" {
		return true
	}
	return key == "Comma" && a.menuShortcutModifier()
}

func (a *NativeApp) menuShortcutModifier() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if runtime.GOOS == "darwin" {
		return a.modifiers["MetaLeft"] || a.modifiers["MetaRight"]
	}
	return a.modifiers["ControlLeft"] || a.modifiers["ControlRight"]
}

func (a *NativeApp) setModifier(key string, value bool) {
	switch key {
	case "ControlLeft", "ControlRight", "MetaLeft", "MetaRight":
		a.mu.Lock()
		a.modifiers[key] = value
		a.mu.Unlock()
	}
}

func (a *NativeApp) moveMenuSelection(delta int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := a.visibleMenuItemsLocked()
	if len(items) == 0 {
		a.menuSelected = 0
		return
	}
	idx := a.menuSelected
	for step := 0; step < len(items); step++ {
		idx = (idx + delta + len(items)) % len(items)
		if items[idx].Enabled {
			a.menuSelected = idx
			break
		}
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) updateMenuHover(x, y float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastMenuMouseX = x
	a.lastMenuMouseY = y
	if a.renderer == nil {
		return
	}
	width, height := a.renderer.Size()
	items := a.visibleMenuItemsLocked()
	idx := menuItemIndexAt(width, height, x, y, len(items))
	if idx >= 0 && idx < len(items) && items[idx].Enabled {
		a.menuSelected = idx
		a.refreshOverlayLocked()
	}
}

func (a *NativeApp) activateMenuPointer(x, y float64) error {
	a.mu.RLock()
	if a.renderer == nil {
		a.mu.RUnlock()
		return nil
	}
	width, height := a.renderer.Size()
	items := a.visibleMenuItemsLocked()
	a.mu.RUnlock()

	idx := menuItemIndexAt(width, height, x, y, len(items))
	if idx < 0 || idx >= len(items) || !items[idx].Enabled {
		return nil
	}

	a.mu.Lock()
	a.menuSelected = idx
	a.refreshOverlayLocked()
	a.mu.Unlock()
	return a.ExecuteCommand(items[idx].ID)
}

func (a *NativeApp) executeSelectedMenuItem() error {
	a.mu.RLock()
	items := a.visibleMenuItemsLocked()
	selected := a.menuSelected
	a.mu.RUnlock()
	if selected < 0 || selected >= len(items) {
		return nil
	}
	if !items[selected].Enabled {
		return nil
	}
	if items[selected].Expandable {
		return a.ExecuteCommand(items[selected].ID)
	}
	return a.ExecuteCommand(items[selected].ID)
}

func (a *NativeApp) ExecuteCommand(id string) error {
	id = strings.TrimSpace(id)
	switch id {
	case "menu.toggle":
		a.toggleMenu()
		return nil
	case "menu.up":
		a.moveMenuSelection(-1)
		return nil
	case "menu.down":
		a.moveMenuSelection(1)
		return nil
	case "menu.select":
		return a.executeSelectedMenuItem()
	case "quit":
		a.requestShutdown("menu_quit")
		return nil
	default:
		return a.executeValueCommand(id)
	}
}

func (a *NativeApp) executeValueCommand(id string) error {
	switch {
	case strings.HasSuffix(id, ".menu"):
		a.toggleSubmenu(id)
		return nil
	case strings.HasPrefix(id, "codec.set:"):
		return a.setCodec(strings.TrimPrefix(id, "codec.set:"))
	case strings.HasPrefix(id, "framerate.set:"):
		return a.setFramerate(strings.TrimPrefix(id, "framerate.set:"))
	case strings.HasPrefix(id, "resolution.set:"):
		return a.setResolution(strings.TrimPrefix(id, "resolution.set:"))
	case strings.HasPrefix(id, "hdpi.set:"):
		return a.setHDPI(strings.TrimPrefix(id, "hdpi.set:"))
	case strings.HasPrefix(id, "stats.set:"):
		return a.setStats(strings.TrimPrefix(id, "stats.set:"))
	case strings.HasPrefix(id, "latency.set:"):
		return a.setLatencyProbe(strings.TrimPrefix(id, "latency.set:"))
	case strings.HasPrefix(id, "cursor.set:"):
		return a.setDebugCursor(strings.TrimPrefix(id, "cursor.set:"))
	default:
		return fmt.Errorf("unknown command: %s", id)
	}
}

func (a *NativeApp) setCodec(value string) error {
	a.mu.Lock()
	target := -1
	for idx, option := range a.codecOptions {
		if option.Value == value {
			target = idx
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown codec option: %s", value)
	}
	a.codecIndex = target
	a.refreshOverlayLocked()
	a.mu.Unlock()

	if value != "" || a.session.State().Connected {
		if err := a.session.SendConfig(map[string]any{"videoCodec": value}); err != nil && !strings.Contains(err.Error(), "not connected") {
			return err
		}
	}
	return nil
}

func (a *NativeApp) setFramerate(value string) error {
	targetValue := intFromString(value)
	a.mu.Lock()
	target := -1
	for idx, option := range a.framerateOptions {
		if option.Value == targetValue {
			target = idx
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown framerate option: %s", value)
	}
	a.framerateIndex = target
	a.refreshOverlayLocked()
	a.mu.Unlock()

	if err := a.session.SendConfig(map[string]any{"framerate": targetValue}); err != nil && !strings.Contains(err.Error(), "not connected") {
		return err
	}
	return nil
}

func (a *NativeApp) setResolution(label string) error {
	a.mu.Lock()
	target := -1
	var preset resolutionOption
	for idx, option := range a.resolutionOpts {
		if option.Label == label {
			target = idx
			preset = option
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown resolution option: %s", label)
	}
	a.resolutionIndex = target
	a.refreshOverlayLocked()
	windowWidth := 0
	windowHeight := 0
	if a.renderer != nil {
		windowWidth, windowHeight = a.renderer.Size()
	}
	a.mu.Unlock()

	if err := a.session.SendConfig(map[string]any{"max_res": preset.Value}); err != nil && !strings.Contains(err.Error(), "not connected") {
		return err
	}
	if windowWidth > 0 && windowHeight > 0 {
		targetWidth, targetHeight := targetStreamSizeForResolution(windowWidth, windowHeight, preset.Value)
		if err := a.session.SendResize(targetWidth, targetHeight); err != nil && !strings.Contains(err.Error(), "not connected") {
			return err
		}
	}
	return nil
}

func (a *NativeApp) setHDPI(value string) error {
	targetValue := intFromString(value)
	a.mu.Lock()
	target := -1
	for idx, option := range a.hdpiOptions {
		if option.Value == targetValue {
			target = idx
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown hdpi option: %s", value)
	}
	a.hdpiIndex = target
	a.refreshOverlayLocked()
	a.mu.Unlock()

	if err := a.session.SendConfig(map[string]any{"hdpi": targetValue}); err != nil && !strings.Contains(err.Error(), "not connected") {
		return err
	}
	return nil
}

func (a *NativeApp) setStats(value string) error {
	enabled, err := parseOnOff(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.showStats = enabled
	a.refreshOverlayLocked()
	a.mu.Unlock()
	return nil
}

func (a *NativeApp) setLatencyProbe(value string) error {
	enabled, err := parseOnOff(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.latencyProbe = enabled
	a.refreshOverlayLocked()
	a.mu.Unlock()
	if a.renderer != nil {
		a.renderer.SetLatencyProbe(enabled)
	}
	return nil
}

func (a *NativeApp) setDebugCursor(value string) error {
	enabled, err := parseOnOff(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.debugCursor = enabled
	a.refreshOverlayLocked()
	a.mu.Unlock()
	if a.renderer != nil {
		a.renderer.SetDebugCursor(enabled)
	}
	return nil
}

func (a *NativeApp) updateResolutionIndex(width, height int) {
	_ = width
	_ = height
}

func (a *NativeApp) toggleSubmenu(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentSubmenu == id {
		a.currentSubmenu = ""
	} else {
		a.currentSubmenu = id
	}
	items := a.visibleMenuItemsLocked()
	a.clampMenuSelectionLocked(items)
	a.refreshOverlayLocked()
}

func (a *NativeApp) dismissMenuLevel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentSubmenu != "" {
		parentID := a.currentSubmenu
		a.currentSubmenu = ""
		items := a.visibleMenuItemsLocked()
		for idx, item := range items {
			if item.ID == parentID {
				a.menuSelected = idx
				break
			}
		}
		a.refreshOverlayLocked()
		return
	}
	a.menuVisible = false
	a.refreshOverlayLocked()
}

func (a *NativeApp) collapseCurrentSubmenu() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentSubmenu == "" {
		return
	}
	parentID := a.currentSubmenu
	a.currentSubmenu = ""
	items := a.visibleMenuItemsLocked()
	for idx, item := range items {
		if item.ID == parentID {
			a.menuSelected = idx
			break
		}
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) clampMenuSelectionLocked(items []MenuItemSnapshot) {
	if len(items) == 0 {
		a.menuSelected = 0
		return
	}
	if a.menuSelected < 0 || a.menuSelected >= len(items) || !items[a.menuSelected].Enabled {
		a.menuSelected = a.firstEnabledVisibleMenuItemLocked(items)
	}
}

func (a *NativeApp) toggleMenu() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.menuVisible = !a.menuVisible
	if a.menuVisible {
		a.currentSubmenu = ""
		a.menuSelected = a.firstEnabledVisibleMenuItemLocked(a.visibleMenuItemsLocked())
	} else {
		a.currentSubmenu = ""
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) setMenuVisible(visible bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.menuVisible = visible
	if visible {
		a.currentSubmenu = ""
		a.menuSelected = a.firstEnabledVisibleMenuItemLocked(a.visibleMenuItemsLocked())
	} else {
		a.currentSubmenu = ""
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) isMenuVisible() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.menuVisible
}

func (a *NativeApp) firstEnabledVisibleMenuItemLocked(items []MenuItemSnapshot) int {
	for idx, item := range items {
		if item.Enabled {
			return idx
		}
	}
	return 0
}

func (a *NativeApp) menuItemsLocked() []MenuItemSnapshot {
	codec := a.codecOptions[a.codecIndex]
	framerate := a.framerateOptions[a.framerateIndex]
	resolution := a.resolutionOpts[a.resolutionIndex]
	hdpi := a.hdpiOptions[a.hdpiIndex]
	return []MenuItemSnapshot{
		{ID: "codec.menu", Label: "Codec", Value: codec.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "codec.menu"},
		{ID: "framerate.menu", Label: "Frame Rate", Value: framerate.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "framerate.menu"},
		{ID: "resolution.menu", Label: "Max Resolution", Value: resolution.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "resolution.menu"},
		{ID: "hdpi.menu", Label: "HDPI", Value: hdpi.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "hdpi.menu"},
		{ID: "stats.menu", Label: "Stats HUD", Value: onOff(a.showStats), Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "stats.menu"},
		{ID: "quit", Label: "Quit", Enabled: true},
	}
}

func (a *NativeApp) visibleMenuItemsLocked() []MenuItemSnapshot {
	baseItems := a.menuItemsLocked()
	items := make([]MenuItemSnapshot, 0, len(baseItems)+12)
	for _, item := range baseItems {
		items = append(items, item)
		if !item.Expanded {
			continue
		}
		switch item.ID {
		case "codec.menu":
			for idx, option := range a.codecOptions {
				items = append(items, MenuItemSnapshot{
					ID:       "codec.set:" + option.Value,
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.codecIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "framerate.menu":
			for idx, option := range a.framerateOptions {
				items = append(items, MenuItemSnapshot{
					ID:       fmt.Sprintf("framerate.set:%d", option.Value),
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.framerateIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "resolution.menu":
			for idx, option := range a.resolutionOpts {
				items = append(items, MenuItemSnapshot{
					ID:       "resolution.set:" + option.Label,
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.resolutionIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "hdpi.menu":
			for idx, option := range a.hdpiOptions {
				items = append(items, MenuItemSnapshot{
					ID:       fmt.Sprintf("hdpi.set:%d", option.Value),
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.hdpiIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "stats.menu":
			items = append(items, a.booleanMenuItemsLocked(item.ID, "stats")...)
		}
	}
	return items
}

func (a *NativeApp) booleanMenuItemsLocked(parentID, prefix string) []MenuItemSnapshot {
	currentOn := false
	switch prefix {
	case "stats":
		currentOn = a.showStats
	case "latency":
		currentOn = a.latencyProbe
	case "cursor":
		currentOn = a.debugCursor
	}
	return []MenuItemSnapshot{
		{ID: prefix + ".set:on", Label: "On", Enabled: true, Current: currentOn, Depth: 1, ParentID: parentID},
		{ID: prefix + ".set:off", Label: "Off", Enabled: true, Current: !currentOn, Depth: 1, ParentID: parentID},
	}
}

func onOff(v bool) string {
	if v {
		return "On"
	}
	return "Off"
}

func parseOnOff(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean option: %s", value)
	}
}

func (a *NativeApp) MenuSnapshot() any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	items := a.visibleMenuItemsLocked()
	for idx := range items {
		items[idx].Selected = idx == a.menuSelected
	}
	return MenuStateSnapshot{
		Visible:       a.menuVisible,
		Title:         "LLRDC NATIVE MENU",
		Hint:          a.menuHintLocked(),
		SelectedIndex: a.menuSelected,
		Items:         items,
	}
}

func (a *NativeApp) menuHintLocked() string {
	server := a.desiredServerURL
	if server == "" {
		server = "<not set>"
	}
	modifier := "CTRL+,"
	if runtime.GOOS == "darwin" {
		modifier = "CMD+,"
	}
	return fmt.Sprintf("SERVER %s | BUILD %s | %s / F1 TOGGLE | ENTER OPEN/SELECT | LEFT BACK | ESC CLOSE", strings.ToUpper(server), strings.ToUpper(a.opts.BuildID), modifier)
}

func (a *NativeApp) overlayState() any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.overlayStateLocked()
}

func (a *NativeApp) overlayStateLocked() OverlayState {
	overlay := OverlayState{
		HUDColor: a.lastHUDColor,
	}
	if a.showStats && a.lastHUDText != "" {
		overlay.HUDLines = []string{a.lastHUDText}
	}
	if a.menuVisible {
		items := a.visibleMenuItemsLocked()
		overlay.MenuVisible = true
		overlay.MenuTitle = "LLRDC NATIVE MENU"
		overlay.MenuHint = a.menuHintLocked()
		overlay.SelectedIndex = a.menuSelected
		overlay.MenuItems = make([]string, 0, len(items))
		for idx, item := range items {
			prefix := "  "
			if idx == a.menuSelected {
				prefix = "> "
			}
			line := prefix
			if item.Depth > 0 {
				line += strings.Repeat("  ", item.Depth)
			}
			if item.Expandable {
				if item.Expanded {
					line += "[-] "
				} else {
					line += "[+] "
				}
			} else if item.Depth > 0 {
				line += "- "
			}
			line += strings.ToUpper(item.Label)
			if item.Value != "" {
				line += " [" + strings.ToUpper(item.Value) + "]"
			}
			if item.Current {
				line += " [CURRENT]"
			}
			if !item.Enabled {
				line += " (DISABLED)"
			}
			overlay.MenuItems = append(overlay.MenuItems, line)
		}
	}
	return overlay
}

func (a *NativeApp) refreshOverlay() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshOverlayLocked()
}

func (a *NativeApp) refreshOverlayLocked() {
	if a.renderer == nil {
		return
	}

	now := time.Now()
	if !a.showStats {
		a.lastHUDText = ""
		a.lastHUDColor = OverlayColor{R: 68, G: 255, B: 68, A: 255}
		a.lastHUDUpdateAt = time.Time{}
	} else if a.lastHUDUpdateAt.IsZero() || now.Sub(a.lastHUDUpdateAt) >= statsHUDRefreshInterval {
		a.lastHUDText, a.lastHUDColor = a.buildHUDLocked(now)
		a.lastHUDUpdateAt = now
	}
	a.renderer.SetOverlayState(a.overlayStateLocked())
}

func (a *NativeApp) buildHUDLocked(now time.Time) (string, OverlayColor) {
	if !a.showStats {
		return "", OverlayColor{R: 68, G: 255, B: 68, A: 255}
	}

	state := a.session.State()

	fps := rollingPresentedFPS(now, state.RecentLatencySamples)
	bwMbps := rollingVideoMbps(now, state.RecentVideoByteSamples)
	displayCodec := strings.TrimPrefix(strings.ToUpper(state.VideoCodec), "VIDEO/")
	if displayCodec == "" {
		displayCodec = "AUTO"
	}
	res := ""
	if state.LastPresentedWidth > 0 && state.LastPresentedHeight > 0 {
		res = fmt.Sprintf("%dx%d ", state.LastPresentedWidth, state.LastPresentedHeight)
	}

	avgLat := rollingLatencyMs(now, state.RecentLatencySamples)

	color := OverlayColor{R: 68, G: 255, B: 68, A: 255}
	if avgLat > 150 {
		color = OverlayColor{R: 255, G: 170, B: 68, A: 255}
	}
	if avgLat > 300 {
		color = OverlayColor{R: 255, G: 68, B: 68, A: 255}
	}

	text := fmt.Sprintf("[%s] %s%d FPS | LAT %dMS | BW %.1fMb", displayCodec, res, fps, int(avgLat), bwMbps)
	if ffmpegCPU, ok := state.LastStats["ffmpegCpu"].(float64); ok {
		text += fmt.Sprintf(" | CPU %d%%", int(ffmpegCPU))
	}
	if gpuUtil, ok := state.LastStats["intelGpuUtil"].(float64); ok && gpuUtil > 0 {
		text += fmt.Sprintf(" | ENC %d%%", int(gpuUtil))
	}
	return text, color
}

func rollingPresentedFPS(now time.Time, samples []LatencyBreakdown) uint64 {
	window := latencySamplesInWindow(now, samples)
	if len(window) == 0 {
		return 0
	}
	fps := float64(len(window)) * 1000.0 / float64(statsMetricWindowMs)
	if fps < 0 {
		return 0
	}
	return uint64(math.Round(fps))
}

func rollingVideoMbps(now time.Time, samples []TimedByteSample) float64 {
	window := byteSamplesInWindow(now, samples)
	if len(window) == 0 {
		return 0
	}

	totalBytes := 0
	for _, sample := range window {
		totalBytes += sample.Bytes
	}
	seconds := float64(statsMetricWindowMs) / 1000.0
	return float64(totalBytes) * 8 / 1024 / 1024 / seconds
}

func rollingLatencyMs(now time.Time, samples []LatencyBreakdown) float64 {
	window := latencySamplesInWindow(now, samples)
	if len(window) == 0 {
		return 0
	}

	sum := 0.0
	count := 0
	for _, sample := range window {
		if sample.PresentationAt <= 0 || sample.ReceiveAt <= 0 || sample.PresentationAt < sample.ReceiveAt {
			continue
		}
		sum += float64(sample.PresentationAt - sample.ReceiveAt)
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func latencySamplesInWindow(now time.Time, samples []LatencyBreakdown) []LatencyBreakdown {
	if len(samples) == 0 {
		return nil
	}
	cutoff := now.UnixMilli() - statsMetricWindowMs
	start := 0
	for start < len(samples) && samples[start].PresentationAt <= cutoff {
		start++
	}
	return samples[start:]
}

func byteSamplesInWindow(now time.Time, samples []TimedByteSample) []TimedByteSample {
	if len(samples) == 0 {
		return nil
	}
	cutoff := now.UnixMilli() - statsMetricWindowMs
	start := 0
	for start < len(samples) && samples[start].AtMs <= cutoff {
		start++
	}
	return samples[start:]
}

func (a *NativeApp) CaptureSnapshotPNG() ([]byte, error) {
	if a.renderer == nil {
		return nil, errors.New("native renderer is unavailable")
	}
	return a.renderer.CaptureSnapshotPNG()
}

func intFromAny(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return 0
	}
}

func (a *NativeApp) targetStreamSize(width, height int) (int, int) {
	a.mu.RLock()
	maxRes := a.currentResolution().Value
	a.mu.RUnlock()
	return targetStreamSizeForResolution(width, height, maxRes)
}

func targetStreamSizeForResolution(width, height, maxRes int) (int, int) {
	if width <= 0 || height <= 0 {
		return width, height
	}
	if maxRes > 0 {
		ratio := float64(width) / float64(height)
		height = maxRes
		width = int(math.Round(float64(height) * ratio))
		if ratio > 1.2 {
			switch maxRes {
			case 720:
				width = 1280
			case 1080:
				width = 1920
			case 1440:
				width = 2560
			case 2160:
				width = 3840
			}
		}
		return width, height
	}
	if height > 2160 {
		ratio := 2160.0 / float64(height)
		height = 2160
		width = int(math.Round(float64(width) * ratio))
	}
	return width, height
}

func floatFromAny(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func stringFromAny(v any) string {
	if value, ok := v.(string); ok {
		return value
	}
	return ""
}

func intFromString(v string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(v))
	return value
}
