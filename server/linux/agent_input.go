package linux

import (
	"bufio"
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

		inputStdinMu.Lock()
		if inputStdin != nil {
			fmt.Fprintln(inputStdin, line)
		}
		inputStdinMu.Unlock()

		updateActivity()
	}
}
