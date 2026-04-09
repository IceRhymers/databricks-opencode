package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooks_CreatesPluginJS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := installHooks(); err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	pluginPath := filepath.Join(home, ".config", "opencode", "plugins", "databricks-proxy", "index.js")
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("failed to read plugin file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "databricks-opencode-proxy") {
		t.Error("plugin JS does not contain expected plugin name")
	}
	if !strings.Contains(content, `"session.start"`) {
		t.Error("plugin JS does not contain session.start hook")
	}
	if !strings.Contains(content, `"session.end"`) {
		t.Error("plugin JS does not contain session.end hook")
	}
	if !strings.Contains(content, "--headless-ensure") {
		t.Error("plugin JS does not contain --headless-ensure")
	}
	if !strings.Contains(content, "--headless-release") {
		t.Error("plugin JS does not contain --headless-release")
	}
	if !strings.Contains(content, "spawnSync") {
		t.Error("plugin JS does not use spawnSync")
	}
}

func TestInstallHooks_RegistersPluginInConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := installHooks(); err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read opencode.json: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to parse opencode.json: %v", err)
	}

	plugins, ok := doc["plugins"].([]interface{})
	if !ok || len(plugins) == 0 {
		t.Fatal("expected plugins array in opencode.json")
	}

	pluginDir := filepath.Join(home, ".config", "opencode", "plugins", "databricks-proxy")
	found := false
	for _, p := range plugins {
		if s, ok := p.(string); ok && s == pluginDir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("plugin dir %q not found in plugins array: %v", pluginDir, plugins)
	}
}

func TestInstallHooks_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Install twice.
	if err := installHooks(); err != nil {
		t.Fatalf("first installHooks: %v", err)
	}
	if err := installHooks(); err != nil {
		t.Fatalf("second installHooks: %v", err)
	}

	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read opencode.json: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to parse opencode.json: %v", err)
	}

	plugins, ok := doc["plugins"].([]interface{})
	if !ok {
		t.Fatal("expected plugins array")
	}

	// Should only have one entry, not duplicates.
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin entry after double install, got %d: %v", len(plugins), plugins)
	}
}

func TestInstallHooks_PreservesExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write an existing opencode.json with some extra config.
	configDir := filepath.Join(home, ".config", "opencode")
	os.MkdirAll(configDir, 0o755)
	existing := map[string]interface{}{
		"theme": "dark",
		"plugins": []interface{}{
			"/some/other/plugin",
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(configDir, "opencode.json"), data, 0o644)

	if err := installHooks(); err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	configPath := filepath.Join(configDir, "opencode.json")
	raw, _ := os.ReadFile(configPath)
	var doc map[string]interface{}
	json.Unmarshal(raw, &doc)

	// Theme should be preserved.
	if doc["theme"] != "dark" {
		t.Error("existing config key 'theme' was lost")
	}

	// Both plugins should be present.
	plugins, _ := doc["plugins"].([]interface{})
	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins (existing + new), got %d: %v", len(plugins), plugins)
	}
}

func TestUninstallHooks_RemovesPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Install first.
	if err := installHooks(); err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	// Verify plugin dir exists.
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins", "databricks-proxy")
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		t.Fatal("plugin dir should exist after install")
	}

	// Uninstall.
	if err := uninstallHooks(); err != nil {
		t.Fatalf("uninstallHooks: %v", err)
	}

	// Plugin dir should be gone.
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Error("plugin dir should be removed after uninstall")
	}

	// Plugin should be removed from opencode.json.
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	raw, _ := os.ReadFile(configPath)
	var doc map[string]interface{}
	json.Unmarshal(raw, &doc)

	// plugins key should be deleted when empty.
	if _, ok := doc["plugins"]; ok {
		t.Error("plugins key should be removed when empty")
	}
}

func TestUninstallHooks_NoopWhenNotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Uninstall without prior install should not error.
	if err := uninstallHooks(); err != nil {
		t.Fatalf("uninstallHooks on clean system: %v", err)
	}
}

func TestParseArgs_InstallHooks(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, installHooksFlag, _, _, _, _ := parseArgs([]string{"--install-hooks"})
	if !installHooksFlag {
		t.Error("expected installHooksFlag=true for --install-hooks")
	}
}

func TestParseArgs_UninstallHooks(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, uninstallHooksFlag, _, _, _ := parseArgs([]string{"--uninstall-hooks"})
	if !uninstallHooksFlag {
		t.Error("expected uninstallHooksFlag=true for --uninstall-hooks")
	}
}

func TestParseArgs_HeadlessEnsure(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, headlessEnsureFlag, _, _ := parseArgs([]string{"--headless-ensure"})
	if !headlessEnsureFlag {
		t.Error("expected headlessEnsureFlag=true for --headless-ensure")
	}
}

func TestParseArgs_HeadlessRelease(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, headlessReleaseFlag, _ := parseArgs([]string{"--headless-release"})
	if !headlessReleaseFlag {
		t.Error("expected headlessReleaseFlag=true for --headless-release")
	}
}

func TestParseArgs_IdleTimeout(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, _, idleTimeout, _, _, _, _, _ := parseArgs([]string{"--idle-timeout", "10m"})
	if idleTimeout.Minutes() != 10 {
		t.Errorf("expected idleTimeout=10m, got %v", idleTimeout)
	}
}

func TestParseArgs_IdleTimeoutBareMinutes(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, _, idleTimeout, _, _, _, _, _ := parseArgs([]string{"--idle-timeout", "5"})
	if idleTimeout.Minutes() != 5 {
		t.Errorf("expected idleTimeout=5m, got %v", idleTimeout)
	}
}

func TestPluginJS_UsesSpawnSync(t *testing.T) {
	if !strings.Contains(pluginJS, "spawnSync") {
		t.Error("pluginJS should use spawnSync, not spawn")
	}
	if strings.Contains(pluginJS, "detached") {
		t.Error("pluginJS should not use detached mode")
	}
}
