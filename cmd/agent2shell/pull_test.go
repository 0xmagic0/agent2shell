package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/transfer"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

// withPullExecFunc replaces the global pullExecFunc with a mock and returns cleanup.
func withPullExecFunc(t *testing.T, exec transfer.ExecFunc) func() {
	t.Helper()
	orig := pullExecBuilder
	pullExecBuilder = func(_ string) transfer.ExecFunc { return exec }
	return func() { pullExecBuilder = orig }
}

// newPullTestCmd returns a fresh pull cobra.Command isolated from the global tree.
func newPullTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "pull <remote-path> <local-path>",
		Args:         cobra.ExactArgs(2),
		RunE:         runPull,
		SilenceUsage: true,
	}
	cmd.Flags().IntP("timeout", "t", 300, "transfer timeout in seconds")
	return cmd
}

// TestRunPull_NoEncoder_ExitsNonZero verifies that when DetectEncoder finds no encoder,
// pull exits non-zero with an error message.
func TestRunPull_NoEncoder_ExitsNonZero(t *testing.T) {
	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		// All encoder probes fail
		if strings.Contains(cmd, "printf") {
			return &types.ExecResponse{ExitCode: 1, Output: ""}, nil
		}
		// Checksummer probes also fail
		return &types.ExecResponse{ExitCode: 1, Output: ""}, nil
	}

	defer withPullExecFunc(t, exec)()
	defer withMockedDiscover(t)()

	localPath := os.TempDir() + "/pull_no_encoder.txt"
	defer os.Remove(localPath)

	cmd := newPullTestCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"/remote/file.txt", localPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit when no encoder found, got nil")
	}
}

// TestRunPull_NilChecksummer_WarnAndProceed verifies:
//   - Exit code 0 when encoder found but no checksummer
//   - Warning printed to stderr
//   - "Checksum NOT verified." in stderr
func TestRunPull_NilChecksummer_WarnAndProceed(t *testing.T) {
	content := []byte("pull content without checksum")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := len(content)

	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		// Encoder probe: base64 succeeds with expected output
		if strings.Contains(cmd, "printf 'test'") && strings.Contains(cmd, "base64") {
			return &types.ExecResponse{ExitCode: 0, Output: "dGVzdA=="}, nil
		}
		// All checksum probes fail → DetectChecksum returns nil
		if strings.Contains(cmd, "echo -n") {
			return &types.ExecResponse{ExitCode: 1, Output: ""}, nil
		}
		// File size
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{ExitCode: 0, Output: string(rune('0'+size/10)) + "\n"}, nil
		}
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{ExitCode: 0, Output: "29\n"}, nil
		}
		// Actual wc -c
		if strings.Contains(cmd, "wc") {
			return &types.ExecResponse{ExitCode: 0, Output: "29\n"}, nil
		}
		// File transfer: base64 encode
		if strings.Contains(cmd, "cat") || strings.Contains(cmd, "base64") {
			return &types.ExecResponse{ExitCode: 0, Output: encoded}, nil
		}
		return &types.ExecResponse{ExitCode: 0, Output: ""}, nil
	}

	defer withPullExecFunc(t, exec)()
	defer withMockedDiscover(t)()

	localPath := os.TempDir() + "/pull_no_checksum.txt"
	defer os.Remove(localPath)

	cmd := newPullTestCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"/remote/file.txt", localPath})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected exit 0, got: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Warning") {
		t.Errorf("expected warning in stderr when checksummer is nil\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Checksum NOT verified.") {
		t.Errorf("expected 'Checksum NOT verified.' in stderr\nstderr:\n%s", stderr)
	}
}

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
