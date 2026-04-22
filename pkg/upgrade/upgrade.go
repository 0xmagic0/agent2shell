// Package upgrade attempts to promote a low-capability shell (e.g. sh) to a
// richer one (e.g. bash) within an already-established TCP session.
//
// The upgrade is performed by writing the target shell command directly to
// the TCP stream via WriteRaw, waiting briefly for the new shell to start,
// then verifying with an Exec("echo $0") probe.
package upgrade

import (
	"context"
	"strings"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

// WriteRawFunc sends raw bytes to the underlying TCP stream without framing.
// It maps directly to session.Session.WriteRaw.
type WriteRawFunc func(ctx context.Context, data []byte) error

// ExecFunc executes a command on the remote shell and returns the response.
// It maps directly to session.Session.Exec.
type ExecFunc func(ctx context.Context, command string, timeout time.Duration) (*types.ExecResponse, error)

// Result describes the outcome of an upgrade attempt.
type Result struct {
	// Upgraded is true when the shell was successfully promoted.
	Upgraded bool

	// FromShell is the shell name that was active before the attempt.
	FromShell string

	// ToShell is the shell name after a successful upgrade; empty when Upgraded is false.
	ToShell string
}

// ProbeTimeout is the per-verification Exec timeout. Short enough to fail fast.
// Exported so tests or callers can override it.
var ProbeTimeout = 5 * time.Second

// UpgradeDelay is how long we wait after sending the shell command before
// verifying — gives the new process time to start and print its prompt.
// Exported so tests can set it to zero for fast execution.
var UpgradeDelay = time.Second

// candidates is the ordered list of upgrade commands to try.
// bash --norc --noprofile is tried first to avoid slow startup scripts.
var candidates = []string{
	"bash --norc --noprofile",
	"bash",
}

// alreadyCapable reports whether shell is already a capable shell that needs
// no upgrade.
func alreadyCapable(shell string) bool {
	switch shell {
	case "bash", "zsh":
		return true
	}
	return false
}

// Attempt tries to upgrade currentShell to bash within the established session.
//
// It returns immediately (Upgraded=false) when:
//   - ctx is already cancelled
//   - currentShell is already bash or zsh
//
// Otherwise it iterates through upgrade candidates:
//  1. WriteRaw sends the candidate command to spawn the new shell.
//  2. After upgradeDelay, Exec("echo $0") verifies the active shell.
//  3. If the output contains "bash" the upgrade succeeded.
//
// On WriteRaw failure for a candidate, that candidate is skipped.
// On Exec failure for a candidate, that candidate is treated as a miss and
// the next candidate is tried.
func Attempt(ctx context.Context, writeRaw WriteRawFunc, exec ExecFunc, currentShell string) Result {
	result := Result{FromShell: currentShell}

	if alreadyCapable(currentShell) {
		return result
	}

	// Bail immediately if the context is already done.
	select {
	case <-ctx.Done():
		return result
	default:
	}

	for _, candidate := range candidates {
		// Check context before each attempt.
		select {
		case <-ctx.Done():
			return result
		default:
		}

		// Send the upgrade command to the remote shell.
		cmd := candidate + "\n"
		if err := writeRaw(ctx, []byte(cmd)); err != nil {
			// WriteRaw failure — skip this candidate.
			continue
		}

		// Wait for the new shell to start.
		select {
		case <-ctx.Done():
			return result
		case <-time.After(UpgradeDelay):
		}

		// Verify the active shell.
		resp, err := exec(ctx, "echo $0", ProbeTimeout)
		if err != nil {
			// Exec failure (timeout, session closed) — try next candidate.
			continue
		}

		if strings.Contains(resp.Output, "bash") {
			result.Upgraded = true
			result.ToShell = "bash"
			return result
		}
	}

	return result
}
