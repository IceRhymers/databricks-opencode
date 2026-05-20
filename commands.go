package main

import (
	"github.com/IceRhymers/databricks-opencode/internal/cmd"
)

// rootCommand is the source-of-truth declaration for the databricks-opencode
// CLI. It drives:
//   - parseArgs → knownFlags (the set of "--flag" names the binary owns;
//     anything else is forwarded transparently to the wrapped opencode binary).
//   - handleHelp → the help body (rendered from rootCommand.Long).
//   - completion <shell> → the bash/zsh/fish completion scripts (fed via
//     pkg/completion using rootCommand.CompletionFlags()).
//
// Adding a new root flag requires three edits:
//  1. Append a FlagDef to Flags (or Persistent for inherited flags) here.
//  2. Add a case to the switch in parseArgs (main.go) that wires the flag
//     into the Args struct.
//  3. Add the matching field to the Args struct.
//
// The parity tests in main_test.go (TestRootTreeFlagsAreParseRecognised,
// TestParseArgsCasesAreDeclaredInRootTree) fail loudly if step 1 and 2
// drift apart — the tree is the single source of truth.
//
// #82 introduced this tree and migrated the *root* command's flag set, plus
// added the `config show` subcommand. #83 added the `hooks` subcommand. #84
// adds the `serve` subcommand and removes the legacy --headless and
// --idle-timeout root flags.
var rootCommand = cmd.Command{
	Name:  "databricks-opencode",
	Short: "Databricks AI Gateway wrapper for OpenCode CLI",
	Long:  rootHelpTemplate,

	// Persistent flags are inherited by every subcommand once those
	// commands migrate onto the tree. For now, declaring them here is a
	// no-op for hooks/headless dispatch (which has its own scanner) but
	// ensures the tree is shaped correctly for the follow-up.
	Persistent: []cmd.FlagDef{
		{
			Name:        "profile",
			Description: "Databricks CLI profile (default: DEFAULT)",
			TakesArg:    true,
			Completer:   "__databricks_profiles",
			StateKey:    "profile",
			MDMKey:      "databricksProfile",
			Default:     "DEFAULT",
		},
		{
			Name:        "port",
			Description: "Proxy listen port (default: 49156)",
			TakesArg:    true,
			StateKey:    "port",
			Default:     "49156",
		},
	},

	// Order matches the legacy flagDefs slice so the bash/zsh/fish
	// completion output stays byte-identical with the pre-tree binary.
	// "profile" is now under Persistent (which renders first in AllFlags),
	// matching its position-1 spot in the legacy completion output.
	//
	// --print-env was removed in #82; its replacement lives at
	// `databricks-opencode config show`.
	Flags: []cmd.FlagDef{
		{Name: "verbose", Short: "v", Description: "Enable debug logging to stderr"},
		{Name: "version", Description: "Print version and exit"},
		{Name: "help", Short: "h", Description: "Show help message"},
		{Name: "model", Description: "Model to use (default: databricks-claude-opus-4-7)", TakesArg: true},
		{Name: "upstream", Description: "Override upstream opencode binary path", TakesArg: true, Completer: "__files"},
		{Name: "log-file", Description: "Write debug logs to file (combinable with --verbose)", TakesArg: true, Completer: "__files"},
		{Name: "proxy-api-key", Description: "Require this API key on all proxy requests", TakesArg: true},
		{Name: "tls-cert", Description: "TLS certificate file for the local proxy (requires --tls-key)", TakesArg: true, Completer: "__files"},
		{Name: "tls-key", Description: "TLS private key file for the local proxy (requires --tls-cert)", TakesArg: true, Completer: "__files"},
		{Name: "no-update-check", Description: "Skip the automatic update check on startup", EnvVar: "DATABRICKS_NO_UPDATE_CHECK"},
	},

	// Subcommands carry their own flags + Long help bodies so handleHelp
	// renders subcommand-aware help and completion scripts surface child
	// names. completion / update are leaf commands with their dispatch
	// still in main(); config has a `show` child whose runner replaces
	// the removed --print-env root flag.
	Subcommands: []cmd.Command{
		{Name: "completion", Short: "Generate shell completion scripts (bash, zsh, fish)"},
		{Name: "update", Short: "Check for a newer release and print upgrade instructions"},
		configCommand,
		hooksCommand,
		serveCommand,
	},
}

