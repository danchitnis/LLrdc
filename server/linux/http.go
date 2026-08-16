package linux

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/server/common"
)

func configPayload(restarted bool) map[string]interface{} {
	directState := snapshotDirectBufferState()
	acceleratorMode := currentAcceleratorMode()
	screenWidth, screenHeight := GetScreenSize()
	return map[string]interface{}{
		"type":                    "config",
		"videoCodec":              VideoCodec,
		"chroma":                  Chroma,
		"acceleratorMode":         acceleratorMode,
		"hardwareAvailable":       usingHardwareAcceleration(),
		"nvidiaAvailable":         UseNVIDIA,
		"intelAvailable":          UseIntel,
		"captureMode":             CaptureMode,
		"directBufferRequested":   directState.Requested,
		"directBufferSupported":   directState.Supported,
		"directBufferActive":      directState.Active,
		"directBufferReason":      directState.Reason,
		"directBufferRenderNode":  directState.RenderNode,
		"directBufferRenderer":    directState.Renderer,
		"av1NvencAvailable":       AV1NVENCAvailable,
		"h264Nvenc444Available":   H264NVENC444Available,
		"h265Nvenc444Available":   H265NVENC444Available,
		"qsvAvailable":            QSVAvailable,
		"h265QsvAvailable":        H265QSVAvailable,
		"av1QsvAvailable":         AV1QSVAvailable,
		"framerate":               FPS,
		"bandwidth":               TargetBandwidthMbps,
		"quality":                 targetQuality,
		"vbr":                     targetVBR,
		"vbr_threshold":           targetVBRThreshold,
		"damageTracking":          targetDamageTracking,
		"mpdecimate":              targetMpdecimate,
		"keyframe_interval":       targetKeyframeInterval,
		"settle_time":             SettleTime,
		"tile_size":               TileSize,
		"enable_audio":            EnableAudio,
		"enableClipboard":         EnableClipboard,
		"audio_bitrate":           AudioBitrate,
		"hdpi":                    HDPI,
		"max_res":                 InitialRes,
		"activity_hz":             ActivityPulseHz,
		"activity_timeout":        ActivityTimeout,
		"nvenc_latency":           NVENCLatencyMode,
		"webtransportFingerprint": common.WebTransportFingerprint,
		"webtransportPort":        Port + 10,
		"screenWidth":             screenWidth,
		"screenHeight":            screenHeight,
		"restarted":               restarted,
		"capabilities": map[string]interface{}{
			"valid_combinations": common.GetValidCombinations(),
		},
	}
}

func getIntelGPUUtil() float64 {
	// Sample for 0.8s with 100ms frequency to get multiple full samples
	cmd := exec.Command("sudo", "timeout", "0.8", "intel_gpu_top", "-d", "drm:"+resolveIntelRenderNode(), "-J", "-s", "100")
	out, _ := cmd.Output()
	raw := string(out)

	if raw == "" {
		return 0
	}

	// Use regex to find the 'busy' percentage for Video and VideoEnhance engines.
	// This is much more robust than parsing the whole JSON stream which might be truncated or misformatted.
	reVideo := regexp.MustCompile(`"Video":\s*\{\s*"busy":\s*([0-9.]+)`)
	reEnhance := regexp.MustCompile(`"VideoEnhance":\s*\{\s*"busy":\s*([0-9.]+)`)

	videoMatches := reVideo.FindAllStringSubmatch(raw, -1)
	enhanceMatches := reEnhance.FindAllStringSubmatch(raw, -1)

	var total float64 = 0

	// We take the last valid match for each engine to get the most recent reading
	if len(videoMatches) > 0 {
		last := videoMatches[len(videoMatches)-1]
		if val, err := strconv.ParseFloat(last[1], 64); err == nil {
			total += val
		}
	}

	if len(enhanceMatches) > 0 {
		last := enhanceMatches[len(enhanceMatches)-1]
		if val, err := strconv.ParseFloat(last[1], 64); err == nil {
			total += val
		}
	}

	return total
}

