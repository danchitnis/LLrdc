package main

import (
	"log"
	"os"

	"github.com/danchitnis/llrdc/server/linux"
)

func main() {
	log.SetOutput(os.Stdout)
	if err := linux.Run(); err != nil {
		log.Fatal(err)
	}
}
