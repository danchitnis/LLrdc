package client

import (
	"testing"
	"time"
)

type testWindowRenderer struct {
	width        int
	height       int
	overlay      OverlayState
	latencyProbe bool
	debugCursor  bool
}

func (r *testWindowRenderer) HandleVideoFrame(string, []byte, uint32) error { return nil }
func (r *testWindowRenderer) RequestKeyframe()                              {}
func (r *testWindowRenderer) Close() error                                  { return nil }
func (r *testWindowRenderer) Run() error                                    { return nil }
func (r *testWindowRenderer) Stop()                                         {}
func (r *testWindowRenderer) SetInputSink(func(map[string]any) error)       {}
func (r *testWindowRenderer) SetLifecycleSink(func(NativeWindowLifecycle))  {}
func (r *testWindowRenderer) UpdateMouse(float64, float64)                  {}
func (r *testWindowRenderer) SetPresentSink(func(NativeFramePresented))     {}
func (r *testWindowRenderer) SetOverlayState(state OverlayState)            { r.overlay = state }
func (r *testWindowRenderer) SetLatencyProbe(enabled bool)                  { r.latencyProbe = enabled }
func (r *testWindowRenderer) SetDebugCursor(enabled bool)                   { r.debugCursor = enabled }
func (r *testWindowRenderer) SetWindowSize(width, height int) error {
	r.width = width
	r.height = height
	return nil
}
func (r *testWindowRenderer) CaptureSnapshotPNG() ([]byte, error) { return []byte("png"), nil }
func (r *testWindowRenderer) Size() (int, int)                    { return r.width, r.height }

func TestNativeAppMenuCommandsUpdateRendererState(t *testing.T) {
	t.Parallel()

	renderer := &testWindowRenderer{width: 1280, height: 720}
	app := NewNativeApp(NativeAppOptions{
		Renderer:     renderer,
		ControlAddr:  "127.0.0.1:0",
		ShowStats:    true,
		LatencyProbe: false,
		DebugCursor:  false,
		BuildID:      "test",
	})

	if got := app.currentFramerateValue(); got != 30 {
		t.Fatalf("expected initial framerate 30, got %d", got)
	}

	if err := app.ExecuteCommand("framerate.set:60"); err != nil {
		t.Fatalf("set framerate: %v", err)
	}
	if got := app.currentFramerateValue(); got != 60 {
		t.Fatalf("expected framerate 60, got %d", got)
	}

	if got := app.currentResolution().Value; got != 0 {
		t.Fatalf("expected initial max resolution to remain browser-aligned responsive until explicitly set, got %d", got)
	}

	if err := app.ExecuteCommand("resolution.set:1080p"); err != nil {
		t.Fatalf("set max resolution: %v", err)
	}
	if got := app.currentResolution().Value; got != 1080 {
		t.Fatalf("expected max resolution 1080, got %d", got)
	}

	if err := app.ExecuteCommand("hdpi.set:150"); err != nil {
		t.Fatalf("set hdpi: %v", err)
	}

	if err := app.ExecuteCommand("menu.toggle"); err != nil {
		t.Fatalf("toggle menu: %v", err)
	}
	menu := app.MenuSnapshot().(MenuStateSnapshot)
	if !menu.Visible {
		t.Fatalf("expected menu to be visible")
	}
	if len(renderer.overlay.MenuItems) == 0 {
		t.Fatalf("expected overlay menu items to be rendered")
	}
	if err := app.ExecuteCommand("hdpi.menu"); err != nil {
		t.Fatalf("open hdpi submenu: %v", err)
	}
	menu = app.MenuSnapshot().(MenuStateSnapshot)
	foundFramerate := false
	foundDebugMenu := false
	foundLatencyMenu := false
	foundHDPI := false
	foundHDPIOption := false
	foundConnect := false
	foundDisconnect := false
	foundReconnect := false
	for _, item := range menu.Items {
		if item.ID == "connect" {
			foundConnect = true
		}
		if item.ID == "disconnect" {
			foundDisconnect = true
		}
		if item.ID == "reconnect" {
			foundReconnect = true
		}
		if item.ID == "framerate.menu" {
			foundFramerate = true
			if item.Value != "60 FPS" {
				t.Fatalf("expected framerate menu value 60 FPS, got %q", item.Value)
			}
		}
		if item.ID == "cursor.menu" {
			foundDebugMenu = true
		}
		if item.ID == "latency.menu" {
			foundLatencyMenu = true
		}
		if item.ID == "hdpi.menu" {
			foundHDPI = true
			if !item.Expanded {
				t.Fatalf("expected hdpi menu to be expanded")
			}
		}
		if item.ID == "hdpi.set:150" {
			foundHDPIOption = true
			if !item.Current {
				t.Fatalf("expected selected hdpi option to be marked current")
			}
		}
	}
	if !foundHDPI {
		t.Fatalf("expected hdpi menu item to be present")
	}
	if !foundHDPIOption {
		t.Fatalf("expected hdpi submenu option to be visible")
	}
	if !foundFramerate {
		t.Fatalf("expected framerate menu item to be present")
	}
	if foundConnect {
		t.Fatalf("did not expect connect menu item to be present")
	}
	if foundDisconnect {
		t.Fatalf("did not expect disconnect menu item to be present")
	}
	if foundReconnect {
		t.Fatalf("did not expect reconnect menu item to be present")
	}
	if foundDebugMenu {
		t.Fatalf("did not expect debug cursor menu item to be present")
	}
	if foundLatencyMenu {
		t.Fatalf("did not expect latency probe menu item to be present")
	}
}

