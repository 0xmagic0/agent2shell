package socket

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

// DefaultReadTimeout is the read deadline applied to each connection when the
// context carries no explicit deadline.
const DefaultReadTimeout = 30 * time.Second

// Handler is the callback invoked by Server for each incoming request.
// The return value is marshalled as the JSON response frame.
// A non-nil error causes the server to write {"error": "message"} instead.
type Handler func(ctx context.Context, req *types.Request) (any, error)

// Server listens on a Unix domain socket and dispatches incoming requests to
// a Handler. Each connection is handled in its own goroutine. The server
// accepts exactly one request per connection, writes one response, then closes
// the connection.
type Server struct {
	path        string
	handler     Handler
	ReadTimeout time.Duration
}

// NewServer creates a Server that will bind to path and dispatch to handler.
// Serve must be called to start accepting connections.
func NewServer(path string, handler Handler) *Server {
	return &Server{
		path:        path,
		handler:     handler,
		ReadTimeout: DefaultReadTimeout,
	}
}

// Serve binds the Unix domain socket, sets permissions to 0600, and accepts
// connections until ctx is cancelled. It returns nil on clean shutdown and a
// wrapped error if binding fails.
//
// Any existing file at path is removed before binding so that stale sockets
// from a previous run do not prevent startup.
func (s *Server) Serve(ctx context.Context) error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("socket server: remove stale socket %s: %w", s.path, err)
	}

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("socket server: listen on %s: %w", s.path, err)
	}

	if err := os.Chmod(s.path, 0600); err != nil {
		// best-effort: shutting down immediately after this return
		_ = ln.Close()
		return fmt.Errorf("socket server: chmod %s: %w", s.path, err)
	}

	var (
		wg    sync.WaitGroup
		conns []net.Conn
		mu    sync.Mutex
	)

	context.AfterFunc(ctx, func() {
		// best-effort: triggering shutdown, errors non-recoverable
		_ = ln.Close()

		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			// best-effort: forcing connection close during shutdown
			_ = c.Close()
		}
	})

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			return fmt.Errorf("socket server: accept: %w", err)
		}

		s.trackConn(&mu, &conns, conn)

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

// trackConn appends conn to the tracked connections slice.
func (s *Server) trackConn(mu *sync.Mutex, conns *[]net.Conn, conn net.Conn) {
	mu.Lock()
	defer mu.Unlock()
	*conns = append(*conns, conn)
}

// handle reads one request frame from conn, invokes the handler, and writes
// the response frame. The connection is always closed before returning.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	timeout := s.ReadTimeout
	if timeout == 0 {
		timeout = DefaultReadTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		// best-effort: unix sockets rarely fail SetReadDeadline
		_ = conn.SetReadDeadline(deadline)
	} else {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
	}

	var req types.Request
	if err := ReadFrame(conn, &req); err != nil {
		// best-effort: conn closing anyway after deferred Close
		_ = WriteFrame(conn, map[string]string{"error": err.Error()})
		return
	}

	result, err := s.handler(ctx, &req)
	if err != nil {
		// best-effort: conn closing anyway after deferred Close
		_ = WriteFrame(conn, map[string]string{"error": err.Error()})
		return
	}

	// best-effort: conn closing anyway after deferred Close
	_ = WriteFrame(conn, result)
}
