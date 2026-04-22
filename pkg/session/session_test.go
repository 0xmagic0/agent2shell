package session_test

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Task 2.1: Sentinel errors ───────────────────────────────────────────────

func TestSentinelErrors(t *testing.T) {
	t.Run("ErrSessionClosed is itself", func(t *testing.T) {
		assert.True(t, errors.Is(session.ErrSessionClosed, session.ErrSessionClosed))
	})
	t.Run("ErrExecTimeout is itself", func(t *testing.T) {
		assert.True(t, errors.Is(session.ErrExecTimeout, session.ErrExecTimeout))
	})
	t.Run("ErrSessionClosed is not ErrExecTimeout", func(t *testing.T) {
		assert.False(t, errors.Is(session.ErrSessionClosed, session.ErrExecTimeout))
	})
}

// ─── Task 2.2: New / readLoop / Info / Done ───────────────────────────────────

func TestNewNilConn(t *testing.T) {
	_, err := session.New(session.Config{Conn: nil})
	require.Error(t, err)
}

func TestNewStartsReadLoop(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	received := make(chan string, 1)
	sess, err := session.New(session.Config{
		Conn: app,
		OnOutput: func(line string) {
			received <- line
		},
	})
	require.NoError(t, err)
	defer sess.Close()

	_, err = shell.Write([]byte("hello from shell\n"))
	require.NoError(t, err)

	select {
	case line := <-received:
		assert.Equal(t, "hello from shell", line)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for onOutput callback")
	}
}

func TestReadLoopExitsOnEOF(t *testing.T) {
	shell, app := net.Pipe()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)

	// Close shell side — readLoop should detect EOF and close doneCh.
	shell.Close()

	select {
	case <-sess.Done():
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneCh to close after EOF")
	}
}

func TestInfoReturnsSnapshot(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{
		Conn:       app,
		RemoteAddr: "1.2.3.4:5678",
		Tag:        "mytag",
	})
	require.NoError(t, err)
	defer sess.Close()

	info := sess.Info()
	assert.Equal(t, "1.2.3.4:5678", info.RemoteAddr)
	assert.Equal(t, "mytag", info.Tag)
	assert.False(t, info.ConnectedAt.IsZero(), "ConnectedAt must be set")
}

// ─── Task 2.5: Close (idempotency) ───────────────────────────────────────────

func TestDoubleClose(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)

	err1 := sess.Close()
	err2 := sess.Close()
	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

func TestCloseSignalsDoneCh(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)

	require.NoError(t, sess.Close())

	select {
	case <-sess.Done():
		// success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Done() channel not closed within 100ms after Close()")
	}
}

// ─── Task 5.1: WriteRaw ───────────────────────────────────────────────────────

func TestWriteRaw_Success(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)
	defer sess.Close()

	data := []byte("hello raw\n")

	// Read from shell end concurrently — net.Pipe is synchronous so
	// WriteRaw blocks until the other end reads.
	type readResult struct {
		buf []byte
		err error
	}
	readCh := make(chan readResult, 1)
	go func() {
		buf := make([]byte, len(data))
		_, err := shell.Read(buf)
		readCh <- readResult{buf: buf, err: err}
	}()

	err = sess.WriteRaw(data)
	require.NoError(t, err)

	select {
	case r := <-readCh:
		require.NoError(t, r.err)
		assert.Equal(t, data, r.buf)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for shell to receive data")
	}
}

func TestWriteRaw_ClosedSession(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)

	require.NoError(t, sess.Close())

	err = sess.WriteRaw([]byte("should fail"))
	assert.ErrorIs(t, err, session.ErrSessionClosed)
}
