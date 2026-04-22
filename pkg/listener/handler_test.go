package listener_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockShell wraps the shell side of a net.Pipe() and responds to wrapped
// commands with start + output + end markers.
type mockShell struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

func newMockShell(conn net.Conn) *mockShell {
	return &mockShell{conn: conn, scanner: bufio.NewScanner(conn)}
}

// respondToProbe reads one wrapped command from the pipe and writes back a
// marker-delimited response. It extracts the marker ID from the wrapped command
// (format: "echo '---A2S-START-<id>---'; <cmd>; echo '---A2S-END-<id>---'$?")
// and uses it to construct the start and end markers.
func (m *mockShell) respondToProbe(output string, exitCode int) error {
	if !m.scanner.Scan() {
		return fmt.Errorf("mockShell: scan failed: %w", m.scanner.Err())
	}
	line := m.scanner.Text()
	id := extractMarkerID(line)
	if id == "" {
		return fmt.Errorf("mockShell: could not extract marker ID from: %q", line)
	}
	return m.writeResponse(id, output, exitCode)
}

// writeResponse writes the start marker, output, and end marker to the shell
// connection.
func (m *mockShell) writeResponse(id, output string, exitCode int) error {
	response := fmt.Sprintf("---A2S-START-%s---\n%s\n---A2S-END-%s---%d\n",
		id, output, id, exitCode)
	_, err := fmt.Fprint(m.conn, response)
	return err
}

// extractMarkerID extracts the marker ID from a wrapped command line.
// The wrapped command format is:
//
//	echo '---A2S-START-<id>---'; <cmd>; echo '---A2S-END-<id>---'$?
func extractMarkerID(wrapped string) string {
	const startPrefix = "echo '---A2S-START-"
	idx := strings.Index(wrapped, startPrefix)
	if idx < 0 {
		return ""
	}
	rest := wrapped[idx+len(startPrefix):]
	end := strings.Index(rest, "---'")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// newTestSession creates a session backed by a net.Pipe() connection (shell
// side is returned for test control). The session uses a short DefaultTimeout
// so probe timeouts resolve quickly in tests.
func newTestSession(t *testing.T) (*session.Session, *mockShell) {
	t.Helper()
	shellSide, sessionSide := net.Pipe()
	t.Cleanup(func() {
		shellSide.Close()
		sessionSide.Close()
	})

	sess, err := session.New(session.Config{
		Conn:           sessionSide,
		DefaultTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	return sess, newMockShell(shellSide)
}

// ─── Task 3.2 — Handler dispatch ────────────────────────────────────────────

func TestHandlerRunRequest(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)

	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{
		Type:    types.RunRequest,
		Command: "echo hello",
	}

	// Respond from the shell side in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- shell.respondToProbe("hello", 0)
	}()

	result, err := handler(ctx, req)
	require.NoError(t, err)
	require.NoError(t, <-errCh)

	resp, ok := result.(*types.ExecResponse)
	require.True(t, ok, "expected *types.ExecResponse, got %T", result)
	assert.Equal(t, "hello", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
}

func TestHandlerStatusRequest(t *testing.T) {
	sess, _ := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{Type: types.StatusRequest}

	result, err := handler(ctx, req)
	require.NoError(t, err)

	info, ok := result.(types.SessionInfo)
	require.True(t, ok, "expected types.SessionInfo, got %T", result)
	assert.NotZero(t, info.ConnectedAt)
}

func TestHandlerListRequest(t *testing.T) {
	sess, _ := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{Type: types.ListRequest}

	result, err := handler(ctx, req)
	require.NoError(t, err)

	resp, ok := result.(types.SessionsResponse)
	require.True(t, ok, "expected types.SessionsResponse, got %T", result)
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, sockPath, resp.Sessions[0].SocketPath)
}

func TestHandlerKillRequest(t *testing.T) {
	sess, _ := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{Type: types.KillRequest}

	// KillRequest responds synchronously with {"status":"ok"} and closes the
	// session asynchronously.
	result, err := handler(ctx, req)
	require.NoError(t, err)

	m, ok := result.(map[string]string)
	require.True(t, ok, "expected map[string]string, got %T", result)
	assert.Equal(t, "ok", m["status"])

	// Session must close asynchronously (not yet necessarily closed, but should
	// be within a short deadline).
	select {
	case <-sess.Done():
		// Good — session closed.
	case <-time.After(2 * time.Second):
		t.Fatal("session did not close within 2s after KillRequest")
	}
}

func TestHandlerUnknownType(t *testing.T) {
	sess, _ := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx := context.Background()
	req := &types.Request{Type: "bogus"}

	_, err := handler(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

// ─── Task 3.2 — Full round-trip via Unix socket ─────────────────────────────

// TestHandlerViaSocket sends a request through a real Unix socket server and
// verifies the dispatch reaches the session and returns the correct response.
func TestHandlerViaSocket(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	handler := l.BuildHandler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := socket.NewServer(sockPath, handler)
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.Serve(ctx) }()

	// Wait for the socket file to appear.
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			c.Close()
			return true
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "socket not ready")

	// Connect socket client.
	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	// Shell responds to the run request.
	go func() {
		shell.respondToProbe("world", 0) //nolint:errcheck // best-effort in test
	}()

	// Send RunRequest.
	req := types.Request{Type: types.RunRequest, Command: "echo world"}
	err = socket.WriteFrame(conn, req)
	require.NoError(t, err)

	var resp types.ExecResponse
	err = socket.ReadFrame(conn, &resp)
	require.NoError(t, err)
	assert.Equal(t, "world", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
}
