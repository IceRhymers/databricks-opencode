package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/IceRhymers/databricks-opencode/pkg/jsonconfig"
)

// headlessEnsure checks whether the proxy is healthy on the given port.
// If not, it starts a detached headless proxy and polls until ready (max 10s).
// Called by the opencode plugin at init via: databricks-opencode --headless-ensure
//
// No refcount is acquired here — OpenCode has no exit hook to release it,
// so the proxy relies on its idle timeout for shutdown instead.
func headlessEnsure(port int) {
	if os.Getenv("DATABRICKS_OPENCODE_MANAGED") == "1" {
		log.Printf("databricks-opencode: --headless-ensure: skipped (managed session)")
		return
	}

	// Determine scheme from saved TLS config.
	state := loadState()
	scheme := "http"
	if state.TLSCert != "" && state.TLSKey != "" {
		scheme = "https"
	}

	if proxyHealthy(port, scheme) {
		return // already running
	}

	self, err := os.Executable()
	if err != nil {
		log.Fatalf("databricks-opencode: --headless-ensure: cannot find self: %v", err)
	}

	args := []string{"--headless", fmt.Sprintf("--port=%d", port)}
	if state.TLSCert != "" && state.TLSKey != "" {
		args = append(args, fmt.Sprintf("--tls-cert=%s", state.TLSCert), fmt.Sprintf("--tls-key=%s", state.TLSKey))
	}
	cmd := exec.Command(self, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Fatalf("databricks-opencode: --headless-ensure: failed to start proxy: %v", err)
	}
	if err := cmd.Process.Release(); err != nil {
		log.Printf("databricks-opencode: --headless-ensure: release warning: %v", err)
	}

	// Poll until healthy or timeout.
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if proxyHealthy(port, scheme) {
			return
		}
	}
	log.Fatalf("databricks-opencode: --headless-ensure: proxy did not become healthy within 10s")
}

// refcountPathForPort returns the file path used for cross-process session counting.
func refcountPathForPort(port int) string {
	return fmt.Sprintf("%s/.databricks-opencode-sessions-%d", os.TempDir(), port)
}

// pluginJSTemplate is the opencode plugin that ensures the headless proxy is running.
// The plugin init body runs at session startup (ESM format required by OpenCode).
// Shutdown is handled by the proxy's idle timeout — OpenCode has no exit hook.
// %s is replaced with the absolute path to the binary at install time so the
// plugin works regardless of Bun's PATH (which may not include ~/go/bin or /opt/homebrew/bin).
const pluginJSTemplate = `export const DatabricksProxy = async ({ $ }) => {
  await $` + "`" + `%s --headless-ensure` + "`" + `;
  return {};
};
`

// installHooks writes the JS plugin and registers it in opencode.json.
// The absolute path to the binary is baked in at install time; rerun --install-hooks after
// reinstalling via a different method (e.g. switching from go install to Homebrew).
func installHooks() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve own binary path: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}

	// Write plugin JS file.
	pluginDir := filepath.Join(homeDir, ".config", "opencode", "plugins", "databricks-proxy")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("creating plugin dir: %w", err)
	}
	pluginJS := fmt.Sprintf(pluginJSTemplate, self)
	pluginPath := filepath.Join(pluginDir, "index.js")
	if err := os.WriteFile(pluginPath, []byte(pluginJS), 0o644); err != nil {
		return fmt.Errorf("writing plugin: %w", err)
	}

	// Register plugin in opencode.json.
	cm := jsonconfig.New()
	return cm.AddPlugin(pluginDir)
}

// uninstallHooks removes the JS plugin file and its entry from opencode.json.
func uninstallHooks() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}

	pluginDir := filepath.Join(homeDir, ".config", "opencode", "plugins", "databricks-proxy")

	// Remove plugin entry from opencode.json.
	cm := jsonconfig.New()
	if err := cm.RemovePlugin(pluginDir); err != nil {
		return fmt.Errorf("removing plugin from config: %w", err)
	}

	// Remove plugin file and directory (only if empty).
	os.Remove(filepath.Join(pluginDir, "index.js"))
	os.Remove(pluginDir)
	return nil
}
