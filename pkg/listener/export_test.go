package listener

import (
	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/0xmagic0/agent2shell/pkg/socket"
)

// NewWithSession creates a Listener with an already-established session and a
// known socket path. Exported only for tests in the listener_test package.
func NewWithSession(sess *session.Session, socketPath string) *Listener {
	return &Listener{
		cfg:     Config{SocketPath: socketPath},
		session: sess,
	}
}

// BuildHandler exposes buildHandler for tests in the listener_test package.
func (l *Listener) BuildHandler() socket.Handler {
	return l.buildHandler(l.cfg.SocketPath)
}

// Cfg returns a copy of the resolved configuration. Exported only for tests.
func (l *Listener) Cfg() Config {
	return l.cfg
}
