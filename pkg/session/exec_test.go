package session_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockShell wraps the test side of a net.Pipe() and provides helpers for
// reading wrapped commands and writing marker-delimited responses.
type mockShell struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

func newMockShell(conn net.Conn) *mockShell {
	return &mockShell{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
	}
}

// readLine reads the next line from the pipe without blocking forever.
// Returns error if scanner returns false (EOF or error).
func (m *mockShell) readLine() (string, error) {
	if m.scanner.Scan() {
		return m.scanner.Text(), nil
	}
	if err := m.scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("EOF")
}

// readCommandID reads wrapped command lines until it finds one with a start
// marker pattern, then extracts and returns the command ID.
func (m *mockShell) readCommandID() (string, error) {
	for {
		line, err := m.readLine()
		if err != nil {
			return "", err
		}
		// The wrapped command is: echo '---A2S-START-<id>---'; cmd; echo '---A2S-END-<id>---'$?
		// Extract id from the echo command line by scanning for the start marker prefix.
		const startMarkerPrefix = "---A2S-START-"
		idx := strings.Index(line, startMarkerPrefix)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(startMarkerPrefix):]
		endIdx := strings.Index(rest, "---")
		if endIdx < 0 {
			continue
		}
		id := rest[:endIdx]
		if id != "" {
			return id, nil
		}
	}
}

// respond writes start marker + output lines + end marker with exit code.
func (m *mockShell) respond(id string, lines []string, exitCode int) error {
	startMarker := fmt.Sprintf("---A2S-START-%s---\n", id)
	if _, err := m.conn.Write([]byte(startMarker)); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := m.conn.Write([]byte(l + "\n")); err != nil {
			return err
		}
	}
	endMarker := fmt.Sprintf("---A2S-END-%s---%d\n", id, exitCode)
	_, err := m.conn.Write([]byte(endMarker))
	return err
}

// respondToProbe reads the next command, extracts the ID, and responds with
// output + exit 0. Used for Detect tests where probe order is known.
func (m *mockShell) respondToProbe(output string) error {
	id, err := m.readCommandID()
	if err != nil {
		return err
	}
	return m.respond(id, []string{output}, 0)
}

// ─── S8.1: Happy path ────────────────────────────────────────────────────────

func TestExecHappyPath(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	go func() {
		id, err := sh.readCommandID()
		if err != nil {
			return
		}
		_ = sh.respond(id, []string{"output line 1", "output line 2"}, 0)
	}()

	resp, err := sess.Exec(context.Background(), "echo hi", 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Output, "output line 1")
	assert.Contains(t, resp.Output, "output line 2")
	assert.Equal(t, 0, resp.ExitCode)
	assert.GreaterOrEqual(t, resp.DurationMS, int64(0))
}

// ─── S8.2: Non-zero exit code ────────────────────────────────────────────────

func TestExecNonZeroExit(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	go func() {
		id, err := sh.readCommandID()
		if err != nil {
			return
		}
		_ = sh.respond(id, []string{"some output"}, 127)
	}()

	resp, err := sess.Exec(context.Background(), "bad-cmd", 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 127, resp.ExitCode)
	assert.Contains(t, resp.Output, "some output")
}

// ─── S8.3: Timeout ───────────────────────────────────────────────────────────

func TestExecTimeout(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	// Write start marker but never the end marker.
	go func() {
		id, err := sh.readCommandID()
		if err != nil {
			return
		}
		startMarker := fmt.Sprintf("---A2S-START-%s---\n", id)
		_, _ = shell.Write([]byte(startMarker))
		// No end marker — let Exec time out.
	}()

	_, err = sess.Exec(context.Background(), "sleep 999", 200*time.Millisecond)
	require.Error(t, err)
	assert.True(t, errors.Is(err, session.ErrExecTimeout))
}

// ─── S8.4: Context cancel ────────────────────────────────────────────────────