// configCommand declares the `config` subcommand tree. #82 introduces it
// with one child — `show` — that lifts the legacy --print-env diagnostic
// dump under a discoverable subcommand. Future sub-issues may grow this
// tree (e.g. config write) but the foundation PR is intentionally narrow.
//
// Tree shape:
//
//	config
//	└── show           (was --print-env)
//
// `show` re-declares --profile and --port locally (instead of relying on
// root Persistent inheritance) because subcommand parsing still uses each
// command's flat AllFlags slice — Persistent inheritance from the root is
// declared in the tree but not yet enforced at parse time.
var configCommand = cmd.Command{
	Name:  "config",
	Short: "Persistent config editor (show)",
	Long:  configHelpTemplate,
	Subcommands: []cmd.Command{
		{
			Name:  "show",
			Short: "Print resolved configuration (was --print-env)",
			Long:  configShowHelpTemplate,
			Flags: []cmd.FlagDef{
				{Name: "profile", Description: "Databricks CLI profile (default: state file > DEFAULT)", TakesArg: true, Completer: "__databricks_profiles", StateKey: "profile", MDMKey: "databricksProfile", Default: "DEFAULT"},
				{Name: "port", Description: "Proxy port for the displayed ANTHROPIC_BASE_URL", TakesArg: true, StateKey: "port", Default: "49156"},
				{Name: "help", Short: "h", Description: "Show help message"},
			},
		},
	},
}

// rootHelpTemplate is the verbatim help body rendered by handleHelp(). The
// "{{Version}}" placeholder is substituted by cmd.Render at print time.
//
// This template preserves byte-for-byte equivalence with the pre-tree help
// output, modulo the --print-env removal (replaced by `config show`) and
// the new `config` line under Subcommands.
const rootHelpTemplate = `databricks-opencode v{{Version}} — Databricks AI Gateway wrapper for OpenCode CLI

Patches the opencode config (opencode.json) and runs a local proxy so the OpenCode CLI
authenticates through a Databricks AI Gateway endpoint with live token refresh.

Usage:
  databricks-opencode [databricks-opencode flags] [opencode flags] [opencode args]

Databricks-OpenCode Flags:
  --profile string      Databricks CLI profile (saved for future sessions; default: env or "DEFAULT")
  --upstream string     Override the AI Gateway URL (default: auto-discovered)
  --model string        Model to use (default: "databricks-claude-opus-4-7")
  --verbose, -v         Enable debug logging to stderr
  --log-file string     Write debug logs to a file (combinable with --verbose)
  --proxy-api-key string    Require this API key on all proxy requests (default: disabled)
  --tls-cert string         Path to TLS certificate file (requires --tls-key)
  --tls-key string          Path to TLS private key file (requires --tls-cert)
  --port int                Local proxy port (default: 49156, saved for future sessions)
  --no-update-check            Skip the automatic update check on startup
  --version             Print version and exit
  --help, -h            Show this help message

Subcommands:
  completion <shell>           Generate shell completions (bash, zsh, fish)
  update                       Check for a newer release and print upgrade instructions
  config <subcommand>          Persistent config editor.
                                 config show                     Print resolved config
                                                                 (replaces the removed
                                                                 root diagnostic flag)
                               Run 'databricks-opencode config --help' for details.
  hooks <subcommand>           OpenCode plugin lifecycle.
                                 hooks install                   Install opencode plugin
                                 hooks uninstall                 Remove opencode plugin
                                 hooks session-start             Plugin-invoked internal
                                                                 (replaces removed root flag)
                               Run 'databricks-opencode hooks --help' for details.
  serve [flags]                Start the proxy without launching opencode
                               (for IDE extensions or hooks). Bare number on
                               the idle-timeout flag = minutes; 0 disables.
                               Run 'databricks-opencode serve --help' for details.

────────────────────────────────────────────────────────────────────────────────
OpenCode CLI Options:
`

