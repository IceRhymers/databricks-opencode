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

	if err := cm.Setup("http://127.0.0.1:9000", "gpt-5-4", "databricks-proxy"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	m := readJSONFile(t, cm.config.Path())

	if m["model"] != "databricks-proxy/gpt-5-4" {
		t.Errorf("model = %v, want %q", m["model"], "databricks-proxy/gpt-5-4")
	}

	providers, _ := m["provider"].(map[string]interface{})
	dbProxy, _ := providers["databricks-proxy"].(map[string]interface{})
	if dbProxy == nil {
		t.Fatal("databricks-proxy provider not found")
	}
	if dbProxy["baseURL"] != "http://127.0.0.1:9000" {
		t.Errorf("baseURL = %v, want %q", dbProxy["baseURL"], "http://127.0.0.1:9000")
	}
}

func TestConfigManagerSetupPreservesExisting(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	// Write existing config.
	existing := `{"theme": "dark", "provider": {"openai": {"apiKey": "sk-test"}}}`
	if err := os.WriteFile(cm.config.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cm.Setup("http://127.0.0.1:8080", "model-a", "key"); err != nil {
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
	if providers["databricks-proxy"] == nil {
		t.Error("databricks-proxy not injected")
	}
}

func TestConfigManagerRestore(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	original := `{"theme": "dark"}`
	if err := os.WriteFile(cm.config.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := cm.Setup("http://127.0.0.1:5000", "m", "k"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Verify patched.
	patched := readJSONFile(t, cm.config.Path())
	if patched["model"] == nil {
		t.Fatal("expected model after patch")
	}

	// Restore.
	cm.Restore()

	data, err := os.ReadFile(cm.config.Path())
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	var restored map[string]interface{}
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal restored: %v", err)
	}
	if restored["theme"] != "dark" {
		t.Errorf("theme = %v, want %q after restore", restored["theme"], "dark")
	}
	if restored["model"] != nil {
		t.Error("model should not exist after restore")
	}
}

func TestConfigManagerCrashRecovery(t *testing.T) {
	cm, _ := setupTestConfigManager(t)

	original := `{"theme": "light"}`
	if err := os.WriteFile(cm.config.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Simulate first session: setup but no restore (crash).
	if err := cm.Setup("http://127.0.0.1:5000", "m", "k"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Verify backup exists (simulating crash — no Restore called).
	if !cm.config.HasBackup() {
		t.Fatal("backup should exist after setup")
	}

	// New session setup should recover: restore first, then re-patch.
	if err := cm.Setup("http://127.0.0.1:6000", "m2", "k2"); err != nil {
		t.Fatalf("Setup after crash: %v", err)
	}

	m := readJSONFile(t, cm.config.Path())
	if m["model"] != "databricks-proxy/m2" {
		t.Errorf("model = %v, want %q", m["model"], "databricks-proxy/m2")
	}
}
