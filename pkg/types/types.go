// Package types defines the shared data types used for IPC communication
// between agent2shell components via Unix domain sockets.
package types

import "time"

// RequestType identifies the kind of operation requested over the socket.
type RequestType string

const (
	// RunRequest asks the session to execute a shell command.
	RunRequest RequestType = "run"

	// StatusRequest asks for the current session metadata.
	StatusRequest RequestType = "status"

	// ListRequest asks for all active sessions.
	ListRequest RequestType = "list"

	// KillRequest asks the session to terminate.
	KillRequest RequestType = "kill"
)

// StreamFrameType is the discriminator value for streaming output frames.
type StreamFrameType string

const (
	// StreamLine identifies a frame carrying one line of command output.
	StreamLine StreamFrameType = "line"

	// StreamEnd identifies the final frame in a streaming response, carrying
	// completion metadata such as exit code, duration, and an optional error.
	StreamEnd StreamFrameType = "end"
)

// StreamFrame is the JSON envelope used during streaming execution. Each frame
// carries a Type discriminator and type-specific fields.
//
// For StreamLine frames: Data contains the output line; other fields are zero.
// For StreamEnd frames: ExitCode, DurationMS, and Error (optional) are set;
// Data is empty.
type StreamFrame struct {
	// Type identifies whether this is a "line" or "end" frame.
	Type StreamFrameType `json:"type"`

	// Data is the output line. Only set on StreamLine frames.
	Data string `json:"data,omitempty"`

	// ExitCode is the process exit code. Only set on StreamEnd frames.
	ExitCode int `json:"exit_code,omitempty"`

	// DurationMS is the wall-clock execution time in milliseconds.
	// Only set on StreamEnd frames.
	DurationMS int64 `json:"duration_ms,omitempty"`

	// Error describes a transport or timeout failure. Only set on StreamEnd
	// frames when an error occurred. Empty string on success.
	Error string `json:"error,omitempty"`
}

// Request is the JSON message sent by a client to the Unix socket.
type Request struct {
	// Type identifies the operation to perform.
	Type RequestType `json:"type"`

	// Command is the shell command to execute. Only used with RunRequest.
	Command string `json:"command,omitempty"`

	// Timeout is the maximum execution time in seconds. Zero means no limit.
	// Only used with RunRequest.
	Timeout int `json:"timeout,omitempty"`

	// Stream, when true, requests line-by-line streaming output instead of a
	// single buffered ExecResponse. Defaults to false; old clients omit this
	// field and receive the unmodified buffered response.
	Stream bool `json:"stream,omitempty"`
}

// ExecResponse is the JSON message returned after a command execution.
type ExecResponse struct {
	// Output is the combined stdout/stderr of the executed command.
	Output string `json:"output"`

	// ExitCode is the process exit code. Zero indicates success.
	ExitCode int `json:"exit_code"`

	// DurationMS is the wall-clock execution time in milliseconds.
	DurationMS int64 `json:"duration_ms"`

	// Error contains a non-empty description when execution failed at the
	// transport or process level (distinct from a non-zero exit code).
	Error string `json:"error,omitempty"`
}

// SessionInfo holds metadata about a connected reverse-shell session.
type SessionInfo struct {
	// RemoteAddr is the TCP address of the connecting shell (host:port).
	RemoteAddr string `json:"remote_addr"`

	// Shell is the path to the detected shell binary (e.g. /bin/bash).
	Shell string `json:"shell"`

	// User is the username running the remote shell process.
	User string `json:"user"`

	// Hostname is the remote machine's hostname.
	Hostname string `json:"hostname"`

	// OS is the remote operating system identifier (e.g. linux, darwin).
	OS string `json:"os"`

	// Arch is the remote CPU architecture (e.g. amd64, arm64).
	Arch string `json:"arch"`

	// Distro is the Linux distribution name when applicable.
	Distro string `json:"distro,omitempty"`

	// ConnectedAt is the UTC timestamp when the session was established.
	ConnectedAt time.Time `json:"connected_at"`

	// CommandsExecuted is the total number of commands run in this session.
	CommandsExecuted int `json:"commands_executed"`

	// Tag is an optional user-supplied label for the session.
	Tag string `json:"tag,omitempty"`

	// Recording indicates whether session I/O is being recorded to disk.
	Recording bool `json:"recording"`

	// Error contains a non-empty description when the session is in an
	// error state.
	Error string `json:"error,omitempty"`
}

// SessionEntry combines SessionInfo with the socket path used to reach the
// session, as returned in list responses.
type SessionEntry struct {
	SessionInfo

	// SocketPath is the absolute path of the Unix domain socket for this
	// session.
	SocketPath string `json:"socket_path"`
}

// SessionsResponse is the JSON message returned in response to a ListRequest.
type SessionsResponse struct {
	// Sessions is the list of active sessions. Marshals as [] when empty.
	Sessions []SessionEntry `json:"sessions"`

	// Error contains a non-empty description when the list could not be
	// retrieved.
	Error string `json:"error,omitempty"`
}