const configHelpTemplate = `Usage: databricks-opencode config <subcommand> [flags]

Persistent config editor. Read-only diagnostics today; future sub-issues may
grow this tree to cover settings.json mutations. The legacy --print-env root
flag has been replaced by 'config show'.

Subcommands:
  show [flags]              Print resolved configuration (token redacted).
                            Read-only — no writes.

Run 'databricks-opencode config <subcommand> --help' for per-subcommand flags.

Examples:
  # Diagnostic dump:
  databricks-opencode config show

  # Override profile for the dump (does not persist):
  databricks-opencode config show --profile my-workspace

Exit codes:
  0   success
  1   discovery / auth failure
  2   missing or unknown subcommand
`

const configShowHelpTemplate = `Usage: databricks-opencode config show [flags]

Print the resolved configuration (token redacted) and exit. Read-only —
zero writes to opencode.json or the state file. Replaces the legacy
--print-env flag.

Resolves: profile, model, Databricks workspace host, AI Gateway URL,
ANTHROPIC_AUTH_TOKEN (redacted), and the upstream opencode binary path.

Flags:
  --profile string   Databricks CLI profile (default: state > DEFAULT)
  --port int         Port used to display the proxy URL (default: state > 49156)
  --help, -h         Show this help message

Example output:

  databricks-opencode configuration:
    Profile:           DEFAULT
    Model:             databricks-claude-opus-4-7
    DATABRICKS_HOST:   https://adb-...azuredatabricks.net
    ANTHROPIC_BASE_URL: https://adb-.../ai-gateway/anthropic
    Auth Token:         **** (redacted)
    OpenCode binary:    /usr/local/bin/opencode
`

// hooksCommand declares the `hooks` subcommand tree introduced in #83.
// Consolidates the 3 hooks-lifecycle root flags (--install-hooks,
// --uninstall-hooks, --headless-ensure) under a discoverable subcommand.
// install/uninstall manage the opencode plugin file at
// <opencode-config-dir>/plugins/databricks-proxy/index.js;
// session-start is the plugin-invoked refcount-free proxy lifecycle internal
// (formerly --headless-ensure).
//
// Tree shape:
//
//	hooks
//	├── install        [--profile P] [--port N]
//	├── uninstall
//	└── session-start  [--port N]   (plugin-invoked internal)
//
// Unlike databricks-claude, OpenCode has no SessionEnd hook event — the
// proxy shuts itself down on its idle timeout. So there is no
// `hooks session-end` counterpart.
//
// The hook-install logic in hooks.go (installHooks/uninstallHooks/
// headlessEnsure) is unchanged; this dispatcher is purely a surface reshape
// so the lifecycle is discoverable and the internal entrypoint stops
// polluting the user-facing root flag namespace. The plugin JS file emitted
// by installHooks is updated to invoke `hooks session-start` instead of
// `--headless-ensure` — users with stale plugins from before #83 must
// re-run `hooks install` to refresh the file.
var hooksCommand = cmd.Command{
	Name:  "hooks",
	Short: "OpenCode plugin lifecycle: install/uninstall + session-start internal",
	Long:  hooksHelpTemplate,
	Subcommands: []cmd.Command{
		{
			Name:  "install",
			Short: "Install the opencode plugin for automatic proxy lifecycle",
			Long:  hooksInstallHelpTemplate,
			Flags: []cmd.FlagDef{
				{Name: "profile", Description: "Databricks CLI profile to persist (default: DEFAULT)", TakesArg: true, Completer: "__databricks_profiles", StateKey: "profile", MDMKey: "databricksProfile", Default: "DEFAULT"},
				{Name: "port", Description: "Proxy listen port to persist (default: 49156)", TakesArg: true, StateKey: "port", Default: "49156"},
				{Name: "help", Short: "h", Description: "Show help message"},
			},
		},
		{
			Name:  "uninstall",
			Short: "Remove the databricks-opencode plugin from opencode",
			Long:  hooksUninstallHelpTemplate,
			Flags: []cmd.FlagDef{
				{Name: "help", Short: "h", Description: "Show help message"},
			},
		},
		{
			Name:  "session-start",
			Short: "Start proxy if not running (invoked by the opencode plugin — internal)",
			Long:  hooksSessionStartHelpTemplate,
			Flags: []cmd.FlagDef{
				{Name: "port", Description: "Proxy listen port (default: saved state > 49156)", TakesArg: true, StateKey: "port", Default: "49156"},
				{Name: "help", Short: "h", Description: "Show help message"},
			},
		},
	},
}

