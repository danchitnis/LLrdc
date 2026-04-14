package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/danchitnis/llrdc/internal/client"
)

var clientBuildID = "dev"

func main() {
        log.Println("DEBUG: Client starting")
        serverURL := flag.String("server", "", "LLrdc server URL (e.g. http://localhost:8080)")
        controlAddr := flag.String("control-addr", "127.0.0.1:18080", "Loopback control API listen address")
	windowTitle := flag.String("title", "LLrdc Native Client", "Native client window title")
	windowWidth := flag.Int("width", 1280, "Initial native client window width")
	windowHeight := flag.Int("height", 720, "Initial native client window height")
	headless := flag.Bool("headless", false, "Run without creating a native window")
	autoStart := flag.Bool("auto-start", false, "Start streaming automatically without waiting for click")
	exitAfter := flag.Duration("exit-after", 0, "Exit automatically after the given duration (e.g. 5s)")
	flag.Parse()

	var renderer client.Renderer = client.NullRenderer{}
	var windowRenderer client.WindowRenderer
	var err error
	if !*headless {
	        windowRenderer, err = client.NewNativeRenderer(client.NativeRendererOptions{
	                Title:     *windowTitle,
	                Width:     *windowWidth,
	                Height:    *windowHeight,
			AutoStart: *autoStart,
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
		windowRenderer.SetInputSink(func(msg map[string]any) error {
			if msgType, _ := msg["type"].(string); msgType == "resize" {
				width, _ := msg["width"].(int)
				height, _ := msg["height"].(int)
				return session.SendResize(width, height)
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
