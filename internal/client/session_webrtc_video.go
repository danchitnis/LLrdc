package client

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

func videoSampleBuilderConfig(lowLatency bool) (uint16, time.Duration) {
	if lowLatency {
		return 8, 12 * time.Millisecond
	}
	return 256, 0
}

func newVideoSampleBuilder(codecName string, lowLatency bool) *samplebuilder.SampleBuilder {
	maxLate, maxDelay := videoSampleBuilderConfig(lowLatency)
	opts := make([]samplebuilder.Option, 0, 1)
	if maxDelay > 0 {
		opts = append(opts, samplebuilder.WithMaxTimeDelay(maxDelay))
	}

	if strings.Contains(codecName, "vp8") {
		return samplebuilder.New(maxLate, &codecs.VP8Packet{}, 90000, opts...)
	}
	if strings.Contains(codecName, "h264") {
		return samplebuilder.New(maxLate, &codecs.H264Packet{}, 90000, opts...)
	}
	if strings.Contains(codecName, "h265") || strings.Contains(codecName, "hevc") {
		return samplebuilder.New(maxLate, &codecs.H265Packet{}, 90000, opts...)
	}
	return nil
}

type timedVideoFrameHandler interface {
	handleVideoFrameWithTiming(
		codec string,
		frame []byte,
		packetTimestamp uint32,
		firstPacketSequenceNumber uint16,
		firstDecryptedPacketQueuedAt int64,
		firstRemotePacketAt int64,
		firstPacketReadAt int64,
		receiveAt int64,
	) error
}

type vp8ULLFrame struct {
	data                         []byte
	packetTimestamp              uint32
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
}

type vp8ULLAssembler struct {
	active                       bool
	timestamp                    uint32
	nextSequence                 uint16
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	frame                        []byte
}

func (a *vp8ULLAssembler) reset() {
	a.active = false
	a.timestamp = 0
	a.nextSequence = 0
	a.firstPacketSequenceNumber = 0
	a.firstDecryptedPacketQueuedAt = 0
	a.firstRemotePacketAt = 0
	a.firstPacketReadAt = 0
	a.frame = nil
}

func (a *vp8ULLAssembler) start(packet *rtp.Packet, timing packetTiming, firstPacketReadAt int64, payload []byte) {
	a.active = true
	a.timestamp = packet.Timestamp
	a.nextSequence = packet.SequenceNumber + 1
	a.firstPacketSequenceNumber = packet.SequenceNumber
	a.firstDecryptedPacketQueuedAt = timing.firstDecryptedPacketQueuedAt
	a.firstRemotePacketAt = timing.firstRemotePacketAt
	a.firstPacketReadAt = firstPacketReadAt
	a.frame = append(a.frame[:0], payload...)
}

func (a *vp8ULLAssembler) push(packet *rtp.Packet, timing packetTiming, packetReadAt int64) (frame vp8ULLFrame, ready bool, dropped bool, err error) {
	packetizer := &codecs.VP8Packet{}

	if a.active && (packet.Timestamp != a.timestamp || packet.SequenceNumber != a.nextSequence) {
		dropped = len(a.frame) > 0
		a.reset()
	}

	if !packetizer.IsPartitionHead(packet.Payload) {
		if !a.active {
			return frame, false, dropped, fmt.Errorf("vp8 low-latency frame missing partition head")
		}
	}

	payload, err := packetizer.Unmarshal(packet.Payload)
	if err != nil {
		a.reset()
		return frame, false, true, err
	}

	if !a.active {
		a.start(packet, timing, packetReadAt, payload)
	} else {
		a.nextSequence = packet.SequenceNumber + 1
		a.frame = append(a.frame, payload...)
	}

	if !packet.Marker {
		return frame, false, dropped, nil
	}

	frame = vp8ULLFrame{
		data:                         append([]byte(nil), a.frame...),
		packetTimestamp:              a.timestamp,
		firstPacketSequenceNumber:    a.firstPacketSequenceNumber,
		firstDecryptedPacketQueuedAt: a.firstDecryptedPacketQueuedAt,
		firstRemotePacketAt:          a.firstRemotePacketAt,
		firstPacketReadAt:            a.firstPacketReadAt,
	}
	a.reset()
	return frame, true, dropped, nil
}