const hooksHelpTemplate = `Usage: databricks-opencode hooks <subcommand> [flags]

OpenCode plugin lifecycle. Installs an opencode plugin at
<opencode-config-dir>/plugins/databricks-proxy/index.js that spins the
local proxy up on session start — making 'databricks-opencode' auto-launch
with every opencode session without a long-lived daemon.

Subcommands:
  install        Write the opencode plugin and register it in
                 opencode.json. Idempotent — safe to re-run after
                 upgrades.
  uninstall      Remove the databricks-opencode plugin file and config
                 entry. Tolerates "not installed".
  session-start  Plugin-invoked internal: start the proxy if it isn't
                 already running. Called by the opencode plugin written
                 by 'hooks install'. Not intended to be invoked directly.

Run 'databricks-opencode hooks <subcommand> --help' for per-subcommand flags.

Examples:
  # First-time install on a developer machine:
  databricks-opencode hooks install

  # Remove plugin (e.g. when switching to a different proxy management mode):
  databricks-opencode hooks uninstall

Migration note (v0.8.0): the legacy root flags --install-hooks,
--uninstall-hooks, and --headless-ensure have been removed in favour of
this subcommand. Users with stale plugins from before v0.8.0 must re-run
'hooks install' so the plugin invokes 'hooks session-start' rather than
the removed '--headless-ensure' flag.

Exit codes:
  0   success
  1   write/discovery failure
  2   missing or unknown subcommand
`

const hooksInstallHelpTemplate = `Usage: databricks-opencode hooks install [flags]

Install the opencode plugin so every OpenCode session auto-starts the
local proxy on session init. Writes the plugin to:
  <opencode-config-dir>/plugins/databricks-proxy/index.js
and registers it in opencode.json. Idempotent — safe to re-run after
upgrades or after switching install methods (Homebrew ↔ go install).

Generated plugin invocation:
  $` + "`" + `<wrapper> hooks session-start` + "`" + `

Flags:
  --profile string   Databricks CLI profile to persist (default: DEFAULT)
  --port int         Proxy listen port to persist (default: 49156)
  --help, -h         Show this help message

Examples:
  # First-time install on a developer machine:
  databricks-opencode hooks install

  # Re-install after upgrade (idempotent):
  databricks-opencode hooks install
`

const hooksUninstallHelpTemplate = `Usage: databricks-opencode hooks uninstall

Remove the databricks-opencode plugin file and its entry from
opencode.json. Tolerates "not installed" — safe to run when no plugin is
present. Other plugins in your opencode plugins directory are untouched.

Flags:
  --help, -h   Show this help message
`

const hooksSessionStartHelpTemplate = `Usage: databricks-opencode hooks session-start [flags]

Plugin-invoked internal: start the local proxy if not already running.
Called by the opencode plugin file written by 'hooks install'. Not
intended to be invoked directly by end users.

Replaces the legacy --headless-ensure root flag.

Flags:
  --port int   Proxy listen port (default: saved state > 49156)
  --help, -h   Show this help message
`

