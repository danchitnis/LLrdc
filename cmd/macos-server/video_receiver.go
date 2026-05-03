package main

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/cmd/macos-server/encoder"
)

func startVideoReceiver(width, height int, vtEncoder *encoder.VTEncoder) {
	ln, err := net.Listen("tcp", ":12345")
	if err != nil {
		log.Fatalf("Failed to listen on :12345: %v", err)
	}
	log.Println("Video Receiver listening on :12345")

	frameSize := int(float64(width) * float64(height) * 1.5)
	buf := make([]byte, frameSize)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		log.Printf("Video producer connected: %v", conn.RemoteAddr())

		go func(c net.Conn) {
			defer c.Close()

			if tcpConn, ok := c.(*net.TCPConn); ok {
				tcpConn.SetReadBuffer(16 * 1024 * 1024)
				tcpConn.SetNoDelay(true)
			}

			frameCount := 0
			lastTime := time.Now()

			// 1-frame deep channel. If the encoder is busy, we drop the frame.
			// This completely eliminates TCP buffer bloat.
			encodeChan := make(chan []byte, 1)

			var framePool = sync.Pool{
				New: func() interface{} {
					return make([]byte, frameSize)
				},
			}

			go func() {
				for frame := range encodeChan {
					if vtEncoder.Encode(frame) == 0 {
						frameCount++
						if frameCount%60 == 0 {
							now := time.Now()
							elapsed := now.Sub(lastTime).Seconds()
							fps := 60.0 / elapsed
							log.Printf("Network Receiver: %.2f FPS (Elapsed: %.2fs)", fps, elapsed)
							lastTime = now
						}
					}
					framePool.Put(frame)
				}
			}()

			for {
				_, err := io.ReadFull(c, buf)
				if err != nil {
					log.Printf("Read error: %v", err)
					break
				}

				frameCopy := framePool.Get().([]byte)
				copy(frameCopy, buf)

				select {
				case encodeChan <- frameCopy:
					// Frame accepted by encoder
				default:
					// Encoder is too slow; drop the frame to prevent the multi-second delay!
					framePool.Put(frameCopy)
					log.Println("Dropped frame to prevent TCP buffer bloat!")
				}
			}
			close(encodeChan)
		}(conn)
	}
}
