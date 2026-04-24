package session_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── S2.1: ExecStream unit tests ─────────────────────────────────────────────

// TestExecStream_HappyPath verifies that onLine is called for each output line
// in arrival order and that ExecStream returns the correct exit code.
func TestExecStream_HappyPath(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	want := []string{"line one", "line two", "line three"}

	go func() {
		id, err := sh.readCommandID()
		if err != nil {
			return
		}
		_ = sh.respond(id, want, 0)
	}()

	var got []string
	var mu sync.Mutex

	exitCode, durationMS, err := sess.ExecStream(context.Background(), "echo", 5*time.Second, func(line string) {
		mu.Lock()
		got = append(got, line)
		mu.Unlock()
	})

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.GreaterOrEqual(t, durationMS, int64(0))
	mu.Lock()
	assert.Equal(t, want, got)
	mu.Unlock()
}

// TestExecStream_EmptyOutput verifies that when the command produces no output,
// onLine is never called and ExecStream returns no error.
func TestExecStream_EmptyOutput(t *testing.T) {
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
		_ = sh.respond(id, nil, 0)
	}()

	callCount := 0
	exitCode, _, err := sess.ExecStream(context.Background(), "true", 5*time.Second, func(line string) {
		callCount++
	})

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 0, callCount, "onLine must not be called when output is empty")
}

// TestExecStream_Timeout verifies that when the timeout fires, ExecStream
// returns ErrExecTimeout and that partial onLine calls already made are not
// rolled back.
func TestExecStream_Timeout(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 5 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	go func() {
		sh := newMockShell(shell)
		id, err := sh.readCommandID()
		if err != nil {
			return
		}
		// Write start marker and two lines, then stall — never send end marker.
		startMarker := fmt.Sprintf("---A2S-START-%s---\n", id)
		_, _ = shell.Write([]byte(startMarker))
		_, _ = shell.Write([]byte("partial line 1\n"))
		_, _ = shell.Write([]byte("partial line 2\n"))
		// No end marker — let ExecStream time out.
	}()

	var got []string
	var mu sync.Mutex

	_, _, err = sess.ExecStream(context.Background(), "stall", 300*time.Millisecond, func(line string) {
		mu.Lock()
		got = append(got, line)
		mu.Unlock()
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, session.ErrExecTimeout))

	// Partial lines already dispatched must not be rolled back.
	mu.Lock()
	assert.Equal(t, 2, len(got), "partial lines received before timeout must be preserved")
	mu.Unlock()
}

// TestExecStream_ContextCancel verifies that cancelling the context causes
// ExecStream to return context.Canceled.
func TestExecStream_ContextCancel(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	// Shell drains commands but never responds.
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

	_, _, err = sess.ExecStream(ctx, "sleep 999", 5*time.Second, func(string) {})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// TestExecStream_ClosedSession verifies that ExecStream on a closed session
// returns ErrSessionClosed immediately.
func TestExecStream_ClosedSession(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)

	require.NoError(t, sess.Close())

	_, _, err = sess.ExecStream(context.Background(), "id", 5*time.Second, func(string) {})
	require.Error(t, err)
	assert.True(t, errors.Is(err, session.ErrSessionClosed))
}

// TestExecStream_Serialization verifies that ExecStream blocks until a
// concurrent Exec holding the mutex finishes.
func TestExecStream_Serialization(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{Conn: app, DefaultTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer sess.Close()

	// Shell responds to exactly 2 commands sequentially.
	go func() {
		for i := 0; i < 2; i++ {
			id, err := sh.readCommandID()
			if err != nil {
				return
			}
			_ = sh.respond(id, []string{fmt.Sprintf("out-%d", i)}, 0)
		}
	}()

	type result struct {
		err error
	}

	results := make([]result, 2)
	var wg sync.WaitGroup

	// First goroutine uses Exec.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := sess.Exec(context.Background(), "cmd-0", 5*time.Second)
		results[0] = result{err: err}
	}()

	// Second goroutine uses ExecStream — must block until Exec finishes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := sess.ExecStream(context.Background(), "cmd-1", 5*time.Second, func(string) {})
		results[1] = result{err: err}
	}()

	wg.Wait()

	require.NoError(t, results[0].err, "Exec failed")
	require.NoError(t, results[1].err, "ExecStream failed")
}
