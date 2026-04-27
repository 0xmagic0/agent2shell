package listener_test

import (
	"context"
	"net"
	"testing"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Task 3.1: Handler stdin wrapping ─────────────────────────────────────────

// TestHandlerRunRequest_StdinWrapsCommand verifies that when req.Stdin is set,
// the command sent to the session is heredoc-wrapped (contains "cat <<'A2S_STDIN_"
// and the original command after " | ").
func TestHandlerRunRequest_StdinWrapsCommand(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{
		Type:    types.RunRequest,
		Command: "bash",
		Stdin:   "id\nwhoami\n",
	}

	// The shell side must read the wrapped command and respond.
	// We capture what the shell receives to verify wrapping.
	var receivedCmd string
	errCh := make(chan error, 1)
	go func() {
		if !shell.scanner.Scan() {
			errCh <- shell.scanner.Err()
			return
		}
		receivedCmd = shell.scanner.Text()
		// Extract marker ID and write back a response.
		id := extractMarkerID(receivedCmd)
		if id == "" {
			errCh <- nil
			return
		}
		errCh <- shell.writeResponse(id, "root\nroot\n", 0)
	}()

	result, err := handler(ctx, req)
	require.NoError(t, err)
	require.NoError(t, <-errCh)

	// Verify the command sent to the shell was heredoc-wrapped.
	assert.Contains(t, receivedCmd, "cat <<'A2S_STDIN_",
		"command must be heredoc-wrapped when Stdin is set")
	assert.Contains(t, receivedCmd, "| bash",
		"wrapped command must pipe to original cmd")

	resp, ok := result.(*types.ExecResponse)
	require.True(t, ok)
	assert.Contains(t, resp.Output, "root")
}

// TestHandlerRunRequest_NoStdinCommandUnchanged verifies that when req.Stdin is
// empty, the command passed to the session is NOT wrapped.
func TestHandlerRunRequest_NoStdinCommandUnchanged(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{
		Type:    types.RunRequest,
		Command: "echo hello",
	}

	var receivedCmd string
	errCh := make(chan error, 1)
	go func() {
		if !shell.scanner.Scan() {
			errCh <- shell.scanner.Err()
			return
		}
		receivedCmd = shell.scanner.Text()
		id := extractMarkerID(receivedCmd)
		errCh <- shell.writeResponse(id, "hello", 0)
	}()

	result, err := handler(ctx, req)
	require.NoError(t, err)
	require.NoError(t, <-errCh)

	// No heredoc wrapping when Stdin is empty.
	assert.NotContains(t, receivedCmd, "cat <<'A2S_STDIN_",
		"command must NOT be wrapped when Stdin is empty")

	resp, ok := result.(*types.ExecResponse)
	require.True(t, ok)
	assert.Equal(t, "hello", resp.Output)
}

// TestStreamHandler_StdinWrapsCommand verifies that the stream handler also
// wraps the command when req.Stdin is non-empty.
func TestStreamHandler_StdinWrapsCommand(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	streamHandler := l.BuildStreamHandler()

	handlerConn, clientConn := net.Pipe()
	defer handlerConn.Close()
	defer clientConn.Close()

	var receivedCmd string
	errCh := make(chan error, 1)
	go func() {
		if !shell.scanner.Scan() {
			errCh <- shell.scanner.Err()
			return
		}
		receivedCmd = shell.scanner.Text()
		id := extractMarkerID(receivedCmd)
		errCh <- shell.writeResponse(id, "uid=0", 0)
	}()

	req := &types.Request{
		Type:    types.RunRequest,
		Command: "bash",
		Stdin:   "id\n",
		Stream:  true,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamHandler(context.Background(), req, handlerConn)
	}()

	// Drain frames until StreamEnd.
	for {
		var f types.StreamFrame
		if err := socket.ReadFrame(clientConn, &f); err != nil || f.Type == types.StreamEnd {
			break
		}
	}
	<-done

	require.NoError(t, <-errCh)
	assert.Contains(t, receivedCmd, "cat <<'A2S_STDIN_",
		"stream handler must heredoc-wrap command when Stdin is set")
	assert.Contains(t, receivedCmd, "| bash")
}

// TestStreamHandler_NoStdinCommandUnchanged verifies the stream handler does
// not wrap the command when req.Stdin is empty.
func TestStreamHandler_NoStdinCommandUnchanged(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	streamHandler := l.BuildStreamHandler()

	handlerConn, clientConn := net.Pipe()
	defer handlerConn.Close()
	defer clientConn.Close()

	var receivedCmd string
	errCh := make(chan error, 1)
	go func() {
		if !shell.scanner.Scan() {
			errCh <- shell.scanner.Err()
			return
		}
		receivedCmd = shell.scanner.Text()
		id := extractMarkerID(receivedCmd)
		errCh <- shell.writeResponse(id, "ok", 0)
	}()

	req := &types.Request{
		Type:    types.RunRequest,
		Command: "echo ok",
		Stream:  true,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamHandler(context.Background(), req, handlerConn)
	}()

	for {
		var f types.StreamFrame
		if err := socket.ReadFrame(clientConn, &f); err != nil || f.Type == types.StreamEnd {
			break
		}
	}
	<-done

	require.NoError(t, <-errCh)
	assert.NotContains(t, receivedCmd, "cat <<'A2S_STDIN_",
		"stream handler must NOT wrap command when Stdin is empty")
}
