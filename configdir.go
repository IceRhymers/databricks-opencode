package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// opencodeConfigDir returns the OS-specific opencode config directory,
// matching the env-paths convention used by opencode since PR #8236:
//   - Windows: %APPDATA%\opencode\Config
//   - macOS:   ~/Library/Preferences/opencode
//   - Linux:   ~/.config/opencode
func opencodeConfigDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		return filepath.Join(appData, "opencode", "Config"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Preferences", "opencode"), nil
	default: // linux and others
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "opencode"), nil
	}
}
