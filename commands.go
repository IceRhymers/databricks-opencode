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
// #82 introduces this tree, migrates the *root* command's flag set, and adds
// the `config show` subcommand (replacing the removed --print-env root flag).
// hooks/serve flag migrations land in #83/#84. --profile and --port are
// declared as Persistent so subcommand inheritance works out of the box once
// those migrations land.
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
		{Name: "headless", Description: "Start proxy without launching opencode (for IDE extensions or hooks)"},
		{Name: "idle-timeout", Description: "Idle timeout for headless mode (default 30m; 0 disables; bare number = minutes)", TakesArg: true},
		{Name: "install-hooks", Description: "Install opencode plugin for automatic proxy lifecycle"},
		{Name: "uninstall-hooks", Description: "Remove databricks-opencode plugin from opencode"},
		{Name: "headless-ensure", Description: "Ensure headless proxy is running (called by opencode plugin)"},
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
  --headless            Start proxy without launching opencode (for IDE extensions)
  --idle-timeout duration   Idle timeout for headless mode (default 30m, 0 disables; use e.g. 30s, 5m, 1h)
  --install-hooks       Install opencode plugin hooks for automatic proxy lifecycle
  --uninstall-hooks     Remove databricks-opencode plugin from opencode config
  --headless-ensure     Start proxy if not running (called by opencode plugin at init)
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
