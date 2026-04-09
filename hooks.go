package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/IceRhymers/databricks-claude/pkg/refcount"
)

// headlessEnsure checks whether the proxy is healthy on the given port.
// If not, it starts a detached headless proxy and polls until ready (max 10s).
// Called by the session.start hook via: databricks-opencode --headless-ensure
func headlessEnsure(port int) {
	if os.Getenv("DATABRICKS_OPENCODE_MANAGED") == "1" {
		log.Printf("databricks-opencode: --headless-ensure: skipped (managed session)")
		return
	}

	// Acquire refcount FIRST so every ensure/release pair is symmetric.
	refcountPath := refcountPathForPort(port)
	if err := refcount.Acquire(refcountPath); err != nil {
		log.Printf("databricks-opencode: --headless-ensure: refcount acquire warning: %v", err)
	}

	if isProxyHealthy(port) {
		return // already running, refcount incremented
	}

	self, err := os.Executable()
	if err != nil {
		refcount.Release(refcountPath) // undo acquire on failure
		log.Fatalf("databricks-opencode: --headless-ensure: cannot find self: %v", err)
	}

	cmd := exec.Command(self, "--headless", fmt.Sprintf("--port=%d", port))
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		refcount.Release(refcountPath) // undo acquire on failure
		log.Fatalf("databricks-opencode: --headless-ensure: failed to start proxy: %v", err)
	}
	if err := cmd.Process.Release(); err != nil {
		log.Printf("databricks-opencode: --headless-ensure: release warning: %v", err)
	}

	// Poll until healthy or timeout.
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if isProxyHealthy(port) {
			return
		}
	}
	refcount.Release(refcountPath) // undo acquire on failure
	log.Fatalf("databricks-opencode: --headless-ensure: proxy did not become healthy within 10s")
}

// headlessRelease calls POST /shutdown on the proxy to decrement the refcount.
// Called by the session.end hook via: databricks-opencode --headless-release
// Errors are logged but not fatal — proxy may already be stopped.
func headlessRelease(port int) {
	if os.Getenv("DATABRICKS_OPENCODE_MANAGED") == "1" {
		log.Printf("databricks-opencode: --headless-release: skipped (managed session)")
		return
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/shutdown", port), "application/json", nil)
	if err != nil {
		log.Printf("databricks-opencode: --headless-release: %v (proxy may already be stopped)", err)
		return
	}
	resp.Body.Close()
}

// isProxyHealthy returns true if the proxy on port responds to GET /health.
func isProxyHealthy(port int) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// refcountPathForPort returns the file path used for cross-process session counting.
func refcountPathForPort(port int) string {
	return fmt.Sprintf("%s/.databricks-opencode-sessions-%d", os.TempDir(), port)
}

// pluginJS is the opencode plugin that hooks session.start and session.end
// to manage the headless proxy lifecycle.
const pluginJS = `const { spawnSync } = require("child_process");

module.exports = {
  name: "databricks-opencode-proxy",
  hooks: {
    "session.start": async () => {
      spawnSync("databricks-opencode", ["--headless-ensure"], { stdio: "inherit" });
    },
    "session.end": async () => {
      spawnSync("databricks-opencode", ["--headless-release"], { stdio: "inherit" });
    }
  }
};
`

// installHooks writes the JS plugin to ~/.config/opencode/plugins/databricks-proxy/index.js
// and registers it in ~/.config/opencode/opencode.json under the "plugins" array.
func installHooks() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}

	// Write plugin JS file.
	pluginDir := filepath.Join(homeDir, ".config", "opencode", "plugins", "databricks-proxy")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("creating plugin dir: %w", err)
	}
	pluginPath := filepath.Join(pluginDir, "index.js")
	if err := os.WriteFile(pluginPath, []byte(pluginJS), 0o644); err != nil {
		return fmt.Errorf("writing plugin: %w", err)
	}

	// Register plugin in opencode.json.
	configPath := filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	doc, err := readJSONFile(configPath)
	if err != nil {
		doc = map[string]interface{}{}
	}

	// Ensure "plugins" array contains the plugin path.
	plugins, _ := doc["plugins"].([]interface{})
	pluginEntry := pluginDir
	found := false
	for _, p := range plugins {
		if s, ok := p.(string); ok && s == pluginEntry {
			found = true
			break
		}
	}
	if !found {
		plugins = append(plugins, pluginEntry)
		doc["plugins"] = plugins
	}

	return writeJSONFile(configPath, doc)
}

// uninstallHooks removes the JS plugin directory and its entry from opencode.json.
func uninstallHooks() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}

	// Remove plugin directory.
	pluginDir := filepath.Join(homeDir, ".config", "opencode", "plugins", "databricks-proxy")
	os.RemoveAll(pluginDir)

	// Remove plugin entry from opencode.json.
	configPath := filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	doc, err := readJSONFile(configPath)
	if err != nil {
		return nil // nothing to remove
	}

	plugins, _ := doc["plugins"].([]interface{})
	filtered := make([]interface{}, 0, len(plugins))
	for _, p := range plugins {
		if s, ok := p.(string); ok && s == pluginDir {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		delete(doc, "plugins")
	} else {
		doc["plugins"] = filtered
	}

	return writeJSONFile(configPath, doc)
}

// readJSONFile reads and parses a JSON file into a map.
func readJSONFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// writeJSONFile writes a map as indented JSON to a file.
func writeJSONFile(path string, doc map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