func TestNativeAppMenuPointerSelectsResolutionOption(t *testing.T) {
	t.Parallel()

	renderer := &testWindowRenderer{width: 1280, height: 720}
	app := NewNativeApp(NativeAppOptions{
		Renderer:    renderer,
		ControlAddr: "127.0.0.1:0",
		BuildID:     "test",
	})

	if err := app.ExecuteCommand("menu.toggle"); err != nil {
		t.Fatalf("toggle menu: %v", err)
	}

	clickItem := func(targetID string) {
		t.Helper()

		menu := app.MenuSnapshot().(MenuStateSnapshot)
		targetIndex := -1
		for idx, item := range menu.Items {
			if item.ID == targetID {
				targetIndex = idx
				break
			}
		}
		if targetIndex < 0 {
			t.Fatalf("menu item %q not found", targetID)
		}

		layout := computeMenuLayout(renderer.width, renderer.height, len(menu.Items))
		x := float64(layout.panelX+layout.panelW/2) / float64(renderer.width)
		y := float64(layout.panelY+layout.itemsStart+targetIndex*layout.itemHeight+layout.itemHeight/2) / float64(renderer.height)

		if err := app.handleRendererInput(map[string]any{
			"type": "mousemove",
			"x":    x,
			"y":    y,
		}); err != nil {
			t.Fatalf("move to %s: %v", targetID, err)
		}
		if err := app.handleRendererInput(map[string]any{
			"type":   "mousebtn",
			"button": 0,
			"action": "mouseup",
			"x":      x,
			"y":      y,
		}); err != nil {
			t.Fatalf("click %s: %v", targetID, err)
		}
	}

	clickItem("resolution.menu")

	menu := app.MenuSnapshot().(MenuStateSnapshot)
	expanded := false
	for _, item := range menu.Items {
		if item.ID == "resolution.menu" {
			expanded = item.Expanded
			break
		}
	}
	if !expanded {
		t.Fatalf("expected resolution menu to expand on click")
	}

	clickItem("resolution.set:1080p")

	if got := app.currentResolution().Value; got != 1080 {
		t.Fatalf("expected max resolution 1080 after pointer selection, got %d", got)
	}
}

func TestTargetStreamSizeForResolutionMatchesBrowserBehavior(t *testing.T) {
	t.Parallel()

	width, height := targetStreamSizeForResolution(1512, 982, 1080)
	if width != 1920 || height != 1080 {
		t.Fatalf("expected 1080p selection to snap to 1920x1080, got %dx%d", width, height)
	}

	width, height = targetStreamSizeForResolution(5120, 2880, 0)
	if width != 3840 || height != 2160 {
		t.Fatalf("expected responsive mode to cap 5K down to 3840x2160, got %dx%d", width, height)
	}
}