func TestExecContextCancel(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	// Shell side drains commands but never responds.
	sh := newMockShell(shell)
	go func() {
		for {
			_, err := sh.readLine()
			if err != nil {
				return
			}
		}
	}()

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = sess.Exec(ctx, "sleep 999", 5*time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// ─── S8.5: Exec on closed session ────────────────────────────────────────────

func TestExecOnClosedSession(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)

	require.NoError(t, sess.Close())

	_, err = sess.Exec(context.Background(), "id", 5*time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, session.ErrSessionClosed))
}

// ─── S8.6: Concurrent serialization ─────────────────────────────────────────

func TestExecConcurrentSerialization(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	// Shell side: respond to exactly 2 commands sequentially.
	go func() {
		for i := 0; i < 2; i++ {
			id, err := sh.readCommandID()
			if err != nil {
				return
			}
			_ = sh.respond(id, []string{fmt.Sprintf("result-%d", i)}, 0)
		}
	}()

	type execResult struct {
		resp *types.ExecResponse
		err  error
	}

	var wg sync.WaitGroup
	results := make([]execResult, 2)
	var mu sync.Mutex

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := sess.Exec(context.Background(), fmt.Sprintf("cmd-%d", idx), 5*time.Second)
			mu.Lock()
			results[idx] = execResult{resp: resp, err: err}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	for i, r := range results {
		require.NoError(t, r.err, "goroutine %d failed", i)
		require.NotNil(t, r.resp, "goroutine %d got nil response", i)
	}
	// Both must have succeeded — collect all outputs to verify no interleaving.
	combined := results[0].resp.Output + results[1].resp.Output
	assert.True(t,
		strings.Contains(combined, "result-0") && strings.Contains(combined, "result-1"),
		"expected both results present, got: %q", combined)
}

// ─── S8.7: Stale output drain ────────────────────────────────────────────────

func TestExecDrainStaleOutput(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	// Write stale junk BEFORE Exec is called.
	_, err = shell.Write([]byte("stale line 1\nstale line 2\n"))
	require.NoError(t, err)

	// Small pause to let readLoop deliver stale lines to the (not yet set) channel.
	time.Sleep(50 * time.Millisecond)

	go func() {
		id, err := sh.readCommandID()
		if err != nil {
			return
		}
		_ = sh.respond(id, []string{"clean output"}, 0)
	}()

	resp, err := sess.Exec(context.Background(), "id", 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotContains(t, resp.Output, "stale")
	assert.Contains(t, resp.Output, "clean output")
}

// ─── S8.11: Close during Exec ─────────────────────────────────────────────────

func TestExecCloseDuringExec(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 10 * time.Second})
	require.NoError(t, err)

	// Shell side: read the command but never respond.
	go func() {
		_, _ = sh.readCommandID()
		// Close the session after the Exec has started waiting.
		time.Sleep(50 * time.Millisecond)
		_ = sess.Close()
	}()

	_, err = sess.Exec(context.Background(), "sleep 999", 10*time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, session.ErrSessionClosed))
}

// ─── S8.14: CommandsExecuted count ───────────────────────────────────────────

func TestExecCommandsExecuted(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	// Goroutine: respond to 3 commands, then stop (4th will time out).
	go func() {
		for i := 0; i < 3; i++ {
			id, err := sh.readCommandID()
			if err != nil {
				return
			}
			_ = sh.respond(id, []string{"ok"}, 0)
		}
		// 4th command: read but don't respond → timeout.
		_, _ = sh.readCommandID()
	}()

	for i := 0; i < 3; i++ {
		resp, err := sess.Exec(context.Background(), "echo ok", 5*time.Second)
		require.NoError(t, err, "exec %d failed", i)
		require.NotNil(t, resp)
	}

	// 4th exec — should time out.
	_, err = sess.Exec(context.Background(), "timeout-cmd", 200*time.Millisecond)
	require.Error(t, err)
	assert.True(t, errors.Is(err, session.ErrExecTimeout))

	info := sess.Info()
	assert.Equal(t, 3, info.CommandsExecuted)
}
