package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

func startVideoReceiver() {
	ln, err := net.Listen("tcp", ":12345")
	if err != nil {
		log.Fatalf("Failed to listen on :12345: %v", err)
	}
	log.Println("Video Receiver listening on :12345")

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

			// 1. Read resolution header from producer
			// Format: 8 bytes width, 8 bytes height (BigEndian)
			header := make([]byte, 16)
			if _, err := io.ReadFull(c, header); err != nil {
				log.Printf("Failed to read resolution header: %v", err)
				return
			}
			width := int(binary.BigEndian.Uint64(header[0:8]))
			height := int(binary.BigEndian.Uint64(header[8:16]))
			log.Printf("Video producer resolution header: %dx%d", width, height)

			vtEncoder := getEncoder()

			// If the encoder resolution doesn't match the incoming stream, recreate it
			if vtEncoder == nil || vtEncoder.Width != width || vtEncoder.Height != height {
				log.Printf("Resolution mismatch: stream %dx%d, encoder %v. Recreating...", width, height, vtEncoder)
				recreateEncoder(width, height)
				vtEncoder = getEncoder()
			}

			if vtEncoder == nil {
				log.Printf("ERROR: No encoder available for %dx%d, dropping connection", width, height)
				return
			}

			frameSize := int(float64(width) * float64(height) * 1.5)
			buf := make([]byte, frameSize)

			frameCount := 0
			lastTime := time.Now()

			// ... (rest of the function)

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
					// Always use the latest encoder in case it was swapped during the session
					// (though usually resolution changes trigger a reconnect)
					enc := getEncoder()
					if enc != nil && enc.Encode(frame) == 0 {
						frameCount++
						if frameCount%60 == 0 {
							now := time.Now()
							elapsed := now.Sub(lastTime).Seconds()
							fps := 60.0 / elapsed
							log.Printf("Network Receiver (%dx%d): %.2f FPS (Elapsed: %.2fs)", width, height, fps, elapsed)
							lastTime = now
						}
					}
					framePool.Put(frame)
				}
			}()

			for {
				_, err := io.ReadFull(c, buf)
				if err != nil {
					log.Printf("Read error (resolution %dx%d): %v", width, height, err)
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
					// Only log occasionally to avoid flooding
					if frameCount%120 == 0 {
						log.Println("Dropped frame to prevent TCP buffer bloat!")
					}
				}
			}
			close(encodeChan)
		}(conn)
	}
}
