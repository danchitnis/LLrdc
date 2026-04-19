package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type AppPaths struct {
	ExecutablePath     string `json:"executablePath"`
	ExecutableDir      string `json:"executableDir"`
	BundleRoot         string `json:"bundleRoot,omitempty"`
	ResourceDir        string `json:"resourceDir"`
	DefaultConfigPath  string `json:"defaultConfigPath"`
	UserConfigDir      string `json:"userConfigDir,omitempty"`
	UserConfigPath     string `json:"userConfigPath,omitempty"`
	CacheDir           string `json:"cacheDir,omitempty"`
	LogDir             string `json:"logDir,omitempty"`
	VisualArtifactsDir string `json:"visualArtifactsDir,omitempty"`
}

func ResolveAppPaths(executablePath string) AppPaths {
	if executablePath == "" {
		if current, err := os.Executable(); err == nil {
			executablePath = current
		}
	}

	if absPath, err := filepath.Abs(executablePath); err == nil {
		executablePath = absPath
	}

	executableDir := filepath.Dir(executablePath)
	resourceDir := executableDir
	bundleRoot := ""
	defaultConfigPath := filepath.Join(executableDir, "config.yaml")

	if runtime.GOOS == "darwin" && strings.HasSuffix(executableDir, filepath.Join("Contents", "MacOS")) {
		bundleRoot = filepath.Dir(filepath.Dir(executableDir))
		resourceDir = filepath.Join(bundleRoot, "Contents", "Resources")
		defaultConfigPath = filepath.Join(bundleRoot, "config.yaml")
	}

	paths := AppPaths{
		ExecutablePath:    executablePath,
		ExecutableDir:     executableDir,
		BundleRoot:        bundleRoot,
		ResourceDir:       resourceDir,
		DefaultConfigPath: defaultConfigPath,
	}

	if cfgDir, err := os.UserConfigDir(); err == nil {
		paths.UserConfigDir = filepath.Join(cfgDir, "llrdc")
		paths.UserConfigPath = filepath.Join(paths.UserConfigDir, "config.yaml")
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		paths.CacheDir = filepath.Join(cacheDir, "llrdc")
		paths.LogDir = filepath.Join(paths.CacheDir, "logs")
		paths.VisualArtifactsDir = filepath.Join(paths.CacheDir, "visual")
	}

	return paths
}
