package main

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"
)

const latencyProbeStatePath = "/tmp/llrdc-latency-probe.json"

type latencyProbeStateFile struct {
	Marker        int     `json:"marker"`
	Color         string  `json:"color"`
	RequestedAtMs float64 `json:"requestedAtMs"`
	DrawnAtMs     float64 `json:"drawnAtMs"`
	PID           int     `json:"pid"`
}

type latencyTraceRecord struct {
	Marker                  int     `json:"marker"`
	Color                   string  `json:"color"`
	RequestedAtMs           float64 `json:"requestedAtMs"`
	DrawnAtMs               float64 `json:"drawnAtMs"`
	FirstFrameBroadcastAtMs float64 `json:"firstFrameBroadcastAtMs"`
}

var (
	latencyTraceMu      sync.RWMutex
	latencyTraceRecords = map[int]latencyTraceRecord{}
)

func recordLatencyProbeFrame(frameTime time.Time) {
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

	frameAtMs := float64(frameTime.UnixNano()) / float64(time.Millisecond)

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record := latencyTraceRecords[state.Marker]
	record.Marker = state.Marker
	record.Color = state.Color
	record.RequestedAtMs = state.RequestedAtMs
	record.DrawnAtMs = state.DrawnAtMs
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
	latencyTraceMu.RLock()
	defer latencyTraceMu.RUnlock()

	if markerStr != "" {
		marker, err := strconv.Atoi(markerStr)
		if err != nil {
			return latencyTraceRecord{}, false
		}
		record, ok := latencyTraceRecords[marker]
		return record, ok
	}

	var (
		bestMarker int
		bestRecord latencyTraceRecord
		found      bool
	)
	for marker, record := range latencyTraceRecords {
		if !found || marker > bestMarker {
			bestMarker = marker
			bestRecord = record
			found = true
		}
	}
	return bestRecord, found
}
