package main

import (
	"log"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

const (
	screenWidth  = 1280
	screenHeight = 720
)

var keyMap = map[string]string{
	"Control":    "Control_L",
	"Shift":      "Shift_L",
	"Alt":        "Alt_L",
	"Meta":       "Super_L",
	"Enter":      "Return",
	"Backspace":  "BackSpace",
	"ArrowUp":    "Up",
	"ArrowDown":  "Down",
	"ArrowLeft":  "Left",
	"ArrowRight": "Right",
	"Escape":     "Escape",
	"Tab":        "Tab",
	"Home":       "Home",
	"End":        "End",
	"PageUp":     "Page_Up",
	"PageDown":   "Page_Down",
	"Delete":     "Delete",
	"Insert":     "Insert",
	" ":          "space",
	"#":          "numbersign",
	"$":          "dollar",
	"%":          "percent",
	"&":          "ampersand",
	"(":          "parenleft",
	")":          "parenright",
	"*":          "asterisk",
	"+":          "plus",
	",":          "comma",
	"-":          "minus",
	".":          "period",
	"/":          "slash",
	":":          "colon",
	";":          "semicolon",
	"<":          "less",
	"=":          "equal",
	">":          "greater",
	"?":          "question",
	"@":          "at",
	"[":          "bracketleft",
	"\\":         "backslash",
	"]":          "bracketright",
	"^":          "asciicircum",
	"_":          "underscore",
	"`":          "grave",
	"{":          "braceleft",
	"|":          "bar",
	"}":          "braceright",
	"~":          "asciitilde",
	"\"":         "quotedbl",
	"'":          "apostrophe",
	"!":          "exclam",
}

func init() {
	for i := 1; i <= 12; i++ {
		key := "F" + strconv.Itoa(i)
		keyMap[key] = key
	}
}

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

func injectKey(key, action, display string) {
	xKey, mapped := keyMap[key]
	if !mapped {
		xKey = key
	}

	isPrintableSingle := len(key) == 1 && key[0] >= 32 && key[0] <= 126
	if !mapped && !validNameRe.MatchString(xKey) && !isPrintableSingle {
		log.Printf("Ignoring potentially unsafe/unknown key: %q (xKey: %q)\n", key, xKey)
		return
	}

	mode := "keydown"
	if action == "keyup" {
		mode = "keyup"
	}

	cmd := exec.Command("xdotool", mode, xKey)
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	_ = cmd.Run()
}

func injectMouseMove(nx, ny float64, display string) {
	x := int(math.Round(nx * screenWidth))
	y := int(math.Round(ny * screenHeight))
	cmd := exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y))
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	_ = cmd.Run()
}

func injectMouseButton(button int, action, display string) {
	xbtn := 1
	if button == 0 {
		xbtn = 1
	} else if button == 1 {
		xbtn = 2
	} else if button == 2 {
		xbtn = 3
	}

	cmd := exec.Command("xdotool", action, strconv.Itoa(xbtn))
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	_ = cmd.Run()
}

func spawnApp(command, display string) {
	log.Printf("Spawning app: %s", command)
	cmd := exec.Command(command)
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	// detached essentially
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to spawn app %s: %v\n", command, err)
	}
}
