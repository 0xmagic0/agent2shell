package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startServer starts a socket.Server on a temp socket path with the given
// handler. The server is automatically stopped when the test ends.
func startServer(t *testing.T, handler socket.Handler) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	srv := socket.NewServer(sockPath, handler)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ctx) //nolint:errcheck // best-effort in test server
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Wait for the socket file to appear — stat-based to avoid consuming a
	// server connection with a dial.
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return sockPath
}

// cannedHandler returns a Handler that dispatches canned responses for all
// four request types. Unknown types return an error.
func cannedHandler() socket.Handler {
	return func(ctx context.Context, req *types.Request) (any, error) {
		switch req.Type {
		case types.RunRequest:
			return types.ExecResponse{Output: "uid=0(root)", ExitCode: 0}, nil
		case types.StatusRequest:
			return types.SessionInfo{
				RemoteAddr: "10.0.0.5:54321",
				Shell:      "/bin/bash",
				User:       "root",
			}, nil
		case types.ListRequest:
			return types.SessionsResponse{
				Sessions: []types.SessionEntry{
					{
						SessionInfo: types.SessionInfo{
							RemoteAddr: "10.0.0.5:54321",
							Shell:      "/bin/bash",
							User:       "root",
						},
						SocketPath: "/tmp/agent2shell/session-1.sock",
					},
				},
			}, nil
		case types.KillRequest:
			return map[string]string{"status": "ok"}, nil
		default:
			return nil, fmt.Errorf("unknown request type: %s", req.Type)
		}
	}
}

// ─── S10.1 — client.Run ────────────────────────────────────────────────────

// TestClientRun_Integration verifies that client.Run correctly sends a
// RunRequest and deserialises the ExecResponse returned by the server.
func TestClientRun_Integration(t *testing.T) {
	sockPath := startServer(t, cannedHandler())

	resp, err := client.Run(context.Background(), sockPath, "id", 0)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "uid=0(root)", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
}

// ─── S10.2 — client.Status ────────────────────────────────────────────────

// TestClientStatus_Integration verifies that client.Status returns the
// SessionInfo sent by the server.
func TestClientStatus_Integration(t *testing.T) {
	sockPath := startServer(t, cannedHandler())

	info, err := client.Status(context.Background(), sockPath)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "10.0.0.5:54321", info.RemoteAddr)
	assert.Equal(t, "/bin/bash", info.Shell)
	assert.Equal(t, "root", info.User)
}

// ─── S10.3 — client.List ──────────────────────────────────────────────────

// TestClientList_Integration verifies that client.List returns a
// SessionsResponse with the expected entries.
func TestClientList_Integration(t *testing.T) {
	sockPath := startServer(t, cannedHandler())

	resp, err := client.List(context.Background(), sockPath)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, "/tmp/agent2shell/session-1.sock", resp.Sessions[0].SocketPath)
	assert.Equal(t, "root", resp.Sessions[0].User)
}

// ─── S10.4 — client.Kill ──────────────────────────────────────────────────

// TestClientKill_Integration verifies that client.Kill returns nil when the
// server responds with {"status":"ok"}.
func TestClientKill_Integration(t *testing.T) {
	sockPath := startServer(t, cannedHandler())

	err := client.Kill(context.Background(), sockPath)
	assert.NoError(t, err)
}

// ─── S10.5 — connection refused ───────────────────────────────────────────

// TestClientRun_ConnectionRefused verifies that client.Run returns a non-nil
// error containing "dial" when the socket does not exist.
func TestClientRun_ConnectionRefused(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	_, err := client.Run(context.Background(), sockPath, "id", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial")
}

// ─── S10.6 — cancelled context ────────────────────────────────────────────

// TestClientRun_ContextCancellation verifies that client.Run returns a
// non-nil error when the context is already cancelled before the call.
func TestClientRun_ContextCancellation(t *testing.T) {
	sockPath := startServer(t, cannedHandler())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Run(ctx, sockPath, "id", 0)
	require.Error(t, err)
}

// ─── S10.7 — server-side error ────────────────────────────────────────────

// TestClientKill_ServerError verifies that client.Kill returns a non-nil
// error when the handler returns an error (server sends {"error":"..."}).
func TestClientKill_ServerError(t *testing.T) {
	errHandler := func(ctx context.Context, req *types.Request) (any, error) {
		return nil, fmt.Errorf("session already dead")
	}
	sockPath := startServer(t, errHandler)

	err := client.Kill(context.Background(), sockPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session already dead")
}

// ─── S10.8 — concurrent calls ─────────────────────────────────────────────

// TestClientConcurrent verifies that multiple concurrent client calls against
// the same server all succeed without data races.
func TestClientConcurrent(t *testing.T) {
	sockPath := startServer(t, cannedHandler())

	const goroutines = 8
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			resp, err := client.Run(context.Background(), sockPath, "id", 0)
			if err != nil {
				errs <- err
				return
			}
			if resp.Output != "uid=0(root)" {
				errs <- fmt.Errorf("unexpected output: %q", resp.Output)
				return
			}
			errs <- nil
		}()
	}

	for i := 0; i < goroutines; i++ {
		assert.NoError(t, <-errs)
	}
}

// ─── S10.9 — deadline propagation ─────────────────────────────────────────

// TestClientRun_DeadlineExceeded verifies that a context with a very short
// deadline causes client.Run to return an error when the server is slow.
func TestClientRun_DeadlineExceeded(t *testing.T) {
	// Handler that blocks longer than the context deadline.
	slowHandler := func(ctx context.Context, req *types.Request) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return types.ExecResponse{Output: "too late"}, nil
		}
	}
	sockPath := startServer(t, slowHandler)

	// Give the OS time to create the socket before we start the timed context.
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			c.Close()
			return true
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "socket did not appear")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Run(ctx, sockPath, "id", 0)
	require.Error(t, err)
}
