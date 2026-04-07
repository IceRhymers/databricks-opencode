# databricks-opencode

A Go binary that wraps the [OpenCode CLI](https://opencode.ai) with Databricks AI Gateway OAuth authentication. It patches `~/.config/opencode/config.json`, starts a local token-refreshing proxy, and launches OpenCode — so every request is authenticated through your Databricks workspace without any manual token management.

## Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [Databricks CLI](https://docs.databricks.com/dev-tools/cli/install.html) (`databricks` on PATH)
- [OpenCode CLI](https://opencode.ai) (`opencode` on PATH)

## Installation

```
go install github.com/IceRhymers/databricks-opencode@latest
```

Or build from source:

```
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
| `--port` | Proxy listen port (saved for future sessions; default: 49155) |
| `--print-env` | Print resolved configuration and exit (token redacted) |
| `--verbose`, `-v` | Enable debug logging to stderr |
| `--log-file` | Write debug logs to a file (combinable with --verbose) |
| `--proxy-api-key` | Require this API key on all proxy requests (default: disabled) |
| `--tls-cert` | Path to TLS certificate file (requires --tls-key) |
| `--tls-key` | Path to TLS private key file (requires --tls-cert) |
| `--version` | Print version and exit |
| `--help`, `-h` | Show help message |

## How it works

1. Authenticates with Databricks using the CLI profile
2. Discovers the workspace host and constructs the AI Gateway URL
3. Binds a local proxy on `127.0.0.1:49155` (fixed port — first session owns it, others join)
4. Writes `~/.config/opencode/config.json` once to point at the proxy (idempotent — no restore on exit)
5. Starts refreshing Databricks OAuth tokens on every proxied request
6. Launches `opencode` as a child process
7. Tracks concurrent sessions with a ref-count; the last session out closes the listener

No shell alias needed — `databricks-opencode` is a standalone binary.

## License

Apache 2.0 — see [LICENSE](LICENSE).
