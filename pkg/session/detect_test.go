package session_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── S8.9: Detect happy path ─────────────────────────────────────────────────

func TestDetectHappyPath(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	sess, err := session.New(session.Config{
		Conn:           app,
		DefaultTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer sess.Close()

	// Shell responds to 6 probes in order:
	//   echo $0        → /bin/bash
	//   uname -s       → Linux
	//   uname -m       → x86_64
	//   cat /etc/os-release → PRETTY_NAME="Ubuntu 22.04"
	//   id             → uid=33(www-data) gid=33(www-data)
	//   hostname       → target-01
	probeResponses := []string{
		"/bin/bash",
		"Linux",
		"x86_64",
		"PRETTY_NAME=\"Ubuntu 22.04\"",
		"uid=33(www-data) gid=33(www-data)",
		"target-01",
	}

	go func() {
		for _, output := range probeResponses {
			if err := sh.respondToProbe(output); err != nil {
				return
			}
		}
	}()

	err = sess.Detect(context.Background())
	require.NoError(t, err)

	info := sess.Info()
	assert.Equal(t, "bash", info.Shell)
	assert.Equal(t, "linux", info.OS)
	assert.Equal(t, "amd64", info.Arch)
	assert.Equal(t, "Ubuntu 22.04", info.Distro)
	assert.Equal(t, "www-data", info.User)
	assert.Equal(t, "target-01", info.Hostname)
}

// ─── S8.10: Detect partial failure ───────────────────────────────────────────

func TestDetectPartialFailure(t *testing.T) {
	shell, app := net.Pipe()
	defer shell.Close()

	sh := newMockShell(shell)

	// Use a short per-probe timeout to make the test fast.
	sess, err := session.New(session.Config{
		Conn:           app,
		DefaultTimeout: 150 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sess.Close()

	// Shell responds to probes 0, 2, 4 (shell, arch, user) but times out on 1, 3, 5.
	// We drive this by only responding to odd-numbered calls (0-indexed):
	//   probe 0: echo $0        → respond with /bin/bash
	//   probe 1: uname -s       → no response (timeout)
	//   probe 2: uname -m       → respond with x86_64
	//   probe 3: cat /etc/os-release → no response (timeout)
	//   probe 4: id             → respond with uid=0(root) ...
	//   probe 5: hostname       → no response (timeout)

	go func() {
		// probe 0: respond
		if err := sh.respondToProbe("/bin/bash"); err != nil {
			return
		}
		// probe 1: read command but don't respond → timeout
		if _, err := sh.readCommandID(); err != nil {
			return
		}
		// probe 2: respond
		if err := sh.respondToProbe("x86_64"); err != nil {
			return
		}
		// probe 3: read command but don't respond → timeout
		if _, err := sh.readCommandID(); err != nil {
			return
		}
		// probe 4: respond
		if err := sh.respondToProbe("uid=0(root) gid=0(root)"); err != nil {
			return
		}
		// probe 5: read command but don't respond → timeout
		_, _ = sh.readCommandID()
	}()

	err = sess.Detect(context.Background())
	// Partial failure must NOT return an error.
	require.NoError(t, err)

	info := sess.Info()
	// Successful probes populated:
	assert.Equal(t, "bash", info.Shell)
	assert.Equal(t, "amd64", info.Arch)
	assert.Equal(t, "root", info.User)
	// Failed probes remain at zero value (empty string — probes are skipped, not written):
	assert.Empty(t, info.OS)
	assert.Empty(t, info.Distro)
	assert.Empty(t, info.Hostname)
}
