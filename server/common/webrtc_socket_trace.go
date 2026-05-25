package common

import "net"

type LatencyProbePacketConn struct {
	net.PacketConn
	Now func() int64
}

func NewLatencyProbePacketConn(conn net.PacketConn, now func() int64) net.PacketConn {
	if conn == nil || now == nil {
		return conn
	}
	return &LatencyProbePacketConn{
		PacketConn: conn,
		Now:        now,
	}
}

func (c *LatencyProbePacketConn) WriteTo(packet []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(packet, addr)
	if err == nil && n > 0 {
		NoteLatencyProbeFirstPacketSocketWrite(packet[:n], c.Now())
	}
	return n, err
}
