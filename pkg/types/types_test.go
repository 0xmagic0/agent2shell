package types_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// S2.1 — Request round-trip (RunRequest with command and timeout).
func TestRequest_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   types.Request
	}{
		{
			name: "run request with command and timeout",
			in: types.Request{
				Type:    types.RunRequest,
				Command: "whoami",
				Timeout: 30,
			},
		},
		{
			name: "status request (no command, no timeout)",
			in: types.Request{
				Type: types.StatusRequest,
			},
		},
		{
			name: "list request",
			in: types.Request{
				Type: types.ListRequest,
			},
		},
		{
			name: "kill request",
			in: types.Request{
				Type: types.KillRequest,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			require.NoError(t, err)

			var got types.Request
			err = json.Unmarshal(data, &got)
			require.NoError(t, err)

			assert.Equal(t, tc.in, got)
		})
	}
}

// S2.2 — ExecResponse round-trip.
func TestExecResponse_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   types.ExecResponse
	}{
		{
			name: "successful response",
			in: types.ExecResponse{
				Output:     "root\n",
				ExitCode:   0,
				DurationMS: 42,
				Error:      "",
			},
		},
		{
			name: "response with error",
			in: types.ExecResponse{
				Output:     "",
				ExitCode:   1,
				DurationMS: 100,
				Error:      "command not found",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			require.NoError(t, err)

			var got types.ExecResponse
			err = json.Unmarshal(data, &got)
			require.NoError(t, err)

			assert.Equal(t, tc.in, got)
		})
	}
}

// S2.3 — SessionInfo round-trip with time.Time in UTC.
func TestSessionInfo_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	in := types.SessionInfo{
		RemoteAddr:       "192.168.1.1:4444",
		Shell:            "/bin/bash",
		User:             "root",
		Hostname:         "victim",
		OS:               "linux",
		Arch:             "amd64",
		Distro:           "ubuntu",
		ConnectedAt:      now,
		CommandsExecuted: 7,
		Tag:              "lab",
		Recording:        true,
		Error:            "",
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	var got types.SessionInfo
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)

	assert.Equal(t, in, got)
}

// S2.4 — Zero-value ExecResponse marshals without error and produces valid JSON.
func TestExecResponse_ZeroValue(t *testing.T) {
	var resp types.ExecResponse

	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Must be valid JSON.
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
}

// S2.5 — SessionEntry preserves SocketPath and all SessionInfo fields.
func TestSessionEntry_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	in := types.SessionEntry{
		SessionInfo: types.SessionInfo{
			RemoteAddr:       "10.0.0.1:5555",
			Shell:            "/bin/sh",
			User:             "ubuntu",
			Hostname:         "box",
			OS:               "linux",
			Arch:             "arm64",
			ConnectedAt:      now,
			CommandsExecuted: 3,
			Recording:        false,
		},
		SocketPath: "/tmp/agent2shell/abc123.sock",
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	var got types.SessionEntry
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)

	assert.Equal(t, in, got)
	assert.Equal(t, "/tmp/agent2shell/abc123.sock", got.SocketPath)
	assert.Equal(t, in.RemoteAddr, got.RemoteAddr)
}

// S2.6 — SessionsResponse with empty slice marshals as "sessions":[] NOT null.
func TestSessionsResponse_EmptySlice(t *testing.T) {
	resp := types.SessionsResponse{
		Sessions: []types.SessionEntry{},
		Error:    "",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"sessions":[]`)
}

// Additional: SessionsResponse round-trip with entries.
func TestSessionsResponse_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	in := types.SessionsResponse{
		Sessions: []types.SessionEntry{
			{
				SessionInfo: types.SessionInfo{
					RemoteAddr:  "1.2.3.4:4444",
					Shell:       "/bin/bash",
					User:        "root",
					Hostname:    "h1",
					OS:          "linux",
					Arch:        "amd64",
					ConnectedAt: now,
					Recording:   true,
				},
				SocketPath: "/tmp/s1.sock",
			},
		},
		Error: "",
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	var got types.SessionsResponse
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)

	assert.Equal(t, in, got)
}

// Additional: RequestType constants have expected string values.
func TestRequestType_Constants(t *testing.T) {
	assert.Equal(t, types.RequestType("run"), types.RunRequest)
	assert.Equal(t, types.RequestType("status"), types.StatusRequest)
	assert.Equal(t, types.RequestType("list"), types.ListRequest)
	assert.Equal(t, types.RequestType("kill"), types.KillRequest)
}

// Additional: omitempty fields are absent when zero.
func TestRequest_OmitemptyFields(t *testing.T) {
	req := types.Request{Type: types.StatusRequest}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	s := string(data)
	assert.NotContains(t, s, `"command"`)
	assert.NotContains(t, s, `"timeout"`)
}

// ─── S1.1: StreamFrame round-trip ────────────────────────────────────────────

// TestStreamFrame_LineRoundTrip verifies a "line" frame marshals and unmarshals
// correctly, and that end-only fields are absent when zero.
func TestStreamFrame_LineRoundTrip(t *testing.T) {
	in := types.StreamFrame{
		Type: types.StreamLine,
		Data: "hello world",
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	var got types.StreamFrame
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, in, got)

	// omitempty: end-only fields must be absent in the JSON.
	s := string(data)
	assert.NotContains(t, s, `"exit_code"`)
	assert.NotContains(t, s, `"duration_ms"`)
	assert.NotContains(t, s, `"error"`)
}

// TestStreamFrame_EndRoundTrip verifies a "end" frame with all fields marshals
// correctly and that Data is absent.
func TestStreamFrame_EndRoundTrip(t *testing.T) {
	in := types.StreamFrame{
		Type:       types.StreamEnd,
		ExitCode:   42,
		DurationMS: 1234,
		Error:      "exec timeout",
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	var got types.StreamFrame
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, in, got)

	s := string(data)
	assert.NotContains(t, s, `"data"`)
}

// TestStreamFrame_EndNoError verifies a successful end frame omits the error
// field when empty.
func TestStreamFrame_EndNoError(t *testing.T) {
	in := types.StreamFrame{
		Type:       types.StreamEnd,
		ExitCode:   0,
		DurationMS: 50,
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	s := string(data)
	assert.NotContains(t, s, `"error"`)
	assert.NotContains(t, s, `"data"`)
}

// TestStreamFrameType_Constants verifies the constant values.
func TestStreamFrameType_Constants(t *testing.T) {
	assert.Equal(t, types.StreamFrameType("line"), types.StreamLine)
	assert.Equal(t, types.StreamFrameType("end"), types.StreamEnd)
}

// TestRequest_StreamField verifies that Stream bool is included when true and
// omitted when false (omitempty).
func TestRequest_StreamField(t *testing.T) {
	t.Run("stream true is present in JSON", func(t *testing.T) {
		req := types.Request{Type: types.RunRequest, Command: "id", Stream: true}
		data, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"stream":true`)
	})

	t.Run("stream false is absent in JSON (omitempty)", func(t *testing.T) {
		req := types.Request{Type: types.RunRequest, Command: "id"}
		data, err := json.Marshal(req)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"stream"`)
	})

	t.Run("round-trip with stream true", func(t *testing.T) {
		in := types.Request{Type: types.RunRequest, Command: "ls", Timeout: 10, Stream: true}
		data, err := json.Marshal(in)
		require.NoError(t, err)
		var got types.Request
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, in, got)
	})
}
