package socket_test

import (
	"net"
	"os"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTempSocketDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := socket.SocketDir
	socket.SocketDir = dir
	t.Cleanup(func() { socket.SocketDir = old })
	return dir
}

func createRealSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	require.NoError(t, err, "create socket %s", path)
	t.Cleanup(func() {
		ln.Close()
		os.Remove(path)
	})
}

func TestDiscoverSocket_Sorted(t *testing.T) {
	dir := useTempSocketDir(t)

	path1 := dir + "/a2s-001.sock"
	path2 := dir + "/a2s-002.sock"

	createRealSocket(t, path2)
	createRealSocket(t, path1)

	paths, err := socket.DiscoverSocket()
	require.NoError(t, err)
	require.Len(t, paths, 2)
	assert.Equal(t, path1, paths[0])
	assert.Equal(t, path2, paths[1])
}

func TestDiscoverSocket_EmptyIsNotError(t *testing.T) {
	useTempSocketDir(t)

	paths, err := socket.DiscoverSocket()
	require.NoError(t, err)
	assert.Empty(t, paths)
}

func TestDiscoverSocket_NilErrorOnEmpty(t *testing.T) {
	useTempSocketDir(t)

	_, err := socket.DiscoverSocket()
	assert.NoError(t, err)
}

func TestNextSocketPath_LowestAvailable(t *testing.T) {
	dir := useTempSocketDir(t)

	createRealSocket(t, dir+"/a2s-1.sock")
	createRealSocket(t, dir+"/a2s-2.sock")

	got, err := socket.NextSocketPath()
	require.NoError(t, err)
	assert.Equal(t, dir+"/a2s-3.sock", got)
}

func TestNextSocketPath_StartsAtOne(t *testing.T) {
	dir := useTempSocketDir(t)

	got, err := socket.NextSocketPath()
	require.NoError(t, err)
	assert.Equal(t, dir+"/a2s-1.sock", got)
}