func startHTTPServer() {
	go func() {
		for {
			time.Sleep(5 * time.Second)

			ffmpegMutex.Lock()
			cmd := ffmpegCmd
			ffmpegMutex.Unlock()

			var cpuUsage float64 = 0

			if cmd != nil && cmd.Process != nil {
				pid := cmd.Process.Pid
				out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=").Output()
				if err == nil {
					valStr := strings.TrimSpace(string(out))
					if val, err := strconv.ParseFloat(valStr, 64); err == nil {
						// Report raw percentage (100.0 = 1 core)
						cpuUsage = val
						if cpuUsage == 0 {
							cpuUsage = 0.1
						}
					}
				}
			}

			var intelGpuUtil float64 = 0
			if currentAcceleratorMode() == acceleratorIntel {
				intelGpuUtil = getIntelGPUUtil()
			}

			statsMsg := map[string]interface{}{
				"type":         "stats",
				"ffmpegCpu":    cpuUsage,
				"intelGpuUtil": intelGpuUtil,
			}

			broadcastJSON(statsMsg)
		}
	}()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	http.HandleFunc("/timez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := fmt.Sprintf("{\"serverTimeMs\": %d}\n", benchmarkClockNowMs())
		fmt.Fprint(w, payload)
	})

	http.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		payload, err := marshalReadinessStatus()
		if err != nil {
			http.Error(w, "failed to marshal readiness state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !readiness.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = w.Write(payload)
	})

	http.HandleFunc("/latencyz", func(w http.ResponseWriter, r *http.Request) {
		record, ok := snapshotLatencyTrace(r.URL.Query().Get("marker"))
		if !ok {
			http.Error(w, "latency trace not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(record); err != nil {
			http.Error(w, "failed to encode latency trace", http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configPayload(false))
	})

	http.HandleFunc("/ws", common.HandleWebSocket)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("HTTP %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		if r.Method == http.MethodGet {
			urlPath := r.URL.Path
			if urlPath == "/" {
				urlPath = "/viewer.html"
			}

			wd, _ := os.Getwd()
			publicDir := filepath.Join(wd, "public")
			filePath := filepath.Join(publicDir, urlPath)

			// Basic path traversal prevention
			if filepath.Clean(filePath)[:len(publicDir)] != publicDir {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			if filepath.Ext(filePath) == ".html" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			}

			http.ServeFile(w, r, filePath)
			return
		}
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	addr := ":" + strconv.Itoa(Port)
	log.Printf("Server listening on http://0.0.0.0%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func CloseAllClients() {
	common.CloseAllSessions()
}

func broadcastJSON(msg interface{}) {
	common.BroadcastJSON(msg)
}

func broadcastVideoFrame(frame EncodedVideoFrame, streamID uint32, codec string) {
	noteStreamFrame(streamID)
	if CaptureMode == CaptureModeDirect {
		markDirectBufferFrameValidated()
	}
	timestampMs := benchmarkClockNowMs()
	if frame.LatencyTrace != nil {
		noteLatencyProbeFrameDispatch(frame.LatencyTrace, timestampMs)
		common.NoteLatencyProbeFirstPacket(frame.LatencyTrace, timestampMs)
	}
	// Copy frame for delivery so we don't share memory with IVF reader
	copyFrame := make([]byte, len(frame.Data))
	copy(copyFrame, frame.Data)
	common.WriteFrame(copyFrame, streamID, timestampMs, frame.LatencyTrace)
}

func broadcastConfig(restarted bool) {
	SyncConfigToCommon()
	broadcastJSON(configPayload(restarted))
}

var (
	displayResizeTimer    *time.Timer
	displayResizeTimerMu  sync.Mutex
	displayChangeTimer    *time.Timer
	displayChangeTimerMu  sync.Mutex
	pendingDisplayChange  *displayChangeRequest
	lastResizeRequestTime time.Time
	lastResizeRequestMu   sync.Mutex
	currentAppliedWidth   int
	currentAppliedHeight  int
	currentAppliedHDPI    int
	currentAppliedFPS     int
)

type displayChangeRequest struct {
	previousStreamID uint32
	width            int
	height           int
	reason           string
}

func initializeAppliedDisplayState() {
	width, height := GetScreenSize()
	displayChangeMu.Lock()
	currentAppliedWidth = width
	currentAppliedHeight = height
	currentAppliedHDPI = HDPI
	currentAppliedFPS = FPS
	displayChangeMu.Unlock()
}

func queueDisplayChange(previousStreamID uint32, width, height int, reason string, delay time.Duration) {
	displayChangeTimerMu.Lock()
	pendingDisplayChange = &displayChangeRequest{
		previousStreamID: previousStreamID,
		width:            width,
		height:           height,
		reason:           reason,
	}
	if displayChangeTimer != nil {
		displayChangeTimer.Stop()
	}
	displayChangeTimer = time.AfterFunc(delay, func() {
		displayChangeTimerMu.Lock()
		request := pendingDisplayChange
		pendingDisplayChange = nil
		displayChangeTimer = nil
		displayChangeTimerMu.Unlock()
		if request == nil {
			return
		}
		applyDisplayChange(request.previousStreamID, request.width, request.height, request.reason)
		broadcastConfig(true)
	})
	displayChangeTimerMu.Unlock()
}

func applyDisplayChange(previousStreamID uint32, width, height int, reason string) {
	displayChangeMu.Lock()
	defer displayChangeMu.Unlock()
	if currentAppliedWidth == width && currentAppliedHeight == height && currentAppliedHDPI == HDPI && currentAppliedFPS == FPS {
		log.Printf("Display change ignored: size %dx%d, HDPI %d%%, and FPS %d are already applied.", width, height, HDPI, FPS)
		return
	}

	if CaptureMode != CaptureModeDirect {
		log.Printf("Applying display change for %s: %dx%d. Closing existing sessions...", reason, width, height)
		common.SetExpectingReconnect(true)
		CloseAllClients()
	} else {
		log.Printf("Applying direct display change for %s: %dx%d without closing client sessions", reason, width, height)
	}

	if TestPattern {
		RestartForResize()
		return
	}

	log.Printf("Applying display change for %s: %dx%d", reason, width, height)
	PauseStreaming()
	if err := resizeDisplay(width, height); err != nil {
		log.Printf("Display resize failed for %s: %v", reason, err)
	}

	applyHdpiSettings(os.Environ())

	if err := waitForDisplayState(width, height, 2*time.Second); err != nil {
		log.Printf("Display change did not reach requested state for %s: %v", reason, err)
	}

	// Update applied state
	currentAppliedWidth = width
	currentAppliedHeight = height
	currentAppliedHDPI = HDPI
	currentAppliedFPS = FPS

	ResumeStreaming()

	PrimeFrameGeneration(0, 5, 100*time.Millisecond)
	if err := waitForStreamReadyAfter(previousStreamID, 8*time.Second); err != nil {
		log.Printf("Display-changed stream did not become ready in time for %s: %v", reason, err)
		PrimeFrameGeneration(0, 10, 100*time.Millisecond)
	}
	if CaptureMode == CaptureModeDirect {
		ForceDirectCaptureKeyframe()
	}
}

func HandleInputMessage(msg map[string]interface{}) {
	msgType, _ := msg["type"].(string)
	ts, _ := msg["ts"].(float64)
	sentTime := int64(ts)
	var sampleID uint64
	if value, ok := msg["sampleId"].(float64); ok && value > 0 {
		sampleID = uint64(value)
	}
	var clientInputSendNs int64
	if value, ok := msg["clientInputSendNs"].(float64); ok && value > 0 {
		clientInputSendNs = int64(value)
	}

	if UseDebugInput && sentTime > 0 && msgType != "mousemove" {
		log.Printf("HOST_RECV: type=%s, delay=%v ms", msgType, benchmarkClockNowMs()-sentTime)
	}

	if sampleID > 0 && (msgType == "mousemove" || msgType == "mousebtn" || msgType == "keydown" || msgType == "keyup" || msgType == "key" || msgType == "wheel") {
		common.SetLastInputSample(sampleID, clientInputSendNs, common.BenchmarkClockNowNs())
	}

	switch msgType {
	case "keydown", "keyup", "key":
		if key, ok := msg["key"].(string); ok {
			injectKey(key, msgType, sentTime, sampleID)
		}
	case "mousemove":
		if x, ok1 := msg["x"].(float64); ok1 {
			if y, ok2 := msg["y"].(float64); ok2 {
				injectMouseMove(x, y, sentTime, sampleID)
			}
		}
	case "mousebtn":
		if btn, ok := msg["button"].(float64); ok {
			if action, ok2 := msg["action"].(string); ok2 {
				injectMouseButton(int(btn), action, sentTime, sampleID)
			}
		}
	case "wheel":
		if dx, ok1 := msg["deltaX"].(float64); ok1 {
			if dy, ok2 := msg["deltaY"].(float64); ok2 {
				injectMouseWheel(dx, dy, sentTime, sampleID)
			}
		}
	case "spawn":
		if cmd, ok := msg["command"].(string); ok {
			allowed := map[string]bool{
				"gnome-calculator": true, "weston-terminal": true, "gedit": true,
				"mousepad": true, "xclock": true, "xeyes": true, "xfce4-terminal": true,
			}
			parts := strings.Fields(cmd)
			if len(parts) > 0 && allowed[parts[0]] {
				spawnApp(cmd)
			}
		}
	}
}

func HandleControlMessage(msg map[string]interface{}, writeJSON func(interface{}) error) {
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "ping":
		ts, _ := msg["ts"].(float64)
		_ = writeJSON(map[string]interface{}{
			"type":     "pong",
			"ts":       ts,
			"serverTs": benchmarkClockNowMs(),
		})
	case "keydown", "keyup", "key", "mousemove", "mousebtn", "wheel", "spawn":
		HandleInputMessage(msg)
	case "clipboard_set":
		handleClipboardSet(msg)
	case "config":
		// Synchronously update all configuration state to prevent race conditions
		prevInitialRes := InitialRes
		restartRequested := false
		displayResizeRequested := false
		displayChangeReason := "config update"
		previousStreamID := getCurrentFFmpegStreamID()

		if hdpiFloat, ok := msg["hdpi"].(float64); ok {
			hdpi := int(hdpiFloat)
			if HDPI != hdpi {
				log.Printf("Received HDPI config: %d%%", hdpi)
				HDPI = hdpi
				common.HDPI = HDPI
				displayResizeRequested = true
				displayChangeReason = "HDPI update"
			}
		}
		if maxResFloat, ok := msg["max_res"].(float64); ok {
			maxRes := int(maxResFloat)
			InitialRes = maxRes
			common.InitialRes = maxRes
			if prevInitialRes != maxRes {
				log.Printf("Received max resolution config: %dp", maxRes)
				if maxRes > 0 {
					UpdateScreenSizeFromInitialRes()
					displayResizeRequested = true
					displayChangeReason = "fixed resolution update"
				}
			}
		}
		if vCodec, ok := msg["videoCodec"].(string); ok {
			oldCodec := VideoCodec
			SetVideoCodec(vCodec)
			common.VideoCodec = VideoCodec
			if VideoCodec != oldCodec {
				log.Printf("[Config] videoCodec changed from %s to %s, restart requested", oldCodec, VideoCodec)
				restartRequested = true
			}
		}
		if chromaStr, ok := msg["chroma"].(string); ok {
			if Chroma != chromaStr {
				log.Printf("[Config] chroma changed from %s to %s, restart requested", Chroma, chromaStr)
				restartRequested = true
			}
			SetChroma(chromaStr)
			common.Chroma = Chroma
		}
		if vbrBool, ok := msg["vbr"].(bool); ok {
			if targetVBR != vbrBool {
				restartRequested = true
			}
			SetVBR(vbrBool)
			common.TargetVBR = targetVBR
		}
		if threshold, ok := msg["vbr_threshold"].(float64); ok {
			if targetVBRThreshold != int(threshold) {
				restartRequested = true
			}
			SetVBRThreshold(int(threshold))
			common.TargetVBRThreshold = targetVBRThreshold
		}
		if dtBool, ok := msg["damageTracking"].(bool); ok {
			if targetDamageTracking != dtBool {
				restartRequested = true
			}
			SetDamageTracking(dtBool)
			common.TargetDamageTracking = targetDamageTracking
		}
		if mpdecimateBool, ok := msg["mpdecimate"].(bool); ok {
			if targetMpdecimate != mpdecimateBool {
				restartRequested = true
			}
			SetMpdecimate(mpdecimateBool)
		}
		if keyframeFloat, ok := msg["keyframe_interval"].(float64); ok {
			keyframe := int(keyframeFloat)
			if targetKeyframeInterval != keyframe {
				restartRequested = true
			}
			SetKeyframeInterval(keyframe)
		}
		if codecStr, ok := msg["video_codec"].(string); ok {
			oldCodec := VideoCodec
			SetVideoCodec(codecStr)
			common.VideoCodec = VideoCodec
			if VideoCodec != oldCodec {
				log.Printf("[Config] video_codec changed from %s to %s, restart requested", oldCodec, VideoCodec)
				restartRequested = true
			}
		}
		if cpuEffortFloat, ok := msg["cpu_effort"].(float64); ok {
			effort := int(cpuEffortFloat)
			if targetCpuEffort != effort {
				restartRequested = true
			}
			SetCpuEffort(effort)
			common.TargetCpuEffort = targetCpuEffort
		}
		if threadsFloat, ok := msg["cpu_threads"].(float64); ok {
			threads := int(threadsFloat)
			if targetCpuThreads != threads {
				restartRequested = true
			}
			SetCpuThreads(threads)
		}
		if mouseBool, ok := msg["enable_desktop_mouse"].(bool); ok {
			if targetDrawMouse != mouseBool {
				restartRequested = true
			}
			SetDrawMouse(mouseBool)
		}
		if settleTime, ok := msg["settle_time"].(float64); ok {
			SettleTime = int(settleTime)
			common.SettleTime = SettleTime
		}
		if tileSize, ok := msg["tile_size"].(float64); ok {
			TileSize = int(tileSize)
			common.TileSize = TileSize
		}
		if enableAudioBool, ok := msg["enable_audio"].(bool); ok {
			SetEnableAudio(enableAudioBool)
			common.EnableAudio = EnableAudio
		}
		if audioBitrateStr, ok := msg["audio_bitrate"].(string); ok {
			SetAudioBitrate(audioBitrateStr)
			common.AudioBitrate = AudioBitrate
		}
		if activityHzFloat, ok := msg["activity_hz"].(float64); ok {
			activityHz := int(activityHzFloat)
			if ActivityPulseHz != activityHz {
				ActivityPulseHz = activityHz
				common.ActivityPulseHz = ActivityPulseHz
			}
		}
		if activityTimeoutFloat, ok := msg["activity_timeout"].(float64); ok {
			activityTimeout := int(activityTimeoutFloat)
			if ActivityTimeout != activityTimeout {
				ActivityTimeout = activityTimeout
				common.ActivityTimeout = ActivityTimeout
			}
		}
		if nvencLatencyBool, ok := msg["nvenc_latency"].(bool); ok {
			if NVENCLatencyMode != nvencLatencyBool {
				NVENCLatencyMode = nvencLatencyBool
				common.NVENCLatencyMode = NVENCLatencyMode
				restartRequested = true
			}
		}

		if bwFloat, ok := msg["bandwidth"].(float64); ok {
			bandwidth := int(bwFloat)
			if targetMode != "bandwidth" || TargetBandwidthMbps != bandwidth {
				restartRequested = true
			}
			SetBandwidth(bandwidth)
			common.TargetBandwidthMbps = TargetBandwidthMbps
		} else if qFloat, ok := msg["quality"].(float64); ok {
			quality := int(qFloat)
			if targetMode != "quality" || targetQuality != quality {
				restartRequested = true
			}
			SetQuality(quality)
		}

		if fpsFloat, ok := msg["framerate"].(float64); ok {
			fps := int(fpsFloat)
			if FPS != fps {
				restartRequested = true
				if CaptureMode == CaptureModeDirect {
					displayResizeRequested = true
					if displayChangeReason == "config update" {
						displayChangeReason = "framerate update"
					}
				}
			}
			SetFramerate(fps)
			common.FPS = FPS
		}

		go func(restartRequested, displayResizeRequested bool, previousStreamID uint32, displayChangeReason string) {
			if displayResizeRequested {
				w, h := GetScreenSize()
				queueDisplayChange(previousStreamID, w, h, displayChangeReason, 100*time.Millisecond)
				return
			}

			if restartRequested {
				log.Println("Config updated, closing clients for codec restart...")
				if CaptureMode != CaptureModeDirect {
					common.SetExpectingReconnect(true)
					CloseAllClients()
				} else {
					log.Println("Direct capture codec restart will preserve client sessions.")
				}

				log.Println("Config updated, waiting for restarted stream to become ready...")
				PrimeFrameGeneration(0, 5, 100*time.Millisecond)
				if err := waitForStreamReadyAfter(previousStreamID, 8*time.Second); err != nil {
					log.Printf("Restarted stream did not become ready in time: %v", err)
					PrimeFrameGeneration(0, 10, 100*time.Millisecond)
				}
				if CaptureMode == CaptureModeDirect {
					ForceDirectCaptureKeyframe()
				}
			}

			broadcastConfig(true)
		}(restartRequested, displayResizeRequested, previousStreamID, displayChangeReason)
	case "resize":
		widthFloat, wOk := msg["width"].(float64)
		heightFloat, hOk := msg["height"].(float64)
		dpr, _ := msg["dpr"].(float64)
		if wOk && hOk {
			width := int(widthFloat)
			height := int(heightFloat)
			log.Printf("Received resize request: %dx%d (DPR: %.2f, Current InitialRes: %d)", width, height, dpr, InitialRes)

			now := time.Now()
			lastResizeRequestMu.Lock()
			timeSinceLast := now.Sub(lastResizeRequestTime)
			lastResizeRequestTime = now
			lastResizeRequestMu.Unlock()

			// If it's a discrete resize (more than 500ms since the last resize request),
			// apply it immediately without debouncing. Otherwise, debounce to handle drag resizes.
			if timeSinceLast > 500*time.Millisecond {
				displayResizeTimerMu.Lock()
				if displayResizeTimer != nil {
					displayResizeTimer.Stop()
					displayResizeTimer = nil
				}
				displayResizeTimerMu.Unlock()

				if SetScreenSize(width, height) {
					clampedW, clampedH := GetScreenSize()
					log.Printf("Accepted instant discrete resize: %dx%d (clamped to %dx%d)", width, height, clampedW, clampedH)
					previousStreamID := getCurrentFFmpegStreamID()
					queueDisplayChange(previousStreamID, clampedW, clampedH, "client resize", 100*time.Millisecond)
				} else {
					log.Printf("Ignored instant discrete resize request: %dx%d (InitialRes active or size unchanged)", width, height)
				}
			} else {
				displayResizeTimerMu.Lock()
				if displayResizeTimer != nil {
					displayResizeTimer.Stop()
				}
				displayResizeTimer = time.AfterFunc(200*time.Millisecond, func() {
					if SetScreenSize(width, height) {
						// Get the actual clamped size
						clampedW, clampedH := GetScreenSize()
						log.Printf("Accepted debounced resize: %dx%d (clamped to %dx%d)", width, height, clampedW, clampedH)
						previousStreamID := getCurrentFFmpegStreamID()
						queueDisplayChange(previousStreamID, clampedW, clampedH, "client resize", 100*time.Millisecond)
					} else {
						log.Printf("Ignored debounced resize request: %dx%d (InitialRes active or size unchanged)", width, height)
					}
				})
				displayResizeTimerMu.Unlock()
			}
		}
	case "force_keyframe":
		log.Printf("Received force_keyframe request from client")
		if common.OnForceKeyframe != nil {
			common.OnForceKeyframe()
		}
	}
}
