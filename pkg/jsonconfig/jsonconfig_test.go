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
	return NewWithPath(filepath.Join(dir, "opencode.json"))
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

	if err := c.Patch("http://127.0.0.1:9000", "gpt-5-4", "databricks-proxy", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())

	// Check model field — should be set when absent.
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
	options, _ := dbProxy["options"].(map[string]interface{})
	if options == nil {
		t.Fatal("databricks-proxy options not found")
	}
	if options["baseURL"] != "http://127.0.0.1:9000/v1" {
		t.Errorf("options.baseURL = %v, want %q", options["baseURL"], "http://127.0.0.1:9000/v1")
	}
	if options["apiKey"] != "databricks-proxy" {
		t.Errorf("options.apiKey = %v, want %q", options["apiKey"], "databricks-proxy")
	}
	if dbProxy["npm"] != "@ai-sdk/anthropic" {
		t.Errorf("npm = %v, want %q", dbProxy["npm"], "@ai-sdk/anthropic")
	}
	models, _ := dbProxy["models"].(map[string]interface{})
	if models == nil {
		t.Fatal("databricks-proxy models not found")
	}
	for _, m := range []string{"databricks-claude-opus-4-6", "databricks-claude-sonnet-4-6", "databricks-claude-haiku-4-5"} {
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

	if err := c.Patch("http://127.0.0.1:8080", "claude-4", "db-key", false); err != nil {
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

func TestPatchPreservesExistingModel(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with user-configured model.
	existing := `{"model": "openai/gpt-4o", "theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Patch without forceModel — should preserve existing model.
	if err := c.Patch("http://127.0.0.1:9000", "gpt-5-4", "key", false); err != nil {
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
	if err := c.Patch("http://127.0.0.1:9000", "gpt-5-4", "key", true); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())
	if m["model"] != "databricks-proxy/gpt-5-4" {
		t.Errorf("model = %v, want %q (forceModel should override)", m["model"], "databricks-proxy/gpt-5-4")
	}
}

func TestPatchSetsModelWhenAbsent(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with no model key.
	existing := `{"theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := c.Patch("http://127.0.0.1:9000", "gpt-5-4", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	m := readJSON(t, c.Path())
	if m["model"] != "databricks-proxy/gpt-5-4" {
		t.Errorf("model = %v, want %q (should set when absent)", m["model"], "databricks-proxy/gpt-5-4")
	}
}

func TestUpdateProxyURL(t *testing.T) {
	c := setupTestConfig(t)

	// Patch first.
	if err := c.Patch("http://127.0.0.1:5000", "model-a", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Update proxy URL.
	if err := c.UpdateProxyURL("http://127.0.0.1:6000"); err != nil {
		t.Fatalf("UpdateProxyURL: %v", err)
	}

	m := readJSON(t, c.Path())
	providers, _ := m["provider"].(map[string]interface{})
	dbProxy, _ := providers["databricks-proxy"].(map[string]interface{})
	options, _ := dbProxy["options"].(map[string]interface{})
	if options == nil {
		t.Fatal("databricks-proxy options not found after UpdateProxyURL")
	}
	if options["baseURL"] != "http://127.0.0.1:6000" {
		t.Errorf("options.baseURL = %v, want %q", options["baseURL"], "http://127.0.0.1:6000")
	}

	// Model should be unchanged.
	if m["model"] != "databricks-proxy/model-a" {
		t.Errorf("model changed unexpectedly: %v", m["model"])
	}
}

func TestNeedsConfig_NoFile(t *testing.T) {
	c := setupTestConfig(t)
	if !c.NeedsConfig("http://127.0.0.1:49156") {
		t.Error("NeedsConfig should return true when config file does not exist")
	}
}

func TestNeedsConfig_AlreadyConfigured(t *testing.T) {
	c := setupTestConfig(t)

	// Patch the config first.
	if err := c.Patch("http://127.0.0.1:49156", "model-a", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Same proxyURL — should return false (no-op).
	if c.NeedsConfig("http://127.0.0.1:49156") {
		t.Error("NeedsConfig should return false when baseURL already matches")
	}
}

func TestNeedsConfig_DifferentURL(t *testing.T) {
	c := setupTestConfig(t)

	// Patch with one URL.
	if err := c.Patch("http://127.0.0.1:49156", "model-a", "key", false); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// Different proxyURL — should return true.
	if !c.NeedsConfig("http://127.0.0.1:50000") {
		t.Error("NeedsConfig should return true when baseURL differs")
	}
}

func TestNeedsConfig_MissingProvider(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with no provider section.
	existing := `{"theme": "dark"}`
	if err := os.WriteFile(c.Path(), []byte(existing), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !c.NeedsConfig("http://127.0.0.1:49156") {
		t.Error("NeedsConfig should return true when provider section is missing")
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
	if err := c.Patch("http://127.0.0.1:7000", "model-b", "key", false); err != nil {
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

func TestAddPlugin_EmptyConfig(t *testing.T) {
	c := setupTestConfig(t)

	if err := c.AddPlugin("/path/to/plugin"); err != nil {
		t.Fatalf("AddPlugin: %v", err)
	}

	m := readJSON(t, c.Path())
	plugins, _ := m["plugin"].([]interface{})
	if len(plugins) != 1 || plugins[0] != "/path/to/plugin" {
		t.Errorf("plugin = %v, want [\"/path/to/plugin\"]", plugins)
	}
}

func TestAddPlugin_Idempotent(t *testing.T) {
	c := setupTestConfig(t)

	c.AddPlugin("/path/to/plugin")
	c.AddPlugin("/path/to/plugin")

	m := readJSON(t, c.Path())
	plugins, _ := m["plugin"].([]interface{})
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin entry after double add, got %d", len(plugins))
	}
}

func TestAddPlugin_PreservesExisting(t *testing.T) {
	c := setupTestConfig(t)

	// Write config with an existing plugin.
	initial := map[string]interface{}{
		"plugin": []interface{}{"existing-plugin"},
		"model":  "some-model",
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	os.WriteFile(c.Path(), data, 0o600)

	if err := c.AddPlugin("/path/to/new"); err != nil {
		t.Fatalf("AddPlugin: %v", err)
	}

	m := readJSON(t, c.Path())
	plugins, _ := m["plugin"].([]interface{})
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d: %v", len(plugins), plugins)
	}
	if plugins[0] != "existing-plugin" {
		t.Errorf("plugins[0] = %v, want %q", plugins[0], "existing-plugin")
	}
	if m["model"] != "some-model" {
		t.Error("model key was clobbered")
	}
}

func TestRemovePlugin(t *testing.T) {
	c := setupTestConfig(t)

	c.AddPlugin("/path/to/plugin")
	if err := c.RemovePlugin("/path/to/plugin"); err != nil {
		t.Fatalf("RemovePlugin: %v", err)
	}

	m := readJSON(t, c.Path())
	if _, exists := m["plugin"]; exists {
		t.Error("expected plugin key to be removed when array is empty")
	}
}

func TestRemovePlugin_PreservesOthers(t *testing.T) {
	c := setupTestConfig(t)

	c.AddPlugin("keep-this")
	c.AddPlugin("remove-this")

	if err := c.RemovePlugin("remove-this"); err != nil {
		t.Fatalf("RemovePlugin: %v", err)
	}

	m := readJSON(t, c.Path())
	plugins, _ := m["plugin"].([]interface{})
	if len(plugins) != 1 || plugins[0] != "keep-this" {
		t.Errorf("plugin = %v, want [\"keep-this\"]", plugins)
	}
}

func TestRemovePlugin_NoFile(t *testing.T) {
	c := setupTestConfig(t)

	// Should not error on missing file.
	if err := c.RemovePlugin("/nonexistent"); err != nil {
		t.Fatalf("RemovePlugin on missing file should return nil, got: %v", err)
	}
}
