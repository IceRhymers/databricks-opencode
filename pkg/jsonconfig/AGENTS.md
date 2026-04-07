<!-- Parent: ../AGENTS.md -->

# pkg/jsonconfig

## Purpose

JSONC-aware OpenCode config manager. Reads, patches, and restores `~/.config/opencode/opencode.json`, supporting JSON with comments and trailing commas. Implements surgical patching (injects only necessary keys) and crash-safe restoration via sidecar file snapshots.

## Key Files

| File | Purpose |
|------|---------|
| `jsonconfig.go` | Config type and methods: readConfig (JSONC stripping), Patch (inject proxy settings), Restore (surgical removal), SaveOriginals/loadSidecar (crash recovery) |
| `jsonconfig_test.go` | Comprehensive tests: JSONC parsing, patch/restore, sentinel handling, key tracking |

## Core Concepts

### Managed Keys

The config manager tracks and patches these keys:
- `model` — active model identifier (set only if --model explicit or absent)
- `provider.anthropic.options.baseURL` — proxy address
- `provider.anthropic.options.apiKey` — placeholder key
- `provider.anthropic.models[claude-opus-4-6|claude-opus-4-5|claude-sonnet-4-6|claude-sonnet-4-5|claude-haiku-4-5]` — model entries

### Surgical Patching

- Snapshots original values before patching via `SaveOriginals()` (writes `.databricks-opencode-originals.json` sidecar)
- Distinguishes "key was absent" from "key had empty value" using sentinel marker
- On restore: only removes keys that were absent before; preserves user's existing keys
- Example: if user had `provider.anthropic.options.timeout: 30`, it's left alone; only injected keys removed

### JSONC Support

```go
// Strips comments and trailing commas before JSON parsing
clean := jsonc.ToJSON(data)
```

Allows users to write:
```jsonc
{
  // OpenCode configuration
  "model": "my-model",  // with trailing comma
  "provider": { ... }
}
```

### Crash Recovery

1. **Sentinel**: `WriteSentinel()` creates empty backup file `opencode.json.databricks-opencode-backup` on startup
2. **Sidecar**: `SaveOriginals()` writes `.databricks-opencode-originals.json` with original values
3. **Detection**: On next startup, presence of sidecar indicates crash
4. **Action**: Restore from sidecar or hand off to surviving session

**Sidecar schema** (JSON):
```json
{
  "model": "original-value",
  "model_absent": false,
  "anthropic_options_baseURL": "original-url",
  "anthropic_options_baseURL_absent": false,
  "anthropic_options_apiKey": "original-key",
  "anthropic_options_apiKey_absent": true,
  "existing_models": {"claude-sonnet-4-6": true}
}
```

### Atomic Writes

```go
// 1. Create temp file in same directory
tmp, _ := os.CreateTemp(dir, ".config-*.tmp")
// 2. Set restrictive permissions (0o600)
os.Chmod(tmpPath, 0o600)
// 3. Write data
tmp.Write(data)
tmp.Close()
// 4. Atomic rename
os.Rename(tmpPath, path)
```

Ensures config is never in a half-written state.

## Methods

### Config Lifecycle

| Method | Purpose |
|--------|---------|
| `New()` | Create Config for `~/.config/opencode/opencode.json` |
| `NewWithPath(configPath, backupPath)` | Create with explicit paths (testing) |
| `Path() / BackupPath() / SidecarPath()` | Get file paths |
| `HasBackup() / HasSidecar()` | Check for sentinel/sidecar files |

### Patching and Restoration

| Method | Purpose |
|--------|---------|
| `SaveOriginals()` | Snapshot managed keys and write sidecar |
| `Patch(proxyURL, modelName, apiKey, forceModel)` | Inject proxy config; set model only if forceModel or absent |
| `Restore()` | Surgically remove only our injected keys; restore originals |
| `UpdateProxyURL(proxyURL)` | Change baseURL only (multi-session handoff) |

### Sentinel Management

| Method | Purpose |
|--------|---------|
| `WriteSentinel()` | Create empty backup file for crash detection |
| `RemoveSentinel()` | Delete sentinel |
| `Backup()` | Alias to WriteSentinel (for backward compat) |

### Internal Helpers

| Method | Purpose |
|--------|---------|
| `readConfig()` | Read, JSONC-strip, parse JSON; return empty map if missing |
| `writeConfig(config)` | Marshal and atomically write config |
| `writeSidecar() / loadSidecar()` | Persist/load originals for crash recovery |
| `cleanup()` | Remove sidecar and sentinel files |
| `getMap(parent, key)` | Safe nested map extraction |
| `atomicWrite(path, data)` | Temp file + rename |

## For AI Agents

### Testing the Config Manager

```bash
cd /tmp/databricks-opencode
make test
```

Key test scenarios in `jsonconfig_test.go`:
- JSONC parsing (comments, trailing commas)
- Patch with forceModel true/false
- Restore: verify only injected keys removed
- Sentinel/sidecar creation and detection
- Crash recovery: restore from sidecar
- Multi-session: UpdateProxyURL handoff
- Empty config, missing keys, user config preservation

### Common Patterns

1. **Setting up a session**:
   ```go
   config := jsonconfig.New()
   config.SaveOriginals()
   config.WriteSentinel()
   config.Patch(proxyURL, modelName, apiKey, forceModel)
   ```

2. **Restoring after exit**:
   ```go
   config.Restore()  // Surgical remove + cleanup
   ```

3. **Handing off to another session**:
   ```go
   config.UpdateProxyURL(anotherProxyURL)
   // Leave sidecar intact for that session's eventual restore
   ```

### Important Notes

- **forceModel parameter**: if true, `model` is always written; if false, only set if absent (preserve user choice)
- **Absent sentinel**: the `absent` struct{} distinguishes "never had a value" from "had empty string"
- **JSONC limitations**: only strips comments and trailing commas; doesn't validate JSON5 features like unquoted keys or single quotes
- **Permissions**: sidecar and temp files written with 0o600 (user read/write only)
- **Crash recovery in config.go**: ConfigManager handles sidecar detection; jsonconfig only provides I/O primitives

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/tidwall/jsonc` | JSONC parsing (strip comments/trailing commas) |
| Go stdlib | `encoding/json`, `os`, `filepath` |
