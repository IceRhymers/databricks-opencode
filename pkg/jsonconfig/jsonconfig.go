// Package jsonconfig manages the OpenCode CLI config.json file.
// It uses github.com/tidwall/jsonc to strip comments and trailing commas
// before parsing, allowing users to write JSONC in their config files.
package jsonconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"
)

// sentinel is used to distinguish "key was absent" from "key was empty string".
var absent = struct{}{}

// Config reads, patches, and restores the OpenCode config.json file.
type Config struct {
	path         string
	backupPath   string
	sidecarPath  string
	originals    map[string]interface{} // key -> original value, or absent sentinel
}

// New creates a Config that manages opencode.json in the given config directory.
// The caller should pass the OS-specific opencode config dir (e.g. from opencodeConfigDir()).
func New(dir string) *Config {
	return &Config{
		path:        filepath.Join(dir, "opencode.json"),
		backupPath:  filepath.Join(dir, "opencode.json.databricks-opencode-backup"),
		sidecarPath: filepath.Join(dir, ".databricks-opencode-originals.json"),
		originals:   make(map[string]interface{}),
	}
}

// NewWithPath creates a Config with explicit paths (for testing).
func NewWithPath(configPath, backupPath string) *Config {
	dir := filepath.Dir(configPath)
	return &Config{
		path:        configPath,
		backupPath:  backupPath,
		sidecarPath: filepath.Join(dir, ".databricks-opencode-originals.json"),
		originals:   make(map[string]interface{}),
	}
}

// Path returns the config file path.
func (c *Config) Path() string {
	return c.path
}

// BackupPath returns the backup file path.
func (c *Config) BackupPath() string {
	return c.backupPath
}

// SidecarPath returns the sidecar file path.
func (c *Config) SidecarPath() string {
	return c.sidecarPath
}

// HasBackup reports whether a backup sentinel file exists.
func (c *Config) HasBackup() bool {
	_, err := os.Stat(c.backupPath)
	return err == nil
}

// HasSidecar reports whether a sidecar file exists (crash recovery indicator).
func (c *Config) HasSidecar() bool {
	_, err := os.Stat(c.sidecarPath)
	return err == nil
}

// WriteSentinel writes an empty backup sentinel file for crash detection.
func (c *Config) WriteSentinel() error {
	return atomicWrite(c.backupPath, nil)
}

// RemoveSentinel removes the backup sentinel file.
func (c *Config) RemoveSentinel() {
	os.Remove(c.backupPath)
}

// SaveOriginals snapshots the current values of managed keys before patching.
// Writes a sidecar file so crash recovery can restore these values.
func (c *Config) SaveOriginals() error {
	config, err := c.readConfig()
	if err != nil {
		return err
	}

	c.originals = make(map[string]interface{})

	// Snapshot model key.
	if v, ok := config["model"]; ok {
		c.originals["model"] = v
	} else {
		c.originals["model"] = absent
	}

	// Snapshot provider.databricks-proxy key.
	providers, _ := config["provider"].(map[string]interface{})
	if providers != nil {
		if v, ok := providers["databricks-proxy"]; ok {
			c.originals["provider.databricks-proxy"] = v
		} else {
			c.originals["provider.databricks-proxy"] = absent
		}
	} else {
		c.originals["provider.databricks-proxy"] = absent
	}

	return c.writeSidecar()
}

