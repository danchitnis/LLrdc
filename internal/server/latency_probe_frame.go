package server

func startLatencyProbeFrameSend(frameAtMs int64) *latencyProbeSendTrace {
	state, ok := readLatencyProbeState()
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
	return &latencyProbeSendTrace{marker: state.Marker}
}

func startLatencyProbeEncodedFrame(frameAtMs int64, containerTimestamp uint64) *latencyProbeSendTrace {
	state, ok := readLatencyProbeState()
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
	return &latencyProbeSendTrace{marker: state.Marker}
}

func noteLatencyProbeFrameDispatch(trace *latencyProbeSendTrace, dispatchAtMs int64) {
	if trace == nil || dispatchAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return
	}
	if record.FirstFrameDispatchAtMs == 0 {
		record.FirstFrameDispatchAtMs = dispatchAtMs
	}
	latencyTraceRecords[trace.marker] = record
}

func noteLatencyProbeFrameSendStart(trace *latencyProbeSendTrace, frameAtMs int64) {
	if trace == nil || frameAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return
	}
	if record.FrameSendStartAtMs == 0 {
		record.FrameSendStartAtMs = frameAtMs
	}
	latencyTraceRecords[trace.marker] = record
}

func finishLatencyProbeFrameSend(trace *latencyProbeSendTrace, frameAtMs int64) {
	if trace == nil {
		return
	}
	if frameAtMs <= 0 {
		frameAtMs = benchmarkClockNowMs()
	}
	noteLatencyProbeFirstPacketAttempt(trace, frameAtMs)
	noteLatencyProbeFirstPacket(trace, frameAtMs)
	noteLatencyProbeLastPacket(trace, frameAtMs)
}

func recordLatencyProbeFrame(frameAtMs int64) {
	trace := startLatencyProbeFrameSend(frameAtMs)
	finishLatencyProbeFrameSend(trace, frameAtMs)
}
