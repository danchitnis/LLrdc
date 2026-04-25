package server

import (
	"encoding/json"
	"os"
	"testing"
)

func resetLatencyProbeState(t *testing.T) {
	t.Helper()
	latencyTraceMu.Lock()
	latencyTraceRecords = map[int]latencyTraceRecord{}
	latencyTraceMu.Unlock()

	pendingInputMu.Lock()
	pendingInputTime = 0
	pendingInputMu.Unlock()

	pendingSampleTraceMu.Lock()
	pendingSampleTrace = nil
	pendingSampleTraceMu.Unlock()

	if err := os.Remove(latencyProbeStatePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove latency probe state: %v", err)
	}
}

func writeLatencyProbeStateFile(t *testing.T, state latencyProbeStateFile) {
	t.Helper()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(latencyProbeStatePath, payload, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

func TestLatencyProbeSendTraceRecordsOrdering(t *testing.T) {
	resetLatencyProbeState(t)
	t.Cleanup(func() { resetLatencyProbeState(t) })

	setLastInputReceivedAt(10)
	writeLatencyProbeStateFile(t, latencyProbeStateFile{
		Marker:        7,
		RequestedAtMs: 20,
		DrawnAtMs:     30,
	})

	trace := startLatencyProbeEncodedFrame(35, 1234)
	if trace == nil {
		t.Fatalf("startLatencyProbeEncodedFrame returned nil")
	}
	noteLatencyProbeFrameDispatch(trace, 37)
	noteLatencyProbeFrameSendStart(trace, 40)
	trace = startLatencyProbeFrameSend(40)
	if trace == nil {
		t.Fatalf("startLatencyProbeFrameSend returned nil")
	}
	noteLatencyProbeFirstPacketIdentity(trace, 22, 333)
	noteLatencyProbeFirstPacketAttempt(trace, 41)
	noteLatencyProbeFirstPacket(trace, 42)
	noteLatencyProbeFirstPacketSocketWrite([]byte{0x80, 0x60, 0x00, 0x16, 0x00, 0x00, 0x01, 0x4d, 0, 0, 0, 1}, 43)
	noteLatencyProbeLastPacket(trace, 45)

	record, ok := snapshotLatencyTrace("7")
	if !ok {
		t.Fatalf("snapshotLatencyTrace did not return record")
	}
	if got, want := record.ServerTimeMs, int64(10); got != want {
		t.Fatalf("ServerTimeMs = %d, want %d", got, want)
	}
	if got, want := record.RequestedAtMs, int64(20); got != want {
		t.Fatalf("RequestedAtMs = %d, want %d", got, want)
	}
	if got, want := record.DrawnAtMs, int64(30); got != want {
		t.Fatalf("DrawnAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstEncodedFrameParsedAtMs, int64(35); got != want {
		t.Fatalf("FirstEncodedFrameParsedAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstEncodedFrameContainerTimestamp, uint64(1234); got != want {
		t.Fatalf("FirstEncodedFrameContainerTimestamp = %d, want %d", got, want)
	}
	if got, want := record.FirstFrameDispatchAtMs, int64(37); got != want {
		t.Fatalf("FirstFrameDispatchAtMs = %d, want %d", got, want)
	}
	if got, want := record.FrameSendStartAtMs, int64(40); got != want {
		t.Fatalf("FrameSendStartAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketSequenceNumber, uint16(22); got != want {
		t.Fatalf("FirstPacketSequenceNumber = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketTimestamp, uint32(333); got != want {
		t.Fatalf("FirstPacketTimestamp = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketWriteAttemptAtMs, int64(41); got != want {
		t.Fatalf("FirstPacketWriteAttemptAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketWriteReturnAtMs, int64(42); got != want {
		t.Fatalf("FirstPacketWriteReturnAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketSocketWriteAtMs, int64(43); got != want {
		t.Fatalf("FirstPacketSocketWriteAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketWrittenAtMs, int64(42); got != want {
		t.Fatalf("FirstPacketWrittenAtMs = %d, want %d", got, want)
	}
	if got, want := record.LastPacketWrittenAtMs, int64(45); got != want {
		t.Fatalf("LastPacketWrittenAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstFrameBroadcastAtMs, int64(42); got != want {
		t.Fatalf("FirstFrameBroadcastAtMs = %d, want %d", got, want)
	}
}

func TestFinishLatencyProbeFrameSendBackfillsPacketTimes(t *testing.T) {
	resetLatencyProbeState(t)
	t.Cleanup(func() { resetLatencyProbeState(t) })

	setLastInputReceivedAt(100)
	writeLatencyProbeStateFile(t, latencyProbeStateFile{
		Marker:        9,
		RequestedAtMs: 110,
		DrawnAtMs:     120,
	})

	trace := startLatencyProbeEncodedFrame(125, 987)
	if trace == nil {
		t.Fatalf("startLatencyProbeEncodedFrame returned nil")
	}
	noteLatencyProbeFrameDispatch(trace, 128)
	noteLatencyProbeFrameSendStart(trace, 130)
	trace = startLatencyProbeFrameSend(130)
	if trace == nil {
		t.Fatalf("startLatencyProbeFrameSend returned nil")
	}
	finishLatencyProbeFrameSend(trace, 135)

	record, ok := snapshotLatencyTrace("9")
	if !ok {
		t.Fatalf("snapshotLatencyTrace did not return record")
	}
	if got, want := record.FirstPacketWrittenAtMs, int64(135); got != want {
		t.Fatalf("FirstPacketWrittenAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstEncodedFrameParsedAtMs, int64(125); got != want {
		t.Fatalf("FirstEncodedFrameParsedAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstEncodedFrameContainerTimestamp, uint64(987); got != want {
		t.Fatalf("FirstEncodedFrameContainerTimestamp = %d, want %d", got, want)
	}
	if got, want := record.FirstFrameDispatchAtMs, int64(128); got != want {
		t.Fatalf("FirstFrameDispatchAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketWriteAttemptAtMs, int64(135); got != want {
		t.Fatalf("FirstPacketWriteAttemptAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketWriteReturnAtMs, int64(135); got != want {
		t.Fatalf("FirstPacketWriteReturnAtMs = %d, want %d", got, want)
	}
	if got, want := record.LastPacketWrittenAtMs, int64(135); got != want {
		t.Fatalf("LastPacketWrittenAtMs = %d, want %d", got, want)
	}
}

func TestPendingSampleTraceCapturesFirstPacketIdentityFromSocketWrite(t *testing.T) {
	resetLatencyProbeState(t)
	t.Cleanup(func() { resetLatencyProbeState(t) })

	setLastInputReceivedAt(200)
	writeLatencyProbeStateFile(t, latencyProbeStateFile{
		Marker:        11,
		RequestedAtMs: 210,
		DrawnAtMs:     220,
	})

	trace := startLatencyProbeFrameSend(225)
	if trace == nil {
		t.Fatalf("startLatencyProbeFrameSend returned nil")
	}
	noteLatencyProbeFirstPacketAttempt(trace, 226)
	armLatencyProbePendingSampleTrace(trace)
	noteLatencyProbeFirstPacketSocketWrite([]byte{0x80, 0x60, 0x01, 0x02, 0x00, 0x00, 0x03, 0x04, 0, 0, 0, 1}, 227)
	finishLatencyProbeFrameSend(trace, 229)

	record, ok := snapshotLatencyTrace("11")
	if !ok {
		t.Fatalf("snapshotLatencyTrace did not return record")
	}
	if got, want := record.FirstPacketSequenceNumber, uint16(258); got != want {
		t.Fatalf("FirstPacketSequenceNumber = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketTimestamp, uint32(772); got != want {
		t.Fatalf("FirstPacketTimestamp = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketSocketWriteAtMs, int64(227); got != want {
		t.Fatalf("FirstPacketSocketWriteAtMs = %d, want %d", got, want)
	}
	if got, want := record.FirstPacketWriteReturnAtMs, int64(227); got != want {
		t.Fatalf("FirstPacketWriteReturnAtMs = %d, want %d", got, want)
	}
	if got, want := record.LastPacketWrittenAtMs, int64(229); got != want {
		t.Fatalf("LastPacketWrittenAtMs = %d, want %d", got, want)
	}
}
