package main

import (
	"os"
	"time"
)

func forceKillProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	_ = proc.Signal(os.Interrupt)
	time.Sleep(100 * time.Millisecond)
	_ = proc.Kill()
}
