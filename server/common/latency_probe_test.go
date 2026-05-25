package common

import (
	"encoding/json"
	"os"
	"testing"
)

func resetLatencyProbeState(t *testing.T) {
	t.Helper()
	latencyTraceMu.Lock()
	latencyTraceRecords = map[int]LatencyTraceRecord{}
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

	SetLastInputReceivedAt(10)
	writeLatencyProbeStateFile(t, latencyProbeStateFile{
		Marker:        7,
		RequestedAtMs: 20,
		DrawnAtMs:     30,
	})

	trace := StartLatencyProbeEncodedFrame(35, 1234)
	if trace == nil {
		t.Fatalf("StartLatencyProbeEncodedFrame returned nil")
	}
	NoteLatencyProbeFrameDispatch(trace, 37)
	NoteLatencyProbeFrameSendStart(trace, 40)
	trace = StartLatencyProbeFrameSend(40)
	if trace == nil {
		t.Fatalf("StartLatencyProbeFrameSend returned nil")
	}
	NoteLatencyProbeFirstPacketIdentity(trace, 22, 333)
	NoteLatencyProbeFirstPacketAttempt(trace, 41)
	NoteLatencyProbeFirstPacket(trace, 42)
	NoteLatencyProbeFirstPacketSocketWrite([]byte{0x80, 0x60, 0x00, 0x16, 0x00, 0x00, 0x01, 0x4d, 0, 0, 0, 1}, 43)
	NoteLatencyProbeLastPacket(trace, 45)

	record, ok := SnapshotLatencyTrace("7")
	if !ok {
		t.Fatalf("SnapshotLatencyTrace did not return record")
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

	SetLastInputReceivedAt(100)
	writeLatencyProbeStateFile(t, latencyProbeStateFile{
		Marker:        9,
		RequestedAtMs: 110,
		DrawnAtMs:     120,
	})

	trace := StartLatencyProbeEncodedFrame(125, 987)
	if trace == nil {
		t.Fatalf("StartLatencyProbeEncodedFrame returned nil")
	}
	NoteLatencyProbeFrameDispatch(trace, 128)
	NoteLatencyProbeFrameSendStart(trace, 130)
	trace = StartLatencyProbeFrameSend(130)
	if trace == nil {
		t.Fatalf("StartLatencyProbeFrameSend returned nil")
	}
	FinishLatencyProbeFrameSend(trace, 135)

	record, ok := SnapshotLatencyTrace("9")
	if !ok {
		t.Fatalf("SnapshotLatencyTrace did not return record")
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

	SetLastInputReceivedAt(200)
	writeLatencyProbeStateFile(t, latencyProbeStateFile{
		Marker:        11,
		RequestedAtMs: 210,
		DrawnAtMs:     220,
	})

	trace := StartLatencyProbeFrameSend(225)
	if trace == nil {
		t.Fatalf("StartLatencyProbeFrameSend returned nil")
	}
	NoteLatencyProbeFirstPacketAttempt(trace, 226)
	ArmLatencyProbePendingSampleTrace(trace)
	NoteLatencyProbeFirstPacketSocketWrite([]byte{0x80, 0x60, 0x01, 0x02, 0x00, 0x00, 0x03, 0x04, 0, 0, 0, 1}, 227)
	FinishLatencyProbeFrameSend(trace, 229)

	record, ok := SnapshotLatencyTrace("11")
	if !ok {
		t.Fatalf("SnapshotLatencyTrace did not return record")
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
