package client

type packetTimingKey struct {
	ssrc      uint32
	timestamp uint32
	sequence  uint16
}

type packetTiming struct {
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
}

func minPositiveTime(current int64, candidate int64) int64 {
	if current <= 0 {
		return candidate
	}
	if candidate <= 0 || candidate > current {
		return current
	}
	return candidate
}

func sequenceBefore(candidate uint16, current uint16) bool {
	return int16(candidate-current) < 0
}

func remotePacketKey(ssrc uint32, timestamp uint32, sequence uint16) packetTimingKey {
	return packetTimingKey{
		ssrc:      ssrc,
		timestamp: timestamp,
		sequence:  sequence,
	}
}

func (s *Session) recordDecryptedPacketAt(ssrc uint32, timestamp uint32, sequence uint16, at int64) {
	if at <= 0 {
		return
	}
	key := remotePacketKey(ssrc, timestamp, sequence)
	s.remotePacketMu.Lock()
	timing := s.remotePacketTimes[key]
	timing.firstPacketSequenceNumber = sequence
	timing.firstDecryptedPacketQueuedAt = minPositiveTime(timing.firstDecryptedPacketQueuedAt, at)
	s.remotePacketTimes[key] = timing
	s.remotePacketMu.Unlock()
}

func (s *Session) recordRemotePacketAt(ssrc uint32, timestamp uint32, sequence uint16, at int64) {
	if at <= 0 {
		return
	}
	key := remotePacketKey(ssrc, timestamp, sequence)
	s.remotePacketMu.Lock()
	timing := s.remotePacketTimes[key]
	timing.firstPacketSequenceNumber = sequence
	timing.firstRemotePacketAt = minPositiveTime(timing.firstRemotePacketAt, at)
	s.remotePacketTimes[key] = timing
	s.remotePacketMu.Unlock()
}

func (s *Session) popRemotePacketTiming(ssrc uint32, timestamp uint32, sequence uint16) packetTiming {
	key := remotePacketKey(ssrc, timestamp, sequence)
	s.remotePacketMu.Lock()
	timing := s.remotePacketTimes[key]
	delete(s.remotePacketTimes, key)
	s.remotePacketMu.Unlock()
	return timing
}
