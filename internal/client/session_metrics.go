package client

import "time"

func (s *Session) RecordPresentedFrame(event NativeFramePresented) {
	s.mu.Lock()
	s.stats.PresentedFrames++
	now := time.Now()
	s.state.LastPresentAt = now
	s.state.Presenting = true
	s.state.LastPresentedWidth = event.Width
	s.state.LastPresentedHeight = event.Height
	if s.state.FirstFramePresentedAt.IsZero() {
		s.state.FirstFramePresentedAt = now
	}

	sample := LatencyBreakdown{
		PacketTimestamp:              event.PacketTimestamp,
		FirstPacketSequenceNumber:    event.FirstPacketSequenceNumber,
		Brightness:                   event.Brightness,
		ProbeMarker:                  event.ProbeMarker,
		FirstDecryptedPacketQueuedAt: event.FirstDecryptedPacketQueuedAt,
		FirstRemotePacketAt:          event.FirstRemotePacketAt,
		FirstPacketReadAt:            event.FirstPacketReadAt,
		ReceiveAt:                    event.ReceiveAt,
		DecodeReadyAt:                event.DecodeReadyAt,
		PresentationAt:               event.PresentationAt,
		PresentationSource:           event.PresentationSource,
	}
	if event.CompositorPresentedAt > 0 {
		sample.CompositorPresentedAt = event.CompositorPresentedAt
	}
	s.state.LastLatencySample = map[string]any{
		"packetTimestamp":              sample.PacketTimestamp,
		"firstPacketSequenceNumber":    sample.FirstPacketSequenceNumber,
		"brightness":                   sample.Brightness,
		"probeMarker":                  sample.ProbeMarker,
		"firstDecryptedPacketQueuedAt": sample.FirstDecryptedPacketQueuedAt,
		"firstRemotePacketAt":          sample.FirstRemotePacketAt,
		"firstPacketReadAt":            sample.FirstPacketReadAt,
		"receiveAt":                    sample.ReceiveAt,
		"decodeReadyAt":                sample.DecodeReadyAt,
		"presentationAt":               sample.PresentationAt,
		"compositorPresentedAt":        sample.CompositorPresentedAt,
		"presentationSource":           sample.PresentationSource,
	}
	s.state.RecentLatencySamples = append(s.state.RecentLatencySamples, sample)
	if len(s.state.RecentLatencySamples) > 300 {
		s.state.RecentLatencySamples = s.state.RecentLatencySamples[1:]
	}

	s.mu.Unlock()
	s.emit(EventFrame, map[string]any{
		"presented":          true,
		"width":              event.Width,
		"height":             event.Height,
		"packetTimestamp":    event.PacketTimestamp,
		"brightness":         event.Brightness,
		"presentationSource": event.PresentationSource,
	})
}

func (s *Session) RecordLocalInput(sample LocalInputSample) {
	s.mu.Lock()
	s.state.RecentLocalInputSamples = append(s.state.RecentLocalInputSamples, sample)
	if len(s.state.RecentLocalInputSamples) > 300 {
		s.state.RecentLocalInputSamples = s.state.RecentLocalInputSamples[1:]
	}
	s.mu.Unlock()
	s.emit(EventInputSent, map[string]any{
		"type":   sample.Type,
		"action": sample.Action,
		"button": sample.Button,
		"key":    sample.Key,
		"x":      sample.X,
		"y":      sample.Y,
		"atMs":   sample.AtMs,
		"source": "local_renderer",
	})
}

func (s *Session) recordVideoByteSampleLocked(at time.Time, size int) {
	s.state.RecentVideoByteSamples = append(s.state.RecentVideoByteSamples, TimedByteSample{
		AtMs:  at.UnixMilli(),
		Bytes: size,
	})
	if len(s.state.RecentVideoByteSamples) > 600 {
		s.state.RecentVideoByteSamples = s.state.RecentVideoByteSamples[len(s.state.RecentVideoByteSamples)-600:]
	}
}

func (s *Session) RecordDecodeAwaitingKeyframe(awaiting bool) {
	s.mu.Lock()
	s.state.DecoderAwaitingKeyframe = awaiting
	s.mu.Unlock()
	s.emit(EventStateChanged, map[string]any{
		"decoderAwaitingKeyframe": awaiting,
	})
}
