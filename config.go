package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/IceRhymers/databricks-claude/pkg/filelock"
	"github.com/IceRhymers/databricks-claude/pkg/registry"
	"github.com/IceRhymers/databricks-opencode/pkg/jsonconfig"
)

// ConfigManager coordinates config.json patching, file locking, and
// multi-session registration for OpenCode.
type ConfigManager struct {
	config   *jsonconfig.Config
	lock     *filelock.FileLock
	registry *registry.SessionRegistry
}

// NewConfigManager creates a ConfigManager that manages ~/.config/opencode/opencode.json.
func NewConfigManager() *ConfigManager {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("databricks-opencode: cannot determine home dir: %v", err)
		home = "."
	}
	opencodeDir := filepath.Join(home, ".config", "opencode")
	return &ConfigManager{
		config:   jsonconfig.New(),
		lock:     filelock.New(filepath.Join(opencodeDir, ".config.lock")),
		registry: registry.New(filepath.Join(opencodeDir, ".sessions.json")),
	}
}

// newConfigManagerWithPaths creates a ConfigManager with explicit paths (for testing).
func newConfigManagerWithPaths(configPath, backupPath, lockPath, registryPath string) *ConfigManager {
	return &ConfigManager{
		config:   jsonconfig.NewWithPath(configPath, backupPath),
		lock:     filelock.New(lockPath),
		registry: registry.New(registryPath),
	}
}

// Setup backs up config.json, patches it with the proxy config, and
// registers the current session. The caller must call Restore on exit.
func (cm *ConfigManager) Setup(proxyURL, modelName, apiKey string) error {
	if err := cm.lock.Lock(); err != nil {
		log.Printf("databricks-opencode: config lock warning: %v", err)
	}
	defer cm.lock.Unlock()

	// Crash recovery: if a backup exists from a previous crashed session,
	// decide whether to restore or hand off.
	if cm.config.HasBackup() {
		survivor, err := cm.registry.MostRecentLive()
		if err == nil && survivor != nil {
			// Another session is alive — hand off to its proxy.
			log.Printf("databricks-opencode: crash recovery: handing off to session %d (proxy: %s)",
				survivor.PID, survivor.ProxyURL)
			if err := cm.config.UpdateProxyURL(survivor.ProxyURL); err != nil {
				log.Printf("databricks-opencode: crash recovery handoff failed: %v", err)
			}
		} else {
			// No live sessions — restore original config first.
			log.Printf("databricks-opencode: restoring config.json from crash backup")
			if err := cm.config.Restore(); err != nil {
				log.Printf("databricks-opencode: crash restore failed: %v", err)
			}
		}
	}

	if err := cm.config.Backup(); err != nil {
		return err
	}

	if err := cm.config.Patch(proxyURL, modelName, apiKey); err != nil {
		return err
	}

	if err := cm.registry.Register(os.Getpid(), proxyURL); err != nil {
		log.Printf("databricks-opencode: session register warning: %v", err)
	}

	log.Printf("databricks-opencode: patched %s (proxy: %s)", cm.config.Path(), proxyURL)
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
		if err := cm.config.UpdateProxyURL(survivor.ProxyURL); err != nil {
			log.Printf("databricks-opencode: handoff failed, restoring original: %v", err)
			cm.config.Restore()
		}
		return
	}

	// Last session — restore original config.
	if err := cm.config.Restore(); err != nil {
		log.Printf("databricks-opencode: config restore failed: %v", err)
	} else {
		log.Printf("databricks-opencode: config.json restored")
	}
}
