package server

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

const latencyProbeStatePath = "/tmp/llrdc-latency-probe.json"

type latencyTraceRecord struct {
	Marker                              int    `json:"marker"`
	ServerTimeMs                        int64  `json:"serverTimeMs"`            // T0: Server received control input
	RequestedAtMs                       int64  `json:"requestedAtMs"`           // T1: Probe app detected motion
	DrawnAtMs                           int64  `json:"drawnAtMs"`               // T2: Probe app frame callback fired
	FirstFrameBroadcastAtMs             int64  `json:"firstFrameBroadcastAtMs"` // T3: Server broadcasted the first probe frame
	FirstEncodedFrameParsedAtMs         int64  `json:"firstEncodedFrameParsedAtMs,omitempty"`
	FirstEncodedFrameContainerTimestamp uint64 `json:"firstEncodedFrameContainerTimestamp,omitempty"`
	FirstFrameDispatchAtMs              int64  `json:"firstFrameDispatchAtMs,omitempty"`
	FrameSendStartAtMs                  int64  `json:"frameSendStartAtMs,omitempty"`
	FirstPacketSequenceNumber           uint16 `json:"firstPacketSequenceNumber,omitempty"`
	FirstPacketTimestamp                uint32 `json:"firstPacketTimestamp,omitempty"`
	FirstPacketWriteAttemptAtMs         int64  `json:"firstPacketWriteAttemptAtMs,omitempty"`
	FirstPacketWriteReturnAtMs          int64  `json:"firstPacketWriteReturnAtMs,omitempty"`
	FirstPacketSocketWriteAtMs          int64  `json:"firstPacketSocketWriteAtMs,omitempty"`
	FirstPacketWrittenAtMs              int64  `json:"firstPacketWrittenAtMs,omitempty"`
	LastPacketWrittenAtMs               int64  `json:"lastPacketWrittenAtMs,omitempty"`
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

	pendingSampleTraceMu sync.Mutex
	pendingSampleTrace   *latencyProbeSendTrace
)

type latencyProbeSendTrace struct {
	marker int
}

func setLastInputReceivedAt(t int64) {
	pendingInputMu.Lock()
	defer pendingInputMu.Unlock()
	pendingInputTime = t
}

func readLatencyProbeState() (latencyProbeStateFile, bool) {
	payload, err := os.ReadFile(latencyProbeStatePath)
	if err != nil {
		return latencyProbeStateFile{}, false
	}

	var state latencyProbeStateFile
	if err := json.Unmarshal(payload, &state); err != nil {
		return latencyProbeStateFile{}, false
	}
	if state.Marker <= 0 || state.DrawnAtMs <= 0 {
		return latencyProbeStateFile{}, false
	}
	return state, true
}

func pruneLatencyTraceRecordsLocked(currentMarker int) {
	const maxTraceRecords = 64
	if len(latencyTraceRecords) <= maxTraceRecords {
		return
	}
	cutoff := currentMarker - maxTraceRecords
	for marker := range latencyTraceRecords {
		if marker < cutoff {
			delete(latencyTraceRecords, marker)
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
