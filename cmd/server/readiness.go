package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	readinessWaylandSocket  = "wayland_socket_ready"
	readinessInputHelper    = "input_helper_ready"
	readinessDesktopSession = "desktop_session_ready"
	readinessPulseAudio     = "pulseaudio_ready"
)

const (
	desktopReadyMarker = "/tmp/llrdc-run/desktop-ready"
)

type readinessTracker struct {
	mu    sync.RWMutex
	flags map[string]bool
}

var readiness = &readinessTracker{
	flags: map[string]bool{
		readinessWaylandSocket:  false,
		readinessInputHelper:    false,
		readinessDesktopSession: false,
		readinessPulseAudio:     false,
	},
}

func (r *readinessTracker) Set(name string, ready bool) {
	r.mu.Lock()
	r.flags[name] = ready
	r.mu.Unlock()
}

func (r *readinessTracker) Snapshot() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]bool, len(r.flags))
	for name, ready := range r.flags {
		snapshot[name] = ready
	}
	return snapshot
}

func (r *readinessTracker) IsReady() bool {
	snapshot := r.Snapshot()
	for _, name := range requiredReadinessFlags() {
		if !snapshot[name] {
			return false
		}
	}
	return true
}

func requiredReadinessFlags() []string {
	flags := []string{
		readinessWaylandSocket,
		readinessInputHelper,
		readinessDesktopSession,
	}
	if EnableAudio {
		flags = append(flags, readinessPulseAudio)
	}
	return flags
}

func initReadiness() {
	readiness.Set(readinessWaylandSocket, TestPattern)
	readiness.Set(readinessInputHelper, TestPattern)
	readiness.Set(readinessDesktopSession, TestPattern)
	readiness.Set(readinessPulseAudio, TestPattern || !EnableAudio)
}

func waitForPredicate(name string, timeout, pollInterval time.Duration, fn func() (bool, error)) error {
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	for {
		ok, err := fn()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s after %s", name, timeout)
		}
		time.Sleep(pollInterval)
	}
}

func waitForFile(path string, timeout, pollInterval time.Duration) error {
	return waitForPredicate(fmt.Sprintf("file %s", path), timeout, pollInterval, func() (bool, error) {
		_, err := os.Stat(path)
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	})
}

func waitForCommandSuccess(name string, args []string, env []string, timeout, pollInterval time.Duration) error {
	return waitForPredicate(fmt.Sprintf("%s %v", name, args), timeout, pollInterval, func() (bool, error) {
		cmd := exec.Command(name, args...)
		if len(env) > 0 {
			cmd.Env = env
		}
		err := cmd.Run()
		if err == nil {
			return true, nil
		}
		return false, nil
	})
}

func marshalReadinessStatus() ([]byte, error) {
	payload := map[string]interface{}{
		"ready":      readiness.IsReady(),
		"conditions": readiness.Snapshot(),
	}
	return json.Marshal(payload)
}
