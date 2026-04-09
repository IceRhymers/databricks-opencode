# databricks-opencode

> **Disclaimer:** This is an unofficial, community-built workaround to enable Databricks OAuth SSO authentication with this AI coding tool. It is not supported, endorsed, or recognized by Databricks. Use at your own risk.


A Go binary that wraps the [OpenCode CLI](https://opencode.ai) with Databricks AI Gateway OAuth authentication. It patches `~/.config/opencode/opencode.json`, starts a local token-refreshing proxy, and launches OpenCode — so every request is authenticated through your Databricks workspace without any manual token management.

## Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [Databricks CLI](https://docs.databricks.com/dev-tools/cli/install.html) (`databricks` on PATH)
- [OpenCode CLI](https://opencode.ai) (`opencode` on PATH)
- A Databricks Model Serving endpoint with [AI Gateway](https://docs.databricks.com/aws/en/ai-gateway/) enabled (currently in public Beta)

## Installation

### Via Homebrew (recommended)

```bash
brew tap IceRhymers/tap
brew install databricks-opencode
```

### From source

```bash
go install github.com/IceRhymers/databricks-opencode@latest
```

Or build from source:

```bash
git clone https://github.com/IceRhymers/databricks-opencode.git
cd databricks-opencode
make build
```

## Quick start

1. Authenticate with Databricks (runs automatically if needed):
   ```
   databricks auth login
   ```

2. Run OpenCode through the Databricks proxy:
   ```
   databricks-opencode [opencode args]
   ```

## Flags

| Flag | Description |
|------|-------------|
| `--profile` | Databricks CLI profile (saved to state file; `--profile` flag writes it once; default: "DEFAULT") |
| `--upstream` | Override the AI Gateway URL (default: auto-discovered) |
| `--model` | Model to use (saved for future sessions; default: "databricks-claude-sonnet-4-6") |
| `--port` | Proxy listen port (saved for future sessions; default: 49156) |
| `--print-env` | Print resolved configuration and exit (token redacted) |
| `--verbose`, `-v` | Enable debug logging to stderr |
| `--log-file` | Write debug logs to a file (combinable with --verbose) |
| `--proxy-api-key` | Require this API key on all proxy requests (default: disabled) |
| `--tls-cert` | Path to TLS certificate file (requires --tls-key) |
| `--tls-key` | Path to TLS private key file (requires --tls-cert) |
| `--headless` | Start proxy without launching opencode (for IDE extensions or hooks) |
| `--idle-timeout` | Idle timeout for headless mode (default 30m; `0` disables; bare number = minutes) |
| `--install-hooks` | Install opencode plugin for automatic proxy lifecycle |
| `--uninstall-hooks` | Remove databricks-opencode plugin from opencode |
| `--version` | Print version and exit |
| `--help`, `-h` | Show help message |

## How it works

1. Authenticates with Databricks using the CLI profile
2. Discovers the workspace host and constructs the AI Gateway URL
3. Binds a local proxy on `127.0.0.1:49156` (fixed port — first session owns it, others join)
4. Writes `~/.config/opencode/opencode.json` once to point at the proxy (idempotent — no restore on exit)
5. Starts refreshing Databricks OAuth tokens on every proxied request
6. Launches `opencode` as a child process
7. Tracks concurrent sessions with a ref-count; the last session out closes the listener

No shell alias needed — `databricks-opencode` is a standalone binary.

## Session Hooks (automatic proxy lifecycle)

Install hooks so every OpenCode session auto-starts the proxy on startup — no manual `--headless` needed.

> **First-time setup:** Run `databricks-opencode` at least once before installing hooks. This writes `~/.config/opencode/opencode.json` so the proxy is used for all OpenCode sessions.

### Install

```bash
databricks-opencode --install-hooks
```

This writes an OpenCode plugin to `~/.config/opencode/plugins/databricks-proxy/index.js` that runs `databricks-opencode --headless-ensure` at session startup.

### Shutdown

Unlike Claude Code, OpenCode does not have a session-end hook event. The proxy shuts itself down automatically after **30 minutes of inactivity** (configurable via `--idle-timeout`). You can also stop it manually with `POST /shutdown` or by sending a signal to the process.

### Uninstall

```bash
databricks-opencode --uninstall-hooks
```

Removes only the databricks-opencode plugin file. Other plugins in your opencode plugins directory are untouched.

### Notes

- Safe to rerun `--install-hooks` after upgrades — the plugin file is overwritten, not duplicated.
- Custom port settings persist automatically via the state file (`~/.config/opencode/.databricks-opencode.json`).

## Shell Tab Completions

`databricks-opencode` can generate shell completion scripts for bash, zsh, and fish. Completions are derived from the binary's own flag metadata and stay in sync automatically.

### Install (one-time)

**bash** — add to `~/.bashrc`:
```bash
eval "$(databricks-opencode completion bash)"
```

**zsh** — add to `~/.zshrc`:
```zsh
eval "$(databricks-opencode completion zsh)"
```

**fish** — add to `~/.config/fish/config.fish`:
```fish
databricks-opencode completion fish | source
```

### Homebrew

If installed via `brew install IceRhymers/tap/databricks-opencode`, completions are installed automatically — no extra setup needed.

### What completes

- `--profile <TAB>` — lists profiles from `~/.databrickscfg` (updated live)
- `--upstream`, `--log-file`, `--tls-cert`, `--tls-key <TAB>` — file path completion
- All other flags — name completion when you type `-`

## License

MIT — see [LICENSE](LICENSE).

