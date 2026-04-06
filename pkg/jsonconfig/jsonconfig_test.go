package jsonconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestConfig(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	return NewWithPath(
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.json.databricks-opencode-backup"),
	)
}

func readJSON(t *testing.T, path string) map[string]interface{} {
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

func TestPatchEmptyFile(t *testing.T) {
	c := setupTestConfig(t)

	if err := c.Patch("http://127.0.0.1:9000", "gpt-5-4", "databricks-proxy"); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())

	// Check model field.
	model, ok := m["model"].(string)
	if !ok || model != "databricks-proxy/gpt-5-4" {
		t.Errorf("model = %q, want %q", model, "databricks-proxy/gpt-5-4")
	}

	// Check provider injected.
	providers, _ := m["provider"].(map[string]interface{})
	dbProxy, _ := providers["databricks-proxy"].(map[string]interface{})
	if dbProxy == nil {
		t.Fatal("databricks-proxy provider not found")
	}
	if dbProxy["baseURL"] != "http://127.0.0.1:9000" {
		t.Errorf("baseURL = %v, want %q", dbProxy["baseURL"], "http://127.0.0.1:9000")
	}
	if dbProxy["apiKey"] != "databricks-proxy" {
		t.Errorf("apiKey = %v, want %q", dbProxy["apiKey"], "databricks-proxy")
	}
}

func TestPatchPreservesUserConfig(t *testing.T) {
	c := setupTestConfig(t)

	// Write existing config with user providers, commands, and agents.
	existing := `{
  "provider": {
    "openai": {
      "apiKey": "sk-test-123",
      "models": ["gpt-4o"]
    }
  },
  "commands": {
    "build": "npm run build"
  },
  "agents": {
    "code-review": {"model": "openai/gpt-4o"}
  }
}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	if err := c.Patch("http://127.0.0.1:8080", "claude-4", "db-key"); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())

	// Verify user providers preserved.
	providers, _ := m["provider"].(map[string]interface{})
	if _, ok := providers["openai"]; !ok {
		t.Error("openai provider was not preserved")
	}
	if _, ok := providers["databricks-proxy"]; !ok {
		t.Error("databricks-proxy provider was not injected")
	}

	// Verify commands preserved.
	commands, _ := m["commands"].(map[string]interface{})
	if commands["build"] != "npm run build" {
		t.Errorf("commands.build = %v, want %q", commands["build"], "npm run build")
	}

	// Verify agents preserved.
	agents, _ := m["agents"].(map[string]interface{})
	if agents["code-review"] == nil {
		t.Error("agents.code-review was not preserved")
	}
}

func TestBackupRestore(t *testing.T) {
	c := setupTestConfig(t)

	original := `{"theme": "dark", "fontSize": 14}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// Backup.
	if err := c.Backup(); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if !c.HasBackup() {
		t.Fatal("HasBackup should be true after Backup")
	}

	// Patch (overwrites the model key).
	if err := c.Patch("http://127.0.0.1:5000", "test-model", "key"); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Verify patched.
	patched := readJSON(t, c.Path())
	if patched["model"] != "databricks-proxy/test-model" {
		t.Fatalf("expected patched model, got %v", patched["model"])
	}

	// Restore.
	if err := c.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify restored to original content.
	data, _ := os.ReadFile(c.Path())
	var restored map[string]interface{}
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal restored: %v", err)
	}
	if restored["theme"] != "dark" {
		t.Errorf("theme = %v, want %q", restored["theme"], "dark")
	}

	// Backup file should be removed.
	if c.HasBackup() {
		t.Error("HasBackup should be false after Restore")
	}
}

func TestCrashRecovery(t *testing.T) {
	c := setupTestConfig(t)

	original := `{"theme": "light"}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// Simulate: backup was created, patch was applied, but no restore (crash).
	if err := c.Backup(); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if err := c.Patch("http://127.0.0.1:5000", "m", "k"); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Simulate crash — HasBackup should return true.
	if !c.HasBackup() {
		t.Fatal("HasBackup should be true after crash (no restore called)")
	}

	// New session recovers by calling Restore.
	if err := c.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	data, _ := os.ReadFile(c.Path())
	var restored map[string]interface{}
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal restored: %v", err)
	}
	if restored["theme"] != "light" {
		t.Errorf("theme = %v, want %q after crash recovery", restored["theme"], "light")
	}
}

func TestUpdateProxyURL(t *testing.T) {
	c := setupTestConfig(t)

	// Patch first.
	if err := c.Patch("http://127.0.0.1:5000", "model-a", "key"); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Update proxy URL.
	if err := c.UpdateProxyURL("http://127.0.0.1:6000"); err != nil {
		t.Fatalf("UpdateProxyURL: %v", err)
	}

	m := readJSON(t, c.Path())
	providers, _ := m["provider"].(map[string]interface{})
	dbProxy, _ := providers["databricks-proxy"].(map[string]interface{})
	if dbProxy["baseURL"] != "http://127.0.0.1:6000" {
		t.Errorf("baseURL = %v, want %q", dbProxy["baseURL"], "http://127.0.0.1:6000")
	}

	// Model should be unchanged.
	if m["model"] != "databricks-proxy/model-a" {
		t.Errorf("model changed unexpectedly: %v", m["model"])
	}
}

func TestInvalidJSONC(t *testing.T) {
	c := setupTestConfig(t)

	// Write JSONC with comments and trailing commas.
	jsoncContent := `{
  // This is a comment
  "theme": "dark",
  "provider": {
    "openai": {
      "apiKey": "sk-test", // inline comment
    },
  },
}`
	if err := os.WriteFile(c.Path(), []byte(jsoncContent), 0o600); err != nil {
		t.Fatalf("write JSONC: %v", err)
	}

	// Patch should parse JSONC correctly.
	if err := c.Patch("http://127.0.0.1:7000", "model-b", "key"); err != nil {
		t.Fatalf("Patch with JSONC: %v", err)
	}

	m := readJSON(t, c.Path())

	// User config preserved.
	if m["theme"] != "dark" {
		t.Errorf("theme = %v, want %q", m["theme"], "dark")
	}

	// Provider injected alongside existing.
	providers, _ := m["provider"].(map[string]interface{})
	if providers["openai"] == nil {
		t.Error("openai provider lost after JSONC patch")
	}
	if providers["databricks-proxy"] == nil {
		t.Error("databricks-proxy not injected")
	}
}
