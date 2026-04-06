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

// Config reads, patches, and restores the OpenCode config.json file.
type Config struct {
	path       string
	backupPath string
}

// New creates a Config that manages ~/.config/opencode/opencode.json.
func New() *Config {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "opencode")
	return &Config{
		path:       filepath.Join(dir, "opencode.json"),
		backupPath: filepath.Join(dir, "opencode.json.databricks-opencode-backup"),
	}
}

// NewWithPath creates a Config with explicit paths (for testing).
func NewWithPath(configPath, backupPath string) *Config {
	return &Config{
		path:       configPath,
		backupPath: backupPath,
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

// Backup saves the current config to the backup file.
func (c *Config) Backup() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file yet — write empty backup marker.
			return atomicWrite(c.backupPath, nil)
		}
		return fmt.Errorf("read config: %w", err)
	}
	if err := atomicWrite(c.backupPath, data); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

// Restore restores the config from backup and deletes the backup file.
func (c *Config) Restore() error {
	data, err := os.ReadFile(c.backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read backup: %w", err)
	}

	if len(data) == 0 {
		// Backup marker for "no config existed" — remove the config file.
		os.Remove(c.path)
	} else {
		if err := atomicWrite(c.path, data); err != nil {
			return fmt.Errorf("restore config: %w", err)
		}
	}
	os.Remove(c.backupPath)
	return nil
}

// HasBackup reports whether a backup file exists.
func (c *Config) HasBackup() bool {
	_, err := os.Stat(c.backupPath)
	return err == nil
}

// Patch injects the databricks-proxy provider and sets the model.
// proxyURL: "http://127.0.0.1:{port}"
// modelName: e.g. "databricks-gpt-5-4" (will be set as "databricks-proxy/modelName")
// apiKey: placeholder, e.g. "databricks-proxy"
func (c *Config) Patch(proxyURL, modelName, apiKey string) error {
	config, err := c.readConfig()
	if err != nil {
		return err
	}

	// Ensure provider map exists.
	providers, _ := config["provider"].(map[string]interface{})
	if providers == nil {
		providers = make(map[string]interface{})
	}

	// Inject the databricks-proxy provider.
	providers["databricks-proxy"] = map[string]interface{}{
		"apiKey":  apiKey,
		"models":  []interface{}{modelName},
		"baseURL": proxyURL,
	}
	config["provider"] = providers

	// Set the active model.
	config["model"] = "databricks-proxy/" + modelName

	return c.writeConfig(config)
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

	dbProxy["baseURL"] = proxyURL
	providers["databricks-proxy"] = dbProxy
	config["provider"] = providers

	return c.writeConfig(config)
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