func shouldUseVP8ULLAssembler(codecName string, lowLatency bool) bool {
	return lowLatency && strings.Contains(codecName, "vp8")
}

func (s *Session) requestVideoKeyframe() {
	s.renderer.RequestKeyframe()
	select {
	case s.keyframeRequests <- struct{}{}:
	default:
	}
}

func (s *Session) consumeVideoTrack(pc *webrtc.PeerConnection, track *webrtc.TrackRemote, lowLatency bool) {
	codecName := strings.ToLower(track.Codec().MimeType)
	useVP8ULLAssembler := shouldUseVP8ULLAssembler(codecName, lowLatency)
	builder := newVideoSampleBuilder(codecName, lowLatency)
	if useVP8ULLAssembler {
		builder = nil
	}
	var vp8Assembler vp8ULLAssembler
	stopKeyframeRequests := make(chan struct{})
	var stopKeyframeOnce sync.Once
	stopKeyframe := func() {
		stopKeyframeOnce.Do(func() {
			close(stopKeyframeRequests)
		})
	}
	if pc != nil {
		go requestInitialKeyframes(pc, uint32(track.SSRC()), stopKeyframeRequests)

		go func() {
			for {
				select {
				case <-stopKeyframeRequests:
					return
				case <-s.keyframeRequests:
					fmt.Printf("DEBUG: Sending PLI on video track (SSRC: %d) due to decode error or packet loss\n", track.SSRC())
					_ = pc.WriteRTCP([]rtcp.Packet{
						&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())},
					})
				}
			}
		}()
	}
	defer stopKeyframe()
	firstDecryptedPacketQueuedAtByTimestamp := make(map[uint32]int64)
	firstRemotePacketAtByTimestamp := make(map[uint32]int64)
	firstPacketReadAtByTimestamp := make(map[uint32]int64)
	firstPacketSequenceNumberByTimestamp := make(map[uint32]uint16)

	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			s.setError(err)
			return
		}

		s.mu.Lock()
		now := time.Now()
		s.stats.VideoPackets++
		s.stats.VideoBytes += uint64(packet.MarshalSize())
		s.state.LastVideoPacketAt = now
		s.recordVideoByteSampleLocked(now, packet.MarshalSize())
		s.mu.Unlock()

		if builder == nil {
			if !useVP8ULLAssembler {
				continue
			}
			timing := s.popRemotePacketTiming(uint32(track.SSRC()), packet.Timestamp, packet.SequenceNumber)
			packetReadAt := benchmarkClockNowMs()
			if timing.firstRemotePacketAt <= 0 {
				timing.firstRemotePacketAt = packetReadAt
			}
			if timing.firstDecryptedPacketQueuedAt <= 0 {
				timing.firstDecryptedPacketQueuedAt = timing.firstRemotePacketAt
			}
			frame, ready, dropped, err := vp8Assembler.push(packet, timing, packetReadAt)
			if dropped || err != nil {
				s.requestVideoKeyframe()
			}
			if err != nil || !ready {
				continue
			}

			s.mu.Lock()
			s.stats.VideoFrames++
			s.state.LastVideoFrameAt = time.Now()
			s.mu.Unlock()
			if shouldStopInitialKeyframeRequests(track.Codec().MimeType, frame.data) {
				stopKeyframe()
			}

			if timedRenderer, ok := s.renderer.(timedVideoFrameHandler); ok {
				if err := timedRenderer.handleVideoFrameWithTiming(
					track.Codec().MimeType,
					frame.data,
					frame.packetTimestamp,
					frame.firstPacketSequenceNumber,
					frame.firstDecryptedPacketQueuedAt,
					frame.firstRemotePacketAt,
					frame.firstPacketReadAt,
					packetReadAt,
				); err != nil {
					s.setError(err)
				}
			} else if err := s.renderer.HandleVideoFrame(track.Codec().MimeType, frame.data, frame.packetTimestamp); err != nil {
				s.setError(err)
			}

			s.emit(EventFrame, map[string]any{
				"codec":           track.Codec().MimeType,
				"packetTimestamp": frame.packetTimestamp,
				"size":            len(frame.data),
				"droppedPackets":  map[bool]int{false: 0, true: 1}[dropped],
			})
			continue
		}

		timing := s.popRemotePacketTiming(uint32(track.SSRC()), packet.Timestamp, packet.SequenceNumber)
		if timing.firstDecryptedPacketQueuedAt > 0 {
			firstDecryptedPacketQueuedAtByTimestamp[packet.Timestamp] = minPositiveTime(firstDecryptedPacketQueuedAtByTimestamp[packet.Timestamp], timing.firstDecryptedPacketQueuedAt)
		}
		if timing.firstRemotePacketAt > 0 {
			firstRemotePacketAtByTimestamp[packet.Timestamp] = minPositiveTime(firstRemotePacketAtByTimestamp[packet.Timestamp], timing.firstRemotePacketAt)
		}
		if current, ok := firstPacketSequenceNumberByTimestamp[packet.Timestamp]; !ok || sequenceBefore(packet.SequenceNumber, current) {
			firstPacketSequenceNumberByTimestamp[packet.Timestamp] = packet.SequenceNumber
		}
		firstPacketReadAtByTimestamp[packet.Timestamp] = minPositiveTime(firstPacketReadAtByTimestamp[packet.Timestamp], benchmarkClockNowMs())
		builder.Push(packet)
		for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
			s.mu.Lock()
			s.stats.VideoFrames++
			s.state.LastVideoFrameAt = time.Now()
			s.mu.Unlock()
			if shouldStopInitialKeyframeRequests(track.Codec().MimeType, sample.Data) {
				stopKeyframe()
			}

			firstDecryptedPacketQueuedAt := firstDecryptedPacketQueuedAtByTimestamp[sample.PacketTimestamp]
			delete(firstDecryptedPacketQueuedAtByTimestamp, sample.PacketTimestamp)
			firstRemotePacketAt := firstRemotePacketAtByTimestamp[sample.PacketTimestamp]
			delete(firstRemotePacketAtByTimestamp, sample.PacketTimestamp)
			firstPacketReadAt := firstPacketReadAtByTimestamp[sample.PacketTimestamp]
			delete(firstPacketReadAtByTimestamp, sample.PacketTimestamp)
			firstPacketSequenceNumber := firstPacketSequenceNumberByTimestamp[sample.PacketTimestamp]
			delete(firstPacketSequenceNumberByTimestamp, sample.PacketTimestamp)
			sampleReadyAt := benchmarkClockNowMs()
			if timedRenderer, ok := s.renderer.(timedVideoFrameHandler); ok {
				if err := timedRenderer.handleVideoFrameWithTiming(
					track.Codec().MimeType,
					sample.Data,
					sample.PacketTimestamp,
					firstPacketSequenceNumber,
					firstDecryptedPacketQueuedAt,
					firstRemotePacketAt,
					firstPacketReadAt,
					sampleReadyAt,
				); err != nil {
					s.setError(err)
				}
			} else if err := s.renderer.HandleVideoFrame(track.Codec().MimeType, sample.Data, sample.PacketTimestamp); err != nil {
				s.setError(err)
			}

			if sample.PrevDroppedPackets > 0 {
				s.renderer.RequestKeyframe()
				select {
				case s.keyframeRequests <- struct{}{}:
				default:
				}
			}

			s.emit(EventFrame, map[string]any{
				"codec":           track.Codec().MimeType,
				"packetTimestamp": sample.PacketTimestamp,
				"size":            len(sample.Data),
				"droppedPackets":  sample.PrevDroppedPackets,
			})
		}
	}
}

func shouldStopInitialKeyframeRequests(codec string, frame []byte) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	if strings.Contains(codec, "vp8") {
		return isVP8KeyframePayload(frame)
	} else if strings.Contains(codec, "h264") {
		return isH264KeyframePayload(frame)
	}
	return len(frame) > 0
}

func isVP8KeyframePayload(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return data[0]&0x01 == 0
}
