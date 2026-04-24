package client_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForUnixSocket polls until the given Unix socket path exists as a socket
// file, up to 2 seconds. It uses os.Stat rather than dialing so it does not
// consume the server's accept slot.
func waitForUnixSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unix socket %s did not appear within 2s", path)
}

// setupStreamServer starts a Unix socket server that handles a single
// connection: it reads one Request frame, then writes the provided frames
// (StreamFrame slice) sequentially.
func setupStreamServer(t *testing.T, frames []types.StreamFrame) string {
	t.Helper()

	sockPath := testutil.TempSocket(t)

	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)

	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the incoming Request frame (we don't inspect it).
		var req types.Request
		if err := socket.ReadFrame(conn, &req); err != nil {
			return
		}

		// Write all provided frames.
		for _, f := range frames {
			if err := socket.WriteFrame(conn, f); err != nil {
				return
			}
		}
	}()

	// Wait until the socket is ready.
	waitForUnixSocket(t, sockPath)

	return sockPath
}

// ─── Task 4.1: StreamRun unit tests ─────────────────────────────────────────

// TestStreamRun_HappyPath verifies that 3 StreamLine frames then 1 StreamEnd
// result in 3 onLine calls in order, and the returned ExecResponse.Output
// equals the lines joined by newline.
func TestStreamRun_HappyPath(t *testing.T) {
	frames := []types.StreamFrame{
		{Type: types.StreamLine, Data: "line one"},
		{Type: types.StreamLine, Data: "line two"},
		{Type: types.StreamLine, Data: "line three"},
		{Type: types.StreamEnd, ExitCode: 0, DurationMS: 42},
	}

	sockPath := setupStreamServer(t, frames)

	var got []string
	resp, err := client.StreamRun(context.Background(), sockPath, "echo", 5, func(line string) {
		got = append(got, line)
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, []string{"line one", "line two", "line three"}, got)
	assert.Equal(t, "line one\nline two\nline three", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, int64(42), resp.DurationMS)
}

// TestStreamRun_EmptyOutput verifies that a StreamEnd with no preceding
// StreamLine frames returns an empty-output ExecResponse and zero onLine calls.
func TestStreamRun_EmptyOutput(t *testing.T) {
	frames := []types.StreamFrame{
		{Type: types.StreamEnd, ExitCode: 0, DurationMS: 5},
	}

	sockPath := setupStreamServer(t, frames)

	callCount := 0
	resp, err := client.StreamRun(context.Background(), sockPath, "true", 5, func(string) {
		callCount++
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "", resp.Output)
	assert.Equal(t, 0, callCount, "onLine must not be called when output is empty")
}

// TestStreamRun_UnknownFrameType verifies that an unrecognized frame type
// causes StreamRun to return a wrapped error describing the unexpected type.
func TestStreamRun_UnknownFrameType(t *testing.T) {
	frames := []types.StreamFrame{
		{Type: types.StreamLine, Data: "first"},
		{Type: "bogus", Data: "unexpected"},
	}

	sockPath := setupStreamServer(t, frames)

	var gotLines []string
	resp, err := client.StreamRun(context.Background(), sockPath, "cmd", 5, func(line string) {
		gotLines = append(gotLines, line)
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
	assert.Nil(t, resp)

	// The first onLine call (for "first") must NOT be replayed.
	assert.Len(t, gotLines, 1, "prior onLine calls must not be replayed on error")
}

// TestStreamRun_ConnectionDropMidStream verifies that a connection drop
// returns a non-nil error and that prior onLine calls are not replayed.
func TestStreamRun_ConnectionDropMidStream(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var req types.Request
		if err := socket.ReadFrame(conn, &req); err != nil {
			return
		}

		// Write 2 StreamLine frames then close — simulate mid-stream drop.
		_ = socket.WriteFrame(conn, types.StreamFrame{Type: types.StreamLine, Data: "a"})
		_ = socket.WriteFrame(conn, types.StreamFrame{Type: types.StreamLine, Data: "b"})
		// conn.Close() via defer — no StreamEnd
	}()

	waitForUnixSocket(t, sockPath)

	var gotLines []string
	resp, err := client.StreamRun(context.Background(), sockPath, "cmd", 5, func(line string) {
		gotLines = append(gotLines, line)
	})

	require.Error(t, err, "connection drop must return a non-nil error")
	assert.Nil(t, resp, "partial response must not be returned on connection drop")

	// Prior onLine calls must not be replayed.
	assert.Len(t, gotLines, 2, "exactly 2 prior onLine calls must have been dispatched")
}

// TestStreamRun_ServerError verifies that a StreamEnd frame with a non-empty
// Error field causes StreamRun to return a non-nil error containing the
// error string.
func TestStreamRun_ServerError(t *testing.T) {
	frames := []types.StreamFrame{
		{Type: types.StreamLine, Data: "partial"},
		{Type: types.StreamEnd, ExitCode: 0, DurationMS: 100, Error: "exec timeout"},
	}

	sockPath := setupStreamServer(t, frames)

	var got []string
	resp, err := client.StreamRun(context.Background(), sockPath, "sleep", 5, func(line string) {
		got = append(got, line)
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec timeout")
	assert.Nil(t, resp)
	assert.Len(t, got, 1)
}

// TestStreamRun_SendsStreamTrueRequest verifies that StreamRun sends a Request
// with Stream == true. We verify by reading the request from the server side.
func TestStreamRun_SendsStreamTrueRequest(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	reqCh := make(chan types.Request, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var req types.Request
		if err := socket.ReadFrame(conn, &req); err != nil {
			return
		}
		reqCh <- req

		// Write a minimal StreamEnd so StreamRun can return.
		_ = socket.WriteFrame(conn, types.StreamFrame{Type: types.StreamEnd, ExitCode: 0})
	}()

	waitForUnixSocket(t, sockPath)

	_, _ = client.StreamRun(context.Background(), sockPath, "id", 5, func(string) {})

	req := <-reqCh
	assert.True(t, req.Stream, "StreamRun must send Request.Stream == true")
	assert.Equal(t, types.RunRequest, req.Type)
	assert.Equal(t, "id", req.Command)
}
