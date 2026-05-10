package main

import (
	"log"
	"sync"

	"github.com/danchitnis/llrdc/cmd/macos-server/encoder"
)

type EncoderManager struct {
	mu          sync.RWMutex
	current     *encoder.VTEncoder
	generation  uint64
	width, height int
	fps         int
}

func NewEncoderManager() *EncoderManager {
	return &EncoderManager{}
}

func (m *EncoderManager) Get() (*encoder.VTEncoder, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current, m.generation
}

func (m *EncoderManager) Recreate(width, height, fps int, generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil && m.width == width && m.height == height && m.fps == fps && m.generation == generation {
		return
	}

	if m.current != nil {
		log.Printf("Closing old VTEncoder (%dx%d@%d FPS, gen %d) synchronously", m.width, m.height, m.fps, m.generation)
		m.current.Close()
	}

	m.width = width
	m.height = height
	m.fps = fps
	m.generation = generation

	bitrateKbps := 8000
	log.Printf("Creating new VTEncoder: %dx%d@%d FPS (gen %d)", width, height, fps, generation)
	m.current = encoder.NewVTEncoder(width, height, fps, bitrateKbps, func(data []byte, isKeyframe bool) {
		broadcastVideoFrame(data, isKeyframe)
	})

	if m.current == nil {
		log.Printf("ERROR: Failed to create VideoToolbox encoder for %dx%d", width, height)
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
