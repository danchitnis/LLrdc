package client

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAppPathsLinuxStyle(t *testing.T) {
	t.Parallel()

	paths := ResolveAppPaths("/tmp/llrdc/bin/llrdc-client")
	if got, want := paths.DefaultConfigPath, filepath.Join("/tmp/llrdc/bin", "config.yaml"); got != want {
		t.Fatalf("unexpected default config path: got %q want %q", got, want)
	}
	if paths.ResourceDir != "/tmp/llrdc/bin" {
		t.Fatalf("unexpected resource dir: %q", paths.ResourceDir)
	}
}

func TestResolveAppPathsDarwinBundle(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		t.Skip("bundle path resolution only applies on darwin")
	}

	paths := ResolveAppPaths("/Applications/LLrdc.app/Contents/MacOS/llrdc-client")
	if got, want := paths.BundleRoot, "/Applications/LLrdc.app"; got != want {
		t.Fatalf("unexpected bundle root: got %q want %q", got, want)
	}
	if got, want := paths.DefaultConfigPath, "/Applications/LLrdc.app/config.yaml"; got != want {
		t.Fatalf("unexpected default config path: got %q want %q", got, want)
	}
}
