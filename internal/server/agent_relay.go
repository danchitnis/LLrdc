package server

import (
	"io"
	"log"
	"net"
	"sync"
)

var (
	agentRelayOnce sync.Once
)

func startAgentRelay() {
	agentRelayOnce.Do(func() {
		go func() {
			relayAddr := "127.0.0.1:12347"
			ln, err := net.Listen("tcp", relayAddr)
			if err != nil {
				log.Printf("Agent relay failed to listen on %s: %v", relayAddr, err)
				return
			}
			defer ln.Close()

			log.Printf("Agent relay listening on %s", relayAddr)
			for {
				src, err := ln.Accept()
				if err != nil {
					log.Printf("Agent relay accept error: %v", err)
					return
				}

				go func(s net.Conn) {
					defer s.Close()

					if tcpConn, ok := s.(*net.TCPConn); ok {
						_ = tcpConn.SetNoDelay(true)
					}

					log.Printf("Agent relay: new connection from %v", s.RemoteAddr())
					dst, err := net.Dial("tcp", AgentAddress)
					if err != nil {
						log.Printf("Agent relay failed to connect to host %s: %v", AgentAddress, err)
						return
					}
					defer dst.Close()

					if tcpDst, ok := dst.(*net.TCPConn); ok {
						_ = tcpDst.SetNoDelay(true)
					}

					// Send resolution header first using split protocol header
					header := getSplitHeader()
					if _, err := dst.Write(header.Encode()); err != nil {
						log.Printf("Agent relay failed to write header: %v", err)
						return
					}

					log.Printf("Agent relay forwarding %dx%d stream (gen %d) to host %s", header.Width, header.Height, header.Generation, AgentAddress)

					NotifyFirstFrame(header.Generation)

					_, _ = io.Copy(dst, s)
					log.Printf("Agent relay stream finished for %dx%d (gen %d)", header.Width, header.Height, header.Generation)
				}(src)
			}
		}()
	})
}
