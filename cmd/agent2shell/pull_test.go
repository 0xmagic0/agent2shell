package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestPullCmd_Use(t *testing.T) {
	if pullCmd.Use != "pull <remote-path> <local-path>" {
		t.Errorf("pullCmd.Use = %q, want %q", pullCmd.Use, "pull <remote-path> <local-path>")
	}
}

func TestPullCmd_ArgsExactly2(t *testing.T) {
	// Cobra stores Args as a validator; verify it rejects wrong counts.
	err := pullCmd.Args(pullCmd, []string{"only-one"})
	if err == nil {
		t.Error("expected error for 1 argument, got nil")
	}

	err = pullCmd.Args(pullCmd, []string{"a", "b", "c"})
	if err == nil {
		t.Error("expected error for 3 arguments, got nil")
	}

	err = pullCmd.Args(pullCmd, []string{"remote", "local"})
	if err != nil {
		t.Errorf("expected no error for 2 arguments, got: %v", err)
	}
}

func TestPullCmd_TimeoutFlag(t *testing.T) {
	f := pullCmd.Flags().Lookup("timeout")
	if f == nil {
		t.Fatal("timeout flag not registered on pullCmd")
	}
	if f.DefValue != "300" {
		t.Errorf("timeout default = %q, want %q", f.DefValue, "300")
	}
	if f.Shorthand != "t" {
		t.Errorf("timeout shorthand = %q, want %q", f.Shorthand, "t")
	}
}

func TestPullCmd_RunEIsSet(t *testing.T) {
	if pullCmd.RunE == nil {
		t.Error("pullCmd.RunE must not be nil")
	}
}

func TestPullCmd_RegisteredOnRoot(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub == pullCmd {
			return
		}
	}
	t.Error("pullCmd not registered on rootCmd")
}

// TestHumanSize_Pull verifies humanSize — defined once in the package and
// shared between push and pull. If push.go defines it this test still passes;
// if pull.go defines it, it also passes.
func TestHumanSize_Pull(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1536, "1.50 KB"},
		{int64(2.5 * 1024 * 1024), "2.50 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := humanSize(tt.input)
			if got != tt.want {
				t.Errorf("humanSize(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestPullCmd_Short verifies the Short description is non-empty and sensible.
func TestPullCmd_Short(t *testing.T) {
	if pullCmd.Short == "" {
		t.Error("pullCmd.Short must not be empty")
	}
}

// Compile-time check: runPull has the right signature for cobra.Command.RunE.
var _ func(*cobra.Command, []string) error = runPull
