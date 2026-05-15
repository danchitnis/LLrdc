package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type inputTask struct {
	Type     string
	NX, NY   float64
	Button   int
	Action   string
	Key      string
	DX, DY   float64
	SentTime int64
}

var inputChan = make(chan inputTask, 5000)
var inputStdin io.WriteCloser
var inputStdinMu sync.Mutex

func SetInputWriter(w io.WriteCloser) {
	inputStdinMu.Lock()
	inputStdin = w
	inputStdinMu.Unlock()
	log.Printf("Input writer set: %T", w)
}

var (
	// lastInputTime tracks the last time any user interaction was received.
	lastInputTime time.Time
	// inputActivityMutex protects access to lastInputTime and pulseStarted.
	inputActivityMutex sync.Mutex
	// pulseStarted indicates if the background activity pulse goroutine is running.
	pulseStarted bool
)

var (
	inputMu sync.Mutex
)

func startWaylandInputHelper() {
	if CaptureMode == CaptureModeAgent {
		log.Println("Agent mode: starting TCP input listener on :12346")
		go func() {
			ln, err := net.Listen("tcp", ":12346")
			if err != nil {
				log.Printf("Failed to listen for input: %v", err)
				return
			}
			for {
				conn, err := ln.Accept()
				if err != nil {
					log.Printf("Input accept error: %v", err)
					continue
				}
				if tcpConn, ok := conn.(*net.TCPConn); ok {
					tcpConn.SetNoDelay(true)
				}
				log.Printf("Input client connected: %v", conn.RemoteAddr())
				go handleAgentInputConnection(conn)
			}
		}()
	}

	go func() {
		for {
			readiness.Set(readinessInputHelper, false)
			cmd := exec.Command("./wayland_input_client")
			cmd.Env = os.Environ()
			cmd.Stderr = os.Stderr

			stdin, err := cmd.StdinPipe()
			if err != nil {
				log.Printf("Failed to create stdin for input helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				log.Printf("Failed to create stdout for input helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start input helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			readyCh := make(chan error, 1)
			go func() {
				line, err := bufio.NewReader(stdout).ReadString('\n')
				if err != nil {
					readyCh <- err
					return
				}
				if strings.TrimSpace(line) != "READY" {
					readyCh <- fmt.Errorf("unexpected input helper handshake: %q", strings.TrimSpace(line))
					return
				}
				readyCh <- nil
			}()

			select {
			case err := <-readyCh:
				if err != nil {
					log.Printf("Input helper failed readiness handshake: %v", err)
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					time.Sleep(1 * time.Second)
					continue
				}
			case <-time.After(5 * time.Second):
				log.Printf("Input helper readiness handshake timed out")
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				time.Sleep(1 * time.Second)
				continue
			}

			inputStdinMu.Lock()
			inputStdin = stdin
			inputStdinMu.Unlock()
			readiness.Set(readinessInputHelper, true)
			log.Println("Wayland persistent input helper started.")

			// In Agent mode, we don't consume inputChan here; it's done by the host.
			// The container receives input via handleAgentInputConnection.
			if CaptureMode != CaptureModeAgent {
				// Consumer loop for non-agent mode.
				// We run this in a separate goroutine so we can wait for cmd.
				go func() {
					for task := range inputChan {
						if err := ExecTask(task); err != nil {
							// If write fails, the pipe is likely closed, which means cmd exited.
							break
						}
					}
				}()
			}

			if err := cmd.Wait(); err != nil {
				log.Printf("Input helper exited with error: %v", err)
			} else {
				log.Println("Input helper exited gracefully.")
			}

			inputStdinMu.Lock()
			if inputStdin == stdin {
				inputStdin = nil
			}
			inputStdinMu.Unlock()
			readiness.Set(readinessInputHelper, false)
			time.Sleep(1 * time.Second)
		}
	}()
}

func updateActivity() {
	inputActivityMutex.Lock()
	isFirst := !pulseStarted
	lastInputTime = time.Now()
	if !pulseStarted {
		pulseStarted = true
		go runActivityPulse()
	}
	inputActivityMutex.Unlock()

	if isFirst {
		// Immediate ping on first input to wake up the encoder.
		TriggerPing()
	}
}

// runActivityPulse pings the compositor periodically for a short time after input.
// This is critical for Wayland VBR mode (Damage Tracking).
// 1. It ensures that animations (like window closing or button hovers) are fully captured.
// 2. It forces the encoder to push out any buffered frames, fixing "one-key-behind" latency.
// 3. It automatically stops after 1 second of inactivity to preserve bandwidth.
func runActivityPulse() {
	for {
		inputActivityMutex.Lock()
		elapsed := time.Since(lastInputTime)
		timeoutMs := ActivityTimeout
		if timeoutMs < 100 {
			timeoutMs = 100
		}
		if elapsed > time.Duration(timeoutMs)*time.Millisecond {
			pulseStarted = false
			inputActivityMutex.Unlock()
			return
		}
		inputActivityMutex.Unlock()

		// Trigger a tiny, invisible damage event in the compositor.
		TriggerPing()

		hz := ActivityPulseHz
		if hz < 1 {
			hz = 1
		} else if hz > 120 {
			hz = 120
		}
		// 1000ms / Hz = sleep time
		sleepMs := 1000 / hz
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
	}
}

func ExecTask(task inputTask) error {
	// Any input task updates the activity timer to keep the pulse running
	updateActivity()

	inputStdinMu.Lock()
	defer inputStdinMu.Unlock()
	if inputStdin == nil {
		return fmt.Errorf("no input helper")
	}

	if UseDebugInput && task.SentTime > 0 {
		fmt.Fprintf(inputStdin, "ts %d ", task.SentTime)
	}

	switch task.Type {
	case "mousemove":
		width, height := GetScreenSize()
		if width <= 0 || height <= 0 {
			width, height = 1920, 1080
		}
		targetX := int(math.Round(task.NX * float64(width)))
		targetY := int(math.Round(task.NY * float64(height)))

		if UseDebugInput {
			log.Printf("Wayland mouse move: %d, %d", targetX, targetY)
		}

		_, err := fmt.Fprintf(inputStdin, "move %d %d %d %d\n", targetX, targetY, width, height)
		return err

	case "mousebtn":
		btnCode := 272
		if task.Button == 1 {
			btnCode = 274
		} else if task.Button == 2 {
			btnCode = 273
		}

		if UseDebugInput {
			log.Printf("Wayland mouse button %d %s", btnCode, task.Action)
		}

		state := 1
		if task.Action == "mouseup" {
			state = 0
		}

		_, err := fmt.Fprintf(inputStdin, "button %d %d\n", btnCode, state)
		return err

	case "keydown", "keyup":
		keyCode := GetLinuxKeyCode(task.Key)
		if keyCode == 0 {
			return nil
		}

		state := 1
		if task.Type == "keyup" {
			state = 0
		}

		if UseDebugInput {
			log.Printf("Wayland key %s (%d) %s", task.Key, keyCode, task.Type)
		}

		_, err := fmt.Fprintf(inputStdin, "key %d %d\n", keyCode, state)
		return err

	case "wheel":
		// DX is horizontal, DY is vertical
		// In Wayland, axis 0 is vertical, axis 1 is horizontal
		if task.DY != 0 {
			if UseDebugInput {
				fmt.Printf("Wayland axis 0 %f\n", task.DY)
			}
			_, _ = fmt.Fprintf(inputStdin, "axis 0 %f\n", task.DY)
		}
		if task.DX != 0 {
			if UseDebugInput {
				fmt.Printf("Wayland axis 1 %f\n", task.DX)
			}
			_, _ = fmt.Fprintf(inputStdin, "axis 1 %f\n", task.DX)
		}
		return nil

	case "ping":
		triggerPingLocked()
		return nil
	}
	return nil
}

// triggerPingLocked sends a 'ping' command. Assumes inputStdinMu is held.
func triggerPingLocked() {
	if inputStdin != nil {
		fmt.Fprintln(inputStdin, "ping")
	}
}

// TriggerPing sends a 'ping' command to the wayland_input_client.
// This command performs a tiny 1-pixel jitter which is invisible to the user
// but forces the Wayland compositor to generate damage and the encoder to emit a frame.
func TriggerPing() {
	inputStdinMu.Lock()
	defer inputStdinMu.Unlock()
	triggerPingLocked()
}

func PrimeFrameGeneration(delay time.Duration, count int, interval time.Duration) {
	if count <= 0 {
		return
	}
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		for i := 0; i < count; i++ {
			select {
			case inputChan <- inputTask{Type: "ping"}:
			default:
				TriggerPing()
			}
			if interval > 0 && i != count-1 {
				time.Sleep(interval)
			}
		}
	}()
}

func injectMouseMove(nx, ny float64, sentTime int64) {
	select {
	case inputChan <- inputTask{Type: "mousemove", NX: nx, NY: ny, SentTime: sentTime}:
	default:
	}
}

func injectMouseButton(button int, action string, sentTime int64) {
	select {
	case inputChan <- inputTask{Type: "mousebtn", Button: button, Action: action, SentTime: sentTime}:
	default:
	}
}

func injectKey(key, action string, sentTime int64) {
	select {
	case inputChan <- inputTask{Type: action, Key: key, SentTime: sentTime}:
	default:
	}
}

func injectMouseWheel(dx, dy float64, sentTime int64) {
	select {
	case inputChan <- inputTask{Type: "wheel", DX: dx, DY: dy, SentTime: sentTime}:
	default:
	}
}

func GetInputChannel() chan inputTask {
	return inputChan
}

func spawnApp(command string) {
	log.Printf("Spawning app (stubbed): %s", command)
}

// GetLinuxKeyCode maps browser e.code to Linux input keycodes
func GetLinuxKeyCode(code string) int {
	m := map[string]int{
		"KeyA": 30, "KeyB": 48, "KeyC": 46, "KeyD": 32, "KeyE": 18, "KeyF": 33, "KeyG": 34, "KeyH": 35,
		"KeyI": 23, "KeyJ": 36, "KeyK": 37, "KeyL": 38, "KeyM": 50, "KeyN": 49, "KeyO": 24, "KeyP": 25,
		"KeyQ": 16, "KeyR": 19, "KeyS": 31, "KeyT": 20, "KeyU": 22, "KeyV": 47, "KeyW": 17, "KeyX": 45,
		"KeyY": 21, "KeyZ": 44,
		"Digit1": 2, "Digit2": 3, "Digit3": 4, "Digit4": 5, "Digit5": 6, "Digit6": 7, "Digit7": 8, "Digit8": 9, "Digit9": 10, "Digit0": 11,
		"Enter": 28, "Escape": 1, "Backspace": 14, "Tab": 15, "Space": 57,
		"Minus": 12, "Equal": 13, "BracketLeft": 26, "BracketRight": 27, "Backslash": 43, "Semicolon": 39, "Quote": 40, "Backquote": 41,
		"Comma": 51, "Period": 52, "Slash": 53,
		"ShiftLeft": 42, "ShiftRight": 54, "ControlLeft": 29, "ControlRight": 97, "AltLeft": 56, "AltRight": 100, "MetaLeft": 125, "MetaRight": 126,
		"ArrowUp": 103, "ArrowDown": 108, "ArrowLeft": 105, "ArrowRight": 106,
		"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66, "F9": 67, "F10": 68, "F11": 87, "F12": 88,
		"Insert": 110, "Delete": 111, "Home": 102, "End": 107, "PageUp": 104, "PageDown": 109,
		"CapsLock": 58, "ScrollLock": 70, "NumLock": 69, "PrintScreen": 99, "Pause": 119,
		"Numpad0": 82, "Numpad1": 79, "Numpad2": 80, "Numpad3": 81, "Numpad4": 75, "Numpad5": 76, "Numpad6": 77, "Numpad7": 71, "Numpad8": 72, "Numpad9": 73,
		"NumpadDecimal": 83, "NumpadDivide": 98, "NumpadMultiply": 55, "NumpadSubtract": 74, "NumpadAdd": 78, "NumpadEnter": 96, "NumpadEqual": 117,
	}
	return m[code]
}
