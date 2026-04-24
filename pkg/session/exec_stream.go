package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/marker"
)

// ExecStream serializes command execution on the session, delivering output
// one line at a time via onLine as lines arrive. It mirrors the marker-
// wrapping, drain, and timer-reset logic of Exec but calls onLine for each
// output line instead of accumulating into a buffer.
//
// timeout == 0 uses the session's DefaultTimeout.
//
// On success it returns the remote exit code and wall-clock duration in
// milliseconds. On failure it returns one of: ErrExecTimeout,
// ErrSessionClosed, context.Canceled, or a wrapped write error.
// Any onLine calls already dispatched before the error are NOT rolled back.
func (s *Session) ExecStream(ctx context.Context, cmd string, timeout time.Duration, onLine func(string)) (exitCode int, durationMS int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return 0, 0, ErrSessionClosed
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

	// Drain stale lines that arrived before this ExecStream.
	s.drain(ch)

	// Generate unique ID and wrap command with markers.
	id := marker.GenerateID()
	wrapped := marker.WrapCommand(id, cmd)

	// Write wrapped command to TCP conn.
	if _, err := fmt.Fprintf(s.conn, "%s\n", wrapped); err != nil {
		return 0, 0, fmt.Errorf("exec stream write to conn: %w", err)
	}

	// Apply timeout.
	if timeout == 0 {
		timeout = s.defaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Record wall-clock start time.
	startedAt := time.Now()

	var started bool

	for {
		select {
		case line := <-ch:
			// Skip the echo of the wrapped command itself (bash -i echoes input).
			if strings.Contains(line, "echo") && strings.Contains(line, marker.MarkerPrefix) {
				continue
			}
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
					return code, time.Since(startedAt).Milliseconds(), nil
				}
				continue
			}
			// Handle markers appearing mid-line (command output without trailing
			// newline merges with the marker).
			if idx := strings.Index(line, marker.MarkerPrefix); idx > 0 {
				prefix := line[:idx]
				suffix := line[idx:]
				if started && prefix != "" {
					onLine(prefix)
				}
				if mid, code, ok := marker.ParseEndMarker(suffix); ok && mid == id {
					s.info.CommandsExecuted++
					return code, time.Since(startedAt).Milliseconds(), nil
				}
				if mid, ok := marker.ParseStartMarker(suffix); ok && mid == id {
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
			if started {
				onLine(line)
			}

		case <-timer.C:
			return 0, 0, ErrExecTimeout

		case <-ctx.Done():
			return 0, 0, ctx.Err()

		case <-s.doneCh:
			return 0, 0, ErrSessionClosed
		}
	}
}