// Patch injects the databricks-proxy provider and optionally sets the model.
// If forceModel is true, the model is always written (explicit --model flag).
// If forceModel is false, the model is only set if absent (preserve-if-present).
func (c *Config) Patch(proxyURL, modelName, apiKey string, forceModel bool) error {
	config, err := c.readConfig()
	if err != nil {
		return err
	}

	// Ensure provider map exists.
	providers, _ := config["provider"].(map[string]interface{})
	if providers == nil {
		providers = make(map[string]interface{})
	}

	// Inject the databricks-proxy provider (always overwrite — we own this key).
	// Uses @ai-sdk/anthropic; the proxy overwrites auth headers with the real
	// Databricks token, so the apiKey here is just a placeholder.
	providers["databricks-proxy"] = map[string]interface{}{
		"npm":  "@ai-sdk/anthropic",
		"name": "Databricks AI Gateway",
		"options": map[string]interface{}{
			"baseURL": proxyURL + "/v1",
			"apiKey":  apiKey,
		},
		// Register all available Databricks Claude models so users can switch
		// between them in OpenCode's model picker without manual config edits.
		// The active model is controlled by the top-level "model" key below.
		// Register all available Databricks Claude models so users can switch
		// between them in OpenCode's model picker without manual config edits.
		// The active model is controlled by the top-level "model" key below.
		"models": map[string]interface{}{
			"databricks-claude-opus-4-7":   map[string]interface{}{},
			"databricks-claude-opus-4-6":   map[string]interface{}{},
			"databricks-claude-opus-4-5":   map[string]interface{}{},
			"databricks-claude-sonnet-4-6": map[string]interface{}{},
			"databricks-claude-sonnet-4-5": map[string]interface{}{},
			"databricks-claude-haiku-4-5":  map[string]interface{}{},
		},
	}
	config["provider"] = providers

	// Set the active model: preserve-if-present unless forced.
	if forceModel {
		config["model"] = "databricks-proxy/" + modelName
	} else {
		if _, exists := config["model"]; !exists {
			config["model"] = "databricks-proxy/" + modelName
		}
	}

	return c.writeConfig(config)
}

// Restore surgically removes only the keys we manage and restores originals.
// Removes provider.databricks-proxy and restores model to its original value.
func (c *Config) Restore() error {
	// Load originals from sidecar if not in memory.
	if len(c.originals) == 0 {
		if err := c.loadSidecar(); err != nil {
			// No sidecar — nothing to restore surgically.
			// Fall back to removing sentinel only.
			os.Remove(c.backupPath)
			return nil
		}
	}

	config, err := c.readConfig()
	if err != nil {
		// Config doesn't exist — just clean up.
		c.cleanup()
		return nil
	}

	// Restore model key.
	if orig, ok := c.originals["model"]; ok {
		if orig == absent {
			delete(config, "model")
		} else {
			config["model"] = orig
		}
	}

	// Remove provider.databricks-proxy.
	if providers, ok := config["provider"].(map[string]interface{}); ok {
		if orig, exists := c.originals["provider.databricks-proxy"]; exists {
			if orig == absent {
				delete(providers, "databricks-proxy")
			} else {
				providers["databricks-proxy"] = orig
			}
		} else {
			delete(providers, "databricks-proxy")
		}
		// If providers map is now empty, remove it.
		if len(providers) == 0 {
			delete(config, "provider")
		} else {
			config["provider"] = providers
		}
	}

	if err := c.writeConfig(config); err != nil {
		return err
	}

	c.cleanup()
	return nil
}

// Backup is kept as a crash-detection sentinel writer.
// It no longer copies the full file — just writes an empty marker.
func (c *Config) Backup() error {
	return c.WriteSentinel()
}

// NeedsConfig returns true if config.json needs to be written (or rewritten)
// because the databricks-proxy provider's baseURL does not already match
// proxyURL + "/v1". Returns true when the config file is missing or the
// provider section is absent/different.
func (c *Config) NeedsConfig(proxyURL string) bool {
	config, err := c.readConfig()
	if err != nil {
		return true
	}
	providers, _ := config["provider"].(map[string]interface{})
	if providers == nil {
		return true
	}
	dbProxy, _ := providers["databricks-proxy"].(map[string]interface{})
	if dbProxy == nil {
		return true
	}
	options, _ := dbProxy["options"].(map[string]interface{})
	if options == nil {
		return true
	}
	baseURL, _ := options["baseURL"].(string)
	if baseURL != proxyURL+"/v1" {
		return true
	}
	// Detect stale auth key name (e.g. authToken → apiKey migration).
	if _, ok := options["apiKey"]; !ok {
		return true
	}
	// Detect stale npm package.
	npm, _ := dbProxy["npm"].(string)
	return npm != "@ai-sdk/anthropic"
}

