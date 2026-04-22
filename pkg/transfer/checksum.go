package transfer

import (
	"context"
	"fmt"
)

// Checksummer describes an MD5 checksum tool available on the target.
type Checksummer struct {
	Name    string
	Command string
}

var checksumCandidates = []struct {
	name    string
	command string
	probe   string
}{
	{"md5sum", "md5sum", "echo -n 'test' | md5sum 2>/dev/null"},
	{"md5", "md5 -q", "echo -n 'test' | md5 2>/dev/null"},
}

// DetectChecksum probes the target for a working MD5 checksummer and returns
// the first available one. probeTimeout is the per-probe timeout in seconds.
// Returns nil, nil when none are available — callers decide whether to fail
// or skip verification.
func DetectChecksum(ctx context.Context, exec ExecFunc, probeTimeout int) (*Checksummer, error) {
	if probeTimeout == 0 {
		probeTimeout = 5
	}
	for _, c := range checksumCandidates {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("transfer: detect checksum: %w", ctx.Err())
		}
		resp, err := exec(ctx, c.probe, probeTimeout)
		if err != nil {
			continue
		}
		if resp.ExitCode == 0 {
			return &Checksummer{Name: c.name, Command: c.command}, nil
		}
	}
	return nil, nil // best-effort: no error when no checksummer found
}
