// Package client provides a typed Unix socket client for the agent2shell IPC
// protocol. All functions open a new connection, send one request, read one
// response, and close the connection.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
)

// do connects to socketPath, sends req, and returns the raw JSON response.
func do(ctx context.Context, socketPath string, req *types.Request) (json.RawMessage, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("client: dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("client: set deadline: %w", err)
		}
	}

	if err := socket.WriteFrame(conn, req); err != nil {
		return nil, fmt.Errorf("client: write request: %w", err)
	}

	var raw json.RawMessage
	if err := socket.ReadFrame(conn, &raw); err != nil {
		return nil, fmt.Errorf("client: read response: %w", err)
	}

	return raw, nil
}

// checkError inspects raw for a server-side {"error":"message"} response.
func checkError(raw json.RawMessage) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &errResp); err != nil {
		return nil // not an error response format
	}
	if errResp.Error != "" {
		return fmt.Errorf("remote: %s", errResp.Error)
	}
	return nil
}

// Run asks the session at socketPath to execute command with the given timeout
// in seconds (0 = no limit). It returns the full ExecResponse on success.
// When the server reports an exec-level error (resp.Error != ""), Run returns
// both the response and a non-nil error so callers can inspect partial output.
// Transport-level errors (dial, write, read) return nil, error.
func Run(ctx context.Context, socketPath, command string, timeout int) (*types.ExecResponse, error) {
	raw, err := do(ctx, socketPath, &types.Request{
		Type:    types.RunRequest,
		Command: command,
		Timeout: timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("client: run on %s: %w", socketPath, err)
	}

	var resp types.ExecResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("client: run on %s: unmarshal: %w", socketPath, err)
	}

	// Both server-error envelopes {"error":"..."} and exec-level errors in
	// ExecResponse.Error unmarshal into resp.Error. Return the partial struct
	// so callers can inspect any output that was captured before the error.
	if resp.Error != "" {
		return &resp, fmt.Errorf("client: run on %s: %s", socketPath, resp.Error)
	}

	return &resp, nil
}

// Status retrieves the session metadata from the socket at socketPath.
func Status(ctx context.Context, socketPath string) (*types.SessionInfo, error) {
	raw, err := do(ctx, socketPath, &types.Request{Type: types.StatusRequest})
	if err != nil {
		return nil, fmt.Errorf("client: status on %s: %w", socketPath, err)
	}
	if err := checkError(raw); err != nil {
		return nil, fmt.Errorf("client: status on %s: %w", socketPath, err)
	}
	var info types.SessionInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("client: status on %s: unmarshal: %w", socketPath, err)
	}
	return &info, nil
}

// List retrieves all active sessions from the manager socket at socketPath.
func List(ctx context.Context, socketPath string) (*types.SessionsResponse, error) {
	raw, err := do(ctx, socketPath, &types.Request{Type: types.ListRequest})
	if err != nil {
		return nil, fmt.Errorf("client: list on %s: %w", socketPath, err)
	}
	if err := checkError(raw); err != nil {
		return nil, fmt.Errorf("client: list on %s: %w", socketPath, err)
	}
	var resp types.SessionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("client: list on %s: unmarshal: %w", socketPath, err)
	}
	if resp.Error != "" {
		return &resp, fmt.Errorf("client: list on %s: %s", socketPath, resp.Error)
	}
	return &resp, nil
}

// Kill sends a kill request to the session at socketPath, asking it to
// terminate.
func Kill(ctx context.Context, socketPath string) error {
	raw, err := do(ctx, socketPath, &types.Request{Type: types.KillRequest})
	if err != nil {
		return fmt.Errorf("client: kill on %s: %w", socketPath, err)
	}
	if err := checkError(raw); err != nil {
		return fmt.Errorf("client: kill on %s: %w", socketPath, err)
	}
	return nil
}
