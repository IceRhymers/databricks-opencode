package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestConfigManager(t *testing.T) (*ConfigManager, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")
	backupPath := configPath + ".databricks-opencode-backup"
	lockPath := filepath.Join(dir, ".config.lock")
	registryPath := filepath.Join(dir, ".sessions.json")
	cm := newConfigManagerWithPaths(configPath, backupPath, lockPath, registryPath)
	return cm, dir
}

func readJSONFile(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return m
}

func TestConfigManagerSetupCreatesConfig(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	if err := cm.Setup("http://127.0.0.1:9000", "anthropic/claude-sonnet-4-6", "databricks-oauth-proxy", false); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	m := readJSONFile(t, cm.config.Path())

	if m["model"] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model = %v, want %q", m["model"], "anthropic/claude-sonnet-4-6")
	}

	providers, _ := m["provider"].(map[string]interface{})
	anthropic, _ := providers["anthropic"].(map[string]interface{})
	if anthropic == nil {
		t.Fatal("anthropic provider not found")
	}
	options, _ := anthropic["options"].(map[string]interface{})
	if options == nil {
		t.Fatal("anthropic options not found")
	}
	if options["baseURL"] != "http://127.0.0.1:9000" {
		t.Errorf("options.baseURL = %v, want %q", options["baseURL"], "http://127.0.0.1:9000")
	}
}

func TestConfigManagerSetupPreservesExisting(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	// Write existing config.
	existing := `{"theme": "dark", "provider": {"openai": {"apiKey": "sk-test"}}}`
	if err := os.WriteFile(cm.config.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cm.Setup("http://127.0.0.1:8080", "anthropic/claude-sonnet-4-6", "key", false); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	m := readJSONFile(t, cm.config.Path())

	// User config preserved.
	if m["theme"] != "dark" {
		t.Errorf("theme = %v, want %q", m["theme"], "dark")
	}

	providers, _ := m["provider"].(map[string]interface{})
	if providers["openai"] == nil {
		t.Error("openai provider was not preserved")
	}
	if providers["anthropic"] == nil {
		t.Error("anthropic not injected")
	}
}

func TestConfigManagerRestore(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	original := `{"theme": "dark"}`
	if err := os.WriteFile(cm.config.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cm.Setup("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", false); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Verify patched.
	patched := readJSONFile(t, cm.config.Path())
	if patched["model"] == nil {
		t.Fatal("expected model after patch")
	}

	// Simulate user adding config mid-session.
	patched["mcpServers"] = map[string]interface{}{"test": "value"}
	data, _ := json.MarshalIndent(patched, "", "  ")
	os.WriteFile(cm.config.Path(), data, 0o600)

	// Restore.
	cm.Restore()

	rdata, err := os.ReadFile(cm.config.Path())
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	var restored map[string]interface{}
	if err := json.Unmarshal(rdata, &restored); err != nil {
		t.Fatalf("unmarshal restored: %v", err)
	}
	if restored["theme"] != "dark" {
		t.Errorf("theme = %v, want %q after restore", restored["theme"], "dark")
	}
	if restored["model"] != nil {
		t.Error("model should not exist after restore")
	}
	// Mid-session user changes should survive surgical restore.
	if restored["mcpServers"] == nil {
		t.Error("mcpServers should survive surgical restore (user added mid-session)")
	}
}

func TestConfigManagerCrashRecovery(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	original := `{"theme": "light"}`
	if err := os.WriteFile(cm.config.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Simulate first session: setup but no restore (crash).
	if err := cm.Setup("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", false); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Verify sidecar exists (simulating crash — no Restore called).
	if !cm.config.HasSidecar() {
		t.Fatal("sidecar should exist after setup")
	}

	// New session setup should recover: surgical restore first, then re-patch.
	if err := cm.Setup("http://127.0.0.1:6000", "anthropic/claude-opus-4-5", "k2", false); err != nil {
		t.Fatalf("Setup after crash: %v", err)
	}

	m := readJSONFile(t, cm.config.Path())
	// Model was set during first session and preserved (not forced).
	// After crash recovery the first session's model is restored (absent -> removed),
	// then re-patched with the new session's model.
	if m["model"] != "anthropic/claude-opus-4-5" {
		t.Errorf("model = %v, want %q", m["model"], "anthropic/claude-opus-4-5")
	}

	// Original user config should still be there.
	if m["theme"] != "light" {
		t.Errorf("theme = %v, want %q after crash recovery", m["theme"], "light")
	}
}

func TestConfigManagerPreservesUserModel(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	// User has their own model configured.
	original := `{"model": "openai/gpt-4o", "theme": "dark"}`
	if err := os.WriteFile(cm.config.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Setup without forceModel — should preserve user's model.
	if err := cm.Setup("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", false); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	m := readJSONFile(t, cm.config.Path())
	if m["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v, want %q (should preserve user model)", m["model"], "openai/gpt-4o")
	}

	// Restore — user's model should still be there.
	cm.Restore()

	rdata, _ := os.ReadFile(cm.config.Path())
	var restored map[string]interface{}
	json.Unmarshal(rdata, &restored)
	if restored["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v, want %q after restore", restored["model"], "openai/gpt-4o")
	}
}
