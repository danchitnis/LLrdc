package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/internal/server"
	"github.com/danchitnis/llrdc/internal/splitproto"
)

type AgentControlClient struct {
	mu           sync.Mutex
	conn         net.Conn
	agentAddr    string
	pendingGen   uint64
	appliedGen   uint64
	firstFrameGen uint64
	connectedOnce bool
}

var globalControlClient *AgentControlClient

func startAgentControlClient(agentHost string) {
	globalControlClient = &AgentControlClient{
		agentAddr: fmt.Sprintf("%s:%d", agentHost, splitproto.ControlPort),
	}
	go globalControlClient.loop()
}

func (c *AgentControlClient) loop() {
	for {
		err := c.connectAndRead()
		if err != nil {
			// If it's a "connection reset by peer" during initial boot, don't spam the logs
			if !c.connectedOnce {
				time.Sleep(1 * time.Second)
				continue
			}
			log.Printf("Agent control client error: %v. Retrying in 1s...", err)
			time.Sleep(1 * time.Second)
		}
	}
}

func (c *AgentControlClient) connectAndRead() error {
	conn, err := net.Dial("tcp", c.agentAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	if c.connectedOnce {
		log.Printf("Agent control: connection established at %s", c.agentAddr)
	}

	// We consider it fully connected once the TCP connection holds past the immediate Docker proxy reset
	time.Sleep(100 * time.Millisecond)

	// Trigger initial config apply if we have one
	c.ApplyCurrentConfig()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		c.connectedOnce = true // Successfully received data
		var msg splitproto.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Agent control: failed to unmarshal message: %v", err)
			continue
		}

		c.handleMessage(msg)
	}

	c.mu.Lock()
	c.conn = nil
	c.mu.Unlock()

	err = scanner.Err()
	if err != nil {
		return fmt.Errorf("connection dropped: %v", err)
	}
	return nil
}

func (c *AgentControlClient) handleMessage(msg splitproto.Message) {
	switch msg.Type {
	case splitproto.MsgConfigApplied:
		if gen, ok := msg.Config["generation"].(float64); ok {
			c.mu.Lock()
			c.appliedGen = uint64(gen)
			c.mu.Unlock()
			log.Printf("Agent confirmed config applied for generation %d", c.appliedGen)
		}
	case splitproto.MsgFirstFrame:
		if gen, ok := msg.Config["generation"].(float64); ok {
			c.mu.Lock()
			c.firstFrameGen = uint64(gen)
			c.mu.Unlock()
			log.Printf("Agent reported first frame for generation %d", c.firstFrameGen)
		}
	}
}

func (c *AgentControlClient) ApplyConfig(width, height, fps, hdpi int, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pendingGen = generation

	if c.conn == nil {
		log.Printf("Agent control not connected, config for gen %d will be applied on reconnect", generation)
		return
	}

	msg := splitproto.Message{
		Type: splitproto.MsgApplyConfig,
		Config: map[string]interface{}{
			"width":      width,
			"height":     height,
			"fps":        fps,
			"hdpi":       hdpi,
			"generation": generation,
			"pixfmt":     0, // YUV420p
		},
	}

	data, _ := json.Marshal(msg)
	_, err := c.conn.Write(append(data, '\n'))
	if err != nil {
		log.Printf("Failed to send apply_config to agent: %v", err)
	}
}

func (c *AgentControlClient) ApplyCurrentConfig() {
	width, height := server.GetScreenSize()
	c.ApplyConfig(width, height, server.FPS, server.HDPI, getGeneration())
}

func (c *AgentControlClient) IsReady(targetGen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appliedGen >= targetGen && c.firstFrameGen >= targetGen
}
