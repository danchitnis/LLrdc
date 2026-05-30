package main

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/internal/splitproto"
	"github.com/danchitnis/llrdc/server/common"
)

func startVideoReceiver() {
	ln, err := net.Listen("tcp4", "0.0.0.0:12345")
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
				tcpConn.SetNoDelay(true)
			}

			// 1. Read split protocol header from producer
			headerBuf := make([]byte, splitproto.HeaderSize)
			if _, err := io.ReadFull(c, headerBuf); err != nil {
				log.Printf("Failed to read split header: %v", err)
				return
			}
			h, err := splitproto.DecodeHeader(headerBuf)
			if err != nil {
				log.Printf("Invalid split header: %v", err)
				return
			}

			width := int(h.Width)
			height := int(h.Height)
			generation := h.Generation
			log.Printf("Video producer header: %dx%d (gen %d)", width, height, generation)

			// OPTIMIZATION: If the producer is sending a stale generation, don't even bother.
			// The agent is likely already restarting it because of a client resize.
			if generation < getGeneration() {
				log.Printf("Ignoring stale producer connection (gen %d < current %d)", generation, getGeneration())
				return
			}

			codecFamily := common.VideoCodec
			pixFmt := int(h.PixFmt)
			enc, encGen := encMgr.Get()
			// If the encoder state doesn't match the incoming stream, recreate it (synchronously in encMgr)
			if enc == nil || enc.Width != width || enc.Height != height || encGen != generation || enc.PixFmt != pixFmt || enc.FPS != common.FPS || enc.BitrateKbps() != common.TargetBandwidthMbps*1000 {
				log.Printf("Encoder mismatch: stream %dx%d (fmt %d, gen %d), encoder %v (gen %d, fps %d, bw %d). Recreating with target FPS %d, BW %d...", 
					width, height, pixFmt, generation, enc, encGen, encMgr.FPS(), encMgr.BitrateKbps(), common.FPS, common.TargetBandwidthMbps*1000)
				encMgr.Recreate(codecFamily, width, height, common.FPS, common.TargetBandwidthMbps*1000, pixFmt, generation)
				enc, encGen = encMgr.Get()
			}

			if enc == nil {
				log.Printf("ERROR: No encoder available for %dx%d, dropping connection", width, height)
				return
			}

			frameSizeMultiplier := 1.5
			if h.PixFmt == 1 {
				frameSizeMultiplier = 3.0
			}
			frameSize := int(float64(width) * float64(height) * frameSizeMultiplier)
			buf := make([]byte, frameSize)

			frameCount := 0
			dropCount := 0
			lastTime := time.Now()

			// 1-frame deep channel. If the encoder is busy, we drop the frame.
			// This completely eliminates TCP buffer bloat.
			encodeChan := make(chan []byte, 1)

			var framePool = sync.Pool{
				New: func() interface{} {
					return make([]byte, frameSize)
				},
			}

			const fpsCheckInterval = 60
			go func() {
				for frame := range encodeChan {
					// Always use the latest encoder, but GUARD against mismatched frames or formats.
					// This prevents mangling during transitions when an old stream is still closing.
					currentEnc, _ := encMgr.Get()
					if currentEnc != nil && currentEnc.Width == width && currentEnc.Height == height && currentEnc.PixFmt == pixFmt && currentEnc.Encode(frame) == 0 {
						frameCount++
						if frameCount%fpsCheckInterval == 0 {
							now := time.Now()
							elapsed := now.Sub(lastTime).Seconds()
							fps := float64(fpsCheckInterval) / elapsed
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
					dropCount++
					// Only log occasionally to avoid flooding
					if dropCount%120 == 0 {
						log.Printf("Dropped %d frames to prevent TCP buffer bloat!", dropCount)
					}
				}
			}
			close(encodeChan)
		}(conn)
	}
}
