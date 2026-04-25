package client

import (
	"io"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/transport/v4/packetio"
)

const latencyBufferSize = 1000 * 1000

type latencyBufferFactory func(packetType packetio.BufferPacketType, ssrc uint32) io.ReadWriteCloser

func newLatencyBufferFactory(
	now func() int64,
	record func(ssrc uint32, timestamp uint32, sequence uint16, at int64),
) latencyBufferFactory {
	return func(packetType packetio.BufferPacketType, ssrc uint32) io.ReadWriteCloser {
		buffer := packetio.NewBuffer()
		buffer.SetLimitSize(latencyBufferSize)
		if packetType != packetio.RTPBufferPacket || now == nil || record == nil {
			return buffer
		}
		return &latencyPacketBuffer{
			buffer: buffer,
			ssrc:   ssrc,
			now:    now,
			record: record,
		}
	}
}

type latencyPacketBuffer struct {
	buffer *packetio.Buffer
	ssrc   uint32
	now    func() int64
	record func(ssrc uint32, timestamp uint32, sequence uint16, at int64)
}

func (b *latencyPacketBuffer) Write(packet []byte) (int, error) {
	if b.record != nil && b.now != nil {
		header := &rtp.Header{}
		if _, err := header.Unmarshal(packet); err == nil {
			b.record(b.ssrc, header.Timestamp, header.SequenceNumber, b.now())
		}
	}
	return b.buffer.Write(packet)
}

func (b *latencyPacketBuffer) Read(packet []byte) (int, error) {
	return b.buffer.Read(packet)
}

func (b *latencyPacketBuffer) Close() error {
	return b.buffer.Close()
}

func (b *latencyPacketBuffer) SetReadDeadline(deadline time.Time) error {
	return b.buffer.SetReadDeadline(deadline)
}
