package transfer

import (
	"context"
	"fmt"
	"strings"
)

// Decoder describes a base64 decoding tool available on the target.
type Decoder struct {
	Name    string
	Command string
}

type decoderCandidate struct {
	name    string
	command string
	probe   string
}

var decoderCandidates = []decoderCandidate{
	{
		"base64",
		"base64 --decode",
		"echo 'dGVzdA==' | base64 --decode 2>/dev/null",
	},
	{
		"openssl",
		"openssl enc -base64 -d",
		"echo 'dGVzdA==' | openssl enc -base64 -d 2>/dev/null",
	},
	{
		"python3",
		"python3 -c 'import base64,sys;sys.stdout.buffer.write(base64.b64decode(sys.stdin.read()))'",
		"echo 'dGVzdA==' | python3 -c 'import base64,sys;sys.stdout.buffer.write(base64.b64decode(sys.stdin.read()))' 2>/dev/null",
	},
	{
		"perl",
		`perl -MMIME::Base64 -e 'print decode_base64(join("",<STDIN>))'`,
		`echo 'dGVzdA==' | perl -MMIME::Base64 -e 'print decode_base64(join("",<STDIN>))' 2>/dev/null`,
	},
}

// DetectDecoder probes the target for a working base64 decoder and returns
// the first one that successfully decodes the string "test".
// probeTimeout is the per-probe timeout in seconds.
// Returns ErrNoDecoder if none are available.
func DetectDecoder(ctx context.Context, exec ExecFunc, probeTimeout int) (*Decoder, error) {
	if probeTimeout == 0 {
		probeTimeout = 5
	}
	for _, c := range decoderCandidates {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("transfer: detect decoder: %w", ctx.Err())
		}
		resp, err := exec(ctx, c.probe, probeTimeout)
		if err != nil {
			continue // transport error = probe failure, try next
		}
		if resp.ExitCode == 0 && strings.TrimSpace(resp.Output) == "test" {
			return &Decoder{Name: c.name, Command: c.command}, nil
		}
	}
	return nil, ErrNoDecoder
}
