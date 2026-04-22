// Package session manages the lifecycle of a single reverse-shell TCP
// connection. It provides serialized command execution via Exec, automatic
// environment detection via Detect, and graceful shutdown via Close.
//
// All exported methods are safe for concurrent use.
package session

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

// ErrSessionClosed is returned when an operation is attempted on a closed
// session or when the session closes while an operation is in progress.
var ErrSessionClosed = errors.New("session closed")

// ErrExecTimeout is returned when an Exec call does not receive the end
// marker within the configured timeout duration.
var ErrExecTimeout = errors.New("exec timeout")

// OutputCallback is invoked for every line read from the TCP connection that
// is not consumed by an active Exec call. It MUST NOT block.
type OutputCallback func(line string)

// Config holds session construction parameters.
type Config struct {
	// Conn is the TCP connection to the target shell. Required.
	Conn net.Conn

	// RemoteAddr is stored in SessionInfo.RemoteAddr. Optional.
	RemoteAddr string

	// DefaultTimeout is the per-Exec timeout when none is specified.
	// If zero, 30 seconds is used.
	DefaultTimeout time.Duration

	// OnOutput is called for each line that arrives outside an active Exec.
	// Optional; nil means discard.
	OnOutput OutputCallback

	// Tag is an optional user-supplied label stored in SessionInfo.Tag.
	Tag string

	// Recording indicates whether session I/O is being recorded to disk.
	// Stored in SessionInfo.Recording so status queries reflect it.
	Recording bool
}

// Session owns a TCP connection to a single reverse shell. Commands are
// serialized — only one Exec runs at a time. All exported methods are safe
// for concurrent use.
type Session struct {
	mu             sync.Mutex   // serializes Exec; guards info writes
	chMu           sync.RWMutex // guards execCh pointer only
	conn           net.Conn
	info           types.SessionInfo
	execCh         chan string // nil when no exec active; buffer 64 when active
	doneCh         chan struct{}
	doneOnce       sync.Once
	onOutput       OutputCallback
	closed         atomic.Bool // closed atomically — does not require mu
	defaultTimeout time.Duration
}

// New creates a Session from cfg, initializes session metadata, and starts
// the internal readLoop goroutine. Returns an error if cfg.Conn is nil.
func New(cfg Config) (*Session, error) {
	if cfg.Conn == nil {
		return nil, fmt.Errorf("session: conn is required")
	}

	timeout := cfg.DefaultTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	s := &Session{
		conn:           cfg.Conn,
		doneCh:         make(chan struct{}),
		onOutput:       cfg.OnOutput,
		defaultTimeout: timeout,
		info: types.SessionInfo{
			RemoteAddr:  cfg.RemoteAddr,
			Tag:         cfg.Tag,
			ConnectedAt: time.Now().UTC(),
			Recording:   cfg.Recording,
		},
	}

	go s.readLoop()
	return s, nil
}

// Info returns a copy of the current session metadata. Safe for concurrent use.
func (s *Session) Info() types.SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// Done returns a channel that is closed when the session shuts down.
// The channel is closed either by Close or when the TCP connection reaches EOF.
// Safe for concurrent use; does not require mu.
func (s *Session) Done() <-chan struct{} {
	return s.doneCh
}

// WriteRaw writes data directly to the underlying TCP connection without
// framing or mutex. conn.Write is goroutine-safe; a mutex would deadlock
// if called during an in-progress Exec (which holds mu).
//
// Returns ErrSessionClosed if the session has been closed.
func (s *Session) WriteRaw(data []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if _, err := s.conn.Write(data); err != nil {
		return fmt.Errorf("session: write raw: %w", err)
	}
	return nil
}

// Close shuts down the session. It is idempotent — calling it multiple times
// is safe and always returns nil. Close causes any in-progress Exec to return
// ErrSessionClosed.
//
// Close uses an atomic flag so it can interrupt an in-progress Exec (which
// holds mu) by closing the conn — causing readLoop to detect EOF and signal
// doneCh — without blocking until Exec finishes.
func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		// Already closed — idempotent.
		return nil
	}

	// Closing conn causes readLoop's scanner to return EOF.
	// best-effort: conn close errors are non-recoverable here
	_ = s.conn.Close()

	// Signal doneCh — wakes any Exec blocked in its select.
	s.doneOnce.Do(func() { close(s.doneCh) })

	return nil
}
