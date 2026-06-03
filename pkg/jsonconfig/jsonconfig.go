// Package jsonconfig manages the OpenCode CLI config.json file.
// It uses github.com/tidwall/jsonc to strip comments and trailing commas
// before parsing, allowing users to write JSONC in their config files.
//
// Design: surgical patching only. opencode is a patch-and-leave-it
// persistent config — we own three provider keys: `databricks-proxy`
// (Anthropic via @ai-sdk/anthropic on /v1), `databricks-gemini-proxy`
// (Gemini Native via @ai-sdk/google on /v1beta), and
// `databricks-openai-proxy` (OpenAI Responses via @ai-sdk/openai on
// /openai/v1). All three are rewritten idempotently via Patch on every
// run that NeedsConfig reports stale. All three route through the same
// local proxy port — Anthropic on the catch-all, Gemini on the /v1beta
// path-prefix route, OpenAI on the /openai/v1 path-prefix route (so the
// proxy's Responses SSE rewriter still fires on /openai/v1/responses).
// No backup, no restore, no crash-recovery sidecar.
package jsonconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"
)

// Config reads and patches the OpenCode config.json file.
type Config struct {
	path string
}

// New creates a Config that manages opencode.json in the given config directory.
// The caller should pass the OS-specific opencode config dir (e.g. from opencodeConfigDir()).
func New(dir string) *Config {
	return &Config{
		path: filepath.Join(dir, "opencode.json"),
	}
}

// NewWithPath creates a Config with an explicit config path (for testing).
func NewWithPath(configPath string) *Config {
	return &Config{
		path: configPath,
	}
}

// Path returns the config file path.
func (c *Config) Path() string {
	return c.path
}

// Patch injects all three managed providers — databricks-proxy
// (Anthropic), databricks-gemini-proxy (Gemini Native), and
// databricks-openai-proxy (OpenAI Responses) — and optionally sets the
// active model. If forceModel is true, the model is always written
// (explicit --model flag). If forceModel is false, the model is only set
// if absent (preserve-if-present).
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
		"models": map[string]interface{}{
			"databricks-claude-opus-4-7":   map[string]interface{}{},
			"databricks-claude-opus-4-6":   map[string]interface{}{},
			"databricks-claude-opus-4-5":   map[string]interface{}{},
			"databricks-claude-sonnet-4-6": map[string]interface{}{},
			"databricks-claude-sonnet-4-5": map[string]interface{}{},
			"databricks-claude-haiku-4-5":  map[string]interface{}{},
		},
	}

	// Inject the databricks-gemini-proxy provider (always overwrite — we own
	// this key too). Uses @ai-sdk/google; the AI SDK requires a non-empty
	// apiKey even though the proxy rewrites auth server-side, hence the
	// placeholder. The Authorization header is set explicitly because the
	// SDK's default auth scheme is x-goog-api-key, not Bearer.
	providers["databricks-gemini-proxy"] = map[string]interface{}{
		"npm":  "@ai-sdk/google",
		"name": "Databricks Gemini",
		"options": map[string]interface{}{
			"baseURL": proxyURL + "/v1beta",
			"apiKey":  apiKey,
			"headers": map[string]interface{}{
				"Authorization": "Bearer " + apiKey,
			},
		},
		// Register all available Databricks Gemini models so users can switch
		// between them in OpenCode's model picker without manual config edits.
		"models": map[string]interface{}{
			"databricks-gemini-3-1-pro":        map[string]interface{}{},
			"databricks-gemini-3-1-flash-lite": map[string]interface{}{},
			"databricks-gemini-3-pro":          map[string]interface{}{},
			"databricks-gemini-3-flash":        map[string]interface{}{},
			"databricks-gemini-3-5-flash":      map[string]interface{}{},
			"databricks-gemini-2-5-pro":        map[string]interface{}{},
			"databricks-gemini-2-5-flash":      map[string]interface{}{},
		},
	}

	// Inject the databricks-openai-proxy provider (always overwrite — we own
	// this key too). Uses @ai-sdk/openai; the proxy overwrites auth headers
	// with the real Databricks token, so the apiKey here is a placeholder.
	// baseURL points at the local proxy's /openai/v1 path-prefix route so
	// the Responses SSE rewriter still fires on /openai/v1/responses.
	providers["databricks-openai-proxy"] = map[string]interface{}{
		"npm":  "@ai-sdk/openai",
		"name": "Databricks AI Gateway (OpenAI Responses)",
		"options": map[string]interface{}{
			"baseURL": proxyURL + "/openai/v1",
			"apiKey":  apiKey,
		},
		// Register Databricks-hosted OpenAI models so users can switch
		// between them in OpenCode's model picker without manual config edits.
		"models": map[string]interface{}{
			"databricks-gpt-5-5": map[string]interface{}{},
		},
	}
	config["provider"] = providers

	// Set the active model: preserve-if-present unless forced.
	// TODO(follow-up): hardcoded "databricks-proxy/" prefix breaks --model
	// selection for non-Anthropic providers (databricks-gemini-proxy,
	// databricks-openai-proxy). Active-model derivation by model→provider
	// mapping is tracked as a follow-up issue. Workaround: switch via
	// OpenCode's in-app model picker.
	if forceModel {
		config["model"] = "databricks-proxy/" + modelName
	} else {
		if _, exists := config["model"]; !exists {
			config["model"] = "databricks-proxy/" + modelName
		}
	}

	return c.writeConfig(config)
}

