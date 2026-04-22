package listener

import (
	"context"
	"fmt"
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
