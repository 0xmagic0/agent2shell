package transfer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerifyChecksum_UsesVerifyTemplate confirms that verifyChecksum builds its
// remote command from checksummer.VerifyTemplate rather than a hard-coded switch.
func TestVerifyChecksum_UsesVerifyTemplate(t *testing.T) {
	t.Parallel()

	const wantHash = "abc123"
	const remotePath = "/tmp/file.bin"

	tests := []struct {
		name    string
		tmpl    string
		wantCmd string
	}{
		{
			name:    "md5sum template",
			tmpl:    "md5sum %s | awk '{print $1}'",
			wantCmd: "md5sum '/tmp/file.bin' | awk '{print $1}'",
		},
		{
			name:    "md5 BSD template",
			tmpl:    "md5 -q %s",
			wantCmd: "md5 -q '/tmp/file.bin'",
		},
		{
			name:    "sha256sum template",
			tmpl:    "sha256sum %s | awk '{print $1}'",
			wantCmd: "sha256sum '/tmp/file.bin' | awk '{print $1}'",
		},
		{
			name:    "sha256 BSD template",
			tmpl:    "sha256 -q %s",
			wantCmd: "sha256 -q '/tmp/file.bin'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var capturedCmd string
			exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
				capturedCmd = cmd
				return &types.ExecResponse{ExitCode: 0, Output: wantHash + "\n"}, nil
			}

			cs := &Checksummer{
				Name:           tt.name,
				VerifyTemplate: tt.tmpl,
				HashAlgo:       "md5",
			}
			err := verifyChecksum(context.Background(), exec, cs, remotePath, wantHash, 5)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCmd, capturedCmd,
				"verifyChecksum must build cmd from VerifyTemplate, not a switch")
		})
	}
}

// TestVerifyChecksum_NilChecksummerSkips verifies that a nil checksummer causes
// verifyChecksum to return nil (skip, not error).
func TestVerifyChecksum_NilChecksummerSkips(t *testing.T) {
	t.Parallel()

	execCalled := false
	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		execCalled = true
		return &types.ExecResponse{ExitCode: 0, Output: "hash\n"}, nil
	}

	err := verifyChecksum(context.Background(), exec, nil, "/tmp/file", "hash", 5)
	require.NoError(t, err)
	assert.False(t, execCalled, "exec must NOT be called when checksummer is nil")
}

// TestVerifyChecksum_MismatchReturnsError verifies that a hash mismatch returns ErrChecksumMismatch.
func TestVerifyChecksum_MismatchReturnsError(t *testing.T) {
	t.Parallel()

	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		// Remote returns a different hash than local
		return &types.ExecResponse{ExitCode: 0, Output: "remote_hash\n"}, nil
	}

	cs := &Checksummer{
		Name:           "sha256sum",
		VerifyTemplate: "sha256sum %s | awk '{print $1}'",
		HashAlgo:       "sha256",
	}
	err := verifyChecksum(context.Background(), exec, cs, "/tmp/file", "local_hash", 5)
	require.ErrorIs(t, err, ErrChecksumMismatch)
}

// TestVerifyChecksum_UnknownTemplateShouldNotPanic verifies that even a custom
// template string is used as-is (no unknown-checksummer error from a switch).
func TestVerifyChecksum_CustomTemplate(t *testing.T) {
	t.Parallel()

	var capturedCmd string
	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		capturedCmd = cmd
		return &types.ExecResponse{ExitCode: 0, Output: "myhash\n"}, nil
	}

	cs := &Checksummer{
		Name:           "custom-tool",
		VerifyTemplate: "custom-checksum --file=%s",
		HashAlgo:       "md5",
	}
	err := verifyChecksum(context.Background(), exec, cs, "/tmp/f", "myhash", 5)
	require.NoError(t, err)
	assert.True(t, strings.Contains(capturedCmd, "custom-checksum"),
		"custom template must be used; got: %s", capturedCmd)
}

func TestComputeHash(t *testing.T) {
	t.Parallel()

	// Known hashes for the string "test":
	//   MD5:    098f6bcd4621d373cade4e832627b4f6
	//   SHA256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
	tests := []struct {
		name      string
		input     string
		algo      string
		wantHash  string
		wantErrIs error
	}{
		{
			name:     "md5 of test",
			input:    "test",
			algo:     "md5",
			wantHash: "098f6bcd4621d373cade4e832627b4f6",
		},
		{
			name:     "sha256 of test",
			input:    "test",
			algo:     "sha256",
			wantHash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		},
		{
			name:     "md5 of empty string",
			input:    "",
			algo:     "md5",
			wantHash: "d41d8cd98f00b204e9800998ecf8427e",
		},
		{
			name:     "sha256 of empty string",
			input:    "",
			algo:     "sha256",
			wantHash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader([]byte(tt.input))
			got, err := computeHash(r, tt.algo)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHash, got)
		})
	}
}
