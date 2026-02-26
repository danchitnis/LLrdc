package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	cmd := exec.Command("xte")
	cmd.Env = append(os.Environ(), "DISPLAY=:99")
	stdin, _ := cmd.StdinPipe()
	cmd.Start()
	
	fmt.Fprintf(stdin, "mousemove 100 100\n")
	time.Sleep(1 * time.Second)
	fmt.Fprintf(stdin, "mousemove 200 200\n")
	time.Sleep(1 * time.Second)
	stdin.Close()
	cmd.Wait()
}
