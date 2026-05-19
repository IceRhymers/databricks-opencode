package main

import "github.com/IceRhymers/databricks-claude/pkg/completion"

// flagDefs is the authoritative list of flags owned by databricks-opencode,
// derived from rootCommand.CompletionFlags() so the tree is the single
// source of truth. Anything not listed here is forwarded transparently to
// the opencode binary.
//
// To add a new flag: declare it as a cmd.FlagDef on rootCommand in
// commands.go (or as Persistent for inherited flags). flagDefs and
// knownFlags update automatically.
var flagDefs = func() []completion.FlagDef {
	return rootCommand.CompletionFlags()
}()

// knownFlags is the set of flag names (with "--" prefix) that databricks-opencode
// owns. Anything not in this set is forwarded to the opencode binary.
// Derived from rootCommand so it can never drift from the tree or the
// completion script.
var knownFlags = rootCommand.KnownFlags()
