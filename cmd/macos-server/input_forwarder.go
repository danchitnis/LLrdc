package main

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/internal/server"
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

var globalInputWriter = &tcpInputWriter{}

func startInputProcessor() {
	log.Println("Starting macOS input task processor")
	inputChan := server.GetInputChannel()
	for task := range inputChan {
		if err := server.ExecTask(task); err != nil {
			// Expected if not connected yet
		}
	}
}

func startInputForwarder() {
	// For local testing, we assume the container is reachable at localhost:12346
	// In a real setup, this might be host.docker.internal or a specific IP.
	// We'll use an environment variable for this later if needed.
	addr := "localhost:12346"

	for {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		log.Printf("TCP Input Forwarder connected to %s", addr)

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
		}

		globalInputWriter.setConn(conn)

		// Monitor connection
		buf := make([]byte, 1024)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				log.Printf("Input connection lost: %v", err)
				break
			}
		}

		globalInputWriter.setConn(nil)
		conn.Close()
		time.Sleep(1 * time.Second)
	}
}
