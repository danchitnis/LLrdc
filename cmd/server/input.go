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
}

var inputChan = make(chan inputTask, 5000)
var mouseStdin io.WriteCloser

func startWaylandInputHelper() {
	go func() {
		for {
			// Start the Wayland mouse helper
			cmd := exec.Command("./wayland_mouse_client")
			cmd.Env = os.Environ()
			cmd.Stderr = os.Stderr
			
			stdin, err := cmd.StdinPipe()
			if err != nil {
				log.Printf("Failed to create stdin for mouse helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}
			mouseStdin = stdin

			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start mouse helper: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			log.Println("Wayland persistent mouse helper started.")

			// Process tasks for this process instance
			for task := range inputChan {
				if err := execTask(task); err != nil {
					log.Printf("Mouse helper communication error: %v", err)
					break
				}
			}
			
			_ = cmd.Wait()
			log.Println("Wayland mouse helper exited, restarting...")
			time.Sleep(1 * time.Second)
		}
	}()
}

func execTask(task inputTask) error {
	if mouseStdin == nil {
		return fmt.Errorf("no mouse helper")
	}

	switch task.Type {
	case "mousemove":
		width, height := GetScreenSize()
		if width <= 0 || height <= 0 {
			width, height = 1280, 720
		}
		targetX := int(math.Round(task.NX * float64(width)))
		targetY := int(math.Round(task.NY * float64(height)))

		if UseDebugInput {
			log.Printf("Wayland mouse move: %d, %d", targetX, targetY)
		}

		_, err := fmt.Fprintf(mouseStdin, "move %d %d %d %d\n", targetX, targetY, width, height)
		return err

	case "mousebtn":
		// BTN_LEFT is 272 in Linux input
		btnCode := 272
		if task.Button == 1 {
			btnCode = 274 // Middle
		} else if task.Button == 2 {
			btnCode = 273 // Right
		}

		state := 1 // Down
		if task.Action == "mouseup" {
			state = 0 // Up
		}

		if UseDebugInput {
			log.Printf("Wayland mouse button %d %s", btnCode, task.Action)
		}

		_, err := fmt.Fprintf(mouseStdin, "button %d %d\n", btnCode, state)
		return err
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

func injectKey(key, action, display string) {}
func injectMouseWheel(dx, dy float64, display string) {}
func spawnApp(command, display string) {
	log.Printf("Spawning app (stubbed): %s", command)
}
