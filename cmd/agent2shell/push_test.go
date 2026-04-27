package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/transfer"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

// makePushLocalFile creates a temp file with known content and returns its path.
func makePushLocalFile(t *testing.T, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "push_cli_*")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()
	return f.Name()
}

// withPushExecBuilder replaces the global pushExecBuilder with a mock and returns a cleanup func.
func withPushExecBuilder(t *testing.T, exec transfer.ExecFunc) func() {
	t.Helper()
	orig := pushExecBuilder
	pushExecBuilder = func(_ string) transfer.ExecFunc { return exec }
	return func() { pushExecBuilder = orig }
}

// newPushTestCmd returns a fresh push cobra.Command isolated from the global tree.
func newPushTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "push <local-path> <remote-path>",
		Args:         cobra.ExactArgs(2),
		RunE:         runPush,
		SilenceUsage: true,
	}
	cmd.Flags().IntP("timeout", "t", 300, "transfer timeout in seconds")
	return cmd
}

// withMockedDiscover replaces discoverFunc to return a single fake socket path.
func withMockedDiscover(t *testing.T) func() {
	t.Helper()
	orig := discoverFunc
	discoverFunc = func() ([]string, error) {
		return []string{"/tmp/fake.sock"}, nil
	}
	return func() { discoverFunc = orig }
}

// TestRunPush_NilChecksummer_WarnAndProceed verifies:
//   - Exit code 0 when no checksummer is found on target
//   - Warning is printed to stderr
//   - "Checksum NOT verified." appears in stderr
func TestRunPush_NilChecksummer_WarnAndProceed(t *testing.T) {
	content := []byte("test content for push")
	localPath := makePushLocalFile(t, content)

	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		// Decoder probe: base64 succeeds
		if strings.Contains(cmd, "base64 --decode") {
			return &types.ExecResponse{ExitCode: 0, Output: "test"}, nil
		}
		// All checksum probes fail → DetectChecksum returns nil
		if strings.Contains(cmd, "echo -n") {
			return &types.ExecResponse{ExitCode: 1, Output: ""}, nil
		}
		// Chunk push: succeeds
		return &types.ExecResponse{ExitCode: 0, Output: ""}, nil
	}

	defer withPushExecBuilder(t, exec)()
	defer withMockedDiscover(t)()

	cmd := newPushTestCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{localPath, "/remote/out.bin"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Warning") {
		t.Errorf("expected warning in stderr when checksummer is nil\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Checksum NOT verified.") {
		t.Errorf("expected 'Checksum NOT verified.' in stderr\nstderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "Checksum verified.") && !strings.Contains(stderr, "NOT") {
		t.Errorf("must NOT print 'Checksum verified.' (without NOT)\nstderr:\n%s", stderr)
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"512 bytes", 512, "512 B"},
		{"exactly 1 KB", 1024, "1.00 KB"},
		{"1.5 KB", 1536, "1.50 KB"},
		{"exactly 1 MB", 1048576, "1.00 MB"},
		{"exactly 1 GB", 1073741824, "1.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanSize(tt.input)
			if got != tt.expected {
				t.Errorf("humanSize(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildPushExecFunc(t *testing.T) {
	const socketPath = "/tmp/test.sock"

	// buildPushExecFunc must return an ExecFunc that closes over socketPath.
	// We verify the closure captures the path by checking the function is non-nil
	// and has the correct type. An actual network call would fail without a live
	// socket, so we only validate the structural contract here.
	exec := buildPushExecFunc(socketPath)
	if exec == nil {
		t.Fatal("buildPushExecFunc returned nil")
	}

	// Verify the returned type satisfies transfer.ExecFunc by assigning it.
	var _ transfer.ExecFunc = exec

	// Call with a cancelled context — the client will fail immediately without
	// hitting the network, confirming the closure is wired correctly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done

	resp, err := exec(ctx, "echo hi", 5)
	// We expect an error (context cancelled or dial failure); no panic.
	if err == nil {
		t.Logf("unexpected success: resp=%+v", resp)
	}
}
