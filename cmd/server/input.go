package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"time"
)

type inputTask struct {
	Type   string
	NX, NY float64
	Button int
	Action string
	Key    string
	DX, DY float64
}

var inputChan = make(chan inputTask, 5000)
var inputStdin io.WriteCloser

func startWaylandInputHelper() {
	go func() {
		for {
			cmd := exec.Command("./wayland_input_client")
			cmd.Env = os.Environ()
			cmd.Stderr = os.Stderr
			
			stdin, err := cmd.StdinPipe()
			if err != nil {
				log.Printf("Failed to create stdin for input helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}
			inputStdin = stdin

			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start input helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			log.Println("Wayland persistent input helper started.")

			for task := range inputChan {
				if err := execTask(task); err != nil {
					log.Printf("Input helper communication error: %v", err)
					break
				}
			}
			
			_ = cmd.Wait()
			log.Println("Wayland input helper exited, restarting...")
			time.Sleep(1 * time.Second)
		}
	}()
}

func execTask(task inputTask) error {
	if inputStdin == nil {
		return fmt.Errorf("no input helper")
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
		keyCode := getLinuxKeyCode(task.Key)
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
	}
	return nil
}

func injectMouseMove(nx, ny float64, display string) {
	select {
	case inputChan <- inputTask{Type: "mousemove", NX: nx, NY: ny}:
	default:
	}
}

func injectMouseButton(button int, action, display string) {
	select {
	case inputChan <- inputTask{Type: "mousebtn", Button: button, Action: action}:
	default:
	}
}

func injectKey(key, action, display string) {
	select {
	case inputChan <- inputTask{Type: action, Key: key}:
	default:
	}
}

func injectMouseWheel(dx, dy float64, display string) {
	select {
	case inputChan <- inputTask{Type: "wheel", DX: dx, DY: dy}:
	default:
	}
}

func spawnApp(command, display string) {
	log.Printf("Spawning app (stubbed): %s", command)
}

// getLinuxKeyCode maps browser e.code to Linux input keycodes
func getLinuxKeyCode(code string) int {
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
	}
	return m[code]
}
