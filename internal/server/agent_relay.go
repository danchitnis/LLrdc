package server

import (
	"encoding/binary"
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
					log.Printf("Agent relay: new connection from %v", s.RemoteAddr())
					dst, err := net.Dial("tcp", AgentAddress)
					if err != nil {
						log.Printf("Agent relay failed to connect to host %s: %v", AgentAddress, err)
						return
					}
					defer dst.Close()

					// Send resolution header first
					w, h := GetScreenSize()
					header := make([]byte, 16)
					binary.BigEndian.PutUint64(header[0:8], uint64(w))
					binary.BigEndian.PutUint64(header[8:16], uint64(h))
					if _, err := dst.Write(header); err != nil {
						log.Printf("Agent relay failed to write header: %v", err)
						return
					}

					log.Printf("Agent relay forwarding %dx%d stream to host %s", w, h, AgentAddress)
					_, _ = io.Copy(dst, s)
					log.Printf("Agent relay stream finished for %dx%d", w, h)
				}(src)
			}
		}()
	})
}
