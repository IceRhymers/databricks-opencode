package main

import (
	"context"
	"testing"
)

// TestRunOpenCode_NotOnPath verifies RunOpenCode returns an error when opencode isn't on PATH.
func TestRunOpenCode_NotOnPath(t *testing.T) {
	// Save PATH and set to empty to ensure opencode isn't found.
	t.Setenv("PATH", "")

	_, err := RunOpenCode(context.Background(), []string{"--help"})
	if err == nil {
		t.Error("expected error when opencode binary not on PATH, got nil")
	}
}
