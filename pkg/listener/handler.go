package listener

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/recorder"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
)

// buildHandler returns a socket.Handler that dispatches incoming requests to
// the appropriate session method.
func (l *Listener) buildHandler(socketPath string) socket.Handler {
	return func(ctx context.Context, req *types.Request) (any, error) {
		sess := l.session.Load()
		switch req.Type {
		case types.RunRequest:
			timeout := time.Duration(req.Timeout) * time.Second
			resp, execErr := sess.Exec(ctx, req.Command, timeout)
			if execErr != nil {
				if l.cfg.Recorder != nil {
					// best-effort: recording errors must not fail the request
					_ = l.cfg.Recorder.Log(recorder.Entry{
						Timestamp: time.Now().UTC().Format(time.RFC3339),
						Command:   req.Command,
						Error:     execErr.Error(),
					})
				}
				return nil, execErr
			}
			if l.cfg.Recorder != nil {
				// best-effort: recording errors must not fail the request
				_ = l.cfg.Recorder.Log(recorder.Entry{
					Timestamp:  time.Now().UTC().Format(time.RFC3339),
					Command:    req.Command,
					Output:     resp.Output,
					ExitCode:   resp.ExitCode,
					DurationMS: resp.DurationMS,
				})
			}
			if l.cfg.OnExec != nil {
				l.cfg.OnExec(req.Command, resp)
			}
			return resp, nil

		case types.StatusRequest:
			return sess.Info(), nil

		case types.ListRequest:
			info := sess.Info()
			return types.SessionsResponse{
				Sessions: []types.SessionEntry{{
					SessionInfo: info,
					SocketPath:  socketPath,
				}},
			}, nil

		case types.KillRequest:
			// R9.6: respond synchronously with {"status":"ok"}, close async.
			go sess.Close() //nolint:errcheck // best-effort async close
			return map[string]string{"status": "ok"}, nil

		default:
			return nil, fmt.Errorf("unknown request type: %s", req.Type)
		}
	}
}

// buildStreamHandler returns a socket.StreamHandler that streams command
// output line-by-line to conn. Each output line is written as a StreamLine
// frame; a final StreamEnd frame carries exit code, duration, and any error.
//
// The recorder (if configured) receives one entry with all lines joined after
// ExecStream returns. Recorder errors are best-effort and do not fail the
// request. The OnExec hook (if configured) is called after ExecStream returns.
func (l *Listener) buildStreamHandler(socketPath string) socket.StreamHandler {
	return func(ctx context.Context, req *types.Request, conn net.Conn) {
		sess := l.session.Load()
		timeout := time.Duration(req.Timeout) * time.Second

		var lines []string

		onLine := func(line string) {
			lines = append(lines, line)
			// best-effort: write errors are non-fatal per-line; connection drop
			// will be caught by the ExecStream side or the final WriteFrame.
			_ = socket.WriteFrame(conn, types.StreamFrame{
				Type: types.StreamLine,
				Data: line,
			})
		}

		exitCode, durationMS, execErr := sess.ExecStream(ctx, req.Command, timeout, onLine)

		endFrame := types.StreamFrame{
			Type:       types.StreamEnd,
			ExitCode:   exitCode,
			DurationMS: durationMS,
		}
		if execErr != nil {
			endFrame.Error = execErr.Error()
		}

		// Write the StreamEnd frame — best-effort, conn closing anyway after return.
		_ = socket.WriteFrame(conn, endFrame)

		// Record the full output (accumulated lines joined by newline).
		output := strings.Join(lines, "\n")

		if l.cfg.Recorder != nil {
			entry := recorder.Entry{
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
				Command:    req.Command,
				Output:     output,
				ExitCode:   exitCode,
				DurationMS: durationMS,
			}
			if execErr != nil {
				entry.Error = execErr.Error()
			}
			// best-effort: recording errors must not fail the request
			_ = l.cfg.Recorder.Log(entry)
		}

		if l.cfg.OnExec != nil {
			l.cfg.OnExec(req.Command, &types.ExecResponse{
				Output:     output,
				ExitCode:   exitCode,
				DurationMS: durationMS,
			})
		}
	}
}
