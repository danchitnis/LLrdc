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
	FirstMoveAtMs float64 `json:"firstMoveAtMs"`
	IsMoving      bool    `json:"isMoving"`
	PID           int     `json:"pid"`
}

type latencyTraceRecord struct {
	Marker                  int     `json:"marker"`
	Color                   string  `json:"color"`
	RequestedAtMs           float64 `json:"requestedAtMs"`
	DrawnAtMs               float64 `json:"drawnAtMs"`
	FirstMoveAtMs           float64 `json:"firstMoveAtMs"`
	FirstFrameBroadcastAtMs float64 `json:"firstFrameBroadcastAtMs"`
	ServerTimeMs            float64 `json:"serverTimeMs"`
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
	record.FirstMoveAtMs = state.FirstMoveAtMs
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

	var record latencyTraceRecord
	var ok bool

	if markerStr != "" {
		marker, err := strconv.Atoi(markerStr)
		if err != nil {
			return latencyTraceRecord{}, false
		}
		record, ok = latencyTraceRecords[marker]
	} else {
		var bestMarker int
		for marker, r := range latencyTraceRecords {
			if !ok || marker > bestMarker {
				bestMarker = marker
				record = r
				ok = true
			}
		}
	}

	if ok {
		record.ServerTimeMs = float64(time.Now().UnixNano()) / float64(time.Millisecond)
	}
	return record, ok
}
