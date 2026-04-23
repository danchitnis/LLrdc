package main

import (
	"flag"
	"log"
	"runtime"

	"github.com/danchitnis/llrdc/internal/client"
)

var clientBuildID = "dev"

func init() {
	if runtime.GOOS == "darwin" {
		// AppKit must run on the main OS thread.
		runtime.LockOSThread()
	}
}

func main() {
	log.Println("DEBUG: Client starting")

	paths := client.ResolveAppPaths("")

	serverURL := flag.String("server", "", "LLrdc server URL (e.g. http://localhost:8080)")
	controlAddr := flag.String("control-addr", "127.0.0.1:18080", "Loopback control API listen address")
	configPathFlag := flag.String("config", paths.DefaultConfigPath, "Path to YAML configuration file")
	windowTitle := flag.String("title", "LLrdc Native Client", "Native client window title")
	windowWidth := flag.Int("width", 1280, "Initial native client window width")
	windowHeight := flag.Int("height", 720, "Initial native client window height")
	fullscreen := flag.Bool("fullscreen", false, "Start the native client in fullscreen mode")
	headless := flag.Bool("headless", false, "Run without creating a native window")
	showStats := flag.Bool("stats", false, "Show stats overlay on the screen")
	defaultAutoStart := runtime.GOOS == "darwin"
	autoStart := flag.Bool("auto-start", defaultAutoStart, "Start streaming automatically without waiting for click")
	latencyProbe := flag.Bool("latency-probe", false, "Enable internal latency probe (checks center pixel brightness)")
	debugCursor := flag.Bool("debug-cursor", false, "Render a red dot at the local mouse position to visualize input latency")
	exitAfter := flag.Duration("exit-after", 0, "Exit automatically after the given duration (e.g. 5s)")
	flag.Parse()

	cfg, err := client.LoadClientConfig(*configPathFlag)
	if err != nil {
		log.Printf("failed to parse config file %s: %v", *configPathFlag, err)
	} else if *configPathFlag != "" {
		log.Printf("loaded configuration from %s", *configPathFlag)
	}

	if cfg.Resolution != nil {
		if cfg.Resolution.Width > 0 {
			*windowWidth = cfg.Resolution.Width
		}
		if cfg.Resolution.Height > 0 {
			*windowHeight = cfg.Resolution.Height
		}
	}

	var windowRenderer client.WindowRenderer
	if !*headless {
		windowRenderer, err = client.NewNativeRenderer(client.NativeRendererOptions{
			Title:        *windowTitle,
			Width:        *windowWidth,
			Height:       *windowHeight,
			AutoStart:    *autoStart,
			Fullscreen:   *fullscreen,
			ProbeLatency: *latencyProbe,
			DebugCursor:  *debugCursor,
		})
		if err != nil {
			log.Fatalf("native renderer unavailable: %v", err)
		}
	}

	app := client.NewNativeApp(client.NativeAppOptions{
		Renderer:     windowRenderer,
		ControlAddr:  *controlAddr,
		ServerURL:    *serverURL,
		ConfigPath:   *configPathFlag,
		Config:       cfg,
		Paths:        paths,
		BuildID:      clientBuildID,
		ShowStats:    *showStats,
		ExitAfter:    *exitAfter,
		LatencyProbe: *latencyProbe,
		DebugCursor:  *debugCursor,
	})

	if err := app.Run(); err != nil {
		log.Fatalf("native app failed: %v", err)
	}
}
