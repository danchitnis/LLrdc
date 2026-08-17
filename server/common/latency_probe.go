package common

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

const latencyProbeStatePath = "/tmp/llrdc-latency-probe.json"
const latencyProbeNextSamplePath = "/tmp/llrdc-latency-probe-next-sample-id"

type LatencyTraceRecord struct {
	Marker                              int    `json:"marker"`
	SampleID                            uint64 `json:"sampleId,omitempty"`
	ClientInputSendNs                   int64  `json:"clientInputSendNs,omitempty"`
	ServerInputReceivedNs               int64  `json:"serverInputReceivedNs,omitempty"`
	ServerInputInjectedNs               int64  `json:"serverInputInjectedNs,omitempty"`
	ProbeCommitNs                       int64  `json:"probeCommitNs,omitempty"`
	ProbeRequestedNs                    int64  `json:"probeRequestedNs,omitempty"`
	ProbeDrawnNs                        int64  `json:"probeDrawnNs,omitempty"`
	SourcePresentedNs                   int64  `json:"sourcePresentedNs,omitempty"`
	SourcePresentationClockID           uint32 `json:"sourcePresentationClockId,omitempty"`
	WebTransportWriteStartNs            int64  `json:"webTransportWriteStartNs,omitempty"`
	WebTransportWriteEndNs              int64  `json:"webTransportWriteEndNs,omitempty"`
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
	Marker              int    `json:"marker"`
	RequestedAtMs       int64  `json:"requestedAtMs"`
	DrawnAtMs           int64  `json:"drawnAtMs"`
	ProbeCommitNs       int64  `json:"probeCommitNs"`
	ProbeRequestedNs    int64  `json:"requestedAtNs"`
	ProbeDrawnNs        int64  `json:"drawnAtNs"`
	SourcePresentedNs   int64  `json:"sourcePresentedNs"`
	PresentationClockID uint32 `json:"presentationClockId"`
}

var (
	latencyTraceMu      sync.RWMutex
	latencyTraceRecords = map[int]LatencyTraceRecord{}

	pendingInput   pendingInputSample
	pendingInputMu sync.Mutex

	pendingSampleTraceMu sync.Mutex
	pendingSampleTrace   *LatencyProbeSendTrace
)

type LatencyProbeSendTrace struct {
	Marker int
}

type pendingInputSample struct {
	SampleID              uint64
	ClientInputSendNs     int64
	ServerInputReceivedNs int64
	ServerInputInjectedNs int64
}

func NoteInputInjected(sampleID uint64, atNs int64) {
	if sampleID == 0 || atNs <= 0 {
		return
	}
	pendingInputMu.Lock()
	if pendingInput.SampleID == sampleID {
		pendingInput.ServerInputInjectedNs = atNs
	}
	pendingInputMu.Unlock()
}

// ArmLatencyProbeSample publishes the control sample identity immediately
// before the corresponding input is injected. The probe consumes this value
// when it receives the button event and embeds the same identity in its video
// marker, eliminating ambiguity from repeated decoded frames while a marker
// remains on screen.
func ArmLatencyProbeSample(sampleID uint64) {
	if sampleID == 0 {
		return
	}
	tmp := latencyProbeNextSamplePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(sampleID, 10)), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, latencyProbeNextSamplePath)
}

func SetLastInputReceivedAt(t int64) {
	SetLastInputSample(0, 0, t*1_000_000)
}

func SetLastInputSample(sampleID uint64, clientInputSendNs, serverInputReceivedNs int64) {
	if sampleID == 0 && clientInputSendNs == 0 && serverInputReceivedNs == 0 {
		return
	}
	pendingInputMu.Lock()
	defer pendingInputMu.Unlock()
	pendingInput = pendingInputSample{
		SampleID:              sampleID,
		ClientInputSendNs:     clientInputSendNs,
		ServerInputReceivedNs: serverInputReceivedNs,
	}
}

func ReadLatencyProbeState() (latencyProbeStateFile, bool) {
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
	// Keep enough records for the benchmark's warm-up plus measured window;
	// collection happens after the run rather than streaming each sample.
	const maxTraceRecords = 512
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

func SnapshotLatencyTrace(markerStr string) (LatencyTraceRecord, bool) {
	targetMarker, _ := strconv.Atoi(markerStr)

	latencyTraceMu.RLock()
	defer latencyTraceMu.RUnlock()

	if targetMarker > 0 {
		r, ok := latencyTraceRecords[targetMarker]
		return r, ok
	}

	// If no marker specified, return the latest
	var bestMarker int
	var record LatencyTraceRecord
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
