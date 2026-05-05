package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
)

func handleAgentInputConnection(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewReader(conn)
	for {
		line, err := scanner.ReadString('\n')
		if err != nil {
			log.Printf("Agent input connection closed: %v", err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "agent_msg ") {
			jsonStr := strings.TrimPrefix(line, "agent_msg ")
			var msg map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
				log.Printf("Failed to unmarshal agent message: %v", err)
				continue
			}

			msgType, _ := msg["type"].(string)
			switch msgType {
			case "resize":
				widthFloat, wOk := msg["width"].(float64)
				heightFloat, hOk := msg["height"].(float64)
				if wOk && hOk {
					width := int(widthFloat)
					height := int(heightFloat)
					if SetScreenSize(width, height) {
						clampedW, clampedH := GetScreenSize()
						log.Printf("Agent received resize: %dx%d (clamped to %dx%d)", width, height, clampedW, clampedH)
						previousStreamID := getCurrentFFmpegStreamID()
						go func() {
							applyDisplayChange(previousStreamID, clampedW, clampedH, "agent client resize")
							broadcastConfig(true)
						}()
					}
				}
			case "config":
				configMsg, ok := msg["config"].(map[string]interface{})
				if !ok {
					configMsg = msg
				}
				log.Printf("Agent received config message: %v", configMsg)

				displayResizeRequested := false
				displayChangeReason := "agent config update"
				previousStreamID := getCurrentFFmpegStreamID()

				if maxResFloat, ok := configMsg["max_res"].(float64); ok {
					maxRes := int(maxResFloat)
					if InitialRes != maxRes {
						log.Printf("Agent received max resolution config: %dp", maxRes)
						InitialRes = maxRes
						if InitialRes > 0 {
							UpdateScreenSizeFromInitialRes()
							displayResizeRequested = true
							displayChangeReason = "agent fixed resolution update"
						}
					}
				}

				if displayResizeRequested {
					width, height := GetScreenSize()
					go func() {
						applyDisplayChange(previousStreamID, width, height, displayChangeReason)
						broadcastConfig(true)
					}()
				}
			}
			continue
		}

		inputStdinMu.Lock()
		if inputStdin != nil {
			fmt.Fprintln(inputStdin, line)
		}
		inputStdinMu.Unlock()

		updateActivity()
	}
}
