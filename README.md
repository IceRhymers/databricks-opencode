# databricks-opencode

A Go binary that wraps the [OpenCode CLI](https://opencode.ai) with Databricks AI Gateway OAuth authentication. It patches `~/.config/opencode/config.json`, starts a local token-refreshing proxy, and launches OpenCode — so every request is authenticated through your Databricks workspace without any manual token management.

## Architecture

```
opencode -> local proxy (OAuth injection) -> Databricks AI Gateway (anthropic endpoint) -> Anthropic
```

The local proxy intercepts requests from OpenCode, injects a fresh Databricks OAuth token, and forwards them to the AI Gateway's `/anthropic` endpoint.

## Supported models

| Model | Identifier |
|-------|------------|
| Claude Opus 4.6 | `claude-opus-4-6` |
| Claude Opus 4.5 | `claude-opus-4-5` |
| Claude Sonnet 4.6 | `claude-sonnet-4-6` |
| Claude Sonnet 4.5 | `claude-sonnet-4-5` |
| Claude Haiku 4.5 | `claude-haiku-4-5` |

Default: `anthropic/claude-sonnet-4-6`

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
| `--profile` | Databricks CLI profile (saved for future sessions; default: env or "DEFAULT") |
| `--upstream` | Override the AI Gateway URL (default: auto-discovered) |
| `--model` | Databricks model (claude-opus-4-6, claude-sonnet-4-6, etc.; default: "anthropic/claude-sonnet-4-6") |
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
2. Discovers the workspace host and resolves the workspace ID via SCIM
3. Constructs the AI Gateway URL: `https://{workspaceId}.ai-gateway.cloud.databricks.com/anthropic`
4. Patches `~/.config/opencode/config.json` to configure the native `anthropic` provider pointing at a local proxy
5. Starts a local HTTP proxy that injects a fresh OAuth token on every request
6. Launches `opencode` as a child process
7. Restores `config.json` on exit (crash-safe via sidecar file)

The config patch injects into `provider.anthropic`:
- Sets `options.baseURL` to the local proxy address
- Sets `options.apiKey` to a placeholder
- Adds the 5 Databricks Claude model entries to `models`
- On restore, only the injected keys are removed (user config is preserved)

Multiple concurrent sessions are supported via a session registry — each proxy hands off config ownership cleanly on exit.

No shell alias needed — `databricks-opencode` is a standalone binary.

## License

Apache 2.0 — see [LICENSE](LICENSE).
