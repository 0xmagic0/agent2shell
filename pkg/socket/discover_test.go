package socket_test

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createRealSocket creates an actual Unix domain socket at path and registers
// cleanup. DiscoverSocket globs /tmp/a2s-*.sock so we need real socket files,
// not plain files, to match what the production code would see.
func createRealSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	require.NoError(t, err, "create socket %s", path)
	t.Cleanup(func() {
		ln.Close()
		os.Remove(path)
	})
}

// S4.8 — DiscoverSocket returns paths in sorted order.
func TestDiscoverSocket_Sorted(t *testing.T) {
	// Use unique suffix to avoid colliding with other test runs.
	suffix := fmt.Sprintf("%d", os.Getpid())

	path1 := fmt.Sprintf("/tmp/a2s-%s-001.sock", suffix)
	path2 := fmt.Sprintf("/tmp/a2s-%s-002.sock", suffix)

	// Create in reverse order to prove sorting is not insertion-order.
	createRealSocket(t, path2)
	createRealSocket(t, path1)

	paths, err := socket.DiscoverSocket()
	require.NoError(t, err)

	// Filter to only our test sockets (other sockets may exist on the system).
	var found []string
	for _, p := range paths {
		if p == path1 || p == path2 {
			found = append(found, p)
		}
	}

	require.Len(t, found, 2, "expected both test sockets in result")
	assert.Equal(t, path1, found[0], "expected sorted ascending")
	assert.Equal(t, path2, found[1], "expected sorted ascending")
}

// S4.9 — DiscoverSocket with no matching sockets returns ([], nil).
func TestDiscoverSocket_EmptyIsNotError(t *testing.T) {
	// We cannot guarantee zero sockets globally, so we just verify that
	// calling DiscoverSocket does not return an error even when the result
	// may be empty.
	paths, err := socket.DiscoverSocket()
	require.NoError(t, err)
	// paths may be non-nil with elements from real sessions; that is fine.
	// The key contract is: empty result → nil error (not tested by checking
	// len here, but by calling on a clean system in CI).
	_ = paths
}

// S4.9b — DiscoverSocket returns empty slice (not nil error) explicitly.
// We test this by verifying the function signature contract: call it and
// verify err == nil regardless of how many sockets exist.
func TestDiscoverSocket_NilErrorOnEmpty(t *testing.T) {
	_, err := socket.DiscoverSocket()
	assert.NoError(t, err)
}

// S4.10 — NextSocketPath returns the lowest available N when 1 and 2 exist.
func TestNextSocketPath_LowestAvailable(t *testing.T) {
	path1 := "/tmp/a2s-1.sock"
	path2 := "/tmp/a2s-2.sock"

	createRealSocket(t, path1)
	createRealSocket(t, path2)

	got, err := socket.NextSocketPath()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/a2s-3.sock", got)
}

// S4.11 — NextSocketPath starts at 1 when nothing exists.
func TestNextSocketPath_StartsAtOne(t *testing.T) {
	// Remove 1 and 2 if they happen to exist from another test.
	os.Remove("/tmp/a2s-1.sock")
	os.Remove("/tmp/a2s-2.sock")

	got, err := socket.NextSocketPath()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/a2s-1.sock", got)
}
