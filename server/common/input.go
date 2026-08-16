package common

import (
	"log"
	"time"
)

type InputTask struct {
	Type     string
	SampleID uint64
	NX, NY   float64
	Button   int
	Action   string
	Key      string
	DX, DY   float64
	SentTime int64
}

var inputChan = make(chan InputTask, 5000)

func GetInputChannel() chan InputTask {
	return inputChan
}

func TriggerPing() {
	SafeTriggerPing()
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
			case inputChan <- InputTask{Type: "ping"}:
			default:
				SafeTriggerPing()
			}
			if interval > 0 && i != count-1 {
				time.Sleep(interval)
			}
		}
	}()
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

func HandleInputMessage(msg map[string]interface{}) {
	msgType, _ := msg["type"].(string)
	ts, _ := msg["ts"].(float64)
	sentTime := int64(ts)
	sampleID := uint64(numberFromMessage(msg, "sampleId"))
	clientInputSendNs := int64(numberFromMessage(msg, "clientInputSendNs"))

	if UseDebugInput && sentTime > 0 && msgType != "mousemove" {
		log.Printf("HOST_RECV: type=%s, delay=%v ms", msgType, BenchmarkClockNowMs()-sentTime)
	}

	// Keep the measured sample armed until its first encoded frame is traced.
	// Unsampled follow-up events (for example mouse-up) must not overwrite it.
	if sampleID > 0 && (msgType == "mousemove" || msgType == "mousebtn" || msgType == "keydown" || msgType == "keyup" || msgType == "key" || msgType == "wheel") {
		SetLastInputSample(sampleID, clientInputSendNs, BenchmarkClockNowNs())
	}

	switch msgType {
	case "keydown", "keyup", "key":
		if key, ok := msg["key"].(string); ok {
			select {
			case inputChan <- InputTask{Type: msgType, SampleID: sampleID, Key: key, SentTime: sentTime}:
			default:
			}
		}
	case "mousemove":
		if x, ok1 := msg["x"].(float64); ok1 {
			if y, ok2 := msg["y"].(float64); ok2 {
				select {
				case inputChan <- InputTask{Type: "mousemove", SampleID: sampleID, NX: x, NY: y, SentTime: sentTime}:
				default:
				}
			}
		}
	case "mousebtn":
		if btn, ok := msg["button"].(float64); ok {
			if action, ok2 := msg["action"].(string); ok2 {
				select {
				case inputChan <- InputTask{Type: "mousebtn", SampleID: sampleID, Button: int(btn), Action: action, SentTime: sentTime}:
				default:
				}
			}
		}
	case "wheel":
		if dx, ok1 := msg["deltaX"].(float64); ok1 {
			if dy, ok2 := msg["deltaY"].(float64); ok2 {
				select {
				case inputChan <- InputTask{Type: "wheel", SampleID: sampleID, DX: dx, DY: dy, SentTime: sentTime}:
				default:
				}
			}
		}
	}
}

func numberFromMessage(msg map[string]interface{}, key string) float64 {
	value, ok := msg[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	default:
		return 0
	}
}
