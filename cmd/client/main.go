package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/danchitnis/llrdc/internal/client"
	"gopkg.in/yaml.v3"
)

var clientBuildID = "dev"

type ClientConfig struct {
	Resolution *struct {
		Width  int `yaml:"width"`
		Height int `yaml:"height"`
	} `yaml:"resolution"`
	FPS   *int    `yaml:"fps"`
	Codec *string `yaml:"codec"`
	DPI   *int    `yaml:"dpi"`
}

func init() {
	if runtime.GOOS == "darwin" {
		// AppKit must run on the main OS thread.
		runtime.LockOSThread()
	}
}

func main() {
	log.Println("DEBUG: Client starting")

	defaultConfigPath := "config.yaml"
	if exePath, err := os.Executable(); err == nil {
		if strings.HasSuffix(filepath.Dir(exePath), "Contents/MacOS") {
			// Running inside LLrdc.app/Contents/MacOS/client
			// Move up to LLrdc.app level for the config
			defaultConfigPath = filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(exePath))), "config.yaml")
		} else {
			defaultConfigPath = filepath.Join(filepath.Dir(exePath), "config.yaml")
		}
	}

	serverURL := flag.String("server", "", "LLrdc server URL (e.g. http://localhost:8080)")
	controlAddr := flag.String("control-addr", "127.0.0.1:18080", "Loopback control API listen address")
	configPathFlag := flag.String("config", defaultConfigPath, "Path to YAML configuration file")
	windowTitle := flag.String("title", "LLrdc Native Client", "Native client window title")
	windowWidth := flag.Int("width", 1280, "Initial native client window width")
	windowHeight := flag.Int("height", 720, "Initial native client window height")
	headless := flag.Bool("headless", false, "Run without creating a native window")
	showStats := flag.Bool("stats", false, "Show stats overlay on the screen")
	defaultAutoStart := runtime.GOOS == "darwin"
	autoStart := flag.Bool("auto-start", defaultAutoStart, "Start streaming automatically without waiting for click")
	latencyProbe := flag.Bool("latency-probe", false, "Enable internal latency probe (checks center pixel brightness)")
	debugCursor := flag.Bool("debug-cursor", false, "Render a red dot at the local mouse position to visualize input latency")
	exitAfter := flag.Duration("exit-after", 0, "Exit automatically after the given duration (e.g. 5s)")
	flag.Parse()

	var clientConfig ClientConfig
	if *configPathFlag != "" {
		if data, err := os.ReadFile(*configPathFlag); err == nil {
			if err := yaml.Unmarshal(data, &clientConfig); err != nil {
				log.Printf("failed to parse config file %s: %v", *configPathFlag, err)
			} else {
				log.Printf("loaded configuration from %s", *configPathFlag)
			}
		}
	}

	// Apply configuration overrides
	if clientConfig.Resolution != nil {
		if clientConfig.Resolution.Width > 0 {
			log.Printf("Config: setting window width to %d", clientConfig.Resolution.Width)
			*windowWidth = clientConfig.Resolution.Width
		}
		if clientConfig.Resolution.Height > 0 {
			log.Printf("Config: setting window height to %d", clientConfig.Resolution.Height)
			*windowHeight = clientConfig.Resolution.Height
		}
	}

	var renderer client.Renderer = client.NullRenderer{}
	var windowRenderer client.WindowRenderer
	var err error
	if !*headless {
		windowRenderer, err = client.NewNativeRenderer(client.NativeRendererOptions{
			Title:        *windowTitle,
			Width:        *windowWidth,
			Height:       *windowHeight,
			AutoStart:    *autoStart,
			ProbeLatency: *latencyProbe,
			DebugCursor:  *debugCursor,
		})
		if err != nil {
			log.Fatalf("native renderer unavailable: %v", err)
		}
		renderer = windowRenderer
	}

	session := client.NewSession(renderer)
	session.SetBuildID(clientBuildID)
	session.ClearShutdown()
	desiredServerURL := strings.TrimSpace(*serverURL)
	reconnectRequests := make(chan struct{}, 1)
	stopConnect := make(chan struct{})
	shutdownCh := make(chan struct{})
	var shutdownOnce sync.Once
	requestShutdown := func(reason string) {
		shutdownOnce.Do(func() {
			session.RequestShutdown(reason)
			close(shutdownCh)
		})
	}
	var reconnecting atomic.Bool
	scheduleReconnect := func() {
		if desiredServerURL == "" {
			return
		}
		select {
		case <-stopConnect:
			return
		default:
		}
		if session.State().ShutdownRequested {
			return
		}
		if reconnecting.Load() {
			return
		}
		select {
		case reconnectRequests <- struct{}{}:
		default:
		}
	}

	controlServer := client.NewControlServer(*controlAddr, session)
	startAuxiliaryTasks := func() {
		session.Hooks().On(client.EventError, func(event client.EventPayload) {
			if message, ok := event.Data["error"].(string); ok {
				log.Printf("client error: %s", message)
			}
		})
		session.Hooks().On(client.EventStateChanged, func(event client.EventPayload) {
			if connected, ok := event.Data["connected"].(bool); ok && !connected {
				if session.State().ShutdownRequested {
					return
				}
				scheduleReconnect()
			}
		})

		go func() {
			log.Printf("client control API listening on http://%s", *controlAddr)
			if err := controlServer.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
				log.Fatalf("control server failed: %v", err)
			}
		}()

		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-shutdownCh
			signal.Stop(sigs)
		}()
		if *exitAfter > 0 {
			go func() {
				timer := time.NewTimer(*exitAfter)
				defer timer.Stop()
				<-timer.C
				requestShutdown("exit_after")
			}()
		}
		go func() {
			select {
			case <-sigs:
				requestShutdown("signal")
			case <-shutdownCh:
			}
		}()

		if *showStats && windowRenderer != nil {
			go func() {
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()

				var lastFrames uint64
				var lastBytes uint64

				for {
					select {
					case <-shutdownCh:
						return
					case <-ticker.C:
						stats := session.Stats()
						state := session.State()

						fps := stats.PresentedFrames - lastFrames
						lastFrames = stats.PresentedFrames

						bwMbps := float64(stats.VideoBytes-lastBytes) * 8 / 1024 / 1024
						lastBytes = stats.VideoBytes

						codec := state.VideoCodec
						displayCodec := strings.TrimPrefix(strings.ToLower(codec), "video/")
						displayCodec = strings.ReplaceAll(displayCodec, "h264", "H264")
						displayCodec = strings.ReplaceAll(displayCodec, "h265", "H265")
						displayCodec = strings.ReplaceAll(displayCodec, "av1", "AV1")
						displayCodec = strings.ReplaceAll(displayCodec, "vp8", "VP8")
						if strings.Contains(strings.ToLower(codec), "nvenc") || strings.Contains(strings.ToLower(codec), "qsv") {
							displayCodec += " 🚀 GPU"
						}

						res := ""
						if state.LastPresentedWidth > 0 {
							res = fmt.Sprintf("%dx%d | ", state.LastPresentedWidth, state.LastPresentedHeight)
						}

						avgLat := 0.0
						if len(state.RecentLatencySamples) > 0 {
							var sum float64
							count := 0
							for _, s := range state.RecentLatencySamples {
								if s.PresentationAt > 0 && s.ReceiveAt > 0 {
									sum += float64(s.PresentationAt - s.ReceiveAt)
									count++
								}
							}
							if count > 0 {
								avgLat = sum / float64(count)
							}
						}

						statsText := fmt.Sprintf("[%s] %s%d FPS | Lat: %dms | BW: %.1fMb",
							displayCodec, res, fps, int(avgLat), bwMbps)

						if ffmpegCpu, ok := state.LastStats["ffmpegCpu"].(float64); ok {
							statsText += fmt.Sprintf(" | CPU: %d%%", int(ffmpegCpu))
						}
						if gpuUtil, ok := state.LastStats["intelGpuUtil"].(float64); ok && gpuUtil > 0 {
							statsText += fmt.Sprintf(" | Enc: %d%%", int(gpuUtil))
						}

						windowRenderer.SetStatusText(statsText)
					}
				}
			}()
		}

		if windowRenderer != nil {
			go func() {
				<-shutdownCh
				windowRenderer.Stop()
			}()
		}
	}

	startConnectLoop := func() {
		if desiredServerURL == "" {
			return
		}
		go func() {
			for {
				select {
				case <-stopConnect:
					return
				case <-reconnectRequests:
				}

				if !reconnecting.CompareAndSwap(false, true) {
					continue
				}

				for {
					select {
					case <-stopConnect:
						reconnecting.Store(false)
						return
					default:
					}

					if err := session.Connect(desiredServerURL); err != nil {
						log.Printf("connect failed: %v", err)
						time.Sleep(2 * time.Second)
						continue
					}
					log.Printf("connected to %s", desiredServerURL)

					// Send configuration from YAML if provided
					if clientConfig.FPS != nil || clientConfig.Codec != nil || clientConfig.DPI != nil {
						configMap := make(map[string]any)
						if clientConfig.FPS != nil {
							log.Printf("Config: setting server framerate to %d", *clientConfig.FPS)
							configMap["framerate"] = *clientConfig.FPS
						}
						if clientConfig.Codec != nil {
							log.Printf("Config: setting server videoCodec to %s", *clientConfig.Codec)
							configMap["videoCodec"] = *clientConfig.Codec
						}
						if clientConfig.DPI != nil {
							log.Printf("Config: setting server hdpi to %d", *clientConfig.DPI)
							configMap["hdpi"] = *clientConfig.DPI
						}
						if err := session.SendConfig(configMap); err != nil {
							log.Printf("failed to send initial config: %v", err)
						}
					}

					if windowRenderer != nil {
						width, height := windowRenderer.Size()
						if err := session.SendResize(width, height); err != nil {
							log.Printf("initial resize failed: %v", err)
						}
					}
					reconnecting.Store(false)
					break
				}
			}
		}()
	}
	var auxiliaryOnce sync.Once
	startAuxiliaryOnce := func() {
		auxiliaryOnce.Do(startAuxiliaryTasks)
	}
	var connectLoopOnce sync.Once
	startConnectLoopOnce := func() {
		connectLoopOnce.Do(startConnectLoop)
	}

	if windowRenderer != nil {
		var firstPresentLogged atomic.Bool
		session.Hooks().On(client.EventInputSent, func(payload client.EventPayload) {
			if msgType, ok := payload.Data["type"].(string); ok && msgType == "mousemove" {
				if x, ok1 := payload.Data["x"].(float64); ok1 {
					if y, ok2 := payload.Data["y"].(float64); ok2 {
						windowRenderer.UpdateMouse(x, y)
					}
				}
			}
		})
		windowRenderer.SetInputSink(func(msg map[string]any) error {
			if msgType, _ := msg["type"].(string); msgType == "resize" {
				var width, height int
				if w, ok := msg["width"].(float64); ok {
					width = int(w)
				}
				if h, ok := msg["height"].(float64); ok {
					height = int(h)
				}
				if width > 0 && height > 0 {
					return session.SendResize(width, height)
				}
				return nil
			}
			return session.SendInput(msg)
		})
		windowRenderer.SetLifecycleSink(func(event client.NativeWindowLifecycle) {
			session.UpdateWindowState(event)
			if event.Event == "started" {
				startConnectLoopOnce()
				scheduleReconnect()
			}
			if event.Event == "close" {
				requestShutdown("window_close")
			}
			if event.Created || event.Shown || event.Mapped || event.Visible || event.Event == "close" || event.Event == "hidden" || event.Event == "unshown" || event.RenderLoopStarted {
				log.Printf("native window lifecycle: backend=%s id=%d event=%s created=%t shown=%t mapped=%t visible=%t desktop=%d loop=%t flags=%d focus=%t surface=%t awaiting_keyframe=%t", event.Backend, event.WindowID, event.Event, event.Created, event.Shown, event.Mapped, event.Visible, event.Desktop, event.RenderLoopStarted, event.Flags, event.HasFocus, event.HasSurface, event.DecoderAwaitingKeyframe)
			}
			if event.Error != "" {
				log.Printf("native window error: %s", event.Error)
			}
		})
		windowRenderer.SetPresentSink(func(event client.NativeFramePresented) {
			session.RecordPresentedFrame(event)
			if firstPresentLogged.CompareAndSwap(false, true) {
				log.Printf("native frame presented: %dx%d ts=%d", event.Width, event.Height, event.PacketTimestamp)
			}
		})
	} else {
		startConnectLoopOnce()
		scheduleReconnect()
	}

	startAuxiliaryOnce()

	if windowRenderer != nil {
		if err := windowRenderer.Run(); err != nil {
			log.Printf("native renderer stopped with error: %v", err)
			requestShutdown("renderer_error")
		}
		requestShutdown("renderer_stopped")
	} else {
		<-shutdownCh
	}

	close(stopConnect)
	_ = controlServer.Close()
	if windowRenderer != nil {
		windowRenderer.Stop()
	}
	disconnectDone := make(chan struct{})
	go func() {
		_ = session.Disconnect()
		close(disconnectDone)
	}()
	select {
	case <-disconnectDone:
	case <-time.After(2 * time.Second):
		log.Printf("session disconnect timed out during shutdown")
	}
}
