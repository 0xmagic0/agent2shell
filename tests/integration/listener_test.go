// Package integration contains integration tests for the listener + session +
// socket stack. Tests use real TCP connections and real Unix domain sockets to
// exercise the full data path: TCP accept → session → socket server → client.
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Fake shell helper ──────────────────────────────────────────────────────

// fakeShell simulates a reverse shell over a TCP connection. It responds to
// wrapped commands with marker-delimited output.
type fakeShell struct {
	conn    net.Conn
	scanner *bufio.Scanner
	done    chan struct{}
}

func dialFakeShell(t *testing.T, addr string) *fakeShell {
	t.Helper()

	var conn net.Conn
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			conn = c
			return true
		}
		return false
	}, 3*time.Second, 10*time.Millisecond, "fake shell could not connect to %s", addr)

	fs := &fakeShell{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
		done:    make(chan struct{}),
	}
	t.Cleanup(func() {
		conn.Close()
		select {
		case <-fs.done:
		default:
		}
	})
	return fs
}

// respondToProbe reads one wrapped command from the TCP connection, extracts
// the marker ID, and writes back a marker-delimited response.
func (fs *fakeShell) respondToProbe(output string, exitCode int) error {
	if !fs.scanner.Scan() {
		if err := fs.scanner.Err(); err != nil {
			return fmt.Errorf("fakeShell: scan: %w", err)
		}
		return fmt.Errorf("fakeShell: connection closed")
	}
	line := fs.scanner.Text()
	id := extractMarkerID(line)
	if id == "" {
		return fmt.Errorf("fakeShell: could not extract marker ID from: %q", line)
	}
	return fs.writeResponse(id, output, exitCode)
}

func (fs *fakeShell) writeResponse(id, output string, exitCode int) error {
	response := fmt.Sprintf("---A2S-START-%s---\n%s\n---A2S-END-%s---%d\n",
		id, output, id, exitCode)
	_, err := fmt.Fprint(fs.conn, response)
	return err
}

// drainProbes reads and responds to n probes with empty output and exit 0.
// Used when we don't care about detect results.
func (fs *fakeShell) drainProbes(n int) {
	for i := 0; i < n; i++ {
		fs.respondToProbe("", 0) //nolint:errcheck // best-effort in test drain
	}
}

// extractMarkerID extracts the marker ID from a wrapped command.
// Format: echo '---A2S-START-<id>---'; <cmd>; echo '---A2S-END-<id>---'$?
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

// ─── Socket client helper ───────────────────────────────────────────────────

// socketClient wraps a Unix socket connection and provides frame-based send/recv.
type socketClient struct {
	conn net.Conn
}

func dialSocketClient(t *testing.T, sockPath string) *socketClient {
	t.Helper()

	var conn net.Conn
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			conn = c
			return true
		}
		return false
	}, 4*time.Second, 20*time.Millisecond, "socket client could not connect to %s", sockPath)

	t.Cleanup(func() { conn.Close() })
	return &socketClient{conn: conn}
}

func (sc *socketClient) send(req types.Request) error {
	return socket.WriteFrame(sc.conn, req)
}

func (sc *socketClient) recvRaw(v any) error {
	return socket.ReadFrame(sc.conn, v)
}

// ─── Listener test harness ──────────────────────────────────────────────────

// startListener creates and starts a Listener in a goroutine, returning the
// port, socket path, and a cancel function. The caller must call cancel when done.
func startListener(t *testing.T, cfg listener.Config) (port int, sockPath string, cancel context.CancelFunc, listenerDone chan error) {
	t.Helper()

	port, err := testutil.FreePort()
	require.NoError(t, err)
	sockPath = testutil.TempSocket(t)

	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.SocketPath = sockPath

	l, err := listener.New(cfg)
	require.NoError(t, err)

	ctx, cancelFn := context.WithCancel(context.Background())

	listenerDone = make(chan error, 1)
	go func() { listenerDone <- l.Listen(ctx) }()

	return port, sockPath, cancelFn, listenerDone
}

// waitForSocket blocks until the Unix socket at path is connectable.
func waitForSocket(t *testing.T, sockPath string) {
	t.Helper()
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			c.Close()
			return true
		}
		return false
	}, 4*time.Second, 20*time.Millisecond, "socket %s did not appear", sockPath)
}

// ─── S9.1 — Full flow ──────────────────────────────────────────────────────

