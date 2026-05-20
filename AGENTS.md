<!-- Parent: None (root) -->

# databricks-opencode

## Purpose

A lightweight Go proxy wrapper for OpenCode CLI that routes inference requests through Databricks AI Gateway with automatic OAuth token refresh. The binary patches OpenCode's config.json, starts a local token-refreshing proxy, and launches OpenCode as a child process—so every request to the AI Gateway has a fresh, valid Databricks OAuth token without manual token management.

**Architecture**: OpenCode → local proxy (OAuth injection + token refresh) → Databricks AI Gateway (/anthropic endpoint) → Anthropic

## Key Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point: parses flags, resolves profile/model, discovers gateway URL, starts proxy, patches config, runs opencode. Hosts `runOpencode` (lifted in #84) so the `serve` dispatcher reuses the same body in headless mode. |
| `commands.go` | Command-tree registry (rootCommand + config/hooks/serve subcommands) — single source of truth for flag set, help text, and shell completion |
| `serve_cmd.go` | `serve` subcommand dispatcher (#84) — replaces removed `--headless` / `--idle-timeout` root flags; implements bare-number-is-minutes idle-timeout grammar |
| `hooks_cmd.go` | `hooks` subcommand dispatcher (#83) — install/uninstall opencode plugin + session-start internal |
| `config_cmd.go` | `config` subcommand dispatcher (#82) — `config show` replaces removed `--print-env` |
| `config.go` | EnsureConfig: idempotent patch of config.json (no backup, no restore — config persists pointing at the fixed proxy port) |
| `token.go` | Token management: databricksFetcher implements tokencache.TokenFetcher via Databricks CLI; host discovery |
| `proxy.go` | ProxyConfig and proxy wrappers; wraps databricks-claude/pkg/proxy |
| `process.go` | RunOpenCode: executes opencode as a child process with args |
| `state.go` | loadState/saveState: persists profile and model selection to ~/.config/opencode/.state.json |
| `lock.go` | filelock wrapper: multi-session safe config access |
| `registry.go` | session registry wrapper: multi-session proxy handoff |
| `go.mod` | Dependencies: databricks-claude v0.5.0, tidwall/jsonc v0.3.2 |
| `Makefile` | Build targets: build, install, test, dist (cross-compile), clean, lint |
| `README.md` | User guide: quick start, flags, architecture, installation |
| `.github/workflows/ci.yml` | CI: runs tests, vet, build on pull requests to master |
| `pkg/jsonconfig/jsonconfig.go` | Config patching: JSONC-aware reader/writer, surgical Patch/NeedsConfig/UpdateProxyURL/AddPlugin/RemovePlugin |
| `pkg/jsonconfig/jsonconfig_test.go` | Tests for jsonconfig |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `pkg/jsonconfig/` | JSONC-aware OpenCode config manager (JSONC parsing, surgical patch — no restore) |
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

4. **Config Patching (JSONC-Aware, Patch-and-Leave-It)**
   - Patches `~/.config/opencode/opencode.json`: injects `provider.databricks-proxy` with the local proxy `baseURL`, placeholder `apiKey`, and the registered Databricks Claude model entries
   - Idempotent: `NeedsConfig` short-circuits when the existing file already points at the same proxy URL
   - Surgical: only the managed keys are touched — user keys (`commands`, `agents`, other providers, `theme`, etc.) are preserved
   - The config persists pointing at the fixed local port; **there is no backup, no restore, and no crash-recovery sidecar**. Subsequent runs simply re-validate via `NeedsConfig` and re-patch only if needed.
   - Supports JSONC (JSON with comments and trailing commas) via `tidwall/jsonc`

5. **Multi-Session Support**
   - Session registry at `~/.config/opencode/.sessions.json` tracks active proxies by PID
   - File locking (`filelock.FileLock`) prevents concurrent config mutations
   - On startup with another live session: hand off via `UpdateProxyURL` rather than re-patching the full provider block

6. **Proxy Startup**
   - Proxy listens on `127.0.0.1:0` (random available port)
   - Port discovered from listener.Addr(), passed to config as `baseURL`
   - Supports HTTP (default) and HTTPS (with `--tls-cert` and `--tls-key`)
   - No OTEL upstream needed for OpenCode (inference-only)

7. **OpenCode Launch**
   - Patches config (idempotently), starts proxy, launches opencode as child with user-provided args
   - On exit, the patched config is left in place — opencode's persistent config keeps pointing at the local proxy URL across runs, and the next run re-patches only if `NeedsConfig` reports drift

8. **Flag Parsing**
   - Databricks-opencode root flags: `--profile`, `--model`, `--upstream`, `--log-file`, `--verbose`, `--version`, `--help`, `--proxy-api-key`, `--tls-cert`, `--tls-key`, `--port`, `--no-update-check`
   - Subcommands: `completion <shell>`, `update`, `config show`, `hooks {install|uninstall|session-start}`, `serve`
   - Separator: `--` passes remaining args to opencode (e.g., `databricks-opencode -- --chat`)
   - Flags not recognized as databricks-opencode flags are passed through to opencode
   - Removed in #82/#83/#84: `--print-env` (→ `config show`), `--install-hooks` / `--uninstall-hooks` / `--headless-ensure` (→ `hooks` subcommand), `--headless` / `--idle-timeout` (→ `serve` subcommand)

### Important Notes for Development

- **Token expiry conservative default**: 55 minutes (vs. typical 60) to build safety margin
- **Timeout on token fetch and host discovery**: 10 seconds; context-aware
- **Security warnings**: `proxy.SecurityChecks()` emitted to stderr on startup (e.g., binding to localhost only)
- **Logging**: disabled by default (io.Discard); `--verbose` sends logs to stderr; `--log-file` writes to file (combinable)
- **Atomic writes**: config changes use temp file + rename for crash safety
- **No restore on exit**: the patched config persists pointing at the fixed local proxy port across runs; `NeedsConfig` gates idempotent re-patching on subsequent startups

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/IceRhymers/databricks-claude` | v0.5.0 | Core packages: `pkg/proxy` (HTTP proxy with token injection), `pkg/tokencache` (token caching), `pkg/authcheck` (auth validation), `pkg/childproc` (child process helpers), `pkg/filelock` (file-based locking), `pkg/registry` (session registry) |
| `github.com/tidwall/jsonc` | v0.3.2 | JSONC parsing: strips comments and trailing commas from JSON5-like config |
| Go stdlib | 1.22+ | `net/http`, `os`, `log`, `context`, `json`, `filepath`, `io` |
