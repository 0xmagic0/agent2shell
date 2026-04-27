package transfer

import (
	"context"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectChecksum(t *testing.T) {
	t.Parallel()

	md5sumProbe := checksumCandidates[0].probe
	md5Probe := checksumCandidates[1].probe

	tests := []struct {
		name      string
		responses map[string]*types.ExecResponse
		wantName  string
		wantNil   bool
	}{
		{
			name: "md5sum available",
			responses: map[string]*types.ExecResponse{
				md5sumProbe: {ExitCode: 0, Output: "098f6bcd4621d373cade4e832627b4f6  -"},
			},
			wantName: "md5sum",
		},
		{
			name: "only md5 available",
			responses: map[string]*types.ExecResponse{
				md5sumProbe: {ExitCode: 127, Output: ""},
				md5Probe:    {ExitCode: 0, Output: "098f6bcd4621d373cade4e832627b4f6"},
			},
			wantName: "md5",
		},
		{
			name:    "neither available",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := mockExec(tt.responses, nil)
			got, err := DetectChecksum(context.Background(), exec, 5)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

func TestDetectChecksum_HashAlgoAndVerifyTemplate(t *testing.T) {
	t.Parallel()

	// Find probes by name from candidates slice (order-independent lookup).
	probeFor := func(name string) string {
		for _, c := range checksumCandidates {
			if c.name == name {
				return c.probe
			}
		}
		t.Fatalf("candidate %q not found in checksumCandidates", name)
		return ""
	}

	md5sumProbe := probeFor("md5sum")
	sha256sumProbe := probeFor("sha256sum")
	sha256bsdProbe := probeFor("sha256")

	tests := []struct {
		name               string
		responses          map[string]*types.ExecResponse
		wantName           string
		wantHashAlgo       string
		wantVerifyContains string // substring the VerifyTemplate should contain
	}{
		{
			name: "md5sum sets HashAlgo=md5 and VerifyTemplate",
			responses: map[string]*types.ExecResponse{
				md5sumProbe: {ExitCode: 0, Output: "098f6bcd4621d373cade4e832627b4f6  -"},
			},
			wantName:           "md5sum",
			wantHashAlgo:       "md5",
			wantVerifyContains: "md5sum",
		},
		{
			name: "sha256sum sets HashAlgo=sha256 and VerifyTemplate",
			responses: map[string]*types.ExecResponse{
				md5sumProbe:    {ExitCode: 127, Output: ""},
				sha256sumProbe: {ExitCode: 0, Output: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08  -"},
			},
			wantName:           "sha256sum",
			wantHashAlgo:       "sha256",
			wantVerifyContains: "sha256sum",
		},
		{
			name: "sha256 BSD sets HashAlgo=sha256 and VerifyTemplate",
			responses: map[string]*types.ExecResponse{
				md5sumProbe:    {ExitCode: 127, Output: ""},
				sha256sumProbe: {ExitCode: 127, Output: ""},
				sha256bsdProbe: {ExitCode: 0, Output: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
			},
			wantName:           "sha256",
			wantHashAlgo:       "sha256",
			wantVerifyContains: "sha256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := mockExec(tt.responses, nil)
			got, err := DetectChecksum(context.Background(), exec, 5)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantHashAlgo, got.HashAlgo)
			assert.NotEmpty(t, got.VerifyTemplate, "VerifyTemplate must be populated")
			assert.Contains(t, got.VerifyTemplate, tt.wantVerifyContains)
		})
	}
}

func TestDetectChecksum_CandidateOrder(t *testing.T) {
	t.Parallel()

	// Verify md5-family appears before sha256-family in candidates.
	// The first md5 candidate must appear at a lower index than the first sha256 candidate.
	firstMD5Idx := -1
	firstSHA256Idx := -1
	for i, c := range checksumCandidates {
		if firstMD5Idx == -1 && c.hashAlgo == "md5" {
			firstMD5Idx = i
		}
		if firstSHA256Idx == -1 && c.hashAlgo == "sha256" {
			firstSHA256Idx = i
		}
	}
	require.NotEqual(t, -1, firstMD5Idx, "at least one md5 candidate required")
	require.NotEqual(t, -1, firstSHA256Idx, "at least one sha256 candidate required")
	assert.Less(t, firstMD5Idx, firstSHA256Idx, "md5 candidates must appear before sha256 candidates")
}

func TestDetectChecksum_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exec := mockExec(nil, nil)
	got, err := DetectChecksum(ctx, exec, 5)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}
