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

// databricksModels are the model keys we inject into provider.anthropic.models.
var databricksModels = []string{
	"claude-opus-4-6",
	"claude-opus-4-5",
	"claude-sonnet-4-6",
	"claude-sonnet-4-5",
	"claude-haiku-4-5",
}

// Config reads, patches, and restores the OpenCode config.json file.
type Config struct {
	path        string
	backupPath  string
	sidecarPath string
	originals   map[string]interface{} // key -> original value, or absent sentinel
}

// New creates a Config that manages ~/.config/opencode/opencode.json.
func New() *Config {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "opencode")
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

	// Snapshot provider.anthropic.options.baseURL and apiKey.
	providers, _ := config["provider"].(map[string]interface{})
	anthropic, _ := getMap(providers, "anthropic")
	options, _ := getMap(anthropic, "options")

	if v, ok := options["baseURL"]; ok {
		c.originals["anthropic.options.baseURL"] = v
	} else {
		c.originals["anthropic.options.baseURL"] = absent
	}

	if v, ok := options["apiKey"]; ok {
		c.originals["anthropic.options.apiKey"] = v
	} else {
		c.originals["anthropic.options.apiKey"] = absent
	}

	// Snapshot which Databricks model keys existed before.
	models, _ := getMap(anthropic, "models")
	for _, m := range databricksModels {
		key := "anthropic.models." + m
		if _, ok := models[m]; ok {
			c.originals[key] = true // existed
		} else {
			c.originals[key] = absent
		}
	}

	return c.writeSidecar()
}

// Patch injects the anthropic provider config and optionally sets the model.
// proxyURL is the local proxy address (e.g. http://127.0.0.1:8080).
// modelName is the full model identifier (e.g. anthropic/claude-sonnet-4-6).
// apiKey is a placeholder key for the proxy.
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

	// Ensure provider.anthropic exists (preserve user's existing keys).
	anthropic, _ := providers["anthropic"].(map[string]interface{})
	if anthropic == nil {
		anthropic = make(map[string]interface{})
	}

	// Ensure options map exists (preserve user's existing options like timeout).
	options, _ := anthropic["options"].(map[string]interface{})
	if options == nil {
		options = make(map[string]interface{})
	}

	// Inject our managed keys into options.
	options["baseURL"] = proxyURL
	options["apiKey"] = apiKey
	anthropic["options"] = options

	// Ensure models map exists (preserve user's existing models).
	models, _ := anthropic["models"].(map[string]interface{})
	if models == nil {
		models = make(map[string]interface{})
	}

	// Add the 5 Databricks model keys.
	for _, m := range databricksModels {
		models[m] = map[string]interface{}{}
	}
	anthropic["models"] = models

	providers["anthropic"] = anthropic
	config["provider"] = providers

	// Set the active model: preserve-if-present unless forced.
	if forceModel {
		config["model"] = modelName
	} else {
		if _, exists := config["model"]; !exists {
			config["model"] = modelName
		}
	}

	return c.writeConfig(config)
}

// Restore surgically removes only the keys we manage and restores originals.
// Removes injected anthropic options and model keys, restores model to its original value.
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

	// Restore anthropic provider keys.
	providers, _ := config["provider"].(map[string]interface{})
	if providers != nil {
		anthropic, _ := providers["anthropic"].(map[string]interface{})
		if anthropic != nil {
			options, _ := anthropic["options"].(map[string]interface{})
			if options != nil {
				// Restore baseURL.
				if orig, ok := c.originals["anthropic.options.baseURL"]; ok {
					if orig == absent {
						delete(options, "baseURL")
					} else {
						options["baseURL"] = orig
					}
				}
				// Restore apiKey.
				if orig, ok := c.originals["anthropic.options.apiKey"]; ok {
					if orig == absent {
						delete(options, "apiKey")
					} else {
						options["apiKey"] = orig
					}
				}
				if len(options) == 0 {
					delete(anthropic, "options")
				} else {
					anthropic["options"] = options
				}
			}

			// Remove only our injected model keys.
			models, _ := anthropic["models"].(map[string]interface{})
			if models != nil {
				for _, m := range databricksModels {
					key := "anthropic.models." + m
					if orig, ok := c.originals[key]; ok && orig == absent {
						delete(models, m)
					}
					// If it existed before, leave it alone.
				}
				if len(models) == 0 {
					delete(anthropic, "models")
				} else {
					anthropic["models"] = models
				}
			}

			// If anthropic provider is now empty, remove it.
			if len(anthropic) == 0 {
				delete(providers, "anthropic")
			} else {
				providers["anthropic"] = anthropic
			}
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

// UpdateProxyURL updates only the baseURL in the existing anthropic provider options.
func (c *Config) UpdateProxyURL(proxyURL string) error {
	config, err := c.readConfig()
	if err != nil {
		return err
	}

	providers, _ := config["provider"].(map[string]interface{})
	if providers == nil {
		return fmt.Errorf("no provider section in config")
	}

	anthropic, _ := providers["anthropic"].(map[string]interface{})
	if anthropic == nil {
		return fmt.Errorf("no anthropic provider in config")
	}

	options, _ := anthropic["options"].(map[string]interface{})
	if options == nil {
		options = make(map[string]interface{})
	}
	options["baseURL"] = proxyURL
	anthropic["options"] = options
	providers["anthropic"] = anthropic
	config["provider"] = providers

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
	Model        interface{} `json:"model"`
	ModelAbsent  bool        `json:"model_absent"`

	BaseURL        interface{} `json:"anthropic_options_baseURL"`
	BaseURLAbsent  bool        `json:"anthropic_options_baseURL_absent"`
	APIKey         interface{} `json:"anthropic_options_apiKey"`
	APIKeyAbsent   bool        `json:"anthropic_options_apiKey_absent"`

	// Which Databricks model keys existed before patching.
	// Maps model name -> true if existed, omitted if absent.
	ExistingModels map[string]bool `json:"existing_models,omitempty"`
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

	if v, ok := c.originals["anthropic.options.baseURL"]; ok {
		if v == absent {
			sd.BaseURLAbsent = true
		} else {
			sd.BaseURL = v
		}
	}

	if v, ok := c.originals["anthropic.options.apiKey"]; ok {
		if v == absent {
			sd.APIKeyAbsent = true
		} else {
			sd.APIKey = v
		}
	}

	sd.ExistingModels = make(map[string]bool)
	for _, m := range databricksModels {
		key := "anthropic.models." + m
		if v, ok := c.originals[key]; ok && v != absent {
			sd.ExistingModels[m] = true
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

	if sd.BaseURLAbsent {
		c.originals["anthropic.options.baseURL"] = absent
	} else {
		c.originals["anthropic.options.baseURL"] = sd.BaseURL
	}

	if sd.APIKeyAbsent {
		c.originals["anthropic.options.apiKey"] = absent
	} else {
		c.originals["anthropic.options.apiKey"] = sd.APIKey
	}

	for _, m := range databricksModels {
		key := "anthropic.models." + m
		if sd.ExistingModels[m] {
			c.originals[key] = true
		} else {
			c.originals[key] = absent
		}
	}

	return nil
}

// getMap safely extracts a nested map from a parent map.
func getMap(parent map[string]interface{}, key string) (map[string]interface{}, bool) {
	if parent == nil {
		return nil, false
	}
	v, ok := parent[key].(map[string]interface{})
	return v, ok
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
