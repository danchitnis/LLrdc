package main

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

const latencyProbeStatePath = "/tmp/llrdc-latency-probe.json"

type latencyTraceRecord struct {
	Marker                  int   `json:"marker"`
	ServerTimeMs            int64 `json:"serverTimeMs"`            // T0: Server received control input
	RequestedAtMs           int64 `json:"requestedAtMs"`           // T1: Probe app detected motion
	DrawnAtMs               int64 `json:"drawnAtMs"`               // T2: Probe app frame callback fired
	FirstFrameBroadcastAtMs int64 `json:"firstFrameBroadcastAtMs"` // T3: Server broadcasted the first probe frame
}

type latencyProbeStateFile struct {
	Marker        int   `json:"marker"`
	RequestedAtMs int64 `json:"requestedAtMs"`
	DrawnAtMs     int64 `json:"drawnAtMs"`
}

var (
	latencyTraceMu      sync.RWMutex
	latencyTraceRecords = map[int]latencyTraceRecord{}

	pendingInputTime int64
	pendingInputMu   sync.Mutex
)

func setLastInputReceivedAt(t int64) {
	pendingInputMu.Lock()
	defer pendingInputMu.Unlock()
	pendingInputTime = t
}

func recordLatencyProbeFrame(frameAtMs int64) {
	payload, err := os.ReadFile(latencyProbeStatePath)
	if err != nil {
		return
	}

	var state latencyProbeStateFile
	if err := json.Unmarshal(payload, &state); err != nil {
		return
	}
	if state.Marker <= 0 || state.DrawnAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	pendingInputMu.Lock()
	inputAt := pendingInputTime
	pendingInputMu.Unlock()

	record, exists := latencyTraceRecords[state.Marker]
	if !exists {
		record.Marker = state.Marker
		record.ServerTimeMs = inputAt
		record.RequestedAtMs = state.RequestedAtMs
		record.DrawnAtMs = state.DrawnAtMs
	}

	if record.FirstFrameBroadcastAtMs == 0 && frameAtMs >= state.DrawnAtMs {
		record.FirstFrameBroadcastAtMs = frameAtMs
	}

	latencyTraceRecords[state.Marker] = record

	const maxTraceRecords = 64
	if len(latencyTraceRecords) > maxTraceRecords {
		cutoff := state.Marker - maxTraceRecords
		for marker := range latencyTraceRecords {
			if marker < cutoff {
				delete(latencyTraceRecords, marker)
			}
		}
	}
}

func snapshotLatencyTrace(markerStr string) (latencyTraceRecord, bool) {
	targetMarker, _ := strconv.Atoi(markerStr)

	latencyTraceMu.RLock()
	defer latencyTraceMu.RUnlock()

	if targetMarker > 0 {
		r, ok := latencyTraceRecords[targetMarker]
		return r, ok
	}

	// If no marker specified, return the latest
	var bestMarker int
	var record latencyTraceRecord
	var ok bool
	for marker, r := range latencyTraceRecords {
		if !ok || marker > bestMarker {
			bestMarker = marker
			record = r
			ok = true
		}
	}
	return record, ok
}
