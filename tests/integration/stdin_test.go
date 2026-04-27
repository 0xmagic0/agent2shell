// Package integration — stdin injection integration tests.
// These tests verify the full stdin path:
//
//	CLI (--stdin flag) → client.Run/StreamRun → IPC socket →
//	handler.buildHandler (WrapStdinCommand) → session.Exec/ExecStream → TCP.
package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// respondToStdinProbe reads the heredoc-wrapped command from the TCP connection,
// drains all heredoc body lines until the closing delimiter, then writes back a
// marker-delimited response.
//
// The heredoc-wrapped command sent to the shell is:
//
//	echo '---A2S-START-<id>---'; cat <<'A2S_STDIN_<hex>' | <cmd>
//	<stdin content lines>
//	A2S_STDIN_<hex>
//	; echo '---A2S-END-<id>---'$?
//
// We scan lines until we see the closing delimiter (line starting with
// "A2S_STDIN_"), then read the trailing "; echo ..." line, and finally write
// the response.
func (fs *fakeShell) respondToStdinProbe(output string, exitCode int) error {
	// Read the first line — contains the START echo and heredoc open line.
	if !fs.scanner.Scan() {
		if err := fs.scanner.Err(); err != nil {
			return fmt.Errorf("fakeShell stdin: scan first line: %w", err)
		}
		return fmt.Errorf("fakeShell stdin: connection closed on first line")
	}
	firstLine := fs.scanner.Text()

	// Extract the marker ID from the first line.
	id := extractMarkerID(firstLine)
	if id == "" {
		return fmt.Errorf("fakeShell stdin: could not extract marker ID from: %q", firstLine)
	}

	// Drain lines until we see the heredoc closing delimiter (starts with "A2S_STDIN_").
	for fs.scanner.Scan() {
		line := fs.scanner.Text()
		if strings.HasPrefix(line, "A2S_STDIN_") {
			// Closing delimiter found — break to consume trailing marker line.
			break
		}
	}
	if err := fs.scanner.Err(); err != nil {
		return fmt.Errorf("fakeShell stdin: scan heredoc body: %w", err)
	}

	// Read the "; echo '---A2S-END-<id>---'$?" line that follows the delimiter.
	if !fs.scanner.Scan() {
		if err := fs.scanner.Err(); err != nil {
			return fmt.Errorf("fakeShell stdin: scan end-marker line: %w", err)
		}
		return fmt.Errorf("fakeShell stdin: connection closed before end-marker line")
	}

	// Write back the marker-delimited response.
	return fs.writeResponse(id, output, exitCode)
}

// ─── S stdin.1 — Full buffered stdin path ──────────────────────────────────

// TestStdinFullBufferedPath verifies the complete buffered path with stdin:
//
//	client.Run (with stdin) → IPC → handler wraps heredoc → session.Exec →
//	TCP → fakeShell drains heredoc → writes back marker response →
//	ExecResponse.Output matches expected.
func TestStdinFullBufferedPath(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 5 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	fs.drainProbes(7)
	waitForSocket(t, sockPath)

	shellDone := make(chan error, 1)
	go func() {
		shellDone <- fs.respondToStdinProbe("root\nroot\n", 0)
	}()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	stdinContent := "id\nwhoami\n"
	resp, err := client.Run(ctx, sockPath, "bash", 5, stdinContent)

	require.NoError(t, <-shellDone, "fake shell stdin respond failed")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Contains(t, resp.Output, "root")
	assert.Equal(t, 0, resp.ExitCode)
}

// TestStdinExitCodeForwarding verifies that a non-zero exit code from the
// piped command is correctly forwarded in ExecResponse.ExitCode.
func TestStdinExitCodeForwarding(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 5 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	fs.drainProbes(7)
	waitForSocket(t, sockPath)

	shellDone := make(chan error, 1)
	go func() {
		shellDone <- fs.respondToStdinProbe("", 1)
	}()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	resp, err := client.Run(ctx, sockPath, "bash", 5, "exit 1\n")

	require.NoError(t, <-shellDone)
	// ExitCode 1 is returned in resp even when it's non-zero (non-zero exit is
	// not a transport error).
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.ExitCode, "exit code must be forwarded from piped command")
	_ = err // err may be nil or non-nil depending on whether server returns exec error
}

// ─── S stdin.2 — Streaming stdin path ──────────────────��──────────────────

// TestStdinStreamingPath verifies the full streaming path with stdin injection:
//
//	client.StreamRun (with stdin) → IPC → handler wraps heredoc →
//	session.ExecStream → TCP → fakeShell drains heredoc →
//	StreamLine frames received per line.
func TestStdinStreamingPath(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 5 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	fs.drainProbes(7)
	waitForSocket(t, sockPath)

	wantLines := []string{"line1", "line2", "line3"}
	shellOutput := strings.Join(wantLines, "\n")

	shellDone := make(chan error, 1)
	go func() {
		shellDone <- fs.respondToStdinProbe(shellOutput, 0)
	}()

	var gotLines []string
	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	stdinContent := "echo line1; echo line2; echo line3\n"
	resp, err := client.StreamRun(ctx, sockPath, "bash", 5, stdinContent, func(line string) {
		gotLines = append(gotLines, line)
	})

	require.NoError(t, <-shellDone, "fake shell stdin streaming respond failed")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, wantLines, gotLines, "onLine must be called per output line")
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, strings.Join(wantLines, "\n"), resp.Output)
}

// ─── S stdin.3 — Backward compatibility ───────────────────��──────────────

// TestStdinEmptyRequestUsesStandardPath verifies that when Request.Stdin is
// empty, the handler sends the command to the session WITHOUT heredoc wrapping.
// The standard fakeShell.respondToProbe (single-line read) must work correctly.
func TestStdinEmptyRequestUsesStandardPath(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 5 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	fs.drainProbes(7)
	waitForSocket(t, sockPath)

	shellDone := make(chan error, 1)
	go func() {
		shellDone <- fs.respondToProbe("uid=0", 0)
	}()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	// No stdin — standard path must still work.
	resp, err := client.Run(ctx, sockPath, "id", 5, "")

	require.NoError(t, <-shellDone)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "uid=0", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
}
