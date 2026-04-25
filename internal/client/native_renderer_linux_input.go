//go:build native && linux && cgo

package client

import (
	"strings"

	"github.com/veandco/go-sdl2/sdl"
)

func isKeyframe(codec string, data []byte) bool {
	if strings.Contains(strings.ToLower(codec), "vp8") {
		if len(data) == 0 {
			return false
		}
		return data[0]&0x01 == 0
	}
	if strings.Contains(strings.ToLower(codec), "h264") {
		return isH264KeyframePayload(data)
	}
	return false
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func sdlButtonToDOM(button uint8) int {
	switch button {
	case sdl.BUTTON_LEFT:
		return 0
	case sdl.BUTTON_MIDDLE:
		return 1
	case sdl.BUTTON_RIGHT:
		return 2
	default:
		return 0
	}
}

func sdlScancodeToDOM(code sdl.Scancode) string {
	switch {
	case code >= sdl.SCANCODE_A && code <= sdl.SCANCODE_Z:
		return "Key" + string(rune('A'+(code-sdl.SCANCODE_A)))
	case code >= sdl.SCANCODE_1 && code <= sdl.SCANCODE_9:
		return "Digit" + string(rune('1'+(code-sdl.SCANCODE_1)))
	}

	switch code {
	case sdl.SCANCODE_0:
		return "Digit0"
	case sdl.SCANCODE_RETURN:
		return "Enter"
	case sdl.SCANCODE_ESCAPE:
		return "Escape"
	case sdl.SCANCODE_BACKSPACE:
		return "Backspace"
	case sdl.SCANCODE_TAB:
		return "Tab"
	case sdl.SCANCODE_SPACE:
		return "Space"
	case sdl.SCANCODE_MINUS:
		return "Minus"
	case sdl.SCANCODE_EQUALS:
		return "Equal"
	case sdl.SCANCODE_LEFTBRACKET:
		return "BracketLeft"
	case sdl.SCANCODE_RIGHTBRACKET:
		return "BracketRight"
	case sdl.SCANCODE_BACKSLASH:
		return "Backslash"
	case sdl.SCANCODE_SEMICOLON:
		return "Semicolon"
	case sdl.SCANCODE_APOSTROPHE:
		return "Quote"
	case sdl.SCANCODE_GRAVE:
		return "Backquote"
	case sdl.SCANCODE_COMMA:
		return "Comma"
	case sdl.SCANCODE_PERIOD:
		return "Period"
	case sdl.SCANCODE_SLASH:
		return "Slash"
	case sdl.SCANCODE_CAPSLOCK:
		return "CapsLock"
	case sdl.SCANCODE_F1:
		return "F1"
	case sdl.SCANCODE_F2:
		return "F2"
	case sdl.SCANCODE_F3:
		return "F3"
	case sdl.SCANCODE_F4:
		return "F4"
	case sdl.SCANCODE_F5:
		return "F5"
	case sdl.SCANCODE_F6:
		return "F6"
	case sdl.SCANCODE_F7:
		return "F7"
	case sdl.SCANCODE_F8:
		return "F8"
	case sdl.SCANCODE_F9:
		return "F9"
	case sdl.SCANCODE_F10:
		return "F10"
	case sdl.SCANCODE_F11:
		return "F11"
	case sdl.SCANCODE_F12:
		return "F12"
	case sdl.SCANCODE_PRINTSCREEN:
		return "PrintScreen"
	case sdl.SCANCODE_SCROLLLOCK:
		return "ScrollLock"
	case sdl.SCANCODE_PAUSE:
		return "Pause"
	case sdl.SCANCODE_INSERT:
		return "Insert"
	case sdl.SCANCODE_HOME:
		return "Home"
	case sdl.SCANCODE_PAGEUP:
		return "PageUp"
	case sdl.SCANCODE_DELETE:
		return "Delete"
	case sdl.SCANCODE_END:
		return "End"
	case sdl.SCANCODE_PAGEDOWN:
		return "PageDown"
	case sdl.SCANCODE_RIGHT:
		return "ArrowRight"
	case sdl.SCANCODE_LEFT:
		return "ArrowLeft"
	case sdl.SCANCODE_DOWN:
		return "ArrowDown"
	case sdl.SCANCODE_UP:
		return "ArrowUp"
	case sdl.SCANCODE_LCTRL:
		return "ControlLeft"
	case sdl.SCANCODE_LSHIFT:
		return "ShiftLeft"
	case sdl.SCANCODE_LALT:
		return "AltLeft"
	case sdl.SCANCODE_LGUI:
		return "MetaLeft"
	case sdl.SCANCODE_RCTRL:
		return "ControlRight"
	case sdl.SCANCODE_RSHIFT:
		return "ShiftRight"
	case sdl.SCANCODE_RALT:
		return "AltRight"
	case sdl.SCANCODE_RGUI:
		return "MetaRight"
	default:
		return ""
	}
}
