package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/marker"
	"github.com/0xmagic0/agent2shell/pkg/types"
)

// DrainTimeout is the per-iteration timeout used by drain to detect silence.
// Exported as a var so callers or tests can adjust it.
var DrainTimeout = 100 * time.Millisecond

// Exec serializes command execution on the session. It wraps cmd with start
// and end markers, writes it to the TCP connection, and collects output until
// the end marker arrives, the timeout fires, ctx is cancelled, or the session
// closes.
//
// timeout == 0 uses the session's DefaultTimeout.
//
// On success it returns a non-nil *types.ExecResponse. On failure it returns
// nil and one of: ErrExecTimeout, ErrSessionClosed, context.Canceled, or a
// wrapped write error.
func (s *Session) Exec(ctx context.Context, cmd string, timeout time.Duration) (*types.ExecResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return nil, ErrSessionClosed
	}

	// Create and install buffered channel — readLoop routes lines here.
	ch := make(chan string, 64)
	s.chMu.Lock()
	s.execCh = ch
	s.chMu.Unlock()
	defer func() {
		s.chMu.Lock()
		s.execCh = nil
		s.chMu.Unlock()
	}()

	// Drain stale lines that arrived before this Exec.
	s.drain(ch)

	// Generate unique ID and wrap command with markers.
	id := marker.GenerateID()
	wrapped := marker.WrapCommand(id, cmd)

	// Write wrapped command to TCP conn.
	if _, err := fmt.Fprintf(s.conn, "%s\n", wrapped); err != nil {
		return nil, fmt.Errorf("exec write to conn: %w", err)
	}

	// Apply timeout.
	if timeout == 0 {
		timeout = s.defaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Record wall-clock start time.
	startedAt := time.Now()

	// Collection loop — read lines until end marker, timeout, cancel, or close.
	var (
		buf     []string
		started bool
	)

	for {
		select {
		case line := <-ch:
			if mid, ok := marker.ParseStartMarker(line); ok {
				if mid == id {
					started = true
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(timeout)
				}
				continue
			}
			if mid, code, ok := marker.ParseEndMarker(line); ok {
				if mid == id {
					s.info.CommandsExecuted++
					return &types.ExecResponse{
						Output:     strings.Join(buf, "\n"),
						ExitCode:   code,
						DurationMS: time.Since(startedAt).Milliseconds(),
					}, nil
				}
				continue
			}
			if started {
				buf = append(buf, line)
			}

		case <-timer.C:
			return nil, ErrExecTimeout

		case <-ctx.Done():
			return nil, ctx.Err()

		case <-s.doneCh:
			return nil, ErrSessionClosed
		}
	}
}

// drain reads and discards lines from ch until DrainTimeout of silence elapses.
// This removes stale output (shell banner, prompts) that accumulated before
// this Exec began.
func (s *Session) drain(ch <-chan string) {
	for {
		select {
		case <-ch:
			// discard stale line; best-effort: loop continues
		case <-time.After(DrainTimeout):
			return
		}
	}
}
