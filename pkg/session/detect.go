package session

import (
	"context"
	"errors"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/detect"
	"github.com/0xmagic0/agent2shell/pkg/types"
)

// DetectProbeTimeout is the per-probe execution timeout used by Detect.
// Exported as a var so callers or tests can adjust it.
var DetectProbeTimeout = 5 * time.Second

// probe describes a single detection command, its output parser, and the
// field setter that writes the parsed result into types.SessionInfo.
type probe struct {
	cmd   string
	parse func(string) string
	field func(*types.SessionInfo, string)
}

// probes is the ordered list of environment detection commands.
var probes = []probe{
	{
		cmd:   "echo $0",
		parse: detect.ParseShell,
		field: func(i *types.SessionInfo, v string) { i.Shell = v },
	},
	{
		cmd:   "uname -s",
		parse: detect.ParseOS,
		field: func(i *types.SessionInfo, v string) { i.OS = v },
	},
	{
		cmd:   "uname -m",
		parse: detect.ParseArch,
		field: func(i *types.SessionInfo, v string) { i.Arch = v },
	},
	{
		cmd:   "cat /etc/os-release",
		parse: detect.ParseDistro,
		field: func(i *types.SessionInfo, v string) { i.Distro = v },
	},
	{
		cmd:   "id",
		parse: detect.ParseUser,
		field: func(i *types.SessionInfo, v string) { i.User = v },
	},
	{
		cmd:   "hostname",
		parse: detect.ParseHostname,
		field: func(i *types.SessionInfo, v string) { i.Hostname = v },
	},
}

// probeResult holds a single successfully parsed probe outcome.
type probeResult struct {
	idx int
	val string
}

// Detect runs the six environment-detection probes sequentially, each with
// DetectProbeTimeout (or the session's defaultTimeout if shorter). It collects
// raw outputs, parses them, and writes the results to SessionInfo under mu.
//
// Individual probe timeouts are non-fatal — Detect continues to the next
// probe. ErrSessionClosed and ctx.Err() abort all remaining probes.
//
// Returns nil when all probes have been attempted (regardless of success),
// ErrSessionClosed if the session closes, or ctx.Err() if the context is
// cancelled.
func (s *Session) Detect(ctx context.Context) error {
	// Each probe calls Exec, which acquires mu on its own. Detect MUST NOT hold
	// mu during probes to avoid deadlock (mu is not reentrant in Go).
	results := make([]probeResult, 0, len(probes))

	for i, p := range probes {
		// Abort early if context is cancelled.
		select {
		case <-ctx.Done():
			s.applyDetectResults(results)
			return ctx.Err()
		default:
		}

		// Use the session's defaultTimeout as per-probe timeout so that tests
		// using a short defaultTimeout get fast timeouts. Production sessions use
		// 30s default which is well above DetectProbeTimeout (5s), so we take the
		// smaller of the two.
		timeout := DetectProbeTimeout
		if s.defaultTimeout < timeout {
			timeout = s.defaultTimeout
		}

		resp, err := s.Exec(ctx, p.cmd, timeout)
		if err != nil {
			if errors.Is(err, ErrSessionClosed) {
				s.applyDetectResults(results)
				return ErrSessionClosed
			}
			if ctx.Err() != nil {
				s.applyDetectResults(results)
				return ctx.Err()
			}
			// Individual probe timeout — best-effort, skip this probe.
			continue
		}

		results = append(results, probeResult{idx: i, val: resp.Output})
	}

	s.applyDetectResults(results)
	return nil
}

// applyDetectResults acquires mu briefly to parse and write probe results
// into s.info. Must be called after all Exec calls have completed.
func (s *Session) applyDetectResults(results []probeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range results {
		p := probes[r.idx]
		p.field(&s.info, p.parse(r.val))
	}
}
