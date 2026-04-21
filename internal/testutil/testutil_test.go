package testutil_test

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreePort covers S6.1: the returned port must be immediately usable for a TCP listen.
func TestFreePort(t *testing.T) {
	t.Run("returns usable port", func(t *testing.T) {
		port, err := testutil.FreePort()
		require.NoError(t, err, "FreePort must not return an error")
		require.Greater(t, port, 0, "port must be positive")

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		require.NoError(t, err, "port returned by FreePort must be immediately bindable")
		ln.Close()
	})
}

// TestTempSocket covers S6.2 and S6.3.
func TestTempSocket(t *testing.T) {
	t.Run("path does not exist at return time", func(t *testing.T) {
		// S6.2
		path := testutil.TempSocket(t)
		require.NotEmpty(t, path)

		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err), "TempSocket path must not exist at return time, got err: %v", err)
	})

	t.Run("cleanup removes created socket file", func(t *testing.T) {
		// S6.3: spin up an inner test to observe cleanup firing.
		var capturedPath string

		// We need a real *testing.T with cleanup support. Run a sub-test so
		// its cleanup runs before we assert — t.Run waits for the subtest to
		// finish including its Cleanup hooks.
		t.Run("inner", func(inner *testing.T) {
			capturedPath = testutil.TempSocket(inner)

			// Create the socket file so there is actually something to clean up.
			// Keep the listener open — closing it removes the socket file on Linux.
			ln, err := net.Listen("unix", capturedPath)
			require.NoError(inner, err, "must be able to bind the socket path")
			inner.Cleanup(func() { ln.Close() })

			_, err = os.Stat(capturedPath)
			require.NoError(inner, err, "socket file must exist inside the test")
		})

		// After "inner" returns all Cleanup hooks have fired.
		_, err := os.Stat(capturedPath)
		assert.True(t, os.IsNotExist(err), "socket file must be removed after test ends, got err: %v", err)
	})
}
