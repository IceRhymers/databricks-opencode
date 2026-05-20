package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/IceRhymers/databricks-opencode/internal/cmd"
)

// runServeCommand implements `databricks-opencode serve ...`. args is
// everything after the literal "serve" token. Replaces the removed
// --headless / --idle-timeout root flags with a discoverable subcommand
// (issue #84). Mirrors databricks-claude #174 with the same deliberately
// smaller scope as databricks-codex #89: no daemon mode and no
// install/uninstall/status — `serve` is the session-scoped lifecycle the
// removed --headless flag drove, nothing more.
//
// The dispatcher parses `serve`-local flags via the tree, synthesises an
// *Args with Headless=true plus the resolved IdleTimeout, and then forwards
// to runOpencode — which is the lifted post-parseArgs body of main(). That
// shared call site is what backs the AC "byte-identical behaviour to
// `databricks-opencode --headless`": both legacy --headless and the new
// `serve` codepath converge on the same proxy startup, EnsureConfig patch,
// and lifecycle wrapper, so the hooks-invoked plugin path (`hooks
// session-start` → `headless.Ensure` → spawned `<wrapper> serve`) is
// indistinguishable from the pre-#84 behaviour.
func runServeCommand(args []string) {
	r, _ := serveCommand.Parse(args)
	if r.Bools["help"] {
		_ = cmd.Render(os.Stdout, serveCommand, nil)
		os.Exit(0)
	}

	// Reject sub-subcommand-like positionals so a typo like `serve foo`
	// doesn't silently boot the proxy. install/uninstall/status are
	// DEFERRED for opencode (per issue #84) — surfacing them as "unknown"
	// here keeps the error story honest and matches the hooks dispatcher's
	// convention.
	for _, p := range r.Positional {
		fmt.Fprintf(os.Stderr, "databricks-opencode: serve: unknown argument %q\n\n", p)
		_ = cmd.Render(os.Stderr, serveCommand, nil)
		os.Exit(1)
	}

	idle, err := parseServeIdleTimeout(r.Strings["idle-timeout"], r.Set["idle-timeout"])
	if err != nil {
		fmt.Fprintf(os.Stderr, "databricks-opencode: serve: %v\n", err)
		os.Exit(1)
	}

	port := 0
	if v := r.Strings["port"]; v != "" {
		port, _ = strconv.Atoi(v)
	}

	a := &Args{
		Verbose:       r.Bools["verbose"],
		Model:         r.Strings["model"],
		Upstream:      r.Strings["upstream"],
		LogFile:       r.Strings["log-file"],
		Profile:       r.Strings["profile"],
		ProxyAPIKey:   r.Strings["proxy-api-key"],
		TLSCert:       r.Strings["tls-cert"],
		TLSKey:        r.Strings["tls-key"],
		Port:          port,
		Headless:      true,
		IdleTimeout:   idle,
		NoUpdateCheck: r.Bools["no-update-check"],
	}
	runOpencode(a)
}

// defaultServeIdleTimeout is the default idle timeout when `serve` is invoked
// without --idle-timeout. Matches the pre-#84 root-flag default so the
// migration is byte-equivalent. Exposed as a package var so tests can pin
// the constant without re-hardcoding it in expectations.
var defaultServeIdleTimeout = 30 * time.Minute

// parseServeIdleTimeout parses the --idle-timeout value with the AC #84
// "bare number = minutes" grammar layered on top of time.ParseDuration:
//
//   - empty / unset → defaultServeIdleTimeout (30m)
//   - bare integer (e.g. "5", "30", "0") → N minutes
//   - "0" (any duration form parsing to zero) → idle timeout disabled
//   - any time.Duration string ("30s", "5m", "1h") → that duration
//   - anything else → error
//
// The bare-number convenience deliberately diverges from the pre-#84 strict
// root-flag parser (PR #76 rejected bare numbers because the root flag's
// resolution chain made the ambiguity actively dangerous: a user typing
// `--idle-timeout 30` got a silent reinterpretation that didn't match other
// duration flags in the wrapper). Under `serve`, the issue spells it out as
// an AC and there is no overloaded resolution chain to mask the intent —
// `serve --idle-timeout 5` unambiguously means "five minutes."
func parseServeIdleTimeout(raw string, present bool) (time.Duration, error) {
	if !present || raw == "" {
		return defaultServeIdleTimeout, nil
	}
	// Bare integer: interpret as minutes. AC #84.
	if n, err := strconv.Atoi(raw); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("--idle-timeout: %q is negative; use 0 to disable or a positive value", raw)
		}
		return time.Duration(n) * time.Minute, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("--idle-timeout: %q is not a valid duration (use e.g. 30s, 5m, 1h, or a bare number for minutes)", raw)
	}
	if d < 0 {
		return 0, fmt.Errorf("--idle-timeout: %q is negative; use 0 to disable or a positive value", raw)
	}
	return d, nil
}
