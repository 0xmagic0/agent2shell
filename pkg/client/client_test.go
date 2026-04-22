package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMockServer starts a socket.Server on a temp Unix socket path with the
// given handler. It blocks until the socket is ready to accept connections.
// Cleanup cancels the server context and removes the socket file.
func setupMockServer(t *testing.T, handler socket.Handler) string {
	t.Helper()

	sockPath := testutil.TempSocket(t)
	srv := socket.NewServer(sockPath, handler)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		srv.Serve(ctx) //nolint:errcheck
	}()

	// Poll until a connection succeeds — server is ready.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", sockPath, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return sockPath
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("mock server at %s did not become ready within 2s", sockPath)
	return ""
}

// --------------------------------------------------------------------------
// do() + checkError() — Task 3.1
// --------------------------------------------------------------------------

func TestDo_Success(t *testing.T) {
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return map[string]string{"output": "hello"}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	resp, err := client.Run(ctx, sockPath, "echo hello", 5)
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Output)
}

func TestDo_ConnectionRefused(t *testing.T) {
	// Use a path that does not exist — no server running.
	sockPath := testutil.TempSocket(t)

	ctx := context.Background()
	_, err := client.Run(ctx, sockPath, "id", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client:")
}

func TestDo_ContextDeadlineExceeded(t *testing.T) {
	// Use a path that does not exist with an already-expired context.
	sockPath := testutil.TempSocket(t)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	_, err := client.Run(ctx, sockPath, "id", 5)
	require.Error(t, err)
}

func TestCheckError_ErrorField(t *testing.T) {
	// Handler returns a Go error; server writes {"error":"..."}.
	// Run should surface this as a non-nil error.
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return nil, assert.AnError
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	_, err := client.Run(ctx, sockPath, "id", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client:")
}

func TestCheckError_StatusOK(t *testing.T) {
	handler := func(_ context.Context, req *types.Request) (any, error) {
		return types.ExecResponse{Output: "hello", ExitCode: 0}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	resp, err := client.Run(ctx, sockPath, "echo hello", 5)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.ExitCode)
}

func TestCheckError_OutputNoError(t *testing.T) {
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.ExecResponse{Output: "hello", ExitCode: 0}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	resp, err := client.Run(ctx, sockPath, "echo hello", 5)
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Output)
}

func TestCheckError_EmptyErrorField(t *testing.T) {
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.ExecResponse{Output: "ok", ExitCode: 0, Error: ""}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	resp, err := client.Run(ctx, sockPath, "true", 5)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Output)
}

// --------------------------------------------------------------------------
// Run — Task 3.2
// --------------------------------------------------------------------------

func TestRun_Success(t *testing.T) {
	want := types.ExecResponse{Output: "uid=0", ExitCode: 0, DurationMS: 10}
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return want, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	got, err := client.Run(ctx, sockPath, "id", 5)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.Output, got.Output)
	assert.Equal(t, want.ExitCode, got.ExitCode)
	assert.Equal(t, want.DurationMS, got.DurationMS)
}

func TestRun_RemoteError(t *testing.T) {
	// Exec-level error: response returned AND non-empty Error field.
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.ExecResponse{Error: "exec timeout"}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	resp, err := client.Run(ctx, sockPath, "sleep 100", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec timeout")
	// Response must be non-nil so the caller can inspect partial output.
	require.NotNil(t, resp)
}

func TestRun_ServerError(t *testing.T) {
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return nil, assert.AnError
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	_, err := client.Run(ctx, sockPath, "id", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client:")
}

// --------------------------------------------------------------------------
// Status — Task 3.3
// --------------------------------------------------------------------------

func TestStatus_Success(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	want := types.SessionInfo{
		RemoteAddr:  "127.0.0.1:4444",
		Shell:       "/bin/bash",
		User:        "root",
		Hostname:    "target",
		ConnectedAt: now,
	}
	handler := func(_ context.Context, req *types.Request) (any, error) {
		assert.Equal(t, types.StatusRequest, req.Type)
		return want, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	got, err := client.Status(ctx, sockPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.RemoteAddr, got.RemoteAddr)
	assert.Equal(t, want.Shell, got.Shell)
	assert.Equal(t, want.User, got.User)
	assert.Equal(t, want.Hostname, got.Hostname)
}

func TestStatus_ConnectionRefused(t *testing.T) {
	sockPath := testutil.TempSocket(t)

	ctx := context.Background()
	_, err := client.Status(ctx, sockPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client:")
}

// --------------------------------------------------------------------------
// List — Task 3.4
// --------------------------------------------------------------------------

func TestList_WithEntries(t *testing.T) {
	want := types.SessionsResponse{
		Sessions: []types.SessionEntry{
			{
				SessionInfo: types.SessionInfo{
					RemoteAddr: "10.0.0.1:4444",
					User:       "root",
				},
				SocketPath: "/tmp/a.sock",
			},
			{
				SessionInfo: types.SessionInfo{
					RemoteAddr: "10.0.0.2:4444",
					User:       "www-data",
				},
				SocketPath: "/tmp/b.sock",
			},
		},
	}
	handler := func(_ context.Context, req *types.Request) (any, error) {
		assert.Equal(t, types.ListRequest, req.Type)
		return want, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	got, err := client.List(ctx, sockPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Sessions, 2)
	assert.Equal(t, want.Sessions[0].RemoteAddr, got.Sessions[0].RemoteAddr)
	assert.Equal(t, want.Sessions[1].User, got.Sessions[1].User)
}

func TestList_EmptySessions(t *testing.T) {
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return types.SessionsResponse{Sessions: []types.SessionEntry{}}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	got, err := client.List(ctx, sockPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got.Sessions)
}

// --------------------------------------------------------------------------
// Kill — Task 3.5
// --------------------------------------------------------------------------

func TestKill_Success(t *testing.T) {
	handler := func(_ context.Context, req *types.Request) (any, error) {
		assert.Equal(t, types.KillRequest, req.Type)
		return map[string]string{"status": "ok"}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	err := client.Kill(ctx, sockPath)
	require.NoError(t, err)
}

func TestKill_SessionLocked(t *testing.T) {
	handler := func(_ context.Context, _ *types.Request) (any, error) {
		return nil, assert.AnError
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	err := client.Kill(ctx, sockPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client:")
}
