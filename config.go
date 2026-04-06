package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/IceRhymers/databricks-claude/pkg/filelock"
	"github.com/IceRhymers/databricks-claude/pkg/registry"
	"github.com/tidwall/jsonc"
)

// ConfigManager coordinates config.json patching, file locking, and
// multi-session registration for OpenCode.
type ConfigManager struct {
	configPath string
	backupPath string
	original   []byte
	lock       *filelock.FileLock
	registry   *registry.SessionRegistry
}

// NewConfigManager creates a ConfigManager that manages ~/.config/opencode/config.json.
func NewConfigManager() *ConfigManager {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("databricks-opencode: cannot determine home dir: %v", err)
		home = "."
	}
	opencodeDir := filepath.Join(home, ".config", "opencode")
	configPath := filepath.Join(opencodeDir, "config.json")
	return &ConfigManager{
		configPath: configPath,
		backupPath: configPath + ".databricks-opencode-backup",
		lock:       filelock.New(filepath.Join(opencodeDir, ".config.lock")),
		registry:   registry.New(filepath.Join(opencodeDir, ".sessions.json")),
	}
}

// Setup backs up config.json, patches it with the proxy config, and
// registers the current session. The caller must call Restore on exit.
func (cm *ConfigManager) Setup(proxyURL, model, otelEndpoint string) error {
	if err := cm.lock.Lock(); err != nil {
		log.Printf("databricks-opencode: config lock warning: %v", err)
	}
	defer cm.lock.Unlock()

	// Recover from a previous crash if needed.
	cm.restoreFromBackup()

	if err := cm.backup(); err != nil {
		return err
	}

	if err := cm.patch(proxyURL, model); err != nil {
		return err
	}

	if err := cm.registry.Register(os.Getpid(), proxyURL); err != nil {
		log.Printf("databricks-opencode: session register warning: %v", err)
	}

	log.Printf("databricks-opencode: patched %s (proxy: %s)", cm.configPath, proxyURL)
	return nil
}

// Restore unregisters the current session and restores config.json.
// If other sessions are still alive, it updates the config to point at
// the most recent survivor's proxy instead of fully restoring.
func (cm *ConfigManager) Restore() {
	if err := cm.lock.Lock(); err != nil {
		log.Printf("databricks-opencode: config lock warning: %v", err)
	}
	defer cm.lock.Unlock()

	cm.registry.Unregister(os.Getpid())

	// Check for surviving sessions.
	survivor, err := cm.registry.MostRecentLive()
	if err == nil && survivor != nil {
		log.Printf("databricks-opencode: handing off config.json to session %d (proxy: %s)",
			survivor.PID, survivor.ProxyURL)
		if err := cm.updateProxyURL(survivor.ProxyURL); err != nil {
			log.Printf("databricks-opencode: handoff failed, restoring original: %v", err)
			cm.restoreConfig()
		}
		return
	}

	// Last session — restore original config.
	if err := cm.restoreConfig(); err != nil {
		log.Printf("databricks-opencode: config restore failed: %v", err)
	} else {
		log.Printf("databricks-opencode: config.json restored")
	}
}

// backup reads the current config.json and saves the original content.
func (cm *ConfigManager) backup() error {
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cm.original = nil
			return nil
		}
		return fmt.Errorf("read config.json: %w", err)
	}
	cm.original = data
	if err := atomicWrite(cm.backupPath, data); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

// patch writes a new config.json that points OpenCode at the local proxy.
// OpenCode config.json uses JSONC format with provider configuration.
func (cm *ConfigManager) patch(proxyURL, model string) error {
	// Read existing config, strip JSONC comments for merging.
	var existing []byte
	if cm.original != nil {
		existing = jsonc.ToJSON(cm.original)
	}
	_ = existing // We rebuild the proxy-relevant parts but could merge user settings.

	config := fmt.Sprintf(`{
  "provider": {
    "databricks-proxy": {
      "apiKey": "databricks-proxy",
      "model": %q,
      "baseURL": %q
    }
  }
}
`, model, proxyURL)

	if err := atomicWrite(cm.configPath, []byte(config)); err != nil {
		return fmt.Errorf("write patched config.json: %w", err)
	}
	return nil
}

// restoreConfig writes the original config.json content back.
func (cm *ConfigManager) restoreConfig() error {
	if cm.original == nil {
		os.Remove(cm.configPath)
	} else {
		if err := atomicWrite(cm.configPath, cm.original); err != nil {
			return fmt.Errorf("restore config.json: %w", err)
		}
	}
	os.Remove(cm.backupPath)
	return nil
}

// restoreFromBackup recovers from a crash by restoring from the backup file.
func (cm *ConfigManager) restoreFromBackup() {
	data, err := os.ReadFile(cm.backupPath)
	if err != nil {
		return
	}
	log.Printf("databricks-opencode: restoring config.json from crash backup")
	cm.original = data
	_ = cm.restoreConfig()
}

// updateProxyURL updates only the baseURL in the managed config.json.
func (cm *ConfigManager) updateProxyURL(newURL string) error {
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("read config for proxy URL update: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "\"baseURL\"") && strings.Contains(trimmed, ":") {
			lines[i] = fmt.Sprintf("      \"baseURL\": %q", newURL)
			break
		}
	}

	return atomicWrite(cm.configPath, []byte(strings.Join(lines, "\n")))
}

// atomicWrite writes data to a temp file and renames it into place.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
