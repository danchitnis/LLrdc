package main

import (
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
)

type inputTask struct {
	Type   string
	NX, NY float64
	Button int
	Action string
}

var inputChan = make(chan inputTask, 5000)

func init() {
	go func() {
		for task := range inputChan {
			execTask(task)
		}
	}()
}

func execTask(task inputTask) {
	switch task.Type {
	case "mousemove":
		width, height := GetScreenSize()
		if width <= 0 || height <= 0 {
			return
		}
		targetX := int(math.Round(task.NX * float64(width)))
		targetY := int(math.Round(task.NY * float64(height)))

		if UseDebugInput {
			log.Printf("Wayland mouse move to absolute: %d, %d", targetX, targetY)
		}

		// Using ydotool for absolute movement. ydotoold must be running.
		cmd := exec.Command("ydotool", "mousemove", "--absolute", strconv.Itoa(targetX), strconv.Itoa(targetY))
		cmd.Env = os.Environ()
		_ = cmd.Run()

	case "mousebtn":
		// ydotool key codes for mouse buttons: Left=272, Right=273, Middle=274
		btnCode := 272
		if task.Button == 1 {
			btnCode = 274 // Middle
		} else if task.Button == 2 {
			btnCode = 273 // Right
		}

		state := "1" // Down
		if task.Action == "mouseup" {
			state = "0" // Up
		}

		if UseDebugInput {
			log.Printf("Wayland mouse button %d %s", btnCode, task.Action)
		}

		cmd := exec.Command("ydotool", "key", strconv.Itoa(btnCode)+":"+state)
		cmd.Env = os.Environ()
		_ = cmd.Run()
	}
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

func injectKey(key, action, display string) {}
func injectMouseWheel(dx, dy float64, display string) {}
func spawnApp(command, display string) {
	log.Printf("Spawning app (stubbed): %s", command)
}
