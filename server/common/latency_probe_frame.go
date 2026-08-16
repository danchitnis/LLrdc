package common

func StartLatencyProbeFrameSend(frameAtMs int64) *LatencyProbeSendTrace {
	state, ok := ReadLatencyProbeState()
	if !ok || state.SourcePresentedNs <= 0 || frameAtMs < state.DrawnAtMs {
		return nil
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	pendingInputMu.Lock()
	input := pendingInput
	pendingInputMu.Unlock()

	record, exists := latencyTraceRecords[state.Marker]
	if !exists {
		record.Marker = state.Marker
		record.SampleID = input.SampleID
		record.ClientInputSendNs = input.ClientInputSendNs
		record.ServerInputReceivedNs = input.ServerInputReceivedNs
		record.ServerInputInjectedNs = input.ServerInputInjectedNs
		record.ServerTimeMs = input.ServerInputReceivedNs / 1_000_000
		record.ProbeCommitNs = state.ProbeCommitNs
		record.ProbeRequestedNs = state.ProbeRequestedNs
		record.ProbeDrawnNs = state.ProbeDrawnNs
		record.SourcePresentedNs = state.SourcePresentedNs
		record.SourcePresentationClockID = state.PresentationClockID
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
	if !ok || state.SourcePresentedNs <= 0 || frameAtMs < state.DrawnAtMs {
		return nil
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	pendingInputMu.Lock()
	input := pendingInput
	pendingInputMu.Unlock()

	record, exists := latencyTraceRecords[state.Marker]
	if !exists {
		record.Marker = state.Marker
		record.SampleID = input.SampleID
		record.ClientInputSendNs = input.ClientInputSendNs
		record.ServerInputReceivedNs = input.ServerInputReceivedNs
		record.ServerInputInjectedNs = input.ServerInputInjectedNs
		record.ServerTimeMs = input.ServerInputReceivedNs / 1_000_000
		record.ProbeCommitNs = state.ProbeCommitNs
		record.ProbeRequestedNs = state.ProbeRequestedNs
		record.ProbeDrawnNs = state.ProbeDrawnNs
		record.SourcePresentedNs = state.SourcePresentedNs
		record.SourcePresentationClockID = state.PresentationClockID
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
	if trace == nil || frameAtMs <= 0 {
		return
	}
	NoteLatencyProbeFirstPacketAttempt(trace, frameAtMs)
	NoteLatencyProbeFirstPacket(trace, frameAtMs)
	NoteLatencyProbeLastPacket(trace, frameAtMs)
}

func NoteLatencyProbeWebTransportWriteStart(trace *LatencyProbeSendTrace, atNs int64) {
	if trace == nil || atNs <= 0 {
		return
	}
	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()
	record, ok := latencyTraceRecords[trace.Marker]
	if !ok {
		return
	}
	if record.WebTransportWriteStartNs == 0 {
		record.WebTransportWriteStartNs = atNs
	}
	latencyTraceRecords[trace.Marker] = record
}

func NoteLatencyProbeWebTransportWriteEnd(trace *LatencyProbeSendTrace, atNs int64) {
	if trace == nil || atNs <= 0 {
		return
	}
	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()
	record, ok := latencyTraceRecords[trace.Marker]
	if !ok {
		return
	}
	if record.WebTransportWriteEndNs == 0 {
		record.WebTransportWriteEndNs = atNs
	}
	latencyTraceRecords[trace.Marker] = record
}

func RecordLatencyProbeFrame(frameAtMs int64) {
	trace := StartLatencyProbeFrameSend(frameAtMs)
	FinishLatencyProbeFrameSend(trace, frameAtMs)
}
