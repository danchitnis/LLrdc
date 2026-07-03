package main

import (
	"io"
	"os"
)

type OutputHandler struct {
	writer io.Writer
}

func NewOutputHandler() *OutputHandler {
	return &OutputHandler{
		writer: os.Stdout,
	}
}

func (h *OutputHandler) Stream(dataChan chan []byte) error {
	for data := range dataChan {
		_, err := h.writer.Write(data)
		if err != nil {
			return err
		}
	}
	return nil
}
