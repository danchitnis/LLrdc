package main

import (
	"log"
	"math"
)

type inputTask struct {
	Type   string
	NX, NY float64
	Button int
	Action string
}

var inputChan = make(chan inputTask, 5000)
var uinputDev *UinputDevice

func init() {
	go func() {
		var err error
		uinputDev, err = CreateUinputDevice()
		if err != nil {
			log.Printf("Warning: Failed to create uinput device: %v. Mouse will not work.", err)
		} else {
			log.Println("Created virtual uinput mouse device.")
			cleanupTasks = append(cleanupTasks, func() {
				uinputDev.Close()
			})
		}

		for task := range inputChan {
			if uinputDev != nil {
				execTask(task)
			}
		}
	}()
}

func execTask(task inputTask) {
	switch task.Type {
	case "mousemove":
		width, height := GetScreenSize()
		if width <= 0 || height <= 0 {
			width, height = 1280, 720
		}
		targetX := int(math.Round(task.NX * float64(width)))
		targetY := int(math.Round(task.NY * float64(height)))

		if UseDebugInput {
			log.Printf("Uinput mouse move to absolute: %d, %d", targetX, targetY)
		}

		uinputDev.MoveAbs(targetX, targetY)

	case "mousebtn":
		btnCode := uint16(BTN_LEFT)
		if task.Button == 1 {
			btnCode = BTN_MIDDLE
		} else if task.Button == 2 {
			btnCode = BTN_RIGHT
		}

		down := (task.Action == "mousedown")

		if UseDebugInput {
			log.Printf("Uinput mouse button %d down=%v", btnCode, down)
		}

		uinputDev.Button(btnCode, down)
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
