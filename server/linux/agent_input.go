package linux

import (
	"bufio"
	"encoding/base64"
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

		if strings.HasPrefix(line, "clipboard ") {
			parts := strings.Split(line, " ")
			if len(parts) >= 2 {
				b64 := parts[1]
				paste := false
				if len(parts) >= 3 && parts[2] == "1" {
					paste = true
				}
				data, err := base64.StdEncoding.DecodeString(b64)
				if err == nil {
					handleClipboardSet(map[string]interface{}{
						"text":  string(data),
						"paste": paste,
					})
				} else {
					log.Printf("Agent input: failed to decode base64 clipboard: %v", err)
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