func TestRollingPresentedFPSUsesRecentWindow(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(10_000)
	samples := make([]LatencyBreakdown, 0, 180)
	for ts := int64(5_000); ts <= 7_900; ts += 33 {
		samples = append(samples, LatencyBreakdown{PresentationAt: ts})
	}
	for ts := int64(9_000); ts <= 10_000; ts += 67 {
		samples = append(samples, LatencyBreakdown{PresentationAt: ts})
	}

	fps := rollingPresentedFPS(now, samples)
	if fps < 14 || fps > 16 {
		t.Fatalf("expected rolling fps near 15, got %d", fps)
	}
}

func TestRollingPresentedFPSReturnsZeroWithoutEnoughRecentSamples(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(10_000)
	samples := []LatencyBreakdown{
		{PresentationAt: 7_500},
	}

	if fps := rollingPresentedFPS(now, samples); fps != 0 {
		t.Fatalf("expected zero fps with no frames in the last second, got %d", fps)
	}
}

func TestRollingVideoMbpsUsesRecentWindow(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(10_000)
	samples := []TimedByteSample{
		{AtMs: 7_500, Bytes: 500_000},
		{AtMs: 9_100, Bytes: 125_000},
		{AtMs: 9_500, Bytes: 125_000},
		{AtMs: 10_000, Bytes: 125_000},
	}

	mbps := rollingVideoMbps(now, samples)
	if mbps < 2.8 || mbps > 2.9 {
		t.Fatalf("expected rolling bandwidth near 2.86 Mbps, got %.2f", mbps)
	}
}

func TestRollingVideoMbpsReturnsZeroWithoutEnoughRecentSamples(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(10_000)
	samples := []TimedByteSample{{AtMs: 9_900, Bytes: 1024}}

	if mbps := rollingVideoMbps(now, samples); mbps == 0 {
		t.Fatalf("expected bandwidth from bytes observed in the last second, got %.2f", mbps)
	}
}

func TestRollingLatencyMsUsesRecentWindow(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(10_000)
	samples := []LatencyBreakdown{
		{ReceiveAt: 7_500, PresentationAt: 7_560},
		{ReceiveAt: 9_200, PresentationAt: 9_250},
		{ReceiveAt: 9_600, PresentationAt: 9_670},
		{ReceiveAt: 9_900, PresentationAt: 9_990},
	}

	latency := rollingLatencyMs(now, samples)
	if latency < 69 || latency > 71 {
		t.Fatalf("expected rolling latency near 70ms, got %.2f", latency)
	}
}

func TestRefreshOverlayLockedThrottlesHUDToOneSecond(t *testing.T) {
	t.Parallel()

	renderer := &testWindowRenderer{width: 1280, height: 720}
	app := NewNativeApp(NativeAppOptions{
		Renderer:    renderer,
		ControlAddr: "127.0.0.1:0",
		ShowStats:   true,
		BuildID:     "test",
	})

	app.lastHUDText = "OLD HUD"
	app.lastHUDColor = OverlayColor{R: 1, G: 2, B: 3, A: 255}
	app.lastHUDUpdateAt = time.Now()
	app.refreshOverlayLocked()
	if got := renderer.overlay.HUDLines; len(got) != 1 || got[0] != "OLD HUD" {
		t.Fatalf("expected throttled HUD to keep cached text, got %#v", got)
	}

	app.lastHUDUpdateAt = time.Now().Add(-1100 * time.Millisecond)
	app.refreshOverlayLocked()
	if got := renderer.overlay.HUDLines; len(got) != 1 || got[0] == "OLD HUD" {
		t.Fatalf("expected HUD to refresh after throttle window, got %#v", got)
	}
}

func TestOverlayStatePreservesHUDTextCase(t *testing.T) {
	t.Parallel()

	app := NewNativeApp(NativeAppOptions{
		ControlAddr: "127.0.0.1:0",
		ShowStats:   true,
		BuildID:     "test",
	})

	app.lastHUDText = "[h264] 1920x1080 60 FPS | LAT 12MS | BW 8.5Mb"
	overlay := app.overlayStateLocked()
	if len(overlay.HUDLines) != 1 || overlay.HUDLines[0] != app.lastHUDText {
		t.Fatalf("expected HUD text case to be preserved, got %#v", overlay.HUDLines)
	}
}
