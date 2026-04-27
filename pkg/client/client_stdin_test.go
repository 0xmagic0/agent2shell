package client_test

import (
	"context"
	"net"
	"testing"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Task 3.3: client.Run and client.StreamRun stdin parameter ────────────────

// TestRun_StdinIsForwardedInRequest verifies that when stdin is non-empty,
// client.Run sends Request.Stdin with the supplied value.
func TestRun_StdinIsForwardedInRequest(t *testing.T) {
	var capturedReq types.Request

	handler := func(_ context.Context, req *types.Request) (any, error) {
		capturedReq = *req
		return types.ExecResponse{Output: "root", ExitCode: 0}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	_, err := client.Run(ctx, sockPath, "bash", 5, "id\nwhoami\n")
	require.NoError(t, err)

	assert.Equal(t, "id\nwhoami\n", capturedReq.Stdin,
		"Run must forward stdin to Request.Stdin")
}

// TestRun_EmptyStdinOmittedFromRequest verifies that when stdin is empty,
// Request.Stdin is not set (zero value — omitempty serialization).
func TestRun_EmptyStdinOmittedFromRequest(t *testing.T) {
	var capturedReq types.Request

	handler := func(_ context.Context, req *types.Request) (any, error) {
		capturedReq = *req
		return types.ExecResponse{Output: "ok", ExitCode: 0}, nil
	}
	sockPath := setupMockServer(t, handler)

	ctx := context.Background()
	_, err := client.Run(ctx, sockPath, "id", 5, "")
	require.NoError(t, err)

	assert.Equal(t, "", capturedReq.Stdin,
		"Run must not set Request.Stdin when stdin param is empty")
}

// TestStreamRun_StdinIsForwardedInRequest verifies that StreamRun sends
// Request.Stdin with the supplied stdin content.
func TestStreamRun_StdinIsForwardedInRequest(t *testing.T) {
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

		_ = socket.WriteFrame(conn, types.StreamFrame{Type: types.StreamEnd, ExitCode: 0})
	}()

	waitForUnixSocket(t, sockPath)

	_, _ = client.StreamRun(context.Background(), sockPath, "bash", 5, "id\n", func(string) {})

	req := <-reqCh
	assert.Equal(t, "id\n", req.Stdin,
		"StreamRun must forward stdin to Request.Stdin")
}

// TestStreamRun_EmptyStdinOmittedFromRequest verifies that StreamRun does not
// set Request.Stdin when stdin param is empty.
func TestStreamRun_EmptyStdinOmittedFromRequest(t *testing.T) {
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

		_ = socket.WriteFrame(conn, types.StreamFrame{Type: types.StreamEnd, ExitCode: 0})
	}()

	waitForUnixSocket(t, sockPath)

	_, _ = client.StreamRun(context.Background(), sockPath, "id", 5, "", func(string) {})

	req := <-reqCh
	assert.Equal(t, "", req.Stdin,
		"StreamRun must not set Request.Stdin when stdin param is empty")
}
