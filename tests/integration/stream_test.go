// Package integration — streaming integration tests.
// These tests verify the full streaming path:
//
//	TCP fake shell → session.ExecStream → socket.StreamHandler →
//	Unix socket → client.StreamRun
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

// ─── S6.1 — Full streaming path ─────────────────────────────────────────────

// TestStreamFullPath verifies the complete streaming path:
//
//	Listener binds TCP → fake shell connects → detect probes answered →
//	client.StreamRun sends Request{Stream:true} → server streams StreamLine
//	frames → onLine called once per line in order.
func TestStreamFullPath(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 2 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	// Drain detect probes.
	fs.drainProbes(6)
	waitForSocket(t, sockPath)

	// Shell goroutine: respond to the streaming run request with 5 output lines.
	wantLines := []string{"line-1", "line-2", "line-3", "line-4", "line-5"}
	shellOutput := strings.Join(wantLines, "\n")

	shellDone := make(chan error, 1)
	go func() {
		shellDone <- fs.respondToProbe(shellOutput, 0)
	}()

	var gotLines []string
	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	resp, err := client.StreamRun(ctx, sockPath, "seq 1 5", 5, func(line string) {
		gotLines = append(gotLines, line)
	})

	require.NoError(t, <-shellDone, "fake shell respond failed")
	require.NoError(t, err, "StreamRun failed")
	require.NotNil(t, resp)

	assert.Equal(t, wantLines, gotLines, "onLine must be called once per output line in order")
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, strings.Join(wantLines, "\n"), resp.Output)
}

// ─── S6.2 — Streaming timeout mid-stream ────────────────────────────────────

// TestStreamTimeout_MidStream verifies that when a streaming command stalls
// (no end marker arrives before timeout), partial lines already received are
// preserved in the onLine calls, and the client receives a non-nil error.
func TestStreamTimeout_MidStream(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 500 * time.Millisecond, // short timeout for test speed
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	// Drain detect probes.
	fs.drainProbes(6)
	waitForSocket(t, sockPath)

	// Shell goroutine: send start marker + 2 partial lines, then stall.
	go func() {
		if !fs.scanner.Scan() {
			return
		}
		line := fs.scanner.Text()
		id := extractMarkerID(line)
		if id == "" {
			return
		}
		// Write start marker + 2 lines; no end marker — let timeout fire.
		payload := fmt.Sprintf("---A2S-START-%s---\npartial-1\npartial-2\n", id)
		_, _ = fmt.Fprint(fs.conn, payload)
		// Stall until the test finishes — the connection will be closed by cleanup.
		time.Sleep(10 * time.Second)
	}()

	var gotLines []string
	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	_, err := client.StreamRun(ctx, sockPath, "stall-cmd", 0, func(line string) {
		gotLines = append(gotLines, line)
	})

	require.Error(t, err, "StreamRun must return an error when the session times out")
	assert.Contains(t, err.Error(), "timeout", "error must mention timeout")

	// Partial lines already dispatched via onLine must be present.
	assert.Len(t, gotLines, 2, "partial onLine calls before timeout must be preserved")
	assert.Equal(t, []string{"partial-1", "partial-2"}, gotLines)
}

// ─── S6.3 — Backward compatibility: buffered Run unaffected ─────────────────

// TestBufferedRunUnchanged verifies that client.Run still works correctly
// after the streaming changes — non-streaming requests use the buffered path.
func TestBufferedRunUnchanged(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 2 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	fs.drainProbes(6)
	waitForSocket(t, sockPath)

	shellDone := make(chan error, 1)
	go func() {
		shellDone <- fs.respondToProbe("buffered output", 0)
	}()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer ctxCancel()

	resp, err := client.Run(ctx, sockPath, "echo buffered", 5)

	require.NoError(t, <-shellDone)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "buffered output", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
}