// UpdateProxyURL updates only the baseURL in the existing databricks-proxy provider.
func (c *Config) UpdateProxyURL(proxyURL string) error {
	config, err := c.readConfig()
	if err != nil {
		return err
	}

	providers, _ := config["provider"].(map[string]interface{})
	if providers == nil {
		return fmt.Errorf("no provider section in config")
	}

	dbProxy, _ := providers["databricks-proxy"].(map[string]interface{})
	if dbProxy == nil {
		return fmt.Errorf("no databricks-proxy provider in config")
	}

	options, _ := dbProxy["options"].(map[string]interface{})
	if options == nil {
		options = make(map[string]interface{})
	}
	options["baseURL"] = proxyURL
	dbProxy["options"] = options
	providers["databricks-proxy"] = dbProxy
	config["provider"] = providers

	return c.writeConfig(config)
}

// AddPlugin surgically adds pluginPath to the "plugin" array in opencode.json.
// Idempotent — does not duplicate if already present.
func (c *Config) AddPlugin(pluginPath string) error {
	config, err := c.readConfig()
	if err != nil {
		return err
	}

	plugins, _ := config["plugin"].([]interface{})
	for _, p := range plugins {
		if s, ok := p.(string); ok && s == pluginPath {
			return nil // already registered
		}
	}
	plugins = append(plugins, pluginPath)
	config["plugin"] = plugins

	return c.writeConfig(config)
}

// RemovePlugin surgically removes pluginPath from the "plugin" array in opencode.json.
// Removes the "plugin" key entirely if the array becomes empty.
func (c *Config) RemovePlugin(pluginPath string) error {
	config, err := c.readConfig()
	if err != nil {
		return nil // nothing to remove
	}

	plugins, _ := config["plugin"].([]interface{})
	if plugins == nil {
		return nil
	}

	filtered := make([]interface{}, 0, len(plugins))
	for _, p := range plugins {
		if s, ok := p.(string); ok && s == pluginPath {
			continue
		}
		filtered = append(filtered, p)
	}

	if len(filtered) == 0 {
		delete(config, "plugin")
	} else {
		config["plugin"] = filtered
	}

	return c.writeConfig(config)
}

// cleanup removes sidecar and backup sentinel files.
func (c *Config) cleanup() {
	os.Remove(c.sidecarPath)
	os.Remove(c.backupPath)
	c.originals = make(map[string]interface{})
}

// sidecarData is the JSON schema for the sidecar file.
type sidecarData struct {
	Model              interface{} `json:"model"`
	ModelAbsent        bool        `json:"model_absent"`
	ProviderDBProxy    interface{} `json:"provider_databricks_proxy"`
	ProviderDBPAbsent  bool        `json:"provider_databricks_proxy_absent"`
}

// writeSidecar persists originals to disk for crash recovery.
func (c *Config) writeSidecar() error {
	sd := sidecarData{}

	if v, ok := c.originals["model"]; ok {
		if v == absent {
			sd.ModelAbsent = true
		} else {
			sd.Model = v
		}
	}

	if v, ok := c.originals["provider.databricks-proxy"]; ok {
		if v == absent {
			sd.ProviderDBPAbsent = true
		} else {
			sd.ProviderDBProxy = v
		}
	}

	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(c.sidecarPath, data)
}

// loadSidecar reads originals from the sidecar file.
func (c *Config) loadSidecar() error {
	data, err := os.ReadFile(c.sidecarPath)
	if err != nil {
		return err
	}

	var sd sidecarData
	if err := json.Unmarshal(data, &sd); err != nil {
		return err
	}

	c.originals = make(map[string]interface{})

	if sd.ModelAbsent {
		c.originals["model"] = absent
	} else {
		c.originals["model"] = sd.Model
	}

	if sd.ProviderDBPAbsent {
		c.originals["provider.databricks-proxy"] = absent
	} else {
		c.originals["provider.databricks-proxy"] = sd.ProviderDBProxy
	}

	return nil
}

// readConfig reads the config file and returns a parsed map.
// Returns an empty map if the file doesn't exist.
func (c *Config) readConfig() (map[string]interface{}, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Strip JSONC comments and trailing commas.
	clean := jsonc.ToJSON(data)

	var config map[string]interface{}
	if err := json.Unmarshal(clean, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return config, nil
}

// writeConfig marshals and writes the config map to disk.
func (c *Config) writeConfig(config map[string]interface{}) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := atomicWrite(c.path, data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
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
