// Package testutil provides shared test helpers for the agent2shell test suite.
// It MUST NOT be imported by any pkg/ or cmd/ package — test use only.
package testutil

import (
	"fmt"
	"net"
	"os"
	"testing"
)

// FreePort finds an available TCP port on the loopback interface.
// It works by opening a listener on ":0" (OS-assigned port), recording the
// port number, and immediately closing the listener.
//
// Note: there is an inherent TOCTOU race between closing the listener and the
// caller binding the port. This is acceptable for test helpers.
func FreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, fmt.Errorf("FreePort: listen on :0: %w", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close() // best-effort: already returning a different error
		return 0, fmt.Errorf("FreePort: unexpected address type %T", ln.Addr())
	}
	port := addr.Port
	if err := ln.Close(); err != nil {
		return 0, fmt.Errorf("FreePort: close listener: %w", err)
	}
	return port, nil
}

// TempSocket returns a path inside a temporary directory suitable for a Unix
// domain socket. The path does not exist at return time. A Cleanup hook is
// registered on t to remove the path (and any socket file at that path) when
// the test finishes.
//
// Paths are kept short to stay well within the 108-character Unix socket path
// limit imposed by most kernels.
func TempSocket(t *testing.T) string {
	t.Helper()
	dir := t.TempDir() // t.TempDir already registers its own Cleanup
	path := dir + "/s.sock"

	t.Cleanup(func() {
		os.Remove(path) //nolint:errcheck // best-effort removal in test cleanup
	})

	return path
}
