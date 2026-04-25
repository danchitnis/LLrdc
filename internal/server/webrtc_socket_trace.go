package server

import "net"

type latencyProbePacketConn struct {
	net.PacketConn
	now func() int64
}

func newLatencyProbePacketConn(conn net.PacketConn, now func() int64) net.PacketConn {
	if conn == nil || now == nil {
		return conn
	}
	return &latencyProbePacketConn{
		PacketConn: conn,
		now:        now,
	}
}

func (c *latencyProbePacketConn) WriteTo(packet []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(packet, addr)
	if err == nil && n > 0 {
		noteLatencyProbeFirstPacketSocketWrite(packet[:n], c.now())
	}
	return n, err
}
