package linux

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danchitnis/llrdc/server/common"
)

var cleanupTasks []func()

func HandleClientConnected() {
	if CaptureMode == CaptureModeDirect {
		log.Printf("[Server-Handshake] [%s] HandleClientConnected: keeping direct-buffer capture warm", time.Now().Format("15:04:05.000"))
		broadcastConfig(false)
		return
	}

	log.Printf("[Server-Handshake] [%s] HandleClientConnected: low-latency stream restart requested", time.Now().Format("15:04:05.000"))
	broadcastConfig(false)
	go func() {
		time.Sleep(50 * time.Millisecond)
		KillFFmpegWithTimestamp()
	}()
}

func Run() error {
	log.Println("Starting llrdc (Go)...")
	log.Printf("Args: %v", os.Args)

	InitConfig()
	log.Printf("Parsed CaptureMode: %v", CaptureMode)
	initScreenSize(3840, 2160)
	initReadiness()

	if CaptureMode == CaptureModeAgent {
		go startAgentControl()
	}

	// Register connection callbacks to common package
	if CaptureMode == CaptureModeDirect {
		common.OnForceKeyframe = ForceDirectCaptureKeyframe
	} else {
		common.OnForceKeyframe = KillFFmpegWithTimestamp
	}
	common.OnClientConnected = HandleClientConnected
	common.OnClientMediaConnected = func() {
		if CaptureMode == CaptureModeDirect {
			log.Printf("[Server-Handshake] [%s] Media stream ready; forcing direct-capture keyframe", time.Now().Format("15:04:05.000"))
			ForceDirectCaptureKeyframe()
		}
	}
	common.OnPauseStreaming = PauseStreamingTimeout
	common.OnResumeStreaming = ResumeStreamingTimeout
	common.OnTriggerPing = func() {
		inputStdinMu.Lock()
		defer inputStdinMu.Unlock()
		triggerPingLocked()
	}
	common.OnInputMessage = HandleInputMessage
	common.OnFallbackCodec = func(codec string) {
		SetVideoCodec(codec)
		broadcastConfig(true)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		shutdown()
	}()

	if !TestPattern {
		if err := startWayland(); err != nil {
			return fmt.Errorf("failed to initialize Wayland: %w", err)
		}
		initializeAppliedDisplayState()
		StartClipboardPoller()
	} else {
		log.Println("TEST_PATTERN mode: skipping display server setup.")
		initializeAppliedDisplayState()
	}

	wtAddr := ":" + fmt.Sprintf("%d", Port+10)
	common.MessageHandler = func(msg map[string]interface{}, writeJSON func(interface{}) error) {
		HandleControlMessage(msg, writeJSON)
	}
	common.InitWebTransport(wtAddr)
	startStreaming(broadcastVideoFrame)
	if CaptureMode != CaptureModeAgent {
		startAudioStreaming()
	}
	if CaptureMode != CaptureModeAgent {
		common.StartClientTimeoutTracker()
	}
	startHTTPServer()
	return nil
}

func shutdown() {
	log.Println("Shutting down...")
	for i := len(cleanupTasks) - 1; i >= 0; i-- {
		cleanupTasks[i]()
	}
	os.Exit(0)
}
