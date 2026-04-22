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
