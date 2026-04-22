// Package listener manages the TCP accept loop and Unix socket server for a
// single reverse-shell session. It binds a TCP port, accepts exactly one
// connection, runs environment detection, then exposes the session via a Unix
// domain socket using the socket.Server dispatch mechanism.
package listener

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/0xmagic0/agent2shell/pkg/socket"
)

// Config holds listener construction parameters.
type Config struct {
	// Host is the TCP address to bind. Defaults to "0.0.0.0" when empty.
	Host string

	// Port is the TCP port to bind. Defaults to 4444 when zero.
	// Must be in the range [1, 65535].
	Port int

	// SocketPath is the Unix domain socket path to expose the session on.
	// If empty, socket.NextSocketPath() is used to auto-assign a path.
	SocketPath string

	// DefaultTimeout is the per-Exec timeout forwarded to the underlying
	// session. Zero means "use session default" (30 s).
	DefaultTimeout time.Duration

	// Tag is an optional user-supplied label forwarded to the session.
	Tag string

	// OnOutput is called for each line arriving outside an active Exec.
	OnOutput session.OutputCallback

	// OnStatus is called for lifecycle events (connection, session ready).
	// Optional; nil means no status output.
	OnStatus func(msg string)

	// OnSessionReady is called in a new goroutine once the session is fully
	// established and the Unix socket is ready. The context is cancelled when
	// Listen shuts down. Optional; nil means no callback.
	OnSessionReady func(ctx context.Context, sess *session.Session, socketPath string)
}

// Listener binds a TCP port and manages the lifecycle of a single reverse-shell
// session, exposing it via a Unix domain socket.
type Listener struct {
	cfg     Config
	session atomic.Pointer[session.Session]
	server  *socket.Server
}

// New creates a Listener from cfg, applying defaults and validating the port.
// Returns an error if the port is out of [1, 65535] (after defaulting).
func New(cfg Config) (*Listener, error) {
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == 0 {
		cfg.Port = 4444
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("listener: port %d out of range [1, 65535]", cfg.Port)
	}

	return &Listener{cfg: cfg}, nil
}

// Session returns the active session, or nil if Listen has not been called yet.
func (l *Listener) Session() *session.Session {
	return l.session.Load()
}

// notify calls OnStatus if configured.
func (l *Listener) notify(format string, args ...any) {
	if l.cfg.OnStatus != nil {
		l.cfg.OnStatus(fmt.Sprintf(format, args...))
	}
}

// Listen binds the TCP port, accepts exactly one connection, creates a session,
// runs environment detection, starts the Unix domain socket server, then blocks
// until ctx is cancelled or the session closes.
//
// Returns nil on clean shutdown. Returns a wrapped error if TCP bind or accept
// fails (outside of context cancellation).
func (l *Listener) Listen(ctx context.Context) error {
	// Resolve socket path.
	socketPath := l.cfg.SocketPath
	if socketPath == "" {
		sp, err := socket.NextSocketPath()
		if err != nil {
			return fmt.Errorf("listener: resolve socket path: %w", err)
		}
		socketPath = sp
		l.cfg.SocketPath = socketPath
	}

	// Bind TCP.
	addr := fmt.Sprintf("%s:%d", l.cfg.Host, l.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listener: bind %s: %w", addr, err)
	}

	// Close the TCP listener when ctx is cancelled (unblocks Accept).
	context.AfterFunc(ctx, func() {
		// best-effort: triggering shutdown, error non-recoverable
		_ = ln.Close()
	})

	// Accept exactly one connection.
	conn, err := ln.Accept()
	if err != nil {
		if ctx.Err() != nil {
			return nil // clean shutdown before any connection
		}
		return fmt.Errorf("listener: accept: %w", err)
	}

	// R9.9: close the TCP listener — no second connection accepted.
	// best-effort: already accepted one conn; error here is non-fatal
	_ = ln.Close()

	l.notify("Connection from %s", conn.RemoteAddr().String())

	sess, err := session.New(session.Config{
		Conn:           conn,
		RemoteAddr:     conn.RemoteAddr().String(),
		DefaultTimeout: l.cfg.DefaultTimeout,
		OnOutput:       l.cfg.OnOutput,
		Tag:            l.cfg.Tag,
	})
	if err != nil {
		// best-effort: close conn if session creation fails
		_ = conn.Close()
		return fmt.Errorf("listener: create session: %w", err)
	}
	l.session.Store(sess)

	// Run detect — best-effort: partial metadata is acceptable.
	// best-effort: detect errors (timeouts, partial) are non-fatal
	_ = sess.Detect(ctx)

	// Use a derived context for the socket server so we can cancel it
	// independently when the session closes (e.g. via a KillRequest).
	srvCtx, srvCancel := context.WithCancel(ctx)

	// Build and start the Unix domain socket server.
	handler := l.buildHandler(socketPath)
	srv := socket.NewServer(socketPath, handler)
	l.server = srv

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		// best-effort: serve errors during shutdown are non-fatal
		_ = srv.Serve(srvCtx)
	}()

	l.notify("Session ready: %s", socketPath)

	if l.cfg.OnSessionReady != nil {
		go l.cfg.OnSessionReady(ctx, sess, socketPath)
	}

	// Block until the parent ctx is cancelled or the session closes.
	select {
	case <-ctx.Done():
	case <-sess.Done():
	}

	// Cancel the socket server and clean up.
	srvCancel()

	// best-effort: session close error is non-recoverable here
	_ = sess.Close()
	<-serverDone

	return nil
}
