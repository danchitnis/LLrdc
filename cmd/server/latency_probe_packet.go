package main

func armLatencyProbePendingSampleTrace(trace *latencyProbeSendTrace) {
	if trace == nil {
		return
	}
	pendingSampleTraceMu.Lock()
	defer pendingSampleTraceMu.Unlock()
	pendingSampleTrace = trace
}

func clearLatencyProbePendingSampleTrace(trace *latencyProbeSendTrace) {
	if trace == nil {
		return
	}
	pendingSampleTraceMu.Lock()
	defer pendingSampleTraceMu.Unlock()
	if pendingSampleTrace != nil && pendingSampleTrace.marker == trace.marker {
		pendingSampleTrace = nil
	}
}

func noteLatencyProbePendingSamplePacket(sequence uint16, timestamp uint32, packetAtMs int64) bool {
	if packetAtMs <= 0 || timestamp == 0 {
		return false
	}

	pendingSampleTraceMu.Lock()
	trace := pendingSampleTrace
	if trace != nil {
		pendingSampleTrace = nil
	}
	pendingSampleTraceMu.Unlock()
	if trace == nil {
		return false
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return false
	}
	if record.FirstPacketSequenceNumber == 0 {
		record.FirstPacketSequenceNumber = sequence
	}
	if record.FirstPacketTimestamp == 0 {
		record.FirstPacketTimestamp = timestamp
	}
	if record.FirstPacketWriteReturnAtMs == 0 {
		record.FirstPacketWriteReturnAtMs = packetAtMs
	}
	if record.FirstPacketWrittenAtMs == 0 {
		record.FirstPacketWrittenAtMs = packetAtMs
	}
	if record.FirstFrameBroadcastAtMs == 0 {
		record.FirstFrameBroadcastAtMs = packetAtMs
	}
	if record.FirstPacketSocketWriteAtMs == 0 {
		record.FirstPacketSocketWriteAtMs = packetAtMs
	}
	latencyTraceRecords[trace.marker] = record
	return true
}

func noteLatencyProbeFirstPacket(trace *latencyProbeSendTrace, packetAtMs int64) {
	if trace == nil || packetAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return
	}
	if record.FirstPacketWriteReturnAtMs == 0 {
		record.FirstPacketWriteReturnAtMs = packetAtMs
	}
	if record.FirstPacketWrittenAtMs == 0 {
		record.FirstPacketWrittenAtMs = packetAtMs
	}
	if record.FirstFrameBroadcastAtMs == 0 {
		record.FirstFrameBroadcastAtMs = packetAtMs
	}
	latencyTraceRecords[trace.marker] = record
}

func noteLatencyProbeFirstPacketAttempt(trace *latencyProbeSendTrace, packetAtMs int64) {
	if trace == nil || packetAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return
	}
	if record.FirstPacketWriteAttemptAtMs == 0 {
		record.FirstPacketWriteAttemptAtMs = packetAtMs
	}
	latencyTraceRecords[trace.marker] = record
}

func noteLatencyProbeFirstPacketIdentity(trace *latencyProbeSendTrace, sequence uint16, timestamp uint32) {
	if trace == nil || timestamp == 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return
	}
	if record.FirstPacketSequenceNumber == 0 {
		record.FirstPacketSequenceNumber = sequence
	}
	if record.FirstPacketTimestamp == 0 {
		record.FirstPacketTimestamp = timestamp
	}
	latencyTraceRecords[trace.marker] = record
}

func noteLatencyProbeLastPacket(trace *latencyProbeSendTrace, packetAtMs int64) {
	if trace == nil || packetAtMs <= 0 {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	record, ok := latencyTraceRecords[trace.marker]
	if !ok {
		return
	}
	if record.FirstPacketWriteReturnAtMs == 0 {
		record.FirstPacketWriteReturnAtMs = packetAtMs
	}
	if record.FirstPacketWrittenAtMs == 0 {
		record.FirstPacketWrittenAtMs = packetAtMs
	}
	if record.FirstFrameBroadcastAtMs == 0 {
		record.FirstFrameBroadcastAtMs = packetAtMs
	}
	if record.LastPacketWrittenAtMs == 0 || packetAtMs > record.LastPacketWrittenAtMs {
		record.LastPacketWrittenAtMs = packetAtMs
	}
	latencyTraceRecords[trace.marker] = record
}

func noteLatencyProbeFirstPacketSocketWrite(packet []byte, packetAtMs int64) {
	if len(packet) < 12 || packetAtMs <= 0 {
		return
	}

	var timestamp uint32
	var sequence uint16
	version := packet[0] >> 6
	if version != 2 {
		return
	}
	sequence = (uint16(packet[2]) << 8) | uint16(packet[3])
	timestamp = (uint32(packet[4]) << 24) | (uint32(packet[5]) << 16) | (uint32(packet[6]) << 8) | uint32(packet[7])
	if noteLatencyProbePendingSamplePacket(sequence, timestamp, packetAtMs) {
		return
	}

	latencyTraceMu.Lock()
	defer latencyTraceMu.Unlock()

	for marker, record := range latencyTraceRecords {
		if record.FirstPacketTimestamp != timestamp || record.FirstPacketSequenceNumber != sequence {
			continue
		}
		if record.FirstPacketSocketWriteAtMs == 0 {
			record.FirstPacketSocketWriteAtMs = packetAtMs
			latencyTraceRecords[marker] = record
		}
		return
	}
}
