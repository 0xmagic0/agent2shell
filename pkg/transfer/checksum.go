package transfer

import (
	"context"
	"fmt"
)

// Checksummer describes a checksum tool available on the target.
type Checksummer struct {
	Name           string
	Command        string
	VerifyTemplate string // fmt template with %s for shellQuoted remote path
	HashAlgo       string // "md5" or "sha256"
}

type checksumCandidate struct {
	name           string
	command        string
	probe          string
	verifyTemplate string
	hashAlgo       string
}

var checksumCandidates = []checksumCandidate{
	{
		name:           "md5sum",
		command:        "md5sum",
		probe:          "echo -n 'test' | md5sum 2>/dev/null",
		verifyTemplate: "md5sum %s | awk '{print $1}'",
		hashAlgo:       "md5",
	},
	{
		name:           "md5",
		command:        "md5 -q",
		probe:          "echo -n 'test' | md5 2>/dev/null",
		verifyTemplate: "md5 -q %s",
		hashAlgo:       "md5",
	},
	{
		name:           "sha256sum",
		command:        "sha256sum",
		probe:          "echo -n 'test' | sha256sum 2>/dev/null",
		verifyTemplate: "sha256sum %s | awk '{print $1}'",
		hashAlgo:       "sha256",
	},
	{
		name:           "sha256",
		command:        "sha256 -q",
		probe:          "echo -n 'test' | sha256 2>/dev/null",
		verifyTemplate: "sha256 -q %s",
		hashAlgo:       "sha256",
	},
	{
		name:           "python3-md5",
		command:        `python3 -c "import hashlib,sys;print(hashlib.md5(sys.stdin.buffer.read()).hexdigest())"`,
		probe:          `echo -n 'test' | python3 -c "import hashlib,sys;print(hashlib.md5(sys.stdin.buffer.read()).hexdigest())" 2>/dev/null`,
		verifyTemplate: `python3 -c "import hashlib,sys;print(hashlib.md5(open(sys.argv[1],'rb').read()).hexdigest())" %s`,
		hashAlgo:       "md5",
	},
	{
		name:           "python3-sha256",
		command:        `python3 -c "import hashlib,sys;print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())"`,
		probe:          `echo -n 'test' | python3 -c "import hashlib,sys;print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())" 2>/dev/null`,
		verifyTemplate: `python3 -c "import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest())" %s`,
		hashAlgo:       "sha256",
	},
}

// DetectChecksum probes the target for a working checksum tool and returns
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
			continue // transport error = probe failure, try next
		}
		if resp.ExitCode == 0 {
			return &Checksummer{
				Name:           c.name,
				Command:        c.command,
				VerifyTemplate: c.verifyTemplate,
				HashAlgo:       c.hashAlgo,
			}, nil
		}
	}
	return nil, nil // best-effort: no error when no checksummer found
}
