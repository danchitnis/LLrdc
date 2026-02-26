package main

import (
	"log"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"
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

type inputTask struct {
	Type    string
	Key     string
	NX, NY  float64
	Button  int
	Action  string
	Display string
}

var (
	validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	inputChan   = make(chan inputTask, 2000)
)

func init() {
	for i := 1; i <= 12; i++ {
		key := "F" + strconv.Itoa(i)
		keyMap[key] = key
	}

	go func() {
		var lastMouseTime time.Time
		for task := range inputChan {
			if task.Type == "mousemove" {
				pendingMove := task
				// Coalesce all currently queued mouse moves
				for len(inputChan) > 0 {
					nextTask := <-inputChan
					if nextTask.Type == "mousemove" {
						pendingMove = nextTask
					} else {
						// Rate limit mouse moves to ~125Hz to match client throttle
						if time.Since(lastMouseTime) > 8*time.Millisecond {
							execMouseMove(pendingMove.NX, pendingMove.NY, pendingMove.Display)
							lastMouseTime = time.Now()
						}
						execTask(nextTask)
						pendingMove = inputTask{}
						break
					}
				}
				if pendingMove.Type == "mousemove" && time.Since(lastMouseTime) > 8*time.Millisecond {
					execMouseMove(pendingMove.NX, pendingMove.NY, pendingMove.Display)
					lastMouseTime = time.Now()
				}
			} else {
				execTask(task)
			}
		}
	}()
}

func execMouseMove(nx, ny float64, display string) {
	x := int(math.Round(nx * screenWidth))
	y := int(math.Round(ny * screenHeight))
	cmd := exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y))
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
	}
}

func execTask(task inputTask) {
	switch task.Type {
	case "key":
		xKey, mapped := keyMap[task.Key]
		if !mapped {
			xKey = task.Key
		}
		isPrintableSingle := len(task.Key) == 1 && task.Key[0] >= 32 && task.Key[0] <= 126
		if !mapped && !validNameRe.MatchString(xKey) && !isPrintableSingle {
			return
		}
		mode := "keydown"
		if task.Action == "keyup" {
			mode = "keyup"
		}
		cmd := exec.Command("xdotool", mode, xKey)
		cmd.Env = append(os.Environ(), "DISPLAY="+task.Display)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
		}

	case "mousebtn":
		xbtn := 1
		if task.Button == 0 {
			xbtn = 1
		} else if task.Button == 1 {
			xbtn = 2
		} else if task.Button == 2 {
			xbtn = 3
		}
		mode := "mousedown"
		if task.Action == "mouseup" {
			mode = "mouseup"
		}
		cmd := exec.Command("xdotool", mode, strconv.Itoa(xbtn))
		cmd.Env = append(os.Environ(), "DISPLAY="+task.Display)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
		}
	}
}

func injectKey(key, action, display string) {
	select {
	case inputChan <- inputTask{Type: "key", Key: key, Action: action, Display: display}:
	default:
	}
}

func injectMouseMove(nx, ny float64, display string) {
	select {
	case inputChan <- inputTask{Type: "mousemove", NX: nx, NY: ny, Display: display}:
	default:
	}
}

func injectMouseButton(button int, action, display string) {
	select {
	case inputChan <- inputTask{Type: "mousebtn", Button: button, Action: action, Display: display}:
	default:
	}
}

func spawnApp(command, display string) {
	log.Printf("Spawning app: %s", command)
	cmd := exec.Command(command)
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to spawn app %s: %v\n", command, err)
	}
}
