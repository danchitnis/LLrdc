package main

import (
	"log"
	"os"

	"github.com/danchitnis/llrdc/internal/server"
)

func main() {
	log.SetOutput(os.Stdout)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
