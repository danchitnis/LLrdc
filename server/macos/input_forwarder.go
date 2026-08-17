package main

import (
	"fmt"
	"log"
	"math"
	"net"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/server/common"
)

type tcpInputWriter struct {
	conn net.Conn
	mu   sync.Mutex
}

func (w *tcpInputWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == nil {
		return 0, net.ErrClosed
	}
	w.conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	return w.conn.Write(p)
}

func (w *tcpInputWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		err := w.conn.Close()
		w.conn = nil
		return err
	}
	return nil
}

func (w *tcpInputWriter) setConn(conn net.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		w.conn.Close()
	}
	w.conn = conn
}

func (w *tcpInputWriter) IsConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn != nil
}

var globalInputWriter = &tcpInputWriter{}

func startInputProcessor() {
	log.Println("Starting macOS input task processor")
	inputChan := common.GetInputChannel()
	for task := range inputChan {
		common.NoteInputInjected(task.SampleID, common.BenchmarkClockNowNs())
		var line string
		switch task.Type {
		case "mousemove":
			width, height := common.GetScreenSize()
			targetX := int(math.Round(task.NX * float64(width)))
			targetY := int(math.Round(task.NY * float64(height)))
			line = fmt.Sprintf("move %d %d %d %d\n", targetX, targetY, width, height)
		case "mousebtn":
			btnCode := 272 // Left
			if task.Button == 1 {
				btnCode = 274 // Middle
			} else if task.Button == 2 {
				btnCode = 273 // Right
			}
			state := 1
			if task.Action == "mouseup" {
				state = 0
			}
			line = fmt.Sprintf("button %d %d\n", btnCode, state)
		case "keydown", "keyup":
			keyCode := common.GetLinuxKeyCode(task.Key)
			if keyCode != 0 {
				state := 1
				if task.Type == "keyup" {
					state = 0
				}
				line = fmt.Sprintf("key %d %d\n", keyCode, state)
			}
		case "wheel":
			if task.DY != 0 {
				line += fmt.Sprintf("axis 0 %f\n", task.DY)
			}
			if task.DX != 0 {
				line += fmt.Sprintf("axis 1 %f\n", task.DX)
			}
		case "ping":
			line = "ping\n"
		}

		if line != "" {
			_, _ = globalInputWriter.Write([]byte(line))
		}
	}
}

func startInputForwarder() {
	// For local testing, we assume the container is reachable at localhost:12346
	// In a real setup, this might be host.docker.internal or a specific IP.
	// We'll use an environment variable for this later if needed.
	addr := "localhost:12346"

	var connectedOnce bool
	for {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		if connectedOnce {
			log.Printf("TCP Input Forwarder connected to %s", addr)
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
		}

		globalInputWriter.setConn(conn)

		// Monitor connection
		buf := make([]byte, 1024)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				if connectedOnce {
					log.Printf("Input connection lost: %v", err)
				}
				break
			}
			connectedOnce = true
		}

		globalInputWriter.setConn(nil)
		conn.Close()
		time.Sleep(1 * time.Second)
	}
}