// serveCommand declares the `serve` subcommand introduced in #84. It
// consolidates the legacy --headless and --idle-timeout root flags into a
// discoverable subcommand. Mirrors databricks-claude #174 with the same
// deliberately smaller scope as databricks-codex #89: no daemon mode and no
// install/uninstall/status — just the session-scoped proxy lifecycle the
// removed --headless flag drove.
//
// Tree shape:
//
//	serve   [--profile P] [--port N] [--upstream URL] [--model M]
//	        [--proxy-api-key K] [--tls-cert C] [--tls-key K]
//	        [--log-file F] [--verbose|-v] [--no-update-check]
//	        [--idle-timeout <dur>]
//
// Behavior contract: byte-identical to the removed `databricks-opencode
// --headless` flow. Boots the proxy, patches opencode.json, prints
// PROXY_URL=… on stdout, and blocks until /shutdown, idle timeout, or
// SIGINT/SIGTERM.
//
// --idle-timeout grammar: same default (30m) and same `0 = disabled` semantics
// as the removed root flag, PLUS the AC #84 "bare number = minutes" shape
// (`--idle-timeout 5` ≡ `--idle-timeout 5m`). The bare-number convenience is
// new — the pre-#84 root flag was strict (PR #76 rejected bare numbers). The
// shape is restored under `serve` because the issue spells it out as an AC.
var serveCommand = cmd.Command{
	Name:  "serve",
	Short: "Start the proxy without launching opencode (for IDE extensions or hooks)",
	Long:  serveHelpTemplate,
	Flags: []cmd.FlagDef{
		{Name: "profile", Description: "Databricks CLI profile (default: state file > DEFAULT)", TakesArg: true, Completer: "__databricks_profiles", StateKey: "profile", MDMKey: "databricksProfile", Default: "DEFAULT"},
		{Name: "port", Description: "Proxy listen port (default: state > 49156)", TakesArg: true, StateKey: "port", Default: "49156"},
		{Name: "upstream", Description: "Override the AI Gateway URL (default: auto-discovered)", TakesArg: true, Completer: "__files"},
		{Name: "model", Description: "Model to use (default: databricks-claude-opus-4-7)", TakesArg: true},
		{Name: "proxy-api-key", Description: "Require this API key on all proxy requests", TakesArg: true},
		{Name: "tls-cert", Description: "TLS certificate file for the local proxy (requires --tls-key)", TakesArg: true, Completer: "__files"},
		{Name: "tls-key", Description: "TLS private key file for the local proxy (requires --tls-cert)", TakesArg: true, Completer: "__files"},
		{Name: "log-file", Description: "Write debug logs to file (combinable with --verbose)", TakesArg: true, Completer: "__files"},
		{Name: "verbose", Short: "v", Description: "Enable debug logging to stderr"},
		{Name: "no-update-check", Description: "Skip the automatic update check on startup", EnvVar: "DATABRICKS_NO_UPDATE_CHECK"},
		{Name: "idle-timeout", Description: "Idle timeout (default 30m; 0 disables; bare number = minutes)", TakesArg: true},
		{Name: "help", Short: "h", Description: "Show help message"},
	},
}

const serveHelpTemplate = `Usage: databricks-opencode serve [flags]

Start the local Databricks proxy without launching opencode. Intended for
IDE extensions, the opencode plugin (see 'hooks session-start'), and any
host that wants to drive the proxy lifecycle externally.

Replaces the removed --headless and --idle-timeout root flags. Behavior is
byte-identical to the legacy 'databricks-opencode --headless' flow:
  - Discovers the workspace host and constructs the AI Gateway URL.
  - Binds 127.0.0.1:<port> (or joins an existing healthy proxy on that port).
  - Patches ~/.config/opencode/opencode.json to point at the local proxy.
  - Prints "PROXY_URL=<scheme>://127.0.0.1:<port>" on stdout.
  - Blocks until POST /shutdown, the idle timeout fires, or SIGINT/SIGTERM.

Flags:
  --profile string         Databricks CLI profile (default: state > DEFAULT)
  --port int               Proxy listen port (default: state > 49156)
  --upstream string        Override the AI Gateway URL (default: auto-discovered)
  --model string           Model to use (default: databricks-claude-opus-4-7)
  --proxy-api-key string   Require this API key on all proxy requests
  --tls-cert string        TLS certificate file (requires --tls-key)
  --tls-key string         TLS private key file (requires --tls-cert)
  --log-file string        Write debug logs to a file
  --verbose, -v            Enable debug logging to stderr
  --no-update-check        Skip the automatic update check on startup
  --idle-timeout duration  Idle timeout (default 30m; 0 disables; bare number = minutes)
  --help, -h               Show this help message

--idle-timeout examples:
  --idle-timeout 5m   five minutes
  --idle-timeout 5    five minutes (bare number = minutes)
  --idle-timeout 1h   one hour
  --idle-timeout 0    idle timeout disabled

Migration note (v0.8.0): the legacy root flags --headless and --idle-timeout
have been removed. Replace 'databricks-opencode --headless' with
'databricks-opencode serve' (idle-timeout follows as a serve flag).
`
