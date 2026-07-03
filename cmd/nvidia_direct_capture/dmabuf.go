package main

import (
	"fmt"
	"os"
)

type DMABufImporter struct {
	RenderNode string
}

func NewDMABufImporter(renderNode string) (*DMABufImporter, error) {
	// Verify render node exists and is accessible
	info, err := os.Stat(renderNode)
	if err != nil {
		return nil, fmt.Errorf("render node %s not found: %w", renderNode, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("render node %s is a directory, not a device file", renderNode)
	}

	// Try opening the render node for reading to ensure permissions are correct
	f, err := os.OpenFile(renderNode, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open render node %s (check GID/permissions): %w", renderNode, err)
	}
	_ = f.Close()

	return &DMABufImporter{
		RenderNode: renderNode,
	}, nil
}