// TestFullFlow verifies the complete path:
//
//	Listener binds TCP → fake shell connects → detect probes answered → socket
//	client sends RunRequest → gets ExecResponse with correct output and ExitCode 0.
func TestFullFlow(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 500 * time.Millisecond,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	// Respond to 6 detect probes with empty output (we don't verify detect here).
	fs.drainProbes(6)

	// Wait for the socket server to be ready.
	waitForSocket(t, sockPath)

	// Connect socket client and send a RunRequest.
	client := dialSocketClient(t, sockPath)

	// The shell must respond to the run request.
	errCh := make(chan error, 1)
	go func() {
		errCh <- fs.respondToProbe("hello from shell", 0)
	}()

	err := client.send(types.Request{
		Type:    types.RunRequest,
		Command: "echo hello from shell",
	})
	require.NoError(t, err)

	var resp types.ExecResponse
	err = client.recvRaw(&resp)
	require.NoError(t, err)
	require.NoError(t, <-errCh)

	assert.Equal(t, "hello from shell", resp.Output)
	assert.Equal(t, 0, resp.ExitCode)
}

// ─── S9.5 — Kill request ───────────────────────────────────────────────────

// TestKillRequest verifies that a KillRequest returns {"status":"ok"} and
// causes the session to close asynchronously.
func TestKillRequest(t *testing.T) {
	port, sockPath, cancel, listenerDone := startListener(t, listener.Config{
		DefaultTimeout: 200 * time.Millisecond,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	// Drain all detect probes.
	fs.drainProbes(6)
	waitForSocket(t, sockPath)

	client := dialSocketClient(t, sockPath)

	err := client.send(types.Request{Type: types.KillRequest})
	require.NoError(t, err)

	// Receive the raw map response.
	var raw map[string]string
	err = client.recvRaw(&raw)
	require.NoError(t, err)
	assert.Equal(t, "ok", raw["status"])

	// Listener should shut down because session closed.
	select {
	case err := <-listenerDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not exit within 5s after KillRequest")
	}
}

// ─── S9.6 — Status returns metadata ────────────────────────────────────────

// TestStatusReturnsMetadata verifies that after detect runs successfully, a
// StatusRequest returns SessionInfo with Shell and User populated.
func TestStatusReturnsMetadata(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 2 * time.Second,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	// Respond to detect probes with real-looking output.
	// Probe order: echo $0, uname -s, uname -m, cat /etc/os-release, id, hostname
	probeOutputs := []struct {
		output   string
		exitCode int
	}{
		{"bash", 0},   // echo $0 → shell
		{"Linux", 0},  // uname -s → OS
		{"x86_64", 0}, // uname -m → arch
		{"PRETTY_NAME=\"Ubuntu 22.04\"\nNAME=\"Ubuntu\"", 0},         // os-release
		{"uid=33(www-data) gid=33(www-data) groups=33(www-data)", 0}, // id
		{"target-host", 0}, // hostname
	}

	go func() {
		for _, p := range probeOutputs {
			if err := fs.respondToProbe(p.output, p.exitCode); err != nil {
				return
			}
		}
	}()

	waitForSocket(t, sockPath)

	client := dialSocketClient(t, sockPath)

	err := client.send(types.Request{Type: types.StatusRequest})
	require.NoError(t, err)

	var info types.SessionInfo
	err = client.recvRaw(&info)
	require.NoError(t, err)

	assert.Equal(t, "bash", info.Shell)
	assert.Equal(t, "www-data", info.User)
}

// ─── S9.7 — List returns single entry ──────────────────────────────────────

// TestListReturnsSingleEntry verifies that a ListRequest returns a
// SessionsResponse with exactly one entry whose SocketPath matches.
func TestListReturnsSingleEntry(t *testing.T) {
	port, sockPath, cancel, _ := startListener(t, listener.Config{
		DefaultTimeout: 200 * time.Millisecond,
	})
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fs := dialFakeShell(t, addr)

	// Drain detect probes.
	fs.drainProbes(6)
	waitForSocket(t, sockPath)

	client := dialSocketClient(t, sockPath)

	err := client.send(types.Request{Type: types.ListRequest})
	require.NoError(t, err)

	// Read raw JSON to handle the nested SessionsResponse.
	var raw json.RawMessage
	err = client.recvRaw(&raw)
	require.NoError(t, err)

	var resp types.SessionsResponse
	err = json.Unmarshal(raw, &resp)
	require.NoError(t, err)

	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, sockPath, resp.Sessions[0].SocketPath)
}
