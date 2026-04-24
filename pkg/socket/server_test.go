package socket_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialAndExchange connects to the Unix socket, sends a Request frame, reads
// the raw response bytes, and returns them. The connection is closed after
// reading.
func dialAndExchange(t *testing.T, sockPath string, req types.Request) []byte {
	t.Helper()

	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, socket.WriteFrame(conn, req))

	// Read the response frame length header + payload.
	var header [4]byte
	_, err = conn.Read(header[:])
	require.NoError(t, err)

	length := uint32(header[0])<<24 | uint32(header[1])<<16 | uint32(header[2])<<8 | uint32(header[3])
	payload := make([]byte, length)

	total := 0
	for total < int(length) {
		n, err := conn.Read(payload[total:])
		require.NoError(t, err)
		total += n
	}

	return payload
}

// waitForSocket polls until path exists AND is a Unix domain socket, up to 2
// seconds. This prevents a race in TestServer_RemovesStaleSocket where a stale
// regular file satisfies a simple existence check before the server replaces it.
func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil {
			if info.Mode()&os.ModeSocket != 0 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear within 2s", path)
}

// S4.4 — Server accept→handle→respond: handler returns fixed ExecResponse.
func TestServer_AcceptHandleRespond(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	want := types.ExecResponse{
		Output:     "root\n",
		ExitCode:   0,
		DurationMS: 42,
	}

	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return want, nil
	}

	srv := socket.NewServer(sockPath, socket.Handler(handler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	waitForSocket(t, sockPath)

	payload := dialAndExchange(t, sockPath, types.Request{
		Type:    types.RunRequest,
		Command: "id",
		Timeout: 10,
	})

	var got types.ExecResponse
	require.NoError(t, json.Unmarshal(payload, &got))

	assert.Equal(t, want.Output, got.Output)
	assert.Equal(t, want.ExitCode, got.ExitCode)
	assert.Equal(t, want.DurationMS, got.DurationMS)
}

// S4.5 — Server removes stale socket file before binding.
func TestServer_RemovesStaleSocket(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	// Create a stale file at the socket path.
	require.NoError(t, os.WriteFile(sockPath, []byte("stale"), 0600))

	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.ExecResponse{}, nil
	}

	srv := socket.NewServer(sockPath, socket.Handler(handler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	// Server must bind successfully (stale file replaced).
	waitForSocket(t, sockPath)

	// Verify the path is now a socket, not a regular file.
	info, err := os.Stat(sockPath)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode().Type(),
		"expected socket, got %v", info.Mode().Type())
}

// S4.6 — Socket file has permissions 0600 after server starts.
func TestServer_SocketPermissions(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.ExecResponse{}, nil
	}

	srv := socket.NewServer(sockPath, socket.Handler(handler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { srv.Serve(ctx) }() //nolint:errcheck

	waitForSocket(t, sockPath)

	info, err := os.Stat(sockPath)
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(),
		"expected 0600, got %v", info.Mode().Perm())
}

// ─── S1.3: StreamHandler dispatch ───────────────────────────────────────────

// TestServer_StreamHandlerDispatch verifies that when a request has Stream ==
// true and a StreamHandler is registered, the StreamHandler is called instead
// of the regular Handler.
func TestServer_StreamHandlerDispatch(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	regularCalled := false
	regularHandler := func(_ context.Context, _ *types.Request) (any, error) {
		regularCalled = true
		return types.ExecResponse{Output: "buffered"}, nil
	}

	streamHandlerCalled := make(chan struct{}, 1)
	streamHandler := socket.StreamHandler(func(_ context.Context, req *types.Request, conn net.Conn) {
		streamHandlerCalled <- struct{}{}
		// Write a StreamEnd frame to satisfy the client.
		frame := types.StreamFrame{Type: types.StreamEnd, ExitCode: 0, DurationMS: 1}
		_ = socket.WriteFrame(conn, frame)
	})

	srv := socket.NewServer(sockPath, socket.Handler(regularHandler))
	srv.SetStreamHandler(streamHandler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { srv.Serve(ctx) }() //nolint:errcheck

	waitForSocket(t, sockPath)

	// Send a streaming request.
	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	req := types.Request{Type: types.RunRequest, Command: "id", Stream: true}
	require.NoError(t, socket.WriteFrame(conn, req))

	// Read the StreamEnd frame back.
	var frame types.StreamFrame
	require.NoError(t, socket.ReadFrame(conn, &frame))
	assert.Equal(t, types.StreamEnd, frame.Type)

	// StreamHandler must have been called; regular handler must not.
	select {
	case <-streamHandlerCalled:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("StreamHandler was not called within 2s")
	}
	assert.False(t, regularCalled, "regular handler must not be called for streaming request")
}

// TestServer_NonStreamingRequestUsesRegularHandler verifies that a request
// with Stream == false still goes through the regular Handler.
func TestServer_NonStreamingRequestUsesRegularHandler(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	regularCalled := make(chan struct{}, 1)
	regularHandler := func(_ context.Context, _ *types.Request) (any, error) {
		regularCalled <- struct{}{}
		return types.ExecResponse{Output: "buffered"}, nil
	}

	streamHandlerCalled := false
	streamHandler := socket.StreamHandler(func(_ context.Context, _ *types.Request, _ net.Conn) {
		streamHandlerCalled = true
	})

	srv := socket.NewServer(sockPath, socket.Handler(regularHandler))
	srv.SetStreamHandler(streamHandler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { srv.Serve(ctx) }() //nolint:errcheck

	waitForSocket(t, sockPath)

	payload := dialAndExchange(t, sockPath, types.Request{
		Type:    types.RunRequest,
		Command: "id",
	})

	var got types.ExecResponse
	require.NoError(t, json.Unmarshal(payload, &got))
	assert.Equal(t, "buffered", got.Output)

	select {
	case <-regularCalled:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("regular handler was not called within 2s")
	}
	assert.False(t, streamHandlerCalled, "stream handler must not be called for non-streaming request")
}

// S4.7 — Graceful shutdown: cancelling context stops the server without blocking.
func TestServer_GracefulShutdown(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.ExecResponse{}, nil
	}

	srv := socket.NewServer(sockPath, socket.Handler(handler))

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	waitForSocket(t, sockPath)

	cancel()

	select {
	case err := <-errCh:
		// Serve must return — nil or a context-related error are both fine.
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop within 2s after context cancellation")
	}
}
