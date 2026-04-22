package upgrade_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/0xmagic0/agent2shell/pkg/upgrade"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	// Zero out the upgrade delay so tests don't sleep between candidates.
	upgrade.UpgradeDelay = 0
	os.Exit(m.Run())
}

// makeExec returns an ExecFunc that always returns the given shell name in Output.
func makeExec(shell string) upgrade.ExecFunc {
	return func(_ context.Context, _ string, _ time.Duration) (*types.ExecResponse, error) {
		return &types.ExecResponse{Output: shell}, nil
	}
}

// makeExecErr returns an ExecFunc that always returns an error.
func makeExecErr(err error) upgrade.ExecFunc {
	return func(_ context.Context, _ string, _ time.Duration) (*types.ExecResponse, error) {
		return nil, err
	}
}

func TestAttempt_AlreadyBash(t *testing.T) {
	writeRawCalled := false
	writeRaw := func(_ context.Context, _ []byte) error {
		writeRawCalled = true
		return nil
	}

	result := upgrade.Attempt(context.Background(), writeRaw, makeExec("bash"), "bash")

	assert.False(t, result.Upgraded)
	assert.Equal(t, "bash", result.FromShell)
	assert.Equal(t, "", result.ToShell)
	assert.False(t, writeRawCalled, "writeRaw must not be called when shell is already bash")
}

func TestAttempt_AlreadyZsh(t *testing.T) {
	writeRawCalled := false
	writeRaw := func(_ context.Context, _ []byte) error {
		writeRawCalled = true
		return nil
	}

	result := upgrade.Attempt(context.Background(), writeRaw, makeExec("zsh"), "zsh")

	assert.False(t, result.Upgraded)
	assert.Equal(t, "zsh", result.FromShell)
	assert.Equal(t, "", result.ToShell)
	assert.False(t, writeRawCalled, "writeRaw must not be called when shell is already zsh")
}

func TestAttempt_ShToBash(t *testing.T) {
	var written [][]byte
	writeRaw := func(_ context.Context, data []byte) error {
		written = append(written, data)
		return nil
	}

	result := upgrade.Attempt(context.Background(), writeRaw, makeExec("bash"), "sh")

	assert.True(t, result.Upgraded)
	assert.Equal(t, "sh", result.FromShell)
	assert.Equal(t, "bash", result.ToShell)
	assert.NotEmpty(t, written, "writeRaw must be called to send the upgrade command")
}

func TestAttempt_UpgradeFails(t *testing.T) {
	writeRaw := func(_ context.Context, _ []byte) error { return nil }

	// exec always reports "sh" — upgrade never takes effect
	result := upgrade.Attempt(context.Background(), writeRaw, makeExec("sh"), "sh")

	assert.False(t, result.Upgraded)
	assert.Equal(t, "sh", result.FromShell)
	assert.Equal(t, "", result.ToShell)
}

func TestAttempt_BashNoRcFails_BashSucceeds(t *testing.T) {
	// First call to exec returns "sh" (bash --norc failed), second returns "bash"
	callCount := 0
	exec := func(_ context.Context, _ string, _ time.Duration) (*types.ExecResponse, error) {
		callCount++
		if callCount == 1 {
			return &types.ExecResponse{Output: "sh"}, nil
		}
		return &types.ExecResponse{Output: "bash"}, nil
	}

	writeRaw := func(_ context.Context, _ []byte) error { return nil }

	result := upgrade.Attempt(context.Background(), writeRaw, exec, "sh")

	assert.True(t, result.Upgraded)
	assert.Equal(t, "sh", result.FromShell)
	assert.Equal(t, "bash", result.ToShell)
	assert.Equal(t, 2, callCount, "should probe twice: once for bash --norc, once for bash")
}

func TestAttempt_ExecError_Continues(t *testing.T) {
	// First exec errors out, second returns "bash"
	callCount := 0
	exec := func(_ context.Context, _ string, _ time.Duration) (*types.ExecResponse, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("exec timeout")
		}
		return &types.ExecResponse{Output: "bash"}, nil
	}

	writeRaw := func(_ context.Context, _ []byte) error { return nil }

	result := upgrade.Attempt(context.Background(), writeRaw, exec, "sh")

	assert.True(t, result.Upgraded)
	assert.Equal(t, "bash", result.ToShell)
}

func TestAttempt_WriteRawError_Continues(t *testing.T) {
	// writeRaw always fails; upgrade should not succeed
	writeRaw := func(_ context.Context, _ []byte) error {
		return errors.New("connection closed")
	}

	result := upgrade.Attempt(context.Background(), writeRaw, makeExec("sh"), "sh")

	assert.False(t, result.Upgraded)
}

func TestAttempt_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	writeRaw := func(_ context.Context, _ []byte) error { return nil }

	result := upgrade.Attempt(ctx, writeRaw, makeExec("bash"), "sh")

	assert.False(t, result.Upgraded)
}
