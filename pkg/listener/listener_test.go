package listener_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Task 3.1 — Config, New ────────────────────────────────────────────────

func TestNewDefaults(t *testing.T) {
	l, err := listener.New(listener.Config{})
	require.NoError(t, err)
	require.NotNil(t, l)

	cfg := l.Cfg()
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 4444, cfg.Port)
}

func TestNewInvalidPort(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"too high", 70000, true},
		{"negative", -1, true},
		{"max+1", 65536, true},
		{"zero applies default", 0, false},
		{"valid port", 9999, false},
		{"port 1 boundary", 1, false},
		{"port 65535 boundary", 65535, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := listener.New(listener.Config{Port: tt.port})
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, l)
			} else {
				require.NoError(t, err)
				require.NotNil(t, l)
			}
		})
	}
}

func TestNewValidPort(t *testing.T) {
	_, err := listener.New(listener.Config{Port: 9999})
	require.NoError(t, err)
}

func TestSessionNilBeforeListen(t *testing.T) {
	l, err := listener.New(listener.Config{})
	require.NoError(t, err)
	assert.Nil(t, l.Session())
}

// ─── Task 3.3 — Listen flow ────────────────────────────────────────────────

func TestListenContextCancelBeforeConnection(t *testing.T) {
	port, err := testutil.FreePort()
	require.NoError(t, err)

	sockPath := testutil.TempSocket(t)

	l, err := listener.New(listener.Config{
		Host:       "127.0.0.1",
		Port:       port,
		SocketPath: sockPath,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- l.Listen(ctx) }()

	// Give Listen time to bind and block on Accept, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not return within 2s after context cancel")
	}
}

func TestListenContextCancelAfterConnection(t *testing.T) {
	port, err := testutil.FreePort()
	require.NoError(t, err)

	sockPath := testutil.TempSocket(t)

	l, err := listener.New(listener.Config{
		Host:           "127.0.0.1",
		Port:           port,
		SocketPath:     sockPath,
		DefaultTimeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- l.Listen(ctx) }()

	// Connect a fake shell (retrying until the TCP port is open).
	var shellConn net.Conn
	require.Eventually(t, func() bool {
		c, dialErr := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if dialErr == nil {
			shellConn = c
			return true
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "could not connect fake shell")
	defer shellConn.Close()

	// The listener runs detect probes, each timing out after DefaultTimeout
	// (200 ms). With 6 probes, total detect time is at most ~1.2 s.
	// Wait for the Unix socket to appear — it signals detect is done.
	require.Eventually(t, func() bool {
		c, dialErr := net.Dial("unix", sockPath)
		if dialErr == nil {
			c.Close()
			return true
		}
		return false
	}, 4*time.Second, 20*time.Millisecond, "unix socket did not appear")

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Listen did not return within 3s after context cancel")
	}
}

// ─── Task 3.7 — OnStatus callback ─────────────────────────────────────────

func TestListenOnStatus(t *testing.T) {
	port, err := testutil.FreePort()
	require.NoError(t, err)

	sockPath := testutil.TempSocket(t)

	var msgs []string
	l, err := listener.New(listener.Config{
		Host:           "127.0.0.1",
		Port:           port,
		SocketPath:     sockPath,
		DefaultTimeout: 200 * time.Millisecond,
		OnStatus: func(msg string) {
			msgs = append(msgs, msg)
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- l.Listen(ctx) }()

	// Connect a fake shell.
	var shellConn net.Conn
	require.Eventually(t, func() bool {
		c, dialErr := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if dialErr == nil {
			shellConn = c
			return true
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "could not connect fake shell")
	defer shellConn.Close()

	// Wait for the Unix socket — signals detect is done and session is ready.
	require.Eventually(t, func() bool {
		c, dialErr := net.Dial("unix", sockPath)
		if dialErr == nil {
			c.Close()
			return true
		}
		return false
	}, 4*time.Second, 20*time.Millisecond, "unix socket did not appear")

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Listen did not return within 3s after context cancel")
	}

	// Verify status messages were collected.
	var foundConnection, foundReady bool
	for _, m := range msgs {
		if len(m) >= len("Connection from") && m[:len("Connection from")] == "Connection from" {
			foundConnection = true
		}
		if len(m) >= len("Session ready:") && m[:len("Session ready:")] == "Session ready:" {
			foundReady = true
		}
	}
	assert.True(t, foundConnection, "expected 'Connection from' message, got: %v", msgs)
	assert.True(t, foundReady, "expected 'Session ready:' message, got: %v", msgs)
}
