package main

import (
	"context"
	"os/exec"

	"github.com/IceRhymers/databricks-claude/pkg/childproc"
)

// RunOpenCode starts the opencode CLI as a child process with the supplied arguments
// and waits for it to exit. Environment variables (OPENAI_BASE_URL,
// OPENAI_API_KEY) are expected to be set on os.Environ by main.go
// before calling this function.
func RunOpenCode(ctx context.Context, args []string) (int, error) {
	return childproc.Run(ctx, childproc.Config{
		BinaryName: "opencode",
		Args:       args,
	})
}

// ForwardSignals sets up SIGINT/SIGTERM forwarding from the parent to cmd's
// process. The returned cancel function stops the forwarding goroutine.
func ForwardSignals(cmd *exec.Cmd) (cancel func()) {
	return childproc.ForwardSignals(cmd)
}