// NeedsConfig returns true if config.json needs to be written (or
// rewritten) because any of the three managed providers — databricks-proxy
// (Anthropic), databricks-gemini-proxy (Gemini Native), or
// databricks-openai-proxy (OpenAI Responses) — is absent or has a stale
// baseURL / apiKey / npm value. Returns true when the config file is
// missing, the provider section is absent, or any managed-key drift is
// detected on any provider.
func (c *Config) NeedsConfig(proxyURL string) bool {
	config, err := c.readConfig()
	if err != nil {
		return true
	}
	providers, _ := config["provider"].(map[string]interface{})
	if providers == nil {
		return true
	}
	if needsProviderRefresh(providers, "databricks-proxy", "@ai-sdk/anthropic", proxyURL+"/v1") {
		return true
	}
	if needsProviderRefresh(providers, "databricks-gemini-proxy", "@ai-sdk/google", proxyURL+"/v1beta") {
		return true
	}
	if needsProviderRefresh(providers, "databricks-openai-proxy", "@ai-sdk/openai", proxyURL+"/openai/v1") {
		return true
	}
	return false
}

// needsProviderRefresh returns true when the named provider is missing
// or when any managed key drifts from the expected value: options
// missing, baseURL stale, apiKey absent (catches stale `authToken` keys
// from prior schemas), or the npm package wrong.
func needsProviderRefresh(providers map[string]interface{}, providerKey, wantNPM, wantBaseURL string) bool {
	provider, _ := providers[providerKey].(map[string]interface{})
	if provider == nil {
		return true
	}
	options, _ := provider["options"].(map[string]interface{})
	if options == nil {
		return true
	}
	baseURL, _ := options["baseURL"].(string)
	if baseURL != wantBaseURL {
		return true
	}
	if _, ok := options["apiKey"]; !ok {
		return true
	}
	npm, _ := provider["npm"].(string)
	return npm != wantNPM
}

// UpdateProxyURL updates the baseURL for all three managed providers —
// databricks-proxy (Anthropic) at proxyURL+"/v1", databricks-gemini-proxy
// (Gemini Native) at proxyURL+"/v1beta", and databricks-openai-proxy
// (OpenAI Responses) at proxyURL+"/openai/v1". The argument is the base
// proxy URL (no API-version suffix); per-provider suffixes are applied
// internally so callers do not have to know which upstream lives at which
// path. Returns an error if the databricks-proxy provider is missing —
// the gemini and openai-proxy providers are best-effort (existing
// pre-upgrade configs without them are not failed by hand-off).
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

	dbProxyOpts, _ := dbProxy["options"].(map[string]interface{})
	if dbProxyOpts == nil {
		dbProxyOpts = make(map[string]interface{})
	}
	dbProxyOpts["baseURL"] = proxyURL + "/v1"
	dbProxy["options"] = dbProxyOpts
	providers["databricks-proxy"] = dbProxy

	if dbGemini, _ := providers["databricks-gemini-proxy"].(map[string]interface{}); dbGemini != nil {
		geminiOpts, _ := dbGemini["options"].(map[string]interface{})
		if geminiOpts == nil {
			geminiOpts = make(map[string]interface{})
		}
		geminiOpts["baseURL"] = proxyURL + "/v1beta"
		dbGemini["options"] = geminiOpts
		providers["databricks-gemini-proxy"] = dbGemini
	}

	if dbOpenAI, _ := providers["databricks-openai-proxy"].(map[string]interface{}); dbOpenAI != nil {
		openaiOpts, _ := dbOpenAI["options"].(map[string]interface{})
		if openaiOpts == nil {
			openaiOpts = make(map[string]interface{})
		}
		openaiOpts["baseURL"] = proxyURL + "/openai/v1"
		dbOpenAI["options"] = openaiOpts
		providers["databricks-openai-proxy"] = dbOpenAI
	}

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
