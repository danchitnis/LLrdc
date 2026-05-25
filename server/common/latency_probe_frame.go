package common

func StartLatencyProbeFrameSend(frameAtMs int64) *LatencyProbeSendTrace {
	state, ok := ReadLatencyProbeState()
	if !ok || frameAtMs < state.DrawnAtMs {
		return nil
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
	if record.FirstPacketWrittenAtMs != 0 {
		return nil
	}
	if record.FrameSendStartAtMs == 0 {
		record.FrameSendStartAtMs = frameAtMs
	}

	latencyTraceRecords[state.Marker] = record
	pruneLatencyTraceRecordsLocked(state.Marker)
	return &LatencyProbeSendTrace{Marker: state.Marker}
}

func StartLatencyProbeEncodedFrame(frameAtMs int64, containerTimestamp uint64) *LatencyProbeSendTrace {
	state, ok := ReadLatencyProbeState()
	if !ok || frameAtMs < state.DrawnAtMs {
		return nil
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
	if record.FirstPacketWrittenAtMs != 0 {
		return nil
	}
	if record.FirstEncodedFrameParsedAtMs == 0 {
		record.FirstEncodedFrameParsedAtMs = frameAtMs
	}
	if record.FirstEncodedFrameContainerTimestamp == 0 && containerTimestamp != 0 {
		record.FirstEncodedFrameContainerTimestamp = containerTimestamp
	}

	latencyTraceRecords[state.Marker] = record
	pruneLatencyTraceRecordsLocked(state.Marker)
	return &LatencyProbeSendTrace{Marker: state.Marker}
}

func NoteLatencyProbeFrameDispatch(trace *LatencyProbeSendTrace, dispatchAtMs int64) {
	if trace == nil || dispatchAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.Marker]
	if !ok {
		return
	}
	if record.FirstFrameDispatchAtMs == 0 {
		record.FirstFrameDispatchAtMs = dispatchAtMs
	}
	latencyTraceRecords[trace.Marker] = record
}

func NoteLatencyProbeFrameSendStart(trace *LatencyProbeSendTrace, frameAtMs int64) {
	if trace == nil || frameAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.Marker]
	if !ok {
		return
	}
	if record.FrameSendStartAtMs == 0 {
		record.FrameSendStartAtMs = frameAtMs
	}
	latencyTraceRecords[trace.Marker] = record
}

func FinishLatencyProbeFrameSend(trace *LatencyProbeSendTrace, frameAtMs int64) {
	if trace == nil {
		return
	}
	if frameAtMs <= 0 {
		frameAtMs = BenchmarkClockNowMs()
	}
	NoteLatencyProbeFirstPacketAttempt(trace, frameAtMs)
	NoteLatencyProbeFirstPacket(trace, frameAtMs)
	NoteLatencyProbeLastPacket(trace, frameAtMs)
}

func RecordLatencyProbeFrame(frameAtMs int64) {
	trace := StartLatencyProbeFrameSend(frameAtMs)
	FinishLatencyProbeFrameSend(trace, frameAtMs)
}
