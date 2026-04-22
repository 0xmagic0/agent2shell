package main

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCatchTestCmd returns a fresh Cobra command with the same flags as
// catchCmd. Used in tests so we can set flag values without mutating the
// package-level command registered with rootCmd.
func newCatchTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catch"}
	cmd.Flags().IntP("port", "p", 4444, "TCP port to listen on")
	cmd.Flags().StringP("host", "H", "0.0.0.0", "TCP address to bind")
	cmd.Flags().DurationP("timeout", "t", 30*time.Second, "per-command execution timeout")
	cmd.Flags().String("tag", "", "optional session label")
	return cmd
}

func TestBuildCatchConfig_Defaults(t *testing.T) {
	cmd := newCatchTestCmd()

	cfg, err := buildCatchConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, 4444, cfg.Port)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 30*time.Second, cfg.DefaultTimeout)
	assert.Equal(t, "", cfg.Tag)
}

func TestBuildCatchConfig_CustomFlags(t *testing.T) {
	cmd := newCatchTestCmd()
	require.NoError(t, cmd.Flags().Set("port", "9001"))
	require.NoError(t, cmd.Flags().Set("host", "127.0.0.1"))
	require.NoError(t, cmd.Flags().Set("timeout", "1m"))
	require.NoError(t, cmd.Flags().Set("tag", "my-session"))

	cfg, err := buildCatchConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, 9001, cfg.Port)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, time.Minute, cfg.DefaultTimeout)
	assert.Equal(t, "my-session", cfg.Tag)
}

func TestBuildCatchConfig_OnStatus(t *testing.T) {
	cmd := newCatchTestCmd()

	cfg, err := buildCatchConfig(cmd)
	require.NoError(t, err)

	assert.NotNil(t, cfg.OnStatus, "OnStatus callback must be set")
	assert.Nil(t, cfg.OnOutput, "OnOutput must NOT be set by buildCatchConfig (set in runCatch instead)")
}

// ─── Task 5.3: handleInterrupt ────────────────────────────────────────────────

// newTestSession creates a real Session backed by net.Pipe for handleInterrupt tests.
// Returns (sess, shellConn, cleanup).
func newTestSession(t *testing.T) (*session.Session, net.Conn) {
	t.Helper()
	shell, app := net.Pipe()
	sess, err := session.New(session.Config{Conn: app})
	require.NoError(t, err)
	t.Cleanup(func() {
		sess.Close()
		shell.Close()
	})
	return sess, shell
}

func TestHandleInterrupt_NoSession(t *testing.T) {
	var sessRef atomic.Pointer[session.Session]
	var lastInterrupt time.Time

	shutdown := handleInterrupt(&sessRef, &lastInterrupt)
	assert.True(t, shutdown, "no session → must signal shutdown")
}

func TestHandleInterrupt_FirstTap(t *testing.T) {
	var sessRef atomic.Pointer[session.Session]
	var lastInterrupt time.Time

	sess, shell := newTestSession(t)
	sessRef.Store(sess)

	// Read 0x03 from shell end concurrently (net.Pipe is synchronous).
	readCh := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		if _, err := shell.Read(buf); err == nil {
			readCh <- buf[0]
		}
	}()

	shutdown := handleInterrupt(&sessRef, &lastInterrupt)
	assert.False(t, shutdown, "first tap must NOT signal shutdown")

	select {
	case b := <-readCh:
		assert.Equal(t, byte(0x03), b, "0x03 must be sent to shell on first tap")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for 0x03 on wire")
	}
}

func TestHandleInterrupt_DoubleTap(t *testing.T) {
	var sessRef atomic.Pointer[session.Session]
	var lastInterrupt time.Time

	sess, shell := newTestSession(t)
	sessRef.Store(sess)

	// Drain shell reads so WriteRaw doesn't block.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := shell.Read(buf); err != nil {
				return
			}
		}
	}()

	// First tap.
	shutdown1 := handleInterrupt(&sessRef, &lastInterrupt)
	assert.False(t, shutdown1, "first tap must not shutdown")

	// Second tap immediately (<2s).
	shutdown2 := handleInterrupt(&sessRef, &lastInterrupt)
	assert.True(t, shutdown2, "second tap within 2s must signal shutdown")
}

func TestHandleInterrupt_SlowDoubleTap(t *testing.T) {
	var sessRef atomic.Pointer[session.Session]
	// Set lastInterrupt far enough in the past to exceed the 2s window.
	lastInterrupt := time.Now().Add(-3 * time.Second)

	sess, shell := newTestSession(t)
	sessRef.Store(sess)

	// Drain shell reads so WriteRaw doesn't block.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := shell.Read(buf); err != nil {
				return
			}
		}
	}()

	// Both calls are "first taps" relative to the 2s window.
	shutdown1 := handleInterrupt(&sessRef, &lastInterrupt)
	assert.False(t, shutdown1, "first slow tap must not shutdown")

	// Advance past the 2s window artificially.
	lastInterrupt = lastInterrupt.Add(-3 * time.Second)

	shutdown2 := handleInterrupt(&sessRef, &lastInterrupt)
	assert.False(t, shutdown2, "second slow tap (>2s apart) must not shutdown")
}
