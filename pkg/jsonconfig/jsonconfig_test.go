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

	if err := c.Patch("http://127.0.0.1:9000", "anthropic/claude-sonnet-4-6", "databricks-oauth-proxy", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())

	// Check model field — should be set when absent.
	model, ok := m["model"].(string)
	if !ok || model != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model = %q, want %q", model, "anthropic/claude-sonnet-4-6")
	}

	// Check provider injected.
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
	if options["apiKey"] != "databricks-oauth-proxy" {
		t.Errorf("options.apiKey = %v, want %q", options["apiKey"], "databricks-oauth-proxy")
	}

	// Check all 5 Databricks models are injected.
	models, _ := anthropic["models"].(map[string]interface{})
	if models == nil {
		t.Fatal("anthropic models not found")
	}
	for _, m := range databricksModels {
		if models[m] == nil {
			t.Errorf("models[%q] not found", m)
		}
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

	if err := c.Patch("http://127.0.0.1:8080", "anthropic/claude-sonnet-4-6", "db-key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())

	// Verify user providers preserved.
	providers, _ := m["provider"].(map[string]interface{})
	if _, ok := providers["openai"]; !ok {
		t.Error("openai provider was not preserved")
	}
	if _, ok := providers["anthropic"]; !ok {
		t.Error("anthropic provider was not injected")
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

func TestPatchPreservesExistingModel(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with user-configured model.
	existing := `{"model": "openai/gpt-4o", "theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Patch without forceModel — should preserve existing model.
	if err := c.Patch("http://127.0.0.1:9000", "anthropic/claude-sonnet-4-6", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())
	if m["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v, want %q (should preserve existing)", m["model"], "openai/gpt-4o")
	}
}

func TestPatchForceModelOverridesExisting(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with user-configured model.
	existing := `{"model": "openai/gpt-4o"}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Patch with forceModel — should override.
	if err := c.Patch("http://127.0.0.1:9000", "anthropic/claude-sonnet-4-6", "key", true); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())
	if m["model"] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model = %v, want %q (forceModel should override)", m["model"], "anthropic/claude-sonnet-4-6")
	}
}

func TestPatchSetsModelWhenAbsent(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with no model key.
	existing := `{"theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := c.Patch("http://127.0.0.1:9000", "anthropic/claude-sonnet-4-6", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())
	if m["model"] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model = %v, want %q (should set when absent)", m["model"], "anthropic/claude-sonnet-4-6")
	}
}

func TestSurgicalRestore(t *testing.T) {
	c := setupTestConfig(t)

	original := `{"theme": "dark", "fontSize": 14}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// Snapshot originals.
	if err := c.SaveOriginals(); err != nil {
		t.Fatalf("SaveOriginals: %v", err)
	}

	// Write sentinel.
	if err := c.WriteSentinel(); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if !c.HasBackup() {
		t.Fatal("HasBackup should be true after WriteSentinel")
	}

	// Patch.
	if err := c.Patch("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Verify patched.
	patched := readJSON(t, c.Path())
	if patched["model"] != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("expected patched model, got %v", patched["model"])
	}

	// Surgical restore.
	if err := c.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify: model removed (was absent), user config preserved.
	restored := readJSON(t, c.Path())
	if restored["theme"] != "dark" {
		t.Errorf("theme = %v, want %q", restored["theme"], "dark")
	}
	if _, exists := restored["model"]; exists {
		t.Error("model should not exist after restore (was absent before patch)")
	}
	if _, exists := restored["provider"]; exists {
		t.Error("provider should not exist after restore (was absent before patch)")
	}

	// Sentinel and sidecar should be removed.
	if c.HasBackup() {
		t.Error("HasBackup should be false after Restore")
	}
	if c.HasSidecar() {
		t.Error("HasSidecar should be false after Restore")
	}
}

func TestRestoreOnlyRemovesManagedKeys(t *testing.T) {
	c := setupTestConfig(t)

	original := `{"theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Snapshot + Patch.
	if err := c.SaveOriginals(); err != nil {
		t.Fatalf("SaveOriginals: %v", err)
	}
	if err := c.Patch("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Simulate user adding new config mid-session.
	config := readJSON(t, c.Path())
	config["mcpServers"] = map[string]interface{}{"my-server": map[string]interface{}{"url": "http://localhost:3000"}}
	config["newTheme"] = "solarized"
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(c.Path(), data, 0o600)

	// Restore — should only remove managed keys, preserve user additions.
	if err := c.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored := readJSON(t, c.Path())

	// Original keys preserved.
	if restored["theme"] != "dark" {
		t.Errorf("theme = %v, want %q", restored["theme"], "dark")
	}

	// Mid-session user additions preserved.
	if restored["mcpServers"] == nil {
		t.Error("mcpServers should survive restore (user added mid-session)")
	}
	if restored["newTheme"] != "solarized" {
		t.Errorf("newTheme = %v, want %q (user added mid-session)", restored["newTheme"], "solarized")
	}

	// Managed keys removed.
	if _, exists := restored["model"]; exists {
		t.Error("model should be removed after restore")
	}
	// anthropic provider should be fully removed (was absent before patch).
	if _, exists := restored["provider"]; exists {
		t.Error("provider should not exist after restore (was absent before patch)")
	}
}

func TestRestorePreservesOriginalModel(t *testing.T) {
	c := setupTestConfig(t)

	// Config has an existing user model.
	original := `{"model": "openai/gpt-4o", "theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Snapshot (captures model = "openai/gpt-4o").
	if err := c.SaveOriginals(); err != nil {
		t.Fatalf("SaveOriginals: %v", err)
	}

	// Force-patch with our model.
	if err := c.Patch("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", true); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Restore — should put user's model back.
	if err := c.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored := readJSON(t, c.Path())
	if restored["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v, want %q (should restore original)", restored["model"], "openai/gpt-4o")
	}
}

func TestCrashRecoverySurgical(t *testing.T) {
	c := setupTestConfig(t)

	original := `{"theme": "light"}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// Simulate: SaveOriginals + sentinel + patch, but no Restore (crash).
	if err := c.SaveOriginals(); err != nil {
		t.Fatalf("SaveOriginals: %v", err)
	}
	if err := c.WriteSentinel(); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if err := c.Patch("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Simulate crash — sidecar and sentinel should exist.
	if !c.HasSidecar() {
		t.Fatal("HasSidecar should be true after crash")
	}
	if !c.HasBackup() {
		t.Fatal("HasBackup should be true after crash")
	}

	// New session creates a fresh Config instance (no in-memory originals).
	c2 := NewWithPath(c.Path(), c.BackupPath())

	// Recovery: Restore loads sidecar and surgically restores.
	if err := c2.Restore(); err != nil {
		t.Fatalf("Restore after crash: %v", err)
	}

	restored := readJSON(t, c.Path())
	if restored["theme"] != "light" {
		t.Errorf("theme = %v, want %q after crash recovery", restored["theme"], "light")
	}
	if _, exists := restored["model"]; exists {
		t.Error("model should be removed after crash recovery")
	}
}

func TestUpdateProxyURL(t *testing.T) {
	c := setupTestConfig(t)

	// Patch first.
	if err := c.Patch("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Update proxy URL.
	if err := c.UpdateProxyURL("http://127.0.0.1:6000"); err != nil {
		t.Fatalf("UpdateProxyURL: %v", err)
	}

	m := readJSON(t, c.Path())
	providers, _ := m["provider"].(map[string]interface{})
	anthropic, _ := providers["anthropic"].(map[string]interface{})
	options, _ := anthropic["options"].(map[string]interface{})
	if options == nil {
		t.Fatal("anthropic options not found after UpdateProxyURL")
	}
	if options["baseURL"] != "http://127.0.0.1:6000" {
		t.Errorf("options.baseURL = %v, want %q", options["baseURL"], "http://127.0.0.1:6000")
	}

	// Model should be unchanged.
	if m["model"] != "anthropic/claude-sonnet-4-6" {
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
	if err := c.Patch("http://127.0.0.1:7000", "anthropic/claude-sonnet-4-6", "key", false); err != nil {
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
	if providers["anthropic"] == nil {
		t.Error("anthropic not injected")
	}
}

func TestRestorePreservesExistingAnthropicConfig(t *testing.T) {
	c := setupTestConfig(t)

	// User already has an anthropic provider with their own model and timeout.
	original := `{
  "provider": {
    "anthropic": {
      "options": {
        "timeout": 30000
      },
      "models": {
        "my-custom-model": {"name": "My Model"}
      }
    }
  }
}`
	if err := os.WriteFile(c.Path(), []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Snapshot + Patch.
	if err := c.SaveOriginals(); err != nil {
		t.Fatalf("SaveOriginals: %v", err)
	}
	if err := c.Patch("http://127.0.0.1:5000", "anthropic/claude-sonnet-4-6", "k", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Verify patched: should have our keys plus user's keys.
	patched := readJSON(t, c.Path())
	providers, _ := patched["provider"].(map[string]interface{})
	anthropic, _ := providers["anthropic"].(map[string]interface{})
	options, _ := anthropic["options"].(map[string]interface{})
	if options["baseURL"] != "http://127.0.0.1:5000" {
		t.Errorf("baseURL = %v, want proxy URL", options["baseURL"])
	}
	// User's timeout should still be there.
	if options["timeout"] == nil {
		t.Error("user's timeout option should be preserved during patch")
	}
	// User's custom model should still be there.
	models, _ := anthropic["models"].(map[string]interface{})
	if models["my-custom-model"] == nil {
		t.Error("user's custom model should be preserved during patch")
	}
	// Our models should be there.
	if models["claude-sonnet-4-6"] == nil {
		t.Error("claude-sonnet-4-6 should be injected")
	}

	// Restore.
	if err := c.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored := readJSON(t, c.Path())
	rProviders, _ := restored["provider"].(map[string]interface{})
	rAnthropic, _ := rProviders["anthropic"].(map[string]interface{})
	if rAnthropic == nil {
		t.Fatal("anthropic provider should still exist after restore (was present before)")
	}
	rOptions, _ := rAnthropic["options"].(map[string]interface{})
	// baseURL and apiKey should be removed (were absent before).
	if _, exists := rOptions["baseURL"]; exists {
		t.Error("baseURL should be removed after restore (was absent before)")
	}
	if _, exists := rOptions["apiKey"]; exists {
		t.Error("apiKey should be removed after restore (was absent before)")
	}
	// User's timeout should survive.
	if rOptions["timeout"] == nil {
		t.Error("user's timeout should survive restore")
	}
	// User's custom model should survive.
	rModels, _ := rAnthropic["models"].(map[string]interface{})
	if rModels["my-custom-model"] == nil {
		t.Error("user's custom model should survive restore")
	}
	// Our injected models should be removed.
	for _, m := range databricksModels {
		if rModels[m] != nil {
			t.Errorf("injected model %q should be removed after restore", m)
		}
	}
}
