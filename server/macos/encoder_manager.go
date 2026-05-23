package main

import (
	"log"
	"sync"

	"github.com/danchitnis/llrdc/server/macos/encoder"
)

type EncoderManager struct {
	mu            sync.RWMutex
	current       *encoder.VTEncoder
	generation    uint64
	width, height int
	fps           int
	bitrateKbps   int
	pixFmt        int
	codec         string
}

func NewEncoderManager() *EncoderManager {
	return &EncoderManager{}
}

func (m *EncoderManager) Get() (*encoder.VTEncoder, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current, m.generation
}

func (m *EncoderManager) Codec() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.codec
}

func (m *EncoderManager) Recreate(codec string, width, height, fps, bitrateKbps, pixFmt int, generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil && m.codec == codec && m.width == width && m.height == height && m.fps == fps && m.bitrateKbps == bitrateKbps && m.pixFmt == pixFmt && m.generation == generation {
		return
	}

	if m.current != nil {
		log.Printf("Closing old VTEncoder (%s %dx%d@%d FPS, %d kbps, fmt %d, gen %d) synchronously", m.codec, m.width, m.height, m.fps, m.bitrateKbps, m.pixFmt, m.generation)
		m.current.Close()
	}

	m.codec = codec
	m.width = width
	m.height = height
	m.fps = fps
	m.bitrateKbps = bitrateKbps
	m.pixFmt = pixFmt
	m.generation = generation

	log.Printf("Creating new VTEncoder: %s %dx%d@%d FPS (fmt %d, gen %d), bitrate %d kbps", codec, width, height, fps, pixFmt, generation, bitrateKbps)
	m.current = encoder.NewVTEncoder(codec, width, height, fps, bitrateKbps, pixFmt, func(data []byte, isKeyframe bool) {
		broadcastVideoFrame(data, isKeyframe, codec)
	})

	if m.current == nil {
		log.Printf("ERROR: Failed to create VideoToolbox encoder for %s %dx%d", codec, width, height)
	} else {
		m.current.ForceKeyframe()
	}
}

func (m *EncoderManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		m.current.Close()
		m.current = nil
	}
}
