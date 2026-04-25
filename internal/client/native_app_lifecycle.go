package client

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

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
	a.session.Hooks().On(EventReconnectRequest, func(_ EventPayload) {
		a.scheduleReconnect()
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
			log.Printf("native window lifecycle: backend=%s id=%d event=%s created=%t shown=%t mapped=%t visible=%t desktop=%d loop=%t flags=%d focus=%t pointer_inside=%t surface=%t awaiting_keyframe=%t", event.Backend, event.WindowID, event.Event, event.Created, event.Shown, event.Mapped, event.Visible, event.Desktop, event.RenderLoopStarted, event.Flags, event.HasFocus, event.PointerInside, event.HasSurface, event.DecoderAwaitingKeyframe)
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
				a.drainReconnectRequests()
				a.reconnecting.Store(false)
				a.refreshOverlay()
				break
			}
		}
	}()
}

func (a *NativeApp) drainReconnectRequests() {
	for {
		select {
		case <-a.reconnectRequests:
		default:
			return
		}
	}
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
