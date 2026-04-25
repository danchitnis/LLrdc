package server

import (
	"fmt"
	"sync"
	"time"
)

type streamReadinessTracker struct {
	mu            sync.RWMutex
	currentID     uint32
	latestReadyID uint32
	readyByStream map[uint32]bool
}

var streamReadiness = &streamReadinessTracker{
	readyByStream: make(map[uint32]bool),
}

func noteStreamStarted(streamID uint32) {
	streamReadiness.mu.Lock()
	streamReadiness.currentID = streamID
	streamReadiness.readyByStream[streamID] = false
	streamReadiness.mu.Unlock()
}

func noteStreamFrame(streamID uint32) {
	streamReadiness.mu.Lock()
	if streamReadiness.readyByStream[streamID] {
		streamReadiness.mu.Unlock()
		return
	}
	streamReadiness.readyByStream[streamID] = true
	if streamID > streamReadiness.latestReadyID {
		streamReadiness.latestReadyID = streamID
	}
	streamReadiness.mu.Unlock()
}

func getCurrentFFmpegStreamID() uint32 {
	streamReadiness.mu.RLock()
	defer streamReadiness.mu.RUnlock()
	return streamReadiness.currentID
}

func waitForStreamReadyAfter(previousID uint32, timeout time.Duration) error {
	return waitForPredicate(fmt.Sprintf("stream readiness after %d", previousID), timeout, 100*time.Millisecond, func() (bool, error) {
		streamReadiness.mu.RLock()
		defer streamReadiness.mu.RUnlock()
		return streamReadiness.latestReadyID > previousID, nil
	})
}
