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
var currentX, currentY int = 0, 0

func init() {
	go func() {
		for task := range inputChan {
			execTask(task)
		}
	}()
}

// ResetMouse forces the cursor to 0,0 to synchronize our tracking
func ResetMouse() {
	log.Println("Synchronizing Wayland mouse to 0,0...")
	cmd := exec.Command("wlrctl", "pointer", "move", "-3000", "-3000")
	cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY=wayland-0")
	_ = cmd.Run()
	currentX = 0
	currentY = 0
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

		dx := targetX - currentX
		dy := targetY - currentY

		if dx != 0 || dy != 0 {
			log.Printf("Wayland mouse move to absolute: %d, %d (relative: %d, %d)", targetX, targetY, dx, dy)
			// Move relative to current position
			cmd := exec.Command("wlrctl", "pointer", "move", strconv.Itoa(dx), strconv.Itoa(dy))
			cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY=wayland-0")
			_ = cmd.Run()

			// Update tracked position, clamping to screen bounds to match compositor behavior
			currentX += dx
			currentY += dy
			
			if currentX < 0 { currentX = 0 }
			if currentX >= width { currentX = width - 1 }
			if currentY < 0 { currentY = 0 }
			if currentY >= height { currentY = height - 1 }
		}

	case "mousebtn":
		wlrBtn := "left"
		if task.Button == 1 {
			wlrBtn = "middle"
		} else if task.Button == 2 {
			wlrBtn = "right"
		}

		if task.Action == "mousedown" {
			cmd := exec.Command("wlrctl", "pointer", "click", wlrBtn)
			cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY=wayland-0")
			_ = cmd.Run()
		}
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
