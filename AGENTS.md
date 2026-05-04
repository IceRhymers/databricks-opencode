<!-- Parent: None (root) -->

# databricks-opencode

## Purpose

A lightweight Go proxy wrapper for OpenCode CLI that routes inference requests through Databricks AI Gateway with automatic OAuth token refresh. The binary patches OpenCode's config.json, starts a local token-refreshing proxy, and launches OpenCode as a child process—so every request to the AI Gateway has a fresh, valid Databricks OAuth token without manual token management.

**Architecture**: OpenCode → local proxy (OAuth injection + token refresh) → Databricks AI Gateway (/anthropic endpoint) → Anthropic

## Key Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point: parses flags, resolves profile/model, discovers gateway URL, starts proxy, patches config, runs opencode |
| `config.go` | ConfigManager: coordinates config.json patching, file locking, session registry, crash recovery |
| `token.go` | Token management: databricksFetcher implements tokencache.TokenFetcher via Databricks CLI; host discovery |
| `proxy.go` | ProxyConfig and proxy wrappers; wraps databricks-claude/pkg/proxy |
| `process.go` | RunOpenCode: executes opencode as a child process with args |
| `state.go` | loadState/saveState: persists profile and model selection to ~/.config/opencode/.state.json |
| `lock.go` | filelock wrapper: multi-session safe config access |
| `registry.go` | session registry wrapper: multi-session proxy handoff and crash recovery |
| `go.mod` | Dependencies: databricks-claude v0.5.0, tidwall/jsonc v0.3.2 |
| `Makefile` | Build targets: build, install, test, dist (cross-compile), clean, lint |
| `README.md` | User guide: quick start, flags, architecture, installation |
| `.github/workflows/ci.yml` | CI: runs tests, vet, build on pull requests to master |
| `pkg/jsonconfig/jsonconfig.go` | Config patching: JSONC-aware reader/writer, managed key snapshot/restore |
| `pkg/jsonconfig/jsonconfig_test.go` | Tests for jsonconfig |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `pkg/jsonconfig/` | JSONC-aware OpenCode config manager (JSONC parsing, surgical patch/restore) |
| `.github/workflows/` | CI pipeline configuration |

## For AI Agents

### Building and Testing

```bash
# Build the binary
make build

# Run tests
make test

# Cross-compile for all platforms
make dist

# Run linter
make lint

# Clean build artifacts
make clean
```

### Key Patterns and Concepts

1. **Profile and Model Resolution**
   - Resolution chain for profile: `--profile` flag → `DATABRICKS_CONFIG_PROFILE` env var → saved state → "DEFAULT"
   - Resolution chain for model: `--model` flag → saved state → "anthropic/claude-sonnet-4-6" default
   - When flags are explicit, values are saved to `~/.config/opencode/.state.json` for future sessions

2. **Token Management**
   - TokenProvider is a wrapper around `databricks-claude/pkg/tokencache`
   - Tokens fetched via `databricks auth token --profile <profile>` (10-second timeout)
   - Automatic refresh with 5-minute buffer before expiry (default 55-minute expiry if not provided)
   - Token parser handles both RFC3339 and Unix timestamp formats

3. **Host and Gateway URL Discovery**
   - Host discovered via `databricks auth env --profile <profile> --output json` → `DATABRICKS_HOST`
   - Gateway URL: `{host}/ai-gateway/anthropic`

4. **Config Patching and Restoration (JSONC-Aware)**
   - Snapshots original values of managed keys before patching (stored in `.databricks-opencode-originals.json` sidecar)
   - Patches `~/.config/opencode/opencode.json`: sets `provider.anthropic.options.baseURL` to local proxy, `apiKey` to placeholder, injects 5 model entries
   - On restore: surgically removes only injected keys, preserves user config
   - Supports JSONC (JSON with comments and trailing commas) via `tidwall/jsonc`

5. **Multi-Session Support and Crash Recovery**
   - Session registry at `~/.config/opencode/.sessions.json` tracks active proxies by PID
   - On startup: checks for sidecar/backup sentinels; if found, either hands off to surviving session or restores from crash
   - On exit: unregisters session; if others alive, hands off config to most recent; otherwise restores fully
   - File locking (`filelock.FileLock`) prevents concurrent config mutations

6. **Proxy Startup**
   - Proxy listens on `127.0.0.1:0` (random available port)
   - Port discovered from listener.Addr(), passed to config as `baseURL`
   - Supports HTTP (default) and HTTPS (with `--tls-cert` and `--tls-key`)
   - No OTEL upstream needed for OpenCode (inference-only)

7. **OpenCode Launch and Cleanup**
   - Patches config, starts proxy, launches opencode as child with user-provided args
   - **Critical**: restores config.json explicitly before `os.Exit()` (not deferred, since `os.Exit()` skips defers)
   - Crash recovery: sidecar file survives process death and guides restoration on next run

8. **Flag Parsing**
   - Databricks-opencode flags: `--profile`, `--model`, `--upstream`, `--log-file`, `--verbose`, `--version`, `--help`, `--print-env`, `--proxy-api-key`, `--tls-cert`, `--tls-key`
   - Separator: `--` passes remaining args to opencode (e.g., `databricks-opencode -- --chat`)
   - Flags not recognized as databricks-opencode flags are passed through to opencode

### Important Notes for Development

- **Token expiry conservative default**: 55 minutes (vs. typical 60) to build safety margin
- **Timeout on token fetch and host discovery**: 10 seconds; context-aware
- **Security warnings**: `proxy.SecurityChecks()` emitted to stderr on startup (e.g., binding to localhost only)
- **Logging**: disabled by default (io.Discard); `--verbose` sends logs to stderr; `--log-file` writes to file (combinable)
- **Config backup sentinel**: empty file at `~/.config/opencode/opencode.json.databricks-opencode-backup` for crash detection (no full copy)
- **Sidecar format**: JSON with boolean flags for "absent" markers (to distinguish "key was absent" from "key was empty string")
- **Atomic writes**: config changes use temp file + rename for crash safety

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/IceRhymers/databricks-claude` | v0.5.0 | Core packages: `pkg/proxy` (HTTP proxy with token injection), `pkg/tokencache` (token caching), `pkg/authcheck` (auth validation), `pkg/childproc` (child process helpers), `pkg/filelock` (file-based locking), `pkg/registry` (session registry) |
| `github.com/tidwall/jsonc` | v0.3.2 | JSONC parsing: strips comments and trailing commas from JSON5-like config |
| Go stdlib | 1.22+ | `net/http`, `os`, `log`, `context`, `json`, `filepath`, `io` |
